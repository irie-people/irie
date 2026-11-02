package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/perf"
)

// ctlPerf is `irie ctl perf`: the core's own counters since it started
// (docs/proposals/optimisation.md, iteration 1). tools/perf reads two of
// them for a report of one window.
func ctlPerf(args []string) error {
	fs := flag.NewFlagSet("ctl perf", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the counters as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New("usage: irie ctl perf [--json]")
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	var r ipc.PerfResult
	if err := ipc.Call(sock, 10*time.Second, ipc.OpPerf, nil, &r); err != nil {
		var e *ipc.Error
		if errors.As(err, &e) && e.Code == ipc.CodeUnknownOp {
			return errors.New("this core counts nothing yet; an install of a newer irie brings `perf`")
		}
		return err
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(r)
	}
	return printPerf(os.Stdout, r)
}

// printPerf prints the counters as tables: each op and timed piece of work
// with how many, the median, the 99th percentile and the longest; the lock;
// then the counts.
func printPerf(w io.Writer, r ipc.PerfResult) error {
	up := r.Now.Sub(r.Since)
	secs := max(up.Seconds(), 1)
	fmt.Fprintf(w, "core %s, pid %d, counting for %s: %d sessions, CPU %.1f%% of a core, its processes %.1f%%\n",
		r.Version, r.PID, up.Round(time.Second), r.Sessions,
		float64(r.CPU)/1e4/secs, float64(r.Children)/1e4/secs)
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', tabwriter.AlignRight)
	table := func(head string, m map[string]perf.Times) {
		fmt.Fprintf(tw, "\n%s\tn\t/s\tp50\tp99\tmax\t\n", strings.ToUpper(head))
		for _, name := range perf.Names(m) {
			t := m[name]
			fmt.Fprintf(tw, "%s\t%d\t%.2f\t%s\t%s\t%s\t\n", name, t.N, float64(t.N)/secs, dur(t.P50), dur(t.P99), dur(t.Max))
		}
	}
	table("op", r.Ops)
	table("work", r.Times)
	l := r.Lock
	fmt.Fprintf(tw, "\nLOCK\tn\t/s\tp50\tp99\tmax\t\n")
	fmt.Fprintf(tw, "taken\t%d\t%.2f\t\t\t\t\n", l.Locks, float64(l.Locks)/secs)
	fmt.Fprintf(tw, "waited\t%d\t%.2f\t%s\t%s\t%s\t\n", l.Contended, float64(l.Contended)/secs, dur(l.Wait.P50), dur(l.Wait.P99), dur(l.Wait.Max))
	fmt.Fprintf(tw, "held\t%d\t%.2f\t%s\t%s\t%s\t\n", l.Hold.N, float64(l.Hold.N)/secs, dur(l.Hold.P50), dur(l.Hold.P99), dur(l.Hold.Max))
	fmt.Fprintf(tw, "\nCOUNT\tn\t/s\t\t\t\t\n")
	for _, name := range perf.Names(r.Counts) {
		n := r.Counts[name]
		fmt.Fprintf(tw, "%s\t%d\t%.2f\t\t\t\t\n", name, n, float64(n)/secs)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	var subs []string
	for _, v := range perf.Names(r.Subscribers) {
		subs = append(subs, fmt.Sprintf("%s %d", v, r.Subscribers[v]))
	}
	_, err := fmt.Fprintf(w, "\nsubscriptions: %s\n", strings.Join(subs, ", "))
	return err
}

// dur prints nanoseconds short: 850µs, 12.3ms, 1.20s.
func dur(ns int64) string {
	d := time.Duration(ns)
	switch {
	case d == 0:
		return "-"
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d/time.Microsecond)
	case d < time.Second:
		return fmt.Sprintf("%.1fms", float64(d)/1e6)
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}
