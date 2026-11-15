package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/testutil"
)

// TestAPanesShellRunsInTheSessionsFolderAndEndsWithIt walks a pane's own
// shell through its life (docs/proposals/workspaces.md §4): it starts only
// when asked, in the session's worktree on the dock server, keeps its last
// screen when it exits, restarts, ends, and ends with its session.
func TestAPanesShellRunsInTheSessionsFolderAndEndsWithIt(t *testing.T) {
	// A plain shell, not the user's, which may ask questions on a first run,
	// and not a login shell, which would read the user's own profile: the
	// core starts "$SHELL -l", and this one drops the -l.
	plain := filepath.Join(t.TempDir(), "plain-sh")
	if err := os.WriteFile(plain, []byte("#!/bin/sh\nexec /bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", plain)
	testutil.IsolateGit(t)
	repo := website(t, filepath.Join(t.TempDir(), "website"))
	c := setupWith(t, agentConfig{})
	s := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,stop", Repo: repo})
	c.waitState(s.ID, ipc.StateDone)
	if s.Worktree == "" {
		t.Fatal("the session got no worktree")
	}
	pane := func(a ipc.PaneArgs) ipc.PaneResult {
		t.Helper()
		var r ipc.PaneResult
		a.Session = s.ID
		if err := c.call(ipc.OpPane, a, &r); err != nil {
			t.Fatalf("pane %+v: %v", a, err)
		}
		return r
	}
	dock := func(args ...string) error {
		return exec.Command("tmux", append([]string{"-L", c.Socket + "-dock"}, args...)...).Run()
	}

	// An id that is not one is refused, and nothing runs for it.
	var r ipc.PaneResult
	if err := c.call(ipc.OpPane, ipc.PaneArgs{Session: s.ID, Pane: "../x", Start: true}, &r); err == nil {
		t.Fatal("a pane id with a path in it was taken")
	}
	if err := c.call(ipc.OpPane, ipc.PaneArgs{Session: "nope", Pane: "p1", Start: true}, &r); err == nil {
		t.Fatal("a pane of no session started")
	}

	// Nothing runs until the client asks.
	if r = pane(ipc.PaneArgs{Pane: "p1"}); r.State != ipc.DockStateStopped || r.Target != "" {
		t.Fatalf("before start: %+v, want it stopped with no target", r)
	}
	r = pane(ipc.PaneArgs{Pane: "p1", Start: true})
	if r.State != ipc.DockStateRunning || r.Target == "" || r.Socket == "" || r.Tmux == "" {
		t.Fatalf("after start: %+v, want it running with a target to attach to", r)
	}
	target := r.Target
	wantDir, _ := filepath.EvalSymlinks(s.Worktree)
	if got, _ := filepath.EvalSymlinks(r.Cwd); got != wantDir {
		t.Fatalf("cwd = %q, want the session's worktree %q", r.Cwd, s.Worktree)
	}
	// A second start keeps the shell that runs.
	if again := pane(ipc.PaneArgs{Pane: "p1", Start: true}); again.Target != target || again.State != ipc.DockStateRunning {
		t.Fatalf("a second start gave %+v, want the same running shell %q", again, target)
	}

	// It is a shell in the worktree: what is typed runs there.
	where := filepath.Join(s.Worktree, "pane-where.txt")
	if err := dock("send-keys", "-t", "="+target+":", "pwd > pane-where.txt", "Enter"); err != nil {
		t.Fatal(err)
	}
	testutil.Eventually(t, 10*time.Second, "the shell writes where it runs", func() bool {
		b, err := os.ReadFile(where)
		return err == nil && strings.TrimSpace(string(b)) != ""
	})
	got, _ := os.ReadFile(where)
	if g, _ := filepath.EvalSymlinks(strings.TrimSpace(string(got))); g != wantDir {
		t.Fatalf("the shell ran in %q, want the session's worktree %q", got, wantDir)
	}

	// The shell exits: its last screen stays, with its code, until it
	// restarts.
	if err := dock("send-keys", "-t", "="+target+":", "exit 3", "Enter"); err != nil {
		t.Fatal(err)
	}
	testutil.Eventually(t, 10*time.Second, "the pane says its shell ended", func() bool {
		r = pane(ipc.PaneArgs{Pane: "p1"})
		return r.State == ipc.DockStateExited
	})
	if r.Exit == nil || *r.Exit != 3 || r.Target != target {
		t.Fatalf("after exit: %+v, want code 3 and the same target", r)
	}
	if r = pane(ipc.PaneArgs{Pane: "p1", Start: true}); r.State != ipc.DockStateExited {
		t.Fatalf("a start over an ended shell gave %+v, want it left for a restart", r)
	}
	if r = pane(ipc.PaneArgs{Pane: "p1", Restart: true}); r.State != ipc.DockStateRunning {
		t.Fatalf("after restart: %+v, want it running", r)
	}

	// End ends it; a second pane runs beside it and ends with the session.
	if r = pane(ipc.PaneArgs{Pane: "p1", End: true}); r.State != ipc.DockStateStopped || r.Target != "" {
		t.Fatalf("after end: %+v, want it stopped", r)
	}
	second := pane(ipc.PaneArgs{Pane: "p2", Start: true}).Target
	if second == "" || second == target {
		t.Fatalf("the second pane's target is %q, want one of its own beside %q", second, target)
	}
	if err := c.call(ipc.OpClose, ipc.CloseArgs{Session: s.ID}, nil); err != nil {
		t.Fatal(err)
	}
	testutil.Eventually(t, 10*time.Second, "the pane's shell ends with its session", func() bool {
		return dock("has-session", "-t", "="+second) != nil
	})
}

func TestPaneSessionNamesHoldOnlyWhatTmuxTakes(t *testing.T) {
	if got := paneSession("Asahi/s3x.y:z", "p1"); got != "pane-Asahis3xyz-p1" {
		t.Fatalf("got %q", got)
	}
	for _, id := range []string{"", "P1", "a-b", strings.Repeat("a", 25), "a/b"} {
		if paneID.MatchString(id) {
			t.Errorf("%q passed as a pane id", id)
		}
	}
}

// TestAShellBesideAPeersSessionRunsThere: a pane beside a session on a
// peer is ssh to that machine and a login shell there, in the session's
// folder quoted as one word; with no ssh name for the peer it says what to
// set, and the Dock's sweep counts the peer's rows as live.
func TestAShellBesideAPeersSessionRunsThere(t *testing.T) {
	want := []string{"ssh", "-t", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3", "--", "asahi",
		`cd '/home/alice/code/it'\''s here' 2>/dev/null; exec "$SHELL" -l`}
	if got := remoteShell("asahi", "/home/alice/code/it's here"); strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("remoteShell = %q, want %q", got, want)
	}
	if got := remoteShell("asahi", ""); got[len(got)-1] != `exec "$SHELL" -l` {
		t.Fatalf("with no folder it runs %q, want the login shell at home", got[len(got)-1])
	}

	c := newPeerCore(t)
	p := &peerState{name: "Asahi", up: true, stop: func() {}, rows: map[string]ipc.Session{
		"s1": {ID: "s1", Name: "far", State: ipc.StateWorking, Cwd: "/home/alice/far"},
	}}
	c.peers = map[string]*peerState{"Asahi": p}
	_, err := c.paneOp(ipc.PaneArgs{Session: "Asahi/s1", Pane: "p1"})
	if err == nil || !strings.Contains(err.Error(), "set peers.Asahi.ssh") {
		t.Fatalf("a shell beside a peer with no ssh name: %v, want it to say to set peers.Asahi.ssh", err)
	}
	if _, err := c.paneOp(ipc.PaneArgs{Session: "Asahi/nope", Pane: "p1"}); err == nil {
		t.Fatal("a pane beside a row the peer doesn't have was taken")
	}
}
