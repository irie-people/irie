package core

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/irie-hq/irie/internal/event"
	"github.com/irie-hq/irie/internal/host"
	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/testutil"
	"github.com/irie-hq/irie/internal/tmux"
)

func (c *coreEnv) closeSession(id string) {
	c.t.Helper()
	if err := c.call(ipc.OpClose, ipc.CloseArgs{Session: id}, nil); err != nil {
		c.t.Fatalf("close %s: %v", id, err)
	}
}

func (c *coreEnv) reopen(id string) (ipc.Session, error) {
	var r ipc.ReopenResult
	err := c.call(ipc.OpReopen, ipc.ReopenArgs{Session: id}, &r)
	return r.Session, err
}

// signals lists the signals the fake agent of a session logged.
func (c *coreEnv) signals(id string) []string {
	var out []string
	for _, l := range c.agentLog(id) {
		if l["t"] == "signal" {
			out = append(out, fmt.Sprint(l["signal"]))
		}
	}
	return out
}

// run is one run of a session's fake agent, from its log.
type run struct {
	argv      []string
	sessionID string
}

// runs lists the runs of a session's fake agent.
func (c *coreEnv) runs(id string) []run {
	var out []run
	for _, l := range c.agentLog(id) {
		if l["t"] != "start" {
			continue
		}
		r := run{sessionID: fmt.Sprint(l["session_id"])}
		for _, a := range l["argv"].([]any) {
			r.argv = append(r.argv, fmt.Sprint(a))
		}
		out = append(out, r)
	}
	return out
}

func (c *coreEnv) waitGone(pane string, d time.Duration) {
	c.t.Helper()
	testutil.Eventually(c.t, d, "the closed session's pane is still there", func() bool { return c.pane(pane) == nil })
}

func TestCloseSwapsThePaneOutAndEndsTheAgentGently(t *testing.T) {
	c := setup(t)
	a, b := c.spawn("prompt"), c.spawn("prompt")
	c.waitState(a.ID, ipc.StateWorking)
	c.waitState(b.ID, ipc.StateWorking)
	c.show(a.ID, false)
	if p := c.slot(); p["pane_id"] != a.Pane {
		t.Fatalf("the slot holds %v, want %s", p, a.Pane)
	}

	start := time.Now()
	c.closeSession(a.ID)
	// At once: the placeholder is back in the slot, and only the row goes.
	if p := c.slot(); p["@irie_role"] != tmux.RolePlaceholder {
		t.Errorf("after close the slot holds %v", p)
	}
	if ls := c.list(); len(ls) != 1 || ls[0].ID != b.ID {
		t.Errorf("rows after close = %+v", ls)
	}
	// SIGTERM ends it well before anything would be killed, and its window
	// goes with it.
	c.waitGone(a.Pane, 3*time.Second)
	if d := time.Since(start); d >= closeWait {
		t.Errorf("the window went after %v", d)
	}
	if sig := c.signals(a.ID); !slices.Equal(sig, []string{"terminated"}) {
		t.Errorf("the agent got %q, want SIGTERM alone", sig)
	}
	if log := c.agentLog(a.ID); log[len(log)-1]["t"] != "exit" {
		t.Errorf("the agent did not end its session: %v", log[len(log)-1])
	}
	// Its folder stays, and it can be reopened.
	if fi, err := os.Stat(a.Cwd); err != nil || !fi.IsDir() {
		t.Errorf("the folder went: %v", err)
	}
	r := c.listAll()
	if len(r.Closed) != 1 || r.Closed[0].ID != a.ID || r.Closed[0].Name != a.Name || r.Closed[0].Closed.Before(start.Add(-time.Second)) {
		t.Errorf("closed = %+v", r.Closed)
	}
	evs := c.events(a.ID)
	if e := evs[len(evs)-1]; e.Kind != event.UIClosed || e.Source != event.SourceUser || e.Str("vendor_id") != a.VendorID || e.Data["resume"] != true {
		t.Errorf("the last event = %+v", e)
	}

	// The other session is untouched.
	if r, ok := c.row(b.ID); !ok || r.State != ipc.StateWorking || c.pane(b.Pane) == nil {
		t.Errorf("the other session = %+v", r)
	}
	if err := c.call(ipc.OpClose, ipc.CloseArgs{Session: a.ID}, nil); code(err) != ipc.CodeNotFound {
		t.Errorf("closing it again: %v", err)
	}
}

func TestCloseKillsAnAgentThatIgnoresSIGTERM(t *testing.T) {
	c := setupWith(t, agentConfig{env: map[string]string{"IRIE_FAKE_IGNORE_TERM": "1"}})
	a := c.spawn("prompt")
	c.waitState(a.ID, ipc.StateWorking)
	pid := int(c.agentLog(a.ID)[0]["pid"].(float64))
	c.show(a.ID, true)

	start := time.Now()
	c.closeSession(a.ID)
	// The row is gone while the agent ends, alone in its own window.
	if _, ok := c.row(a.ID); ok {
		t.Error("the row is still listed")
	}
	if p := c.pane(a.Pane); p == nil || p["@irie_role"] != tmux.RoleClosing || p["session_name"] != tmux.AgentsSession || p["window_panes"] != "1" {
		t.Errorf("the closing pane = %v", p)
	}
	if p := c.slot(); p["@irie_role"] != tmux.RolePlaceholder {
		t.Errorf("the slot holds %v", p)
	}
	time.Sleep(time.Second)
	if c.pane(a.Pane) == nil || !alive(pid) {
		t.Fatal("the agent was killed without its time to exit")
	}

	c.waitGone(a.Pane, closeWait+5*time.Second)
	if d := time.Since(start); d < closeWait {
		t.Errorf("killed after %v, before %v", d, closeWait)
	}
	testutil.Eventually(t, 2*time.Second, "the agent still runs", func() bool { return !alive(pid) })
	if sig := c.signals(a.ID); !slices.Equal(sig, []string{"terminated"}) {
		t.Errorf("the agent got %q, want SIGTERM", sig)
	}
}

func TestReopenCarriesTheConversationOn(t *testing.T) {
	testutil.IsolateGit(t)
	repo := website(t, filepath.Join(t.TempDir(), "website"))
	c := setupWith(t, agentConfig{readyTimeout: "1s"})
	a := c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Repo: repo})
	later := c.spawn("prompt")
	c.waitState(a.ID, ipc.StateWorking)
	c.closeSession(a.ID)
	c.waitGone(a.Pane, 3*time.Second)
	// The worktree and its branch stay.
	if fi, err := os.Stat(a.Worktree); err != nil || !fi.IsDir() {
		t.Fatalf("the worktree went: %v", err)
	}
	testutil.Git(t, repo, "rev-parse", "--verify", "refs/heads/"+a.Branch)

	// A restarted core still knows what was closed.
	c.restartCore()
	if r := c.listAll(); len(r.Closed) != 1 || r.Closed[0].ID != a.ID {
		t.Fatalf("closed after a restart = %+v", r.Closed)
	}
	s, err := c.reopen("")
	if err != nil {
		t.Fatal(err)
	}
	// The same row, in its place: id, name, age, folder, worktree and
	// conversation.
	if s.ID != a.ID || s.Name != a.Name || !s.Created.Equal(a.Created) || s.Cwd != a.Cwd || s.Worktree != a.Worktree || s.Branch != a.Branch || s.VendorID != a.VendorID || s.Profile != a.Profile {
		t.Errorf("reopened %+v, closed %+v", s, a)
	}
	if ls := c.list(); len(ls) != 2 || ls[0].ID != a.ID || ls[1].ID != later.ID {
		t.Errorf("rows = %+v, want the reopened one first again", ls)
	}

	// claude --resume <vendor id>, with the profile's flags and Irie's
	// settings, in the same folder.
	spec, err := host.ReadSpec(c.paths.SpecFile(a.ID))
	if err != nil {
		t.Fatal(err)
	}
	argv := spec.Argv
	if n := len(argv); n < 2 || argv[n-2] != "--resume" || argv[n-1] != a.VendorID || slices.Contains(argv, "--session-id") || slices.Contains(argv, "prompt") {
		t.Errorf("argv = %q", argv)
	}
	for _, f := range []string{"--model", "--effort", "--permission-mode", "--settings"} {
		if !slices.Contains(argv, f) {
			t.Errorf("argv %q has no %s", argv, f)
		}
	}
	if spec.Cwd != a.Cwd {
		t.Errorf("cwd = %q, want %q", spec.Cwd, a.Cwd)
	}
	c.waitState(a.ID, ipc.StateIdle)
	if runs := c.runs(a.ID); len(runs) != 2 || !slices.Contains(runs[1].argv, "--resume") || runs[1].sessionID != a.VendorID {
		t.Errorf("the agent's runs = %+v, want the second to resume %s", runs, a.VendorID)
	}
	// Ready on its resume, so the ready timeout never makes it stalled.
	time.Sleep(1500 * time.Millisecond)
	if r, _ := c.row(a.ID); r.State != ipc.StateIdle {
		t.Errorf("after the ready timeout: %+v", r)
	}

	var got []string
	for _, e := range c.events(a.ID) {
		if strings.HasPrefix(e.Kind, "ui.") && e.Kind != event.UIShown || e.Kind == event.SpawnRequested {
			got = append(got, e.Kind)
		}
	}
	if want := []string{event.SpawnRequested, event.UIClosed, event.SpawnRequested, event.UIReopened}; !slices.Equal(got, want) {
		t.Errorf("journal = %v, want %v", got, want)
	}
	if r := c.listAll(); len(r.Closed) != 0 {
		t.Errorf("closed after the reopen = %+v", r.Closed)
	}
	if _, err := c.reopen(""); code(err) != ipc.CodeNotFound {
		t.Errorf("reopen with nothing closed: %v", err)
	}
	if _, err := c.reopen(a.ID); code(err) != ipc.CodeBadArgs {
		t.Errorf("reopen of an open session: %v", err)
	}
}

func TestReopenStartsATaskThatNeverStartedAgain(t *testing.T) {
	// Its first run hangs before Claude Code would start a conversation, as
	// at a trust prompt; later runs are the fake agent.
	fake := testutil.BuildFakeAgent(t)
	dir := t.TempDir()
	script := filepath.Join(dir, "claude")
	body := fmt.Sprintf(`#!/bin/sh
case "$1" in --version|--help) exec %[1]q "$@" ;; esac
if [ ! -e %[2]q ]; then : > %[2]q; exec sleep 600; fi
exec %[1]q "$@"
`, fake, filepath.Join(dir, "ran"))
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	c := setupWith(t, agentConfig{command: script})
	const task = "prompt\nthe rest of the task"
	a := c.spawn(task)
	testutil.Eventually(t, 5*time.Second, "the first run did not start", func() bool {
		_, err := os.Stat(filepath.Join(dir, "ran"))
		return err == nil
	})
	if r, _ := c.row(a.ID); r.State != ipc.StateStarting {
		t.Fatalf("before close: %+v", r)
	}
	c.closeSession(a.ID)

	s, err := c.reopen(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	// There was no conversation to carry on, so the task starts again under a
	// new id.
	if s.ID != a.ID || s.Name != a.Name || s.VendorID == a.VendorID || !uuidV4.MatchString(s.VendorID) {
		t.Errorf("reopened %+v, closed %+v", s, a)
	}
	spec, err := host.ReadSpec(c.paths.SpecFile(a.ID))
	if err != nil {
		t.Fatal(err)
	}
	argv := spec.Argv
	if i := slices.Index(argv, "--session-id"); i < 0 || argv[i+1] != s.VendorID || argv[len(argv)-1] != task || slices.Contains(argv, "--resume") {
		t.Errorf("argv = %q", argv)
	}
	c.waitState(a.ID, ipc.StateWorking)
	if runs := c.runs(a.ID); len(runs) != 1 || runs[0].argv[len(runs[0].argv)-1] != task || runs[0].sessionID != s.VendorID {
		t.Errorf("the fake agent ran %+v", runs)
	}
	for _, e := range c.events(a.ID) {
		if e.Kind == event.SpawnRequested && e.Data["reopen"] == true && (e.Str("resume") != "" || e.Data["task_bytes"] != float64(len(task))) {
			t.Errorf("spawn.requested of the reopen = %+v", e.Data)
		}
	}
}

func TestClosingAFrozenSessionEndsItGently(t *testing.T) {
	// Frozen the way the core freezes a session, host first, then the
	// agent's group: close continues them, so the agent still gets its
	// SIGTERM, and well within the 6 s of DESIGN M6.
	c := setup(t)
	a := c.spawn("prompt")
	c.waitState(a.ID, ipc.StateWorking)
	pid := int(c.agentLog(a.ID)[0]["pid"].(float64))
	hostPID, err := strconv.Atoi(c.Tmux(t, "display-message", "-p", "-t", a.Pane, "#{pane_pid}"))
	if err != nil {
		t.Fatal(err)
	}
	// The host first, or it would continue the agent at once.
	for _, p := range []int{hostPID, -pid} {
		if err := syscall.Kill(p, syscall.SIGSTOP); err != nil {
			t.Fatalf("SIGSTOP %d: %v", p, err)
		}
	}
	start := time.Now()
	c.closeSession(a.ID)
	c.waitGone(a.Pane, closeWait+5*time.Second)
	if d := time.Since(start); d >= closeWait {
		t.Errorf("it took %v to end, so it was killed", d)
	}
	testutil.Eventually(t, 2*time.Second, "the agent still runs", func() bool { return !alive(pid) })
	if sig := c.signals(a.ID); !slices.Equal(sig, []string{"terminated"}) {
		t.Errorf("the agent got %q, want SIGTERM", sig)
	}
}
