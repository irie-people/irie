// Command perf is the optimisation plan's yardstick
// (docs/proposals/optimisation.md, iteration 1). It watches the Irie that
// runs for a window, a minute by default, then prints one report:
//
//   - the frames a client gets, by type and size, which fields each upsert
//     changed, and which parts of the header each header frame changed;
//   - the CPU and memory of the core, the sidebar, the placeholder, the
//     phone gateway, the tmux server, the pane hosts, the agents and the Mac
//     app, from ps at both ends;
//   - from the core's own counters (irie ctl perf), how many `irie
//     statusline`, `irie hook` and `claude agents` processes started, each
//     request's time to answer, waits on the core's lock and its ticks;
//   - after the window, how busy the Mac app's main thread is, by sample(1),
//     and its footprint(1).
//
// It changes nothing: it subscribes as the tmux sidebar does and only reads.
//
//	go run ./tools/perf                       a report of the next minute
//	go run ./tools/perf -for 30s -o a.json    also keep it as JSON
//	go run ./tools/perf -compare a.json b.json
//	                                          whether two reports agree, within
//	                                          15% (-within) on every gated figure
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/harness/claude"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/paths"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "perf:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("perf", flag.ContinueOnError)
	window := fs.Duration("for", time.Minute, "how long to watch")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	out := fs.String("o", "", "also write the report as JSON to this file")
	sampleFor := fs.Duration("sample", 5*time.Second, "how long to sample the Mac app's main thread after the window; 0 skips it")
	socket := fs.String("tmux", "", "the tmux server's socket name; tmux.socket in Irie's config by default")
	cmp := fs.Bool("compare", false, "compare the two reports named, saved with -o, and say whether they agree")
	within := fs.Float64("within", 0.15, "with -compare, how far apart two figures may be, as a share of the larger")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *cmp {
		if fs.NArg() != 2 {
			return errors.New("usage: perf -compare A.json B.json")
		}
		return comparing(fs.Arg(0), fs.Arg(1), *within)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected %q", fs.Arg(0))
	}
	p, err := paths.FromEnv()
	if err != nil {
		return err
	}
	if *socket == "" {
		cfg, err := config.Load(p.ConfigFile())
		if err != nil {
			return fmt.Errorf("config: %w", err)
		}
		*socket = cfg.Tmux.Socket
	}
	r, err := watch(p, *socket, *window, *sampleFor)
	if err != nil {
		return err
	}
	if *out != "" {
		b, _ := json.MarshalIndent(r, "", "  ")
		if err := os.WriteFile(*out, append(b, '\n'), 0o644); err != nil {
			return err
		}
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(r)
	}
	return r.print(os.Stdout)
}

// watch takes one window of the running Irie.
func watch(p paths.Paths, socket string, d, sampleFor time.Duration) (Report, error) {
	sock := p.Socket()
	var hello ipc.HelloResult
	if err := ipc.Call(sock, 5*time.Second, ipc.OpHello, ipc.HelloArgs{Client: "perf", API: ipc.API}, &hello); err != nil {
		return Report{}, fmt.Errorf("no core answers at %s: %w", sock, err)
	}
	counts := false
	for _, f := range hello.Features {
		counts = counts || f == ipc.OpPerf
	}
	readPerf := func() (ipc.PerfResult, bool) {
		var r ipc.PerfResult
		if !counts || ipc.Call(sock, 10*time.Second, ipc.OpPerf, nil, &r) != nil {
			return r, false
		}
		return r, true
	}

	start := time.Now()
	stop := make(chan struct{})
	heard := make(chan Frames, 1)
	errs := make(chan error, 1)
	go func() {
		f, err := listen(sock, stop)
		heard <- f
		errs <- err
	}()
	before, err := readTable()
	if err != nil {
		return Report{}, err
	}
	pa, okA := readPerf()
	time.Sleep(d)
	after, err := readTable()
	if err != nil {
		return Report{}, err
	}
	pb, okB := readPerf()
	close(stop)
	frames := <-heard
	if err := <-errs; err != nil {
		return Report{}, err
	}

	roles, server, err := panes(socket)
	if err != nil {
		return Report{}, err
	}
	r := Report{Start: start, Seconds: after.at.Sub(before.at).Seconds(), Version: hello.Version, Sessions: frames.Rows,
		Frames: frames, Procs: map[string]Proc{}, Load: loadAverage()}
	for name, pids := range parts(after, roles, server) {
		r.Procs[name] = usage(before, after, pids)
	}
	if okA && okB {
		r.Sessions = pb.Sessions
		r.Core = window(pa, pb, refreshes(p, pb.Status))
	}
	if apps := parts(after, nil, 0)["app"]; len(apps) > 0 {
		a := &App{PID: apps[0]}
		var problems []string
		if sampleFor > 0 {
			n, busy, err := sampleApp(a.PID, sampleFor, os.TempDir())
			a.Samples, a.Busy = n, busy
			if err != nil {
				problems = append(problems, err.Error())
			}
		}
		if now, peak, err := footprint(a.PID); err == nil {
			a.Footprint, a.Peak = now, peak
		} else {
			problems = append(problems, err.Error())
		}
		a.Problem = strings.Join(problems, "; ")
		r.App = a
	}
	return r, nil
}

// refreshes reads each session's status line refreshInterval from the
// settings file Irie wrote for it, 0 when it has none.
func refreshes(p paths.Paths, sessions map[string]uint64) map[string]int {
	out := map[string]int{}
	for id := range sessions {
		b, err := os.ReadFile(filepath.Join(p.Session(id), claude.SettingsFile))
		if err != nil {
			continue
		}
		var s struct {
			StatusLine *claude.StatusLine `json:"statusLine"`
		}
		if json.Unmarshal(b, &s) == nil && s.StatusLine != nil {
			out[id] = s.StatusLine.RefreshInterval
		}
	}
	return out
}

// loadAverage is the machine's load average over 1, 5 and 15 minutes.
func loadAverage() [3]float64 {
	var out [3]float64
	b, err := exec.Command("sysctl", "-n", "vm.loadavg").Output()
	if err != nil {
		if b, err = os.ReadFile("/proc/loadavg"); err != nil {
			return out
		}
	}
	f := strings.Fields(strings.Trim(strings.TrimSpace(string(b)), "{}"))
	for i := 0; i < 3 && i < len(f); i++ {
		out[i], _ = strconv.ParseFloat(f[i], 64)
	}
	return out
}

// comparing compares two saved reports.
func comparing(fa, fb string, within float64) error {
	var a, b Report
	for _, x := range []struct {
		file string
		r    *Report
	}{{fa, &a}, {fb, &b}} {
		buf, err := os.ReadFile(x.file)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(buf, x.r); err != nil {
			return fmt.Errorf("%s: %w", x.file, err)
		}
	}
	rows, ok := compare(a, b, within)
	if err := printCompare(os.Stdout, a, b, rows, ok, within); err != nil {
		return err
	}
	if !ok {
		os.Exit(2)
	}
	return nil
}
