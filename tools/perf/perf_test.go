package main

import (
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/perf"
)

func TestParseCPU(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"0:00.08":    80 * time.Millisecond,
		"4:20.86":    4*time.Minute + 20860*time.Millisecond,
		"102:13.10":  102*time.Minute + 13100*time.Millisecond,  // macOS: minutes past 59
		"01:02:03":   time.Hour + 2*time.Minute + 3*time.Second, // Linux
		"2-01:00:00": 49 * time.Hour,
		"1:02:03.50": time.Hour + 2*time.Minute + 3500*time.Millisecond,
	} {
		got, err := parseCPU(in)
		if err != nil || (got-want).Abs() > time.Millisecond {
			t.Errorf("parseCPU(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "12", "a:b"} {
		if _, err := parseCPU(bad); err == nil {
			t.Errorf("parseCPU(%q) took it", bad)
		}
	}
}

const psA = `    1     0 102:13.10  13296 /sbin/launchd
  500     1   0:01.00   4000 /home/alice/.local/share/irie/bin/irie
  501   500   0:10.00  30000 /home/alice/.local/share/irie/bin/irie
  600     1   1:00.00 120000 /home/alice/Applications/Irie.app/Contents/MacOS/Irie
  700     1   0:00.50   2000 /home/alice/.local/share/irie/bin/irie
  701   700   0:30.00 300000 /home/alice/.local/bin/claude
  702   701   0:01.00   1000 /bin/zsh
`

const psB = `    1     0 102:13.20  13296 /sbin/launchd
  500     1   0:01.00   4000 /home/alice/.local/share/irie/bin/irie
  501   500   0:10.60  31000 /home/alice/.local/share/irie/bin/irie
  600     1   1:06.00 130000 /home/alice/Applications/Irie.app/Contents/MacOS/Irie
  700     1   0:00.50   2000 /home/alice/.local/share/irie/bin/irie
  701   700   0:33.00 300000 /home/alice/.local/bin/claude
  703   701   0:00.40   1000 /usr/bin/git with spaces
`

func TestUsageOverAWindow(t *testing.T) {
	at := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	a, b := parseTable([]byte(psA), at), parseTable([]byte(psB), at.Add(60*time.Second))
	if p := b.procs[703]; p.comm != "/usr/bin/git with spaces" {
		t.Errorf("a command with spaces read as %q", p.comm)
	}
	got := parts(b, map[string][]int{"core": {500}, "agent": {700}}, 0)
	core := usage(a, b, got["core"])
	if core.N != 2 || core.CPU < 0.99 || core.CPU > 1.01 || core.RSS != 35000<<10 {
		t.Errorf("core %+v: want 2 processes, 1%% of a core (0.6 s in 60 s), 35000 KiB", core)
	}
	if app := usage(a, b, got["app"]); app.N != 1 || app.CPU < 9.99 || app.CPU > 10.01 {
		t.Errorf("app %+v: want 10%% (6 s in 60 s)", app)
	}
	// claude used 3 s; git started in the window and counts all of its 0.4 s;
	// zsh ended and is gone from the second table.
	if ag := usage(a, b, got["agents"]); ag.N != 2 || ag.CPU < 5.66 || ag.CPU > 5.67 {
		t.Errorf("agents %+v: want 2 processes at 3.4 s in 60 s", ag)
	}
	if h := got["hosts"]; len(h) != 1 || h[0] != 700 {
		t.Errorf("hosts %v", h)
	}
}

// sampleReport is a trimmed sample(1) call graph: the main thread caught 20
// times, 16 of them waiting for its next event.
const sampleReport = `Analysis of sampling Irie (pid 600) every 10 milliseconds
Call graph:
    20 Thread_100   DispatchQueue_1: com.apple.main-thread  (serial)
    + 20 start  (in dyld) + 2360  [0x1]
    +   20 NSApplicationMain  (in AppKit) + 880  [0x2]
    +     18 CFRunLoopRunSpecific  (in CoreFoundation) + 608  [0x3]
    +     ! 15 __CFRunLoopRun  (in CoreFoundation) + 1208  [0x4]
    +     ! : 15 __CFRunLoopServiceMachPort  (in CoreFoundation) + 160  [0x5]
    +     ! :   15 mach_msg2_trap  (in libsystem_kernel.dylib) + 8  [0x6]
    +     ! 3 __CFRunLoopRun  (in CoreFoundation) + 776  [0x7]
    +     !   3 CA::Transaction::commit()  (in QuartzCore) + 320  [0x8]
    +     2 RunCurrentEventLoopInMode  (in HIToolbox) + 292  [0x9]
    +       1 __CFRunLoopServiceMachPort  (in CoreFoundation) + 160  [0x5]
    +       1 SwiftUI graph update  (in SwiftUI)  [0xa]
    20 Thread_101: Irie freeze dog
    + 20 __CFRunLoopServiceMachPort  (in CoreFoundation) + 160  [0x5]

Total number in stack (recursive counted multiple, when >=5):
`

func TestMainThreadBusy(t *testing.T) {
	n, idle := mainThread([]byte(sampleReport))
	if n != 20 || idle != 16 {
		t.Errorf("main thread %d samples, %d idle; want 20 and 16 (the freeze dog's thread is not the main one)", n, idle)
	}
	if n, idle := mainThread([]byte("Call graph:\n    5 Thread_1\n")); n != 0 || idle != 0 {
		t.Errorf("no main thread read as %d, %d", n, idle)
	}
}

func TestParseFootprint(t *testing.T) {
	out := []byte("Irie [600]: 64-bit    Footprint: 296504896 B (16384 bytes per page)\n\nAuxiliary data:\n    phys_footprint_peak: 407555648 B\n")
	now, peak, err := parseFootprint(out)
	if err != nil || now != 296504896 || peak != 407555648 {
		t.Errorf("footprint %d, peak %d, %v", now, peak, err)
	}
	if _, _, err := parseFootprint([]byte("no such process")); err == nil {
		t.Error("an empty report read as a footprint")
	}
}

func TestCounterSaysWhatChanged(t *testing.T) {
	c := newCounter()
	for _, line := range []string{
		`{"t":"snapshot","sessions":[{"id":"s1","name":"a","detail":"x"},{"id":"s2","name":"b"}],"header":{"version":"1","plan":{"five_hour":{"used":35},"seven_day":{"used":66}}}}`,
		`{"t":"upsert","session":{"id":"s1","name":"a","detail":"y"}}`,
		`{"t":"upsert","session":{"id":"s1","name":"a","detail":"y"}}`,
		`{"t":"upsert","session":{"id":"s3","name":"c"}}`,
		`{"t":"header","header":{"version":"1","plan":{"five_hour":{"used":37},"seven_day":{"used":66}}}}`,
		`{"t":"header","header":{"version":"1","plan":{"five_hour":{"used":37},"seven_day":{"used":66}}}}`,
		`{"t":"header","header":{"version":"1","plan":{"five_hour":{"used":37},"seven_day":{"used":66}},"memory":{"agents":5}}}`,
		`{"t":"snapshot","sessions":[{"id":"s1","name":"a","detail":"y"},{"id":"s3","name":"c","cost_usd":1}],"header":{}}`,
		`{"t":"remove","id":"s3"}`,
		`not json`,
	} {
		c.take([]byte(line))
	}
	f := c.f
	if f.Total != 9 || f.Types[ipc.FrameUpsert].N != 3 || f.Types[ipc.FrameHeader].N != 3 {
		t.Fatalf("frames %+v", f.Types)
	}
	if f.UpsertFields["detail"] != 1 || f.UpsertsSame != 1 || f.UpsertFields["name"] != 1 {
		t.Errorf("upsert fields %v, same %d: want detail once, s3's name once, one that changed nothing", f.UpsertFields, f.UpsertsSame)
	}
	if f.HeaderFields["plan.five_hour"] != 1 || f.HeaderFields["memory"] != 1 || f.HeadersSame != 1 || f.HeaderFields["plan.seven_day"] != 0 {
		t.Errorf("header fields %v, same %d", f.HeaderFields, f.HeadersSame)
	}
	if f.LaterSnapshots != 1 || f.SnapshotRows != 2 || f.SnapshotChanged != 1 {
		t.Errorf("later snapshots %d, rows %d, changed %d: want 1, 2 and 1 (s3 gained a cost)", f.LaterSnapshots, f.SnapshotRows, f.SnapshotChanged)
	}
}

func TestWindowOfTheCoresCounts(t *testing.T) {
	var h perf.Hist
	for range 10 {
		h.Add(time.Millisecond)
	}
	at := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	a := ipc.PerfResult{PID: 9, Now: at, CPU: 1_000_000, Ops: map[string]perf.Times{ipc.OpStatus: h.Times()},
		Counts: map[string]uint64{"outside.watch": 4, "outside.cpu_us": 2_000_000}, Status: map[string]uint64{"s1": 10, "gone": 3}}
	for range 30 {
		h.Add(2 * time.Millisecond)
	}
	b := ipc.PerfResult{PID: 9, Now: at.Add(10 * time.Second), CPU: 1_500_000, Ops: map[string]perf.Times{ipc.OpStatus: h.Times(), ipc.OpHook: h.Times()},
		Counts: map[string]uint64{"outside.watch": 6, "outside.list": 1, "outside.cpu_us": 3_000_000}, Status: map[string]uint64{"s1": 15, "new": 2}}
	w := window(a, b, nil)
	if w == nil {
		t.Fatal("no window")
	}
	if w.CPU < 4.99 || w.CPU > 5.01 {
		t.Errorf("core CPU %.2f%%, want 5%% (0.5 s in 10 s)", w.CPU)
	}
	if w.StatusLine != 30 || w.Agents != 3 || w.AgentsWhy["watch"] != 2 || w.AgentsCPU < 9.99 || w.AgentsCPU > 10.01 {
		t.Errorf("starts: status %d, agents %d (%v), agents CPU %.2f", w.StatusLine, w.Agents, w.AgentsWhy, w.AgentsCPU)
	}
	if len(w.Status) != 1 || w.Status["s1"] != 0.5 {
		t.Errorf("per session %v: want s1 at 0.5 a second, and only the sessions at both ends", w.Status)
	}
	if b.PID = 10; window(a, b, nil) != nil {
		t.Error("a core that restarted in the window gave a window")
	}
}

func TestCompareAgreesWithinAShare(t *testing.T) {
	r := func(headers int, app float64) Report {
		return Report{Seconds: 60, Frames: Frames{Types: map[string]FrameType{ipc.FrameHeader: {N: headers}}},
			Procs: map[string]Proc{"app": {N: 1, CPU: app}}}
	}
	rows, ok := compare(r(120, 20), r(132, 22), 0.15)
	if !ok {
		t.Errorf("10%% apart should agree: %+v", rows)
	}
	if _, ok := compare(r(120, 20), r(120, 30), 0.15); ok {
		t.Error("app CPU 20%% against 30%% should differ")
	}
	// Under the floor a share means nothing: 0.5%% against 1%% of a core.
	if _, ok := compare(r(0, 0.5), r(0, 1), 0.15); !ok {
		t.Error("figures within the floor should agree")
	}
	var b strings.Builder
	if err := printCompare(&b, r(1, 1), r(1, 1), rows, ok, 0.15); err != nil || !strings.Contains(b.String(), "headers a second") {
		t.Errorf("printed %q, %v", b.String(), err)
	}
}
