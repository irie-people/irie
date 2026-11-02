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

// TestTheDockRunsTrustedControlsInTheSessionsWorktree walks a repo's Dock
// file from untrusted to trusted, running, stopped and changed, and checks
// that a copy runs in the session's own worktree and ends with the session
// (docs/proposals/right-dock.md §5.2).
func TestTheDockRunsTrustedControlsInTheSessionsWorktree(t *testing.T) {
	testutil.IsolateGit(t)
	repo := website(t, filepath.Join(t.TempDir(), "website"))
	testutil.WriteFiles(t, repo, map[string]string{".irie/dock.json": `{"controls":[
		{"id":"where","title":"Where","command":"pwd > dock-where.txt; exec sleep 120"},
		{"id":"page","title":"Page","type":"browser","url":"http://127.0.0.1:9/","chrome":false}]}`})
	testutil.Git(t, repo, "add", ".irie/dock.json")
	testutil.Git(t, repo, "commit", "-q", "-m", "Add a Dock")
	c := setupWith(t, agentConfig{})
	s := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,stop", Repo: repo})
	c.waitState(s.ID, ipc.StateDone)
	if s.Worktree == "" {
		t.Fatal("the session got no worktree")
	}
	dock := func(a ipc.DockArgs) (ipc.DockResult, error) {
		var r ipc.DockResult
		a.Session = s.ID
		err := c.call(ipc.OpDock, a, &r)
		return r, err
	}

	// Untrusted: listed with what it would run, and nothing runs, not even
	// when the client asks for every control to start.
	r, err := dock(ipc.DockArgs{Ensure: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.Untrusted == nil || len(r.Untrusted.Commands) != 2 || !strings.Contains(r.Untrusted.Commands[0], "pwd > dock-where.txt") {
		t.Fatalf("untrusted = %+v, want the file's two controls and what they run", r.Untrusted)
	}
	if len(r.Controls) != 2 || r.Controls[0].State != ipc.DockStateUntrusted || r.Controls[1].State != ipc.DockStateUntrusted {
		t.Fatalf("controls = %+v, want both untrusted", r.Controls)
	}
	if _, err := dock(ipc.DockArgs{Start: "repo:where"}); err == nil {
		t.Fatal("a control of an untrusted file started")
	}
	where := filepath.Join(s.Worktree, "dock-where.txt")
	if _, err := os.Stat(where); err == nil {
		t.Fatal("a command of an untrusted file ran")
	}

	// Trusted: the terminal control starts in the session's worktree, and
	// the web page is ready to show.
	r, err = dock(ipc.DockArgs{Trust: r.Untrusted.Hash, Ensure: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.Untrusted != nil {
		t.Fatalf("still untrusted after trusting its hash: %+v", r.Untrusted)
	}
	run, page := r.Controls[0], r.Controls[1]
	if run.Key != "repo:where" || run.State != ipc.DockStateRunning || run.Target == "" || r.Socket == "" || r.Tmux == "" {
		t.Fatalf("where = %+v, socket %q, tmux %q: want it running, with a target to attach to", run, r.Socket, r.Tmux)
	}
	if page.State != ipc.DockStateReady || page.Chrome || page.URL != "http://127.0.0.1:9/" {
		t.Fatalf("page = %+v, want a ready page without chrome", page)
	}
	testutil.Eventually(t, 10*time.Second, "the control writes where it runs", func() bool {
		_, err := os.Stat(where)
		return err == nil
	})
	got, _ := os.ReadFile(where)
	want, _ := filepath.EvalSymlinks(s.Worktree)
	if g, _ := filepath.EvalSymlinks(strings.TrimSpace(string(got))); g != want {
		t.Fatalf("the control ran in %q, want the session's worktree %q", got, want)
	}

	// Stopped, then started again.
	if r, err = dock(ipc.DockArgs{Stop: "repo:where"}); err != nil || r.Controls[0].State != ipc.DockStateStopped {
		t.Fatalf("after stop: %+v, %v; want it stopped", r.Controls, err)
	}
	if r, err = dock(ipc.DockArgs{Start: "repo:where"}); err != nil || r.Controls[0].State != ipc.DockStateRunning {
		t.Fatalf("after start: %+v, %v; want it running", r.Controls, err)
	}
	target := r.Controls[0].Target

	// A changed file asks again before anything of it runs.
	testutil.WriteFiles(t, s.Worktree, map[string]string{".irie/dock.json": `{"controls":[{"id":"where","command":"echo changed"}]}`})
	if r, err = dock(ipc.DockArgs{Ensure: true}); err != nil || r.Untrusted == nil || r.Controls[0].State != ipc.DockStateUntrusted {
		t.Fatalf("after a change: %+v, %+v, %v; want it untrusted again", r.Untrusted, r.Controls, err)
	}

	// The copy ends with its session.
	if err := c.call(ipc.OpClose, ipc.CloseArgs{Session: s.ID}, nil); err != nil {
		t.Fatal(err)
	}
	testutil.Eventually(t, 10*time.Second, "the session's Dock copy ends with it", func() bool {
		return exec.Command("tmux", "-L", c.Socket+"-dock", "has-session", "-t", "="+target).Run() != nil
	})
}

// TestTheDockRunsTheUsersOwnControlsWithoutAsking: the user's own Dock file,
// in Irie's config folder, needs no trust, and a session outside any repo
// gets those controls too.
func TestTheDockRunsTheUsersOwnControlsWithoutAsking(t *testing.T) {
	c := setupWith(t, agentConfig{})
	testutil.WriteFiles(t, c.paths.Config, map[string]string{"dock.json": `{"controls":[{"id":"clock","title":"Clock","command":"exec sleep 120"}]}`})
	s := c.spawn("prompt,stop")
	c.waitState(s.ID, ipc.StateDone)
	var r ipc.DockResult
	if err := c.call(ipc.OpDock, ipc.DockArgs{Session: s.ID, Ensure: true}, &r); err != nil {
		t.Fatal(err)
	}
	if r.Untrusted != nil || len(r.Controls) != 1 {
		t.Fatalf("result = %+v, want one control and nothing to trust", r)
	}
	if r.Controls[0].State != ipc.DockStateRunning {
		log, _ := os.ReadFile(filepath.Join(c.paths.State, "core.log"))
		t.Logf("core.log:\n%s", tail(string(log), 30))
	}
	if ctl := r.Controls[0]; ctl.Key != "mine:clock" || ctl.Source != "mine" || ctl.State != ipc.DockStateRunning {
		t.Fatalf("clock = %+v, want the user's own control running", ctl)
	}
	if r.Mine != filepath.Join(c.paths.Config, "dock.json") {
		t.Fatalf("mine = %q, want the file in Irie's config folder", r.Mine)
	}
}

// tail is the last n lines of s.
func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func TestParseDockCopiesKeepsEmptyLastFields(t *testing.T) {
	got := parseDockCopies("dock-r-a-s1\t0\t\ts1\t\ndock-r-b-repo-x\t1\t2\t\tx\n")
	a, b := got["dock-r-a-s1"], got["dock-r-b-repo-x"]
	if len(got) != 2 || a.session != "s1" || a.dead || !b.dead || b.status != 2 || b.repo != "x" {
		t.Fatalf("got %+v, want both copies with their owners", got)
	}
}
