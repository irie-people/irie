package core

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/irie-hq/irie/internal/config"
	"github.com/irie-hq/irie/internal/event"
	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/paths"
	"github.com/irie-hq/irie/internal/tmux"
)

// Session ids of sessions outside Irie in these tests.
const (
	idleID    = "11111111-2222-4333-8444-555555555555"
	busyID    = "22222222-3333-4444-8555-666666666666"
	waitingID = "33333333-4444-4555-8666-777777777777"
	shellID   = "44444444-5555-4666-8777-888888888888"
	stoppedID = "55555555-6666-4777-8888-999999999999"
)

// agentsJSON is what `claude agents --json` prints, out of start order.
const agentsJSON = `[
  {"pid": 4302, "cwd": "/home/alice/code/api", "kind": "background", "startedAt": 1761487260000,
   "sessionId": "` + busyID + `", "name": "flaky-test-fix", "status": "busy", "id": "7c5dcf5d", "state": "working"},
  {"pid": 4301, "cwd": "/home/alice/code/website", "kind": "interactive", "startedAt": 1761487200000,
   "sessionId": "` + idleID + `", "name": "auth\u001b[31m refactor\n", "status": "idle"},
  {"pid": 4303, "cwd": "/home/alice", "kind": "interactive", "startedAt": 1761487320000,
   "sessionId": "` + waitingID + `", "name": "website-3f", "status": "waiting", "waitingFor": "permission prompt"},
  {"pid": 4304, "cwd": "/home/alice", "kind": "interactive", "startedAt": 1761487380000,
   "sessionId": "` + shellID + `", "name": "shell-job", "status": "shell"},
  {"cwd": "/home/alice/code/api", "kind": "background", "startedAt": 1761487440000,
   "sessionId": "` + stoppedID + `", "id": "--all", "state": "blocked"},
  {"pid": "4306", "cwd": "/home/alice", "kind": "interactive", "startedAt": 1761487500000,
   "sessionId": "not-a-uuid", "status": "idle"},
  42
]`

func TestParseAgents(t *testing.T) {
	got, err := parseAgents([]byte(agentsJSON))
	if err != nil {
		t.Fatal(err)
	}
	ms := func(n int64) time.Time { return time.UnixMilli(n).UTC() }
	want := []ipc.OutsideSession{
		{VendorID: idleID, Name: "auth [31m refactor", Kind: "interactive", Status: "idle", PID: 4301, Cwd: "/home/alice/code/website", Started: ms(1761487200000), Movable: true},
		{VendorID: busyID, ID: "7c5dcf5d", Name: "flaky-test-fix", Kind: "background", Status: "busy", State: "working", PID: 4302, Cwd: "/home/alice/code/api", Started: ms(1761487260000)},
		{VendorID: waitingID, Name: "website-3f", Kind: "interactive", Status: "waiting", WaitingFor: "permission prompt", PID: 4303, Cwd: "/home/alice", Started: ms(1761487320000)},
		{VendorID: shellID, Name: "shell-job", Kind: "interactive", Status: "shell", PID: 4304, Cwd: "/home/alice", Started: ms(1761487380000)},
		{VendorID: stoppedID, Kind: "background", State: "blocked", Cwd: "/home/alice/code/api", Started: ms(1761487440000)},
		{Kind: "interactive", Status: "idle", Cwd: "/home/alice", Started: ms(1761487500000)},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d sessions, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		g, w := got[i], want[i]
		g.Why = ""
		if !reflect.DeepEqual(g, w) {
			t.Errorf("session %d:\n got %+v\nwant %+v", i, g, w)
		}
	}
	// Only the idle one can move, and the others say why not.
	for i, why := range []string{"", "busy", "permission prompt", "shell", "not running", "no session id"} {
		if got[i].Movable != (why == "") || !strings.Contains(got[i].Why, why) {
			t.Errorf("session %d: movable %v, why %q, want %q", i, got[i].Movable, got[i].Why, why)
		}
	}
}

func TestParseAgentsSkipsANoticeBeforeTheArray(t *testing.T) {
	got, err := parseAgents([]byte("A new version of Claude Code is available.\n" + agentsJSON))
	if err != nil || len(got) != 6 {
		t.Errorf("got %d sessions, %v", len(got), err)
	}
	if got, err := parseAgents([]byte("  []\n")); err != nil || got == nil || len(got) != 0 {
		t.Errorf("an empty list = %v, %v", got, err)
	}
}

func TestParseAgentsRefusesBadJSON(t *testing.T) {
	for _, out := range []string{"", "not json", `{"sessions":[]}`, `[{"pid": 1`, "notice\n{}"} {
		if got, err := parseAgents([]byte(out)); err == nil {
			t.Errorf("%q: no error, got %+v", out, got)
		}
	}
}

// shellScript writes an executable shell script and returns its path.
func shellScript(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestListOutsideReadsStdoutOnly(t *testing.T) {
	list := filepath.Join(t.TempDir(), "agents.json")
	if err := os.WriteFile(list, []byte(agentsJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	cli := shellScript(t, `[ "$1 $2" = "agents --json" ] || exit 9
echo 'warning: [{"pid": 1, "sessionId": "x"}] is not for you' >&2
echo '[' >&2
cat '`+list+`'
`)
	got, err := listOutside(cli, os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 6 || got[0].VendorID != idleID {
		t.Errorf("got %+v", got)
	}
}

func TestListOutsideSaysWhatWentWrong(t *testing.T) {
	defer func(d time.Duration) { outsideTimeout = d }(outsideTimeout)
	// Only "hangs" waits out the timeout. The others get a long one, so a
	// loaded machine that is slow to start their shell is not a hang.
	for _, c := range []struct {
		name, cli, want string
		timeout         time.Duration
	}{
		{"missing", "/home/alice/no/such/claude", "not found", time.Minute},
		{"old", shellScript(t, "echo \"error: unknown option '--json'\" >&2; exit 1"), "unknown option '--json'", time.Minute},
		{"bad JSON", shellScript(t, "echo '[{'"), "bad JSON", time.Minute},
		{"hangs", shellScript(t, "exec sleep 30"), "did not answer within 300ms", 300 * time.Millisecond},
	} {
		outsideTimeout = c.timeout
		start := time.Now()
		_, err := listOutside(c.cli, os.Environ())
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want an error with %q", c.name, err, c.want)
		}
		if d := time.Since(start); c.timeout < time.Second && d > 3*time.Second {
			t.Errorf("%s: took %v", c.name, d)
		}
	}
}

func TestDropOwnLeavesOutIriesSessions(t *testing.T) {
	list := []ipc.OutsideSession{
		{VendorID: idleID, PID: 4301},             // an Irie session's own id, from --session-id or --resume
		{VendorID: busyID, PID: 5002},             // an Irie agent after /clear: its pid is the host's child
		{VendorID: waitingID, PID: 5003},          // started by a wrapper script under a host
		{VendorID: shellID, PID: 4304},            // truly outside
		{ID: "7c5dcf5d", Kind: "background"},      // no pid, no id of Irie's
		{VendorID: "", PID: 5001, Name: "a host"}, // the host itself
	}
	ids := map[string]bool{idleID: true}
	hosts := []int{5001}
	parents := map[int]int{5002: 5001, 5010: 5001, 5003: 5010, 4304: 1, 5001: 900}
	got := dropOwn(list, ids, hosts, parents)
	var keys []string
	for _, o := range got {
		keys = append(keys, o.Key())
	}
	if want := []string{shellID, "id:7c5dcf5d"}; !reflect.DeepEqual(keys, want) {
		t.Errorf("kept %v, want %v", keys, want)
	}
}

// testCore is a core with no tmux behind it, for what needs none.
func testCore(t *testing.T) *Core {
	t.Helper()
	p, err := paths.New(func(k string) string { return map[string]string{"HOME": "/home/alice"}[k] }, 1000)
	if err != nil {
		t.Fatal(err)
	}
	return newCore(Options{Config: config.Default(), Paths: p, Version: "test", Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, &tmux.Runner{})
}

// frames subscribes to c and returns a function that waits for the next
// frame, or returns false after d.
func frames(t *testing.T, c *Core) func(d time.Duration) (ipc.Frame, bool) {
	t.Helper()
	ch, cancel, err := c.Subscribe(nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cancel)
	return func(d time.Duration) (ipc.Frame, bool) {
		select {
		case f := <-ch:
			return f, true
		case <-time.After(d):
			return ipc.Frame{}, false
		}
	}
}

func (c *Core) setOutsideForTest(list []ipc.OutsideSession, err error) {
	c.mu.Lock()
	changed := c.setOutside(list, nil, err)
	c.mu.Unlock()
	if changed {
		c.publishOutside()
	}
}

func TestOutsideFramesComeOnlyOnAChange(t *testing.T) {
	c := testCore(t)
	next := frames(t, c)
	if f, ok := next(time.Second); !ok || f.T != ipc.FrameSnapshot {
		t.Fatalf("first frame = %+v", f)
	}
	if f, ok := next(time.Second); !ok || f.T != ipc.FrameOutside || len(f.Outside) != 0 || f.Problem != "" {
		t.Fatalf("second frame = %+v, want an empty outside frame", f)
	}
	if f, ok := next(time.Second); !ok || f.T != ipc.FrameClosed || len(f.Closed) != 0 {
		t.Fatalf("third frame = %+v, want an empty closed frame", f)
	}
	if f, ok := next(time.Second); !ok || f.T != ipc.FrameHistory || f.Undo != "" || f.Redo != "" {
		t.Fatalf("fourth frame = %+v, want an empty history frame", f)
	}
	// A first run that finds no one is no change.
	c.setOutsideForTest([]ipc.OutsideSession{}, nil)
	if f, ok := next(300 * time.Millisecond); ok {
		t.Errorf("an empty list after none sent %+v", f)
	}
	list, err := parseAgents([]byte(agentsJSON))
	if err != nil {
		t.Fatal(err)
	}
	c.setOutsideForTest(list, nil)
	if f, ok := next(time.Second); !ok || f.T != ipc.FrameOutside || !reflect.DeepEqual(f.Outside, list) {
		t.Fatalf("after a new list: %+v", f)
	}
	c.setOutsideForTest(list, nil)
	if f, ok := next(300 * time.Millisecond); ok {
		t.Errorf("the same list again sent %+v", f)
	}

	// A failed run keeps the last list and says what went wrong, once.
	fail := errors.New("claude: command not found")
	c.setOutsideForTest(nil, fail)
	f, ok := next(time.Second)
	if !ok || f.T != ipc.FrameOutside || !reflect.DeepEqual(f.Outside, list) || !strings.Contains(f.Problem, "command not found") {
		t.Fatalf("after a failed run: %+v", f)
	}
	c.setOutsideForTest(nil, fail)
	if f, ok := next(300 * time.Millisecond); ok {
		t.Errorf("the same problem again sent %+v", f)
	}
	c.setOutsideForTest(list, nil)
	if f, ok := next(time.Second); !ok || f.Problem != "" {
		t.Errorf("once it works again: %+v", f)
	}

	// A session-scoped subscription gets no outside frames.
	ch, cancel, err := c.Subscribe(json.RawMessage(`{"session":"s0k3j9x2m4a7q"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	<-ch // its snapshot
	c.setOutsideForTest(list[:1], nil)
	select {
	case f := <-ch:
		t.Errorf("a session's subscription got %+v", f)
	case <-time.After(300 * time.Millisecond):
	}
}

// TestOutsideRowsKeepTheirOrder: a status change never moves a row.
func TestOutsideRowsKeepTheirOrder(t *testing.T) {
	c := testCore(t)
	list, err := parseAgents([]byte(agentsJSON))
	if err != nil {
		t.Fatal(err)
	}
	flipped, err := parseAgents([]byte(strings.NewReplacer(`"status": "busy"`, `"status": "idle"`, `"status": "idle"`, `"status": "busy"`).Replace(agentsJSON)))
	if err != nil {
		t.Fatal(err)
	}
	keys := func(l []ipc.OutsideSession) (k []string) {
		for _, o := range l {
			k = append(k, o.Key())
		}
		return k
	}
	if flipped[0].Status != ipc.StatusBusy || flipped[1].Status != ipc.StatusIdle {
		t.Fatalf("test setup: %+v", flipped[:2])
	}
	if !reflect.DeepEqual(keys(list), keys(flipped)) {
		t.Errorf("order %v became %v after a status change", keys(list), keys(flipped))
	}
	c.setOutsideForTest(list, nil)
	c.setOutsideForTest(flipped, nil)
	c.mu.Lock()
	defer c.mu.Unlock()
	if got := keys(c.outsideList()); !reflect.DeepEqual(got, keys(list)) {
		t.Errorf("the core keeps them in order %v", got)
	}
}

// sleeper starts a stand-in for a Claude Code session outside Irie: a
// process that is reaped when it exits.
func sleeper(t *testing.T, args ...string) (*exec.Cmd, <-chan struct{}) {
	t.Helper()
	if len(args) == 0 {
		args = []string{"sleep", "600"}
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	t.Cleanup(func() {
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
	})
	return cmd, done
}

// exits reports whether a sleeper ends within a second: its reaper may
// still be closing done when its pid is already gone.
func exits(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	case <-time.After(time.Second):
		return false
	}
}

// runs reports whether a sleeper still runs.
func runs(done <-chan struct{}) bool {
	select {
	case <-done:
		return false
	default:
		return true
	}
}

func TestEndOutsideStopsTheOutsideCopy(t *testing.T) {
	// An interactive session gets SIGTERM.
	cmd, done := sleeper(t)
	o := ipc.OutsideSession{VendorID: idleID, Kind: ipc.KindInteractive, Status: ipc.StatusIdle, PID: cmd.Process.Pid}
	if err := endOutside(o, func() error { t.Error("claude stop for an interactive session"); return nil }, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if !exits(done) {
		t.Error("the interactive session still runs")
	}

	// A background session gets claude stop, and nothing else.
	cmd, done = sleeper(t)
	o = ipc.OutsideSession{VendorID: busyID, ID: "7c5dcf5d", Kind: ipc.KindBackground, Status: ipc.StatusIdle, PID: cmd.Process.Pid}
	stopped := 0
	stop := func() error { stopped++; return cmd.Process.Signal(syscall.SIGINT) }
	if err := endOutside(o, stop, 2*time.Second); err != nil || stopped != 1 || !exits(done) {
		t.Errorf("background: %v, stopped %d times", err, stopped)
	}

	// When claude stop fails, nothing else happens.
	cmd, done = sleeper(t)
	o.PID = cmd.Process.Pid
	err := endOutside(o, func() error { return errors.New("no such session") }, 2*time.Second)
	if err == nil || !strings.Contains(err.Error(), "no such session") || !runs(done) {
		t.Errorf("a failed claude stop: %v, still runs %v", err, runs(done))
	}
}

func TestEndOutsideNeverForcesAProcess(t *testing.T) {
	cmd, done := sleeper(t, "/bin/sh", "-c", "trap '' TERM; while :; do sleep 1; done")
	time.Sleep(200 * time.Millisecond) // the trap is set
	o := ipc.OutsideSession{VendorID: idleID, Name: "stubborn", Kind: ipc.KindInteractive, Status: ipc.StatusIdle, PID: cmd.Process.Pid}
	start := time.Now()
	err := endOutside(o, nil, 500*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "did not exit") || !strings.Contains(err.Error(), "nothing was moved") {
		t.Errorf("err = %v", err)
	}
	if d := time.Since(start); d < 500*time.Millisecond || d > 3*time.Second {
		t.Errorf("waited %v", d)
	}
	if !runs(done) {
		t.Error("the process was forced to exit")
	}
	// No pid, or one that names a process group or init, is never signalled.
	for _, pid := range []int{0, 1, -cmd.Process.Pid} {
		o.PID = pid
		if err := endOutside(o, nil, time.Second); err == nil {
			t.Errorf("pid %d: no error", pid)
		}
	}
	if !runs(done) {
		t.Error("a bad pid reached the process")
	}
}

// TestAResumedSessionIsReadyAndIdle: a conversation moved into Irie starts
// with SessionStart from a resume and then waits for input, so it is ready
// without a prompt and never stalls for want of one.
func TestAResumedSessionIsReadyAndIdle(t *testing.T) {
	var resumed, fresh track
	for _, x := range []*track{&resumed, &fresh} {
		x.apply(event.Event{Source: event.SourceIrie, Kind: event.SpawnRequested, TS: event.Stamp(time.Now())})
	}
	resumed.apply(event.Event{Source: event.SourceHook, Kind: event.SessionStarted, Data: map[string]any{"source": "resume"}})
	fresh.apply(event.Event{Source: event.SourceHook, Kind: event.SessionStarted, Data: map[string]any{"source": "startup"}})
	if !resumed.ready() || resumed.awaiting() || resumed.state(false) != ipc.StateIdle {
		t.Errorf("resumed: ready %v, awaiting %v, state %s", resumed.ready(), resumed.awaiting(), resumed.state(false))
	}
	if fresh.ready() || !fresh.awaiting() || fresh.state(false) != ipc.StateStarting {
		t.Errorf("a new session without its prompt: ready %v, state %s", fresh.ready(), fresh.state(false))
	}
	// Its next prompt makes it work as usual.
	resumed.apply(event.Event{Source: event.SourceHook, Kind: event.PromptSubmitted, Agent: event.Main})
	if resumed.state(false) != ipc.StateWorking {
		t.Errorf("after a prompt: %s", resumed.state(false))
	}
}

// TestAutoListsWithTheStrongestProfile: auto, the default profile since
// M10, is no profile of its own, so the sessions outside Irie are listed,
// and moved in, with the profile it keeps hard work on.
func TestAutoListsWithTheStrongestProfile(t *testing.T) {
	c := testCore(t)
	if c.cfg().Spawn.DefaultProfile != config.Auto {
		t.Fatalf("default profile %q", c.cfg().Spawn.DefaultProfile)
	}
	if name, p, ok := c.claudeProfile(); !ok || name != "claude" || p.Model != "claude-opus-5-5" {
		t.Errorf("claudeProfile = %q, %+v, %v; want claude", name, p, ok)
	}
	c.cfg().Route.Profiles = []string{"local", "haiku", "sonnet"}
	if name, _, _ := c.claudeProfile(); name != "sonnet" {
		t.Errorf("with sonnet the strongest: claudeProfile = %q", name)
	}
	c.cfg().Spawn.DefaultProfile = "haiku"
	if name, _, _ := c.claudeProfile(); name != "haiku" {
		t.Errorf("with haiku the default: claudeProfile = %q", name)
	}
}
