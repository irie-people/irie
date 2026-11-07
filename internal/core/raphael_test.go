package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/proc"
	"github.com/irie-people/irie/internal/raphael"
	"github.com/irie-people/irie/internal/tmux"
)

// fixRunner stands in for the tools a fix runs: ps says nothing runs, and
// every other command waits until release is closed, then succeeds.
type fixRunner struct {
	mu      sync.Mutex
	ran     []string
	release chan struct{}
}

func (f *fixRunner) Run(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
	line := strings.Join(append([]string{name}, args...), " ")
	f.mu.Lock()
	f.ran = append(f.ran, line)
	f.mu.Unlock()
	if name == "/bin/ps" || name == "/usr/bin/du" {
		return nil, nil, nil
	}
	if f.release != nil {
		<-f.release
	}
	return nil, nil, nil
}

func (f *fixRunner) lines() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.ran...)
}

// raphCore is a core whose Raphael has one finding with a fix, a tool's,
// whose tools fixRunner stands in for.
func raphCore(t *testing.T) (*Core, *fixRunner) {
	t.Helper()
	c, _ := dogCore(t)
	t.Setenv("IRIE_RAPHAEL", "")
	f := &fixRunner{}
	c.raph.runner = f
	fs := findingsWithFix(t)
	c.raph.mu.Lock()
	c.raph.slow, c.raph.checked = fs, time.Now()
	c.raph.mu.Unlock()
	return c, f
}

// findingsWithFix is a look's findings, made by the raphael package from a
// cache it measures: Go's build cache, with its fix, and a startup item.
func findingsWithFix(t *testing.T) []raphael.Finding {
	t.Helper()
	var fs []raphael.Finding
	b, _ := json.Marshal([]map[string]any{
		{"id": "cache:go-build", "area": "caches", "level": "note", "text": "Go's build cache takes 13 GB.", "size": 13e9,
			"fix": map[string]any{"label": "Clear Go's build cache", "run": []string{"go clean -cache"}, "about": "Frees about 13 GB.", "kind": "tool", "tool": "/usr/local/go/bin/go", "path": "/x", "busy": []string{"go"}}},
		{"id": "launch:~/Library/LaunchAgents/com.example.agent.plist", "area": "startup", "level": "note", "text": "com.example.agent is worth a look.", "key": "com.example.agent"},
	})
	if err := json.Unmarshal(b, &fs); err != nil {
		t.Fatal(err)
	}
	if fs[0].Fix == nil || fs[0].Fix.Label == "" {
		t.Fatalf("the fix did not read: %+v", fs[0])
	}
	return fs
}

func TestRaphaelRunsAFixOnlyOnTheOwnersOK(t *testing.T) {
	c, f := raphCore(t)
	// Without OK, nothing runs, and he says what would.
	_, err := c.raphaelOp(0, ipc.RaphaelArgs{Fix: "cache:go-build"})
	if err == nil || !strings.Contains(err.Error(), "say OK") || !strings.Contains(err.Error(), "go clean -cache") {
		t.Fatalf("no OK: %v", err)
	}
	// An agent can't say OK for its user.
	c.snap = tmux.Snapshot{Panes: []tmux.Pane{{ID: "%1", Role: tmux.RoleAgent, PID: os.Getppid()}}}
	if _, err := c.raphaelOp(os.Getpid(), ipc.RaphaelArgs{Fix: "cache:go-build", OK: true}); err == nil || !strings.Contains(err.Error(), "An agent can't") {
		t.Fatalf("from an agent: %v", err)
	}
	if ran := f.lines(); len(ran) != 0 {
		t.Fatalf("ran %v without the owner's OK", ran)
	}
	c.snap = tmux.Snapshot{}
	// An unknown finding has nothing to fix.
	if _, err := c.raphaelOp(0, ipc.RaphaelArgs{Fix: "cache:nothing", OK: true}); err == nil {
		t.Error("fixed a finding that is not there")
	}
	// With OK, it runs, one at a time.
	f.release = make(chan struct{})
	r, err := c.raphaelOp(0, ipc.RaphaelArgs{Fix: "cache:go-build", OK: true})
	if err != nil || r.Fixing != "cache:go-build" {
		t.Fatalf("with OK: %+v, %v", r, err)
	}
	if _, err := c.raphaelOp(0, ipc.RaphaelArgs{Fix: "cache:go-build", OK: true}); err == nil || !strings.Contains(err.Error(), "one at a time") {
		t.Errorf("a second at once: %v", err)
	}
	close(f.release)
	c.raph.mu.Lock()
	done := c.raph.fixed
	c.raph.mu.Unlock()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the fix did not end")
	}
	r = c.raphaelReport()
	if r.Fixing != "" || len(r.Fixes) != 1 || r.Fixes[0].Error || !strings.Contains(r.Fixes[0].Said, "Clear Go's build cache") {
		t.Errorf("after: %+v", r)
	}
	for _, x := range r.Findings {
		if x.ID == "cache:go-build" {
			t.Error("what was fixed is still listed")
		}
	}
	if ran := f.lines(); !containsLine(ran, "/usr/local/go/bin/go clean -cache") {
		t.Errorf("ran %v", ran)
	}
	// What he did is kept for the next core.
	b, err := os.ReadFile(c.raphaelFile())
	if err != nil || !strings.Contains(string(b), "Clear Go's build cache") {
		t.Errorf("raphael.json = %s, %v", b, err)
	}
}

func containsLine(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}

func TestRaphaelMarksAStartupItemAsTheOwners(t *testing.T) {
	c, _ := raphCore(t)
	id := "launch:~/Library/LaunchAgents/com.example.agent.plist"
	c.snap = tmux.Snapshot{Panes: []tmux.Pane{{ID: "%1", Role: tmux.RoleAgent, PID: os.Getppid()}}}
	if _, err := c.raphaelOp(os.Getpid(), ipc.RaphaelArgs{Mine: id}); err == nil {
		t.Error("an agent marked a startup item as its user's")
	}
	c.snap = tmux.Snapshot{}
	if _, err := c.raphaelOp(0, ipc.RaphaelArgs{Mine: "cache:go-build"}); err == nil {
		t.Error("a cache was marked as a startup item")
	}
	r, err := c.raphaelOp(0, ipc.RaphaelArgs{Mine: id})
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range r.Findings {
		if x.ID == id {
			t.Error("still flagged")
		}
	}
	if got := c.cfg().Arch.Raphael.Known; len(got) != 1 || got[0] != "com.example.agent" {
		t.Errorf("known = %v", got)
	}
	b, _ := os.ReadFile(c.paths.ConfigFile())
	if !strings.Contains(string(b), "com.example.agent") {
		t.Errorf("config.toml =\n%s", b)
	}
}

func TestRaphaelsArchRow(t *testing.T) {
	c, _ := raphCore(t)
	var row ipc.ArchAgent
	for _, a := range c.archList().Agents {
		if a.Name == "raphael" {
			row = a
		}
	}
	if row.Nickname != "Raffi" || row.Icon != "❤️‍🩹" || !row.On || !strings.Contains(row.Status, "2 are worth a look") && !strings.Contains(row.Status, "2 things are worth a look") {
		t.Fatalf("row = %+v", row)
	}
	if len(row.Actions) != 1 || row.Actions[0].Op != ipc.OpRaphael || !strings.Contains(string(row.Actions[0].Args), `"check":true`) {
		t.Errorf("actions = %+v", row.Actions)
	}
	if len(row.Items) != 2 {
		t.Fatalf("items = %+v", row.Items)
	}
	var fix, mine *ipc.ArchAction
	for _, it := range row.Items {
		for i, x := range it.Actions {
			switch x.Label {
			case "Clear Go's build cache":
				fix = &it.Actions[i]
			case "It's mine":
				mine = &it.Actions[i]
			}
		}
	}
	// A fix asks first, saying what it runs; marking an item as yours does not.
	if fix == nil || !strings.Contains(fix.Ask, "go clean -cache") || !strings.Contains(fix.Ask, "Frees about 13 GB") || strings.Contains(string(fix.Args), `"ok"`) {
		t.Errorf("fix = %+v", fix)
	}
	if mine == nil || mine.Ask != "" {
		t.Errorf("mine = %+v", mine)
	}
	keys := map[string]bool{}
	for _, s := range row.Settings {
		keys[s.Key] = true
	}
	for _, k := range []string{"arch.raphael.on", "ui.freeze_dog", "arch.raphael.core", "arch.raphael.every", "arch.raphael.known"} {
		if !keys[k] {
			t.Errorf("no setting %s", k)
		}
	}
	// Before his first look, what the minute's samples found counts.
	c.raph.mu.Lock()
	c.raph.checked = time.Time{}
	c.raph.quick = []raphael.Finding{{ID: "disk", Area: raphael.AreaDisk, Level: raphael.Warn, Text: "Your disk is nearly full."}}
	c.raph.mu.Unlock()
	if r := c.raphaelNow(); !strings.Contains(r.status, "So far 1 thing needs care, and 2 are worth a look.") {
		t.Errorf("before his first look: %q", r.status)
	}
	// Off, he says so.
	cfg := *c.cfg()
	cfg.Arch.Raphael.On = false
	c.useConfig(cfg)
	if r := c.raphaelNow(); r.on || !strings.HasPrefix(r.status, "Off") {
		t.Errorf("off: %+v", r)
	}
}

func TestRaphaelRestartsACoreWhoseLoopStandsStill(t *testing.T) {
	c, _ := dogCore(t)
	t.Setenv("IRIE_RAPHAEL", "")
	cfg := config.Default()
	cfg.Arch.Raphael.Core = time.Minute
	c.useConfig(cfg)
	now := time.Now()
	if c.lookAtCore(now, false) {
		t.Fatal("restarted a core whose loop has not started")
	}
	c.raph.beat.Store(now.Add(-30 * time.Second).UnixNano())
	if c.lookAtCore(now, false) {
		t.Fatal("restarted a core whose loop went round 30 s ago")
	}
	c.raph.beat.Store(now.Add(-2 * time.Minute).UnixNano())
	// A late look, the core itself held up: its time starts again.
	if c.lookAtCore(now, true) {
		t.Fatal("restarted a core that was itself held up")
	}
	if still := time.Since(time.Unix(0, c.raph.beat.Load())); still > time.Minute {
		t.Errorf("the loop's time did not start again: %v", still)
	}
	c.raph.beat.Store(now.Add(-2 * time.Minute).UnixNano())
	if !c.lookAtCore(now, false) {
		t.Fatal("a core whose loop stood still for 2 minutes is left be")
	}
	reports, _ := filepath.Glob(filepath.Join(c.paths.Freezes(), "*-core.txt"))
	if len(reports) != 1 {
		t.Fatalf("reports = %v", reports)
	}
	b, _ := os.ReadFile(reports[0])
	if !strings.Contains(string(b), "stood still for 2m0s") || !strings.Contains(string(b), "goroutine") {
		t.Errorf("report:\n%.300s", b)
	}
	// The freeze dog's look leaves with code 3, for the supervisor to start
	// the core again; a late look never.
	var code int
	c.raph.exit = func(n int) { code = n }
	c.raph.beat.Store(now.Add(-2 * time.Minute).UnixNano())
	c.watchCore(now, now.Add(-time.Minute))
	if code != 0 {
		t.Fatal("a late look restarted the core")
	}
	c.raph.beat.Store(now.Add(-2 * time.Minute).UnixNano())
	c.watchCore(now, now.Add(-coreWatchTick))
	if code != 3 {
		t.Fatalf("exit code = %d, want 3", code)
	}
	// Three restarts in half an hour: he leaves it be.
	for i := 1; i <= 2; i++ {
		name := now.Add(-time.Duration(i)*time.Minute).UTC().Format("2006-01-02T15-04-05Z") + "-core.txt"
		os.WriteFile(filepath.Join(c.paths.Freezes(), name), nil, 0o600)
	}
	if c.lookAtCore(now, false) {
		t.Error("restarted a core that stuck three times in half an hour")
	}
	// Off, or with arch.raphael.core 0s, never.
	os.RemoveAll(c.paths.Freezes())
	cfg.Arch.Raphael.Core = 0
	c.useConfig(cfg)
	c.raph.beat.Store(now.Add(-time.Hour).UnixNano())
	if c.lookAtCore(now, false) {
		t.Error("restarted with arch.raphael.core = 0s")
	}
}

func TestRecentRestarts(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	for _, name := range []string{"2026-11-01T11-00-00Z-app-401.txt", "2026-11-01T10-00-00Z-sidebar-402.txt", "2026-11-01T09-00-00Z-core.txt", "2026-11-01T08-00-00Z-403.txt", "2026-10-01T08-00-00Z-core.txt", "notes.txt"} {
		os.WriteFile(filepath.Join(dir, name), nil, 0o600)
	}
	rs := recentRestarts(dir, now)
	var what []string
	for _, r := range rs {
		what = append(what, r.What)
	}
	if got := strings.Join(what, " "); got != "app sidebar core app" {
		t.Errorf("restarts = %s, want the last week's, newest first", got)
	}
}

func TestRaphaelForceQuitsASidebarThatStopsAnswering(t *testing.T) {
	c, f := dogCore(t)
	// The app has used up its restarts: a sidebar has its own.
	for i := 0; i < freezeRestarts; i++ {
		c.fdog.allow("app", time.Now())
	}
	f.mu.Lock()
	f.procs[701] = proc.Proc{PID: 701, Start: 7010, Name: "irie"}
	f.mu.Unlock()
	t.Setenv("IRIE_RAPHAEL", "")
	ch := watchFor(t, c, 701, "")
	t0 := time.Now()
	c.lookAtWindows(t0)
	pinged(t, ch)
	// A sidebar gets a minute however short ui.freeze_dog is: a busy
	// machine stalls a terminal's loop longer than an app's.
	for i := 1; i <= 50; i++ {
		c.lookAtWindows(t0.Add(time.Duration(i) * time.Second))
	}
	if restarting(c) != 0 {
		t.Fatal("a sidebar was restarted before a minute without an answer")
	}
	for i := 51; i <= 61; i++ {
		c.lookAtWindows(t0.Add(time.Duration(i) * time.Second))
	}
	for end := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if killed, _, _ := f.calls(); len(killed) == 1 {
			break
		}
		if time.Now().After(end) {
			t.Fatal("the sidebar was not force quit")
		}
	}
	killed, opened, sampled := f.calls()
	if killed[0] != 701 || len(opened) != 0 || len(sampled) != 1 || !strings.Contains(filepath.Base(sampled[0]), "-sidebar-701") {
		t.Errorf("killed %v, opened %v, sampled %v: its supervisor starts a sidebar again, not open", killed, opened, sampled)
	}
}

func TestATestCoreNeverRestartsASidebar(t *testing.T) {
	c, f := dogCore(t)
	t.Setenv("IRIE_RAPHAEL", "off")
	f.mu.Lock()
	f.procs[702] = proc.Proc{PID: 702, Start: 7020, Name: "irie"}
	f.mu.Unlock()
	ch := watchFor(t, c, 702, "")
	t0 := time.Now()
	c.lookAtWindows(t0)
	pinged(t, ch)
	for i := 1; i <= 120; i++ {
		c.lookAtWindows(t0.Add(time.Duration(i) * time.Second))
	}
	if restarting(c) != 0 {
		t.Error("a test core restarted the test's sidebar")
	}
}

func TestRaphaelOffRestartsNothing(t *testing.T) {
	c, f := dogCore(t)
	cfg := config.Default()
	cfg.Arch.Raphael.On = false
	c.useConfig(cfg)
	f.add(801)
	ch := watchFor(t, c, 801, "/Applications/Irie.app")
	t0 := time.Now()
	c.lookAtWindows(t0)
	pinged(t, ch)
	for i := 1; i <= 20; i++ {
		c.lookAtWindows(t0.Add(time.Duration(i) * time.Second))
	}
	if restarting(c) != 0 {
		t.Error("restarting with Raphael off")
	}
}
