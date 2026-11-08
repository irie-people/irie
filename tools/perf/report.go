package main

import (
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/perf"
)

// Report is one window of the running Irie.
type Report struct {
	Start    time.Time  `json:"start"`
	Seconds  float64    `json:"seconds"`
	Version  string     `json:"version"` // the core's
	Sessions int        `json:"sessions"`
	Load     [3]float64 `json:"load"` // the load average at the end: 1, 5 and 15 minutes
	Frames   Frames     `json:"frames"`
	// Procs are Irie's parts by name: core, sidebar, placeholder, remote,
	// hosts, agents (everything under the hosts), tmux and app.
	Procs map[string]Proc `json:"procs"`
	App   *App            `json:"app,omitempty"`
	Core  *Core           `json:"core,omitempty"`
}

// Core is the window as the core counted it (ipc.PerfResult), when it
// counts.
type Core struct {
	CPU      float64               `json:"cpu"`          // its own, percent of one core
	Children float64               `json:"children_cpu"` // the processes it ran and waited for
	Ops      map[string]perf.Times `json:"ops"`
	Times    map[string]perf.Times `json:"times"`
	Counts   map[string]uint64     `json:"counts"`
	Lock     perf.Lock             `json:"lock"`
	// The processes that started: `irie statusline` (each sends one status
	// request), `irie hook` (one hook request each) and `claude agents`
	// (the core's own runs), with what those runs took.
	StatusLine uint64            `json:"statusline_starts"`
	Hooks      uint64            `json:"hook_starts"`
	Agents     uint64            `json:"claude_agents_runs"`
	AgentsCPU  float64           `json:"claude_agents_cpu"` // percent of one core
	AgentsWhy  map[string]uint64 `json:"claude_agents_why"`
	// Status is each session's status line updates a second, for the
	// sessions there at both ends; Refresh is each one's refreshInterval,
	// from its settings file, 0 when unknown.
	Status  map[string]float64 `json:"status"`
	Refresh map[string]int     `json:"refresh"`
}

// window is the core's counters between two reads.
func window(a, b ipc.PerfResult, refresh map[string]int) *Core {
	secs := b.Now.Sub(a.Now).Seconds()
	if secs <= 0 || a.PID != b.PID {
		return nil // a core that restarted counts from nothing again
	}
	c := &Core{
		CPU: pct(b.CPU-a.CPU, secs), Children: pct(b.Children-a.Children, secs),
		Ops: minus(a.Ops, b.Ops), Times: minus(a.Times, b.Times), Counts: map[string]uint64{},
		Lock: b.Lock.Minus(a.Lock), Status: map[string]float64{}, Refresh: refresh, AgentsWhy: map[string]uint64{},
	}
	for name, n := range b.Counts {
		if d := n - min(a.Counts[name], n); d > 0 {
			c.Counts[name] = d
		}
	}
	c.StatusLine, c.Hooks = c.Ops[ipc.OpStatus].N, c.Ops[ipc.OpHook].N
	for name, n := range c.Counts {
		why, ok := strings.CutPrefix(name, "outside.")
		switch {
		case !ok:
		case why == "cpu_us":
			c.AgentsCPU = pct(int64(n), secs)
		default:
			c.Agents += n
			c.AgentsWhy[why] = n
		}
	}
	for id, n := range b.Status {
		if m, ok := a.Status[id]; ok {
			c.Status[id] = float64(n-min(m, n)) / secs
		}
	}
	return c
}

// pct is microseconds of CPU over secs as a percent of one core.
func pct(us int64, secs float64) float64 { return float64(us) / 1e4 / secs }

func minus(a, b map[string]perf.Times) map[string]perf.Times {
	out := map[string]perf.Times{}
	for name, t := range b {
		if w := t.Minus(a[name]); w.N > 0 {
			out[name] = w
		}
	}
	return out
}

// rate is n over the window, a second.
func (r Report) rate(n int) float64 {
	if r.Seconds <= 0 {
		return 0
	}
	return float64(n) / r.Seconds
}

// Metric is one headline figure of a report, which compare holds two
// reports to. Floor is a difference too small to count, whatever the share,
// and Gate says whether the figure is the yardstick's own (Irie's timers
// and frames) rather than the agents' work, which no two windows repeat.
type Metric struct {
	Name  string
	Unit  string
	Floor float64
	Gate  bool
	Of    func(Report) (float64, bool)
}

// metrics are the figures the optimisation plan's gates use.
var metrics = []Metric{
	{"frames a second", "/s", 0.5, false, func(r Report) (float64, bool) { return r.rate(r.Frames.Total), true }},
	{"headers a second", "/s", 0.1, true, func(r Report) (float64, bool) { return r.rate(r.Frames.Types[ipc.FrameHeader].N), true }},
	{"snapshots after subscribe a second", "/s", 0.05, true, func(r Report) (float64, bool) { return r.rate(r.Frames.LaterSnapshots), true }},
	{"upserts a second", "/s", 0.5, false, func(r Report) (float64, bool) { return r.rate(r.Frames.Types[ipc.FrameUpsert].N), true }},
	{"app CPU", "%", 1, true, procCPU("app")},
	{"core CPU", "%", 1, true, procCPU("core")},
	{"sidebar CPU", "%", 1, true, procCPU("sidebar")},
	{"placeholder CPU", "%", 1, true, procCPU("placeholder")},
	{"tmux server CPU", "%", 1, false, procCPU("tmux")},
	{"app main thread busy", "%", 5, false, func(r Report) (float64, bool) {
		if r.App == nil || r.App.Samples == 0 {
			return 0, false
		}
		return 100 * r.App.Busy, true
	}},
	{"status line starts a session a second", "/s", 0.02, true, func(r Report) (float64, bool) {
		if r.Core == nil || len(r.Core.Status) == 0 {
			return 0, false
		}
		var sum float64
		for _, v := range r.Core.Status {
			sum += v
		}
		return sum / float64(len(r.Core.Status)), true
	}},
	{"claude agents runs a minute", "/min", 1, true, func(r Report) (float64, bool) {
		if r.Core == nil {
			return 0, false
		}
		return 60 * r.rate(int(r.Core.Agents)), true
	}},
	{"hook starts a second", "/s", 0.5, false, func(r Report) (float64, bool) {
		if r.Core == nil {
			return 0, false
		}
		return r.rate(int(r.Core.Hooks)), true
	}},
}

func procCPU(name string) func(Report) (float64, bool) {
	return func(r Report) (float64, bool) {
		p, ok := r.Procs[name]
		return p.CPU, ok && p.N > 0
	}
}

// Agreement is how one figure of two reports compares.
type Agreement struct {
	Metric string
	A, B   float64
	Off    float64 // |a-b| over the larger, 0 within the floor
	Gate   bool
	Agree  bool
}

// compare holds b to a, figure by figure: two figures agree when they
// differ by at most within of the larger, or by no more than the figure's
// floor. It returns each figure and whether every gated one agrees.
func compare(a, b Report, within float64) ([]Agreement, bool) {
	var out []Agreement
	ok := true
	for _, m := range metrics {
		x, okx := m.Of(a)
		y, oky := m.Of(b)
		if !okx || !oky {
			continue
		}
		g := Agreement{Metric: m.Name, A: x, B: y, Gate: m.Gate}
		if d := math.Abs(x - y); d > m.Floor {
			g.Off = d / math.Max(math.Abs(x), math.Abs(y))
		}
		g.Agree = g.Off <= within
		if g.Gate && !g.Agree {
			ok = false
		}
		out = append(out, g)
	}
	return out, ok
}

// print writes the report for a person.
func (r Report) print(w io.Writer) error {
	fmt.Fprintf(w, "Irie over %.0f s from %s: core %s, %d sessions, load %.1f %.1f %.1f\n",
		r.Seconds, r.Start.Local().Format("15:04:05"), r.Version, r.Sessions, r.Load[0], r.Load[1], r.Load[2])
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', tabwriter.AlignRight)
	f := r.Frames
	fmt.Fprintf(tw, "\nFRAMES\tn\t/s\tbytes/s\tmean bytes\t\n")
	fmt.Fprintf(tw, "all\t%d\t%.2f\t%s\t%s\t\n", f.Total, r.rate(f.Total), size(int64(r.rate(int(f.Bytes)))), size(mean(f.Bytes, f.Total)))
	for _, t := range perf.Names(f.Types) {
		x := f.Types[t]
		fmt.Fprintf(tw, "%s\t%d\t%.2f\t%s\t%s\t\n", t, x.N, r.rate(x.N), size(int64(r.rate(int(x.Bytes)))), size(mean(x.Bytes, x.N)))
	}
	tw.Flush()
	fmt.Fprintf(w, "snapshots after subscribe: %d, carrying %d rows of which %d had changed\n", f.LaterSnapshots, f.SnapshotRows, f.SnapshotChanged)
	fmt.Fprintf(w, "upserts changed: %s (%d changed nothing)\n", counted(f.UpsertFields), f.UpsertsSame)
	fmt.Fprintf(w, "headers changed: %s (%d changed nothing)\n", counted(f.HeaderFields), f.HeadersSame)

	fmt.Fprintf(tw, "\nPROCESSES\tn\tCPU %%\tresident\t\n")
	for _, name := range []string{"core", "sidebar", "placeholder", "remote", "tmux", "hosts", "agents", "app"} {
		if p, ok := r.Procs[name]; ok && p.N > 0 {
			fmt.Fprintf(tw, "%s\t%d\t%.1f\t%s\t\n", name, p.N, p.CPU, size(p.RSS))
		}
	}
	tw.Flush()
	if a := r.App; a != nil {
		var parts []string
		if a.Footprint > 0 {
			parts = append(parts, fmt.Sprintf("footprint %s (peak %s)", size(a.Footprint), size(a.Peak)))
		}
		if a.Samples > 0 {
			parts = append(parts, fmt.Sprintf("main thread busy in %.0f%% of %d samples", 100*a.Busy, a.Samples))
		}
		if a.Problem != "" {
			parts = append(parts, a.Problem)
		}
		fmt.Fprintf(w, "app, pid %d: %s\n", a.PID, strings.Join(parts, "; "))
	}

	c := r.Core
	if c == nil {
		_, err := fmt.Fprintln(w, "\nThe core keeps no counters (no perf op), or it restarted in the window.")
		return err
	}
	fmt.Fprintf(w, "\ncore CPU by its own count: %.1f%% itself, %.1f%% in the processes it ran\n", c.CPU, c.Children)
	fmt.Fprintf(tw, "\nSTARTS\tn\t/s\t\t\n")
	fmt.Fprintf(tw, "irie statusline\t%d\t%.2f\t%s\t\n", c.StatusLine, r.rate(int(c.StatusLine)), perSession(c))
	fmt.Fprintf(tw, "irie hook\t%d\t%.2f\t\t\n", c.Hooks, r.rate(int(c.Hooks)))
	fmt.Fprintf(tw, "claude agents\t%d\t%.2f\t%.1f a minute, %.1f%% of a core; %s\t\n", c.Agents, r.rate(int(c.Agents)), 60*r.rate(int(c.Agents)), c.AgentsCPU, counted(intMap(c.AgentsWhy)))
	tw.Flush()
	table := func(head string, m map[string]perf.Times) {
		fmt.Fprintf(tw, "\n%s\tn\t/s\tp50\tp99\tmax\t\n", head)
		for _, name := range perf.Names(m) {
			t := m[name]
			fmt.Fprintf(tw, "%s\t%d\t%.2f\t%s\t%s\t%s\t\n", name, t.N, r.rate(int(t.N)), dur(t.P50), dur(t.P99), dur(t.Max))
		}
	}
	table("OPS", c.Ops)
	table("WORK", c.Times)
	l := c.Lock
	fmt.Fprintf(tw, "\nLOCK\tn\t/s\tp50\tp99\tmax\t\n")
	fmt.Fprintf(tw, "waits\t%d\t%.2f\t%s\t%s\t%s\t\n", l.Contended, r.rate(int(l.Contended)), dur(l.Wait.P50), dur(l.Wait.P99), dur(l.Wait.Max))
	fmt.Fprintf(tw, "holds\t%d\t%.2f\t%s\t%s\t%s\t\n", l.Hold.N, r.rate(int(l.Hold.N)), dur(l.Hold.P50), dur(l.Hold.P99), dur(l.Hold.Max))
	if err := tw.Flush(); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "frames sent by the core: %s\n", counted(prefixed(c.Counts, "frame.")))
	return err
}

// perSession says how often each session's status line ran: the mean, the
// most, and how many sessions have each refresh interval.
func perSession(c *Core) string {
	if len(c.Status) == 0 {
		return ""
	}
	var sum, most float64
	for _, v := range c.Status {
		sum += v
		most = math.Max(most, v)
	}
	by := map[string]int{}
	for id := range c.Status {
		by[fmt.Sprintf("%d s", c.Refresh[id])]++
	}
	return fmt.Sprintf("%.2f a session (most %.2f); refresh %s", sum/float64(len(c.Status)), most, counted(by))
}

func prefixed(m map[string]uint64, p string) map[string]int {
	out := map[string]int{}
	for k, v := range m {
		if rest, ok := strings.CutPrefix(k, p); ok {
			out[rest] = int(v)
		}
	}
	return out
}

func intMap(m map[string]uint64) map[string]int {
	out := map[string]int{}
	for k, v := range m {
		out[k] = int(v)
	}
	return out
}

// counted lists a count by name, the largest first: "detail 32, command 31".
func counted(m map[string]int) string {
	if len(m) == 0 {
		return "none"
	}
	names := perf.Names(m)
	slices.SortStableFunc(names, func(a, b string) int { return m[b] - m[a] })
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = fmt.Sprintf("%s %d", n, m[n])
	}
	return strings.Join(parts, ", ")
}

func mean(total int64, n int) int64 {
	if n == 0 {
		return 0
	}
	return total / int64(n)
}

// size prints bytes short: 980 B, 12.3 KB, 338 MB.
func size(b int64) string {
	switch {
	case b < 1000:
		return fmt.Sprintf("%d B", b)
	case b < 1000*1000:
		return fmt.Sprintf("%.1f KB", float64(b)/1e3)
	case b < 1000*1000*1000:
		return fmt.Sprintf("%.0f MB", float64(b)/1e6)
	}
	return fmt.Sprintf("%.2f GB", float64(b)/1e9)
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

// printCompare writes how two reports compare, figure by figure.
func printCompare(w io.Writer, a, b Report, rows []Agreement, ok bool, within float64) error {
	fmt.Fprintf(w, "A: %.0f s from %s, %d sessions, load %.1f\nB: %.0f s from %s, %d sessions, load %.1f, %s later\n\n",
		a.Seconds, a.Start.Local().Format("15:04:05"), a.Sessions, a.Load[0],
		b.Seconds, b.Start.Local().Format("15:04:05"), b.Sessions, b.Load[0], b.Start.Sub(a.Start).Round(time.Second))
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "FIGURE\tA\tB\tOFF\t\n")
	for _, g := range rows {
		mark := "agree"
		if !g.Agree {
			mark = "DIFFER"
		}
		if !g.Gate {
			mark += " (the agents' work; not gated)"
		}
		fmt.Fprintf(tw, "%s\t%.2f\t%.2f\t%.0f%%\t%s\n", g.Metric, g.A, g.B, 100*g.Off, mark)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	verdict := fmt.Sprintf("every gated figure agrees within %.0f%%", 100*within)
	if !ok {
		verdict = fmt.Sprintf("some gated figure differs by more than %.0f%%", 100*within)
	}
	_, err := fmt.Fprintf(w, "\n%s\n", verdict)
	return err
}
