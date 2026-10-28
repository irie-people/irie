package core

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/testutil"
)

// fakeClaude is a `claude` for the tests of sessions outside Irie. `agents
// --json` prints a warning on stderr, then its list; `stop <id>` records the
// id and ends the process that <id>.pid names; anything else runs the fake
// agent. The sessions it lists run as sleeps.
type fakeClaude struct {
	t      *testing.T
	script string
	dir    string
	list   []map[string]any
}

func newFakeClaude(t *testing.T) *fakeClaude {
	t.Helper()
	fake := testutil.BuildFakeAgent(t)
	dir := t.TempDir()
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
	body := `#!/bin/sh
case "$1" in
agents)
	echo 'warning: a notice on stderr' >&2
	cat ` + q(filepath.Join(dir, "agents.json")) + `
	;;
stop)
	echo "$2" >> ` + q(filepath.Join(dir, "stops")) + `
	kill "$(cat ` + q(dir) + `/"$2".pid)"
	;;
*)
	exec ` + q(fake) + ` "$@"
	;;
esac
`
	f := &fakeClaude{t: t, script: filepath.Join(dir, "claude"), dir: dir}
	if err := os.WriteFile(f.script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	f.write()
	return f
}

// write puts the list where `agents --json` reads it, in one rename.
func (f *fakeClaude) write() {
	f.t.Helper()
	b, err := json.Marshal(f.list)
	if f.list == nil {
		b, err = []byte("[]"), nil
	}
	if err != nil {
		f.t.Fatal(err)
	}
	tmp := filepath.Join(f.dir, "agents.json.tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		f.t.Fatal(err)
	}
	if err := os.Rename(tmp, filepath.Join(f.dir, "agents.json")); err != nil {
		f.t.Fatal(err)
	}
}

// listed adds an entry to the list as it is.
func (f *fakeClaude) listed(e map[string]any) {
	f.t.Helper()
	if _, ok := e["startedAt"]; !ok {
		e["startedAt"] = time.Now().UnixMilli() + int64(len(f.list))
	}
	f.list = append(f.list, e)
	f.write()
}

// add lists a session outside Irie whose process is a sleep, and returns
// what tells when that sleep exits.
func (f *fakeClaude) add(kind, status, id, name, cwd string) <-chan struct{} {
	f.t.Helper()
	cmd, done := sleeper(f.t)
	e := map[string]any{"pid": cmd.Process.Pid, "cwd": cwd, "kind": kind, "sessionId": id, "name": name, "status": status}
	if kind == ipc.KindBackground {
		short := id[:8]
		e["id"], e["state"] = short, "done"
		if err := os.WriteFile(filepath.Join(f.dir, short+".pid"), []byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
			f.t.Fatal(err)
		}
	}
	f.listed(e)
	return done
}

// stops returns the ids `claude stop` was given.
func (f *fakeClaude) stops() []string {
	b, _ := os.ReadFile(filepath.Join(f.dir, "stops"))
	return strings.Fields(string(b))
}

func (c *coreEnv) listAll() ipc.ListResult {
	c.t.Helper()
	var r ipc.ListResult
	if err := c.call(ipc.OpList, nil, &r); err != nil {
		c.t.Fatalf("list: %v", err)
	}
	return r
}

// waitOutside waits until the outside list satisfies ok, and returns it.
func (c *coreEnv) waitOutside(what string, ok func([]ipc.OutsideSession) bool) ipc.ListResult {
	c.t.Helper()
	var r ipc.ListResult
	testutil.Eventually(c.t, 15*time.Second, what, func() bool {
		r = c.listAll()
		return ok(r.Outside)
	})
	return r
}

func (c *coreEnv) move(vendorID string) (ipc.Session, error) {
	var r ipc.MoveResult
	err := c.call(ipc.OpMove, ipc.MoveArgs{VendorID: vendorID}, &r)
	return r.Session, err
}

func code(err error) string {
	var e *ipc.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestOutsideSessionsAreListedApartFromIriesRows(t *testing.T) {
	f := newFakeClaude(t)
	c := setupWith(t, agentConfig{command: f.script})
	cl, err := ipc.Dial(c.paths.Socket(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	frames, err := cl.Subscribe(ipc.SubscribeArgs{})
	if err != nil {
		t.Fatal(err)
	}

	a := c.spawn("prompt")
	c.waitState(a.ID, ipc.StateWorking)
	log := c.agentLog(a.ID)
	agentPID, _ := log[0]["pid"].(float64)
	if agentPID == 0 {
		t.Fatalf("the agent logged no pid: %v", log[0])
	}
	dir := t.TempDir()
	// Claude Code lists Irie's own sessions too: by the id Irie passed with
	// --session-id, and after /clear by a new id, from the agent's process.
	f.listed(map[string]any{"pid": agentPID, "cwd": a.Cwd, "kind": "interactive", "sessionId": a.VendorID, "name": a.Name, "status": "busy"})
	f.listed(map[string]any{"pid": agentPID, "cwd": a.Cwd, "kind": "interactive", "sessionId": "99999999-8888-4777-8666-555555555555", "name": "cleared", "status": "idle"})
	f.add(ipc.KindInteractive, ipc.StatusIdle, idleID, "outside-task", dir)
	f.add(ipc.KindBackground, ipc.StatusBusy, busyID, "flaky-test-fix", dir)

	r := c.waitOutside("the sessions outside Irie were not listed", func(o []ipc.OutsideSession) bool { return len(o) >= 2 })
	if len(r.Outside) != 2 || r.Outside[0].VendorID != idleID || r.Outside[1].VendorID != busyID {
		t.Fatalf("outside = %+v", r.Outside)
	}
	if o := r.Outside[0]; !o.Movable || o.Name != "outside-task" || o.Kind != ipc.KindInteractive || o.Cwd != dir || o.PID == 0 {
		t.Errorf("the idle session = %+v", o)
	}
	if o := r.Outside[1]; o.Movable || o.Status != ipc.StatusBusy || o.ID != busyID[:8] || !strings.Contains(o.Why, "busy") {
		t.Errorf("the busy session = %+v", o)
	}
	if len(r.Sessions) != 1 || r.Sessions[0].ID != a.ID {
		t.Errorf("Irie's rows = %+v", r.Sessions)
	}
	if r.OutsideProblem != "" {
		t.Errorf("problem %q: stderr is not the output", r.OutsideProblem)
	}

	// Subscribers heard of them in an outside frame of their own.
	deadline := time.After(5 * time.Second)
	for heard := false; !heard; {
		select {
		case fr := <-frames:
			heard = fr.T == ipc.FrameOutside && len(fr.Outside) == 2
			if fr.T == ipc.FrameSnapshot && len(fr.Sessions) > 1 {
				t.Errorf("a snapshot carries outside sessions: %+v", fr.Sessions)
			}
		case <-deadline:
			t.Fatal("no outside frame with both sessions")
		}
	}

	// irie ctl list shows them under their own heading.
	out, err := exec.Command(c.bin, "ctl", "list").CombinedOutput()
	text := string(out)
	head := strings.Index(text, "OUTSIDE IRIE")
	if err != nil || head < 0 || strings.Index(text, a.ID) > head || !strings.Contains(text[head:], idleID) ||
		!strings.Contains(text[head:], "outside-task") || !strings.Contains(text[head:], "flaky-test-fix") {
		t.Errorf("ctl list: %v\n%s", err, out)
	}

	// Outside sessions never reach the journal.
	for _, e := range c.journal() {
		if e.Session != a.ID {
			t.Errorf("journal event of another session: %+v", e)
		}
	}
}

func TestMoveRefusesWhatItCannotTake(t *testing.T) {
	f := newFakeClaude(t)
	c := setupWith(t, agentConfig{command: f.script})
	dir := t.TempDir()
	busy := f.add(ipc.KindInteractive, ipc.StatusBusy, busyID, "busy-task", dir)
	asks, asking := sleeper(t)
	f.listed(map[string]any{"pid": asks.Process.Pid, "cwd": dir, "kind": "interactive", "sessionId": waitingID, "name": "asks", "status": "waiting", "waitingFor": "permission prompt"})
	a := c.spawn("prompt")
	c.waitState(a.ID, ipc.StateWorking)

	for _, x := range []struct {
		id, code, msg string
	}{
		{busyID, ipc.CodeBusy, "busy"},
		{waitingID, ipc.CodeBusy, "permission prompt"},
		{"12345678-1234-4234-8234-123456789012", ipc.CodeNotFound, "no Claude Code session"},
		{a.VendorID, ipc.CodeBadArgs, "already runs in Irie"},
		{"", ipc.CodeBadArgs, "vendor_id"},
	} {
		_, err := c.move(x.id)
		if code(err) != x.code || !strings.Contains(err.Error(), x.msg) {
			t.Errorf("move %q: %v (code %q), want %s with %q", x.id, err, code(err), x.code, x.msg)
		}
	}
	var r ipc.MoveResult
	if err := c.call(ipc.OpMove, ipc.MoveArgs{VendorID: busyID, Profile: "nope"}, &r); code(err) != ipc.CodeNotFound {
		t.Errorf("move with an unknown profile: %v", err)
	}
	if !runs(busy) || !runs(asking) {
		t.Error("a refused move stopped a session")
	}
	if ls := c.list(); len(ls) != 1 {
		t.Errorf("a refused move spawned: %+v", ls)
	}
}

func TestMoveCarriesAnIdleSessionOn(t *testing.T) {
	for _, kind := range []string{ipc.KindInteractive, ipc.KindBackground} {
		t.Run(kind, func(t *testing.T) {
			f := newFakeClaude(t)
			c := setupWith(t, agentConfig{command: f.script, readyTimeout: "2s"})
			dir := t.TempDir()
			done := f.add(kind, ipc.StatusIdle, idleID, "outside-task", dir)
			c.waitOutside("the idle session was not listed", func(o []ipc.OutsideSession) bool { return len(o) == 1 && o[0].Movable })

			var s ipc.Session
			if kind == ipc.KindInteractive {
				// By name, as a script would.
				out, err := exec.Command(c.bin, "ctl", "move", "--wait", "outside-task").CombinedOutput()
				if err != nil {
					t.Fatalf("ctl move: %v\n%s", err, out)
				}
				id := strings.TrimSpace(string(out))
				r, ok := c.row(id)
				if !ok {
					t.Fatalf("ctl move printed %q", out)
				}
				s = r
			} else {
				var err error
				if s, err = c.move(idleID); err != nil {
					t.Fatal(err)
				}
				if got := f.stops(); !slices.Equal(got, []string{idleID[:8]}) {
					t.Errorf("claude stop got %q, want %s", got, idleID[:8])
				}
			}
			if !exits(done) {
				t.Fatal("the outside copy still runs")
			}
			if s.Name != "outside-task" || s.Cwd != dir || s.VendorID != idleID || s.Worktree != "" {
				t.Errorf("moved session = %+v", s)
			}

			// It runs in place and resumes: no --session-id and no task.
			spec, err := host.ReadSpec(c.paths.SpecFile(s.ID))
			if err != nil {
				t.Fatal(err)
			}
			argv := spec.Argv
			if n := len(argv); n < 2 || argv[n-2] != "--resume" || argv[n-1] != idleID || slices.Contains(argv, "--session-id") || !slices.Contains(argv, "--settings") {
				t.Errorf("argv = %q", argv)
			}
			if spec.Cwd != dir {
				t.Errorf("cwd = %q, want %q", spec.Cwd, dir)
			}

			// Ready on its resume, idle, and never stalled for want of a prompt.
			c.waitState(s.ID, ipc.StateIdle)
			time.Sleep(2500 * time.Millisecond)
			if r, _ := c.row(s.ID); r.State != ipc.StateIdle {
				t.Errorf("after the ready timeout: %+v", r)
			}
			evs := c.events(s.ID)
			if len(evs) == 0 || evs[0].Kind != event.SpawnRequested || evs[0].Str("resume") != idleID || evs[0].Str("kind") != kind {
				t.Errorf("journal = %v, want spawn.requested with resume %s", kinds(evs), idleID)
			}
			for _, e := range evs {
				if e.Kind == event.InputNeeded {
					t.Errorf("the moved session needed something: %+v", e)
				}
				if e.Kind == event.SessionStarted && e.Str("source") != "resume" {
					t.Errorf("session.started = %+v", e)
				}
			}
			if log := c.agentLog(s.ID); len(log) == 0 || log[0]["session_id"] != idleID {
				t.Errorf("the agent did not resume %s: %v", idleID, log)
			}

			// It left the outside list, though Claude Code still lists it.
			if r := c.listAll(); len(r.Outside) != 0 {
				t.Errorf("outside after the move = %+v", r.Outside)
			}
		})
	}
}

// TestAgentsDoNotInheritTheStartersIdentity starts the server from inside
// another Claude Code session in a terminal pane, as `irie up` run from
// such a pane does. Its agents must count as sessions of their own.
func TestAgentsDoNotInheritTheStartersIdentity(t *testing.T) {
	marks := map[string]string{
		"CLAUDECODE":                "1",
		"CLAUDE_CODE_CHILD_SESSION": "1",
		"CLAUDE_CODE_SESSION_ID":    "6f1c2b4e-8a9d-4c3b-9e7f-0a1b2c3d4e5f",
		"IRIETEST_PANEL_ID":         "panel-7",
	}
	for k, v := range marks {
		t.Setenv(k, v)
	}
	t.Setenv("IRIE_TEST_KEEP", "1")
	c := setup(t)
	if got := c.Tmux(t, "show-environment", "-g", "CLAUDECODE"); got != "CLAUDECODE=1" {
		t.Fatalf("test setup: the server started without the mark: %q", got)
	}
	s := c.spawn("prompt")
	c.waitState(s.ID, ipc.StateWorking)
	log := c.agentLog(s.ID)
	var names []string
	for _, n := range log[0]["env_names"].([]any) {
		names = append(names, n.(string))
	}
	for k := range marks {
		if slices.Contains(names, k) {
			t.Errorf("the agent inherits %s", k)
		}
	}
	if !slices.Contains(names, "IRIE_TEST_KEEP") || !slices.Contains(names, "IRIE_SESSION") {
		t.Errorf("the agent lost its other variables: %v", names)
	}
}
