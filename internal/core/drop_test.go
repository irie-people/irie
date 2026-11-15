package core

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/testutil"
	"github.com/irie-people/irie/internal/worktree"
)

// dropSetup starts Irie with a repo, website, whose work lands in its
// branch live, and whose post-checkout hook notes each checkout in an
// ignored file, as the hooks a repo runs for a new worktree would.
func dropSetup(t *testing.T, extra string) (*coreEnv, string) {
	t.Helper()
	testutil.IsolateGit(t)
	repo := website(t, filepath.Join(t.TempDir(), "website"))
	testutil.Git(t, repo, "branch", "live")
	hook := filepath.Join(repo, ".git", "hooks", "post-checkout")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nmkdir -p build && echo ran >> build/hooked\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	c := setupWith(t, agentConfig{extra: "[dog]\nnudges = 0\nland = { website = \"live\" }\n" + extra})
	return c, repo
}

// treeEvents lists session id's worktree.dropped and worktree.restored.
func (c *coreEnv) treeEvents(id string) []event.Event {
	var out []event.Event
	for _, e := range c.events(id) {
		if e.Kind == event.WorktreeDropped || e.Kind == event.WorktreeRestored {
			out = append(out, e)
		}
	}
	return out
}

func there(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func TestClosingALandedSessionDropsItsWorktreeAndReopenAddsItBack(t *testing.T) {
	c, repo := dropSetup(t, "")
	landed := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,stop", Repo: repo, Name: "landed"})
	dirty := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,write:notes.md,stop", Repo: repo, Name: "dirty"})
	ahead := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,write:notes.md,commit,stop", Repo: repo, Name: "ahead"})
	for _, s := range []ipc.Session{landed, dirty, ahead} {
		c.waitState(s.ID, ipc.StateDone)
	}
	live := testutil.Git(t, repo, "rev-parse", "live")
	c.closeSession(dirty.ID)
	c.closeSession(ahead.ID)
	c.closeSession(landed.ID)

	// Once its agent has ended, the landed session's checkout goes, and
	// its branch stays.
	testutil.Eventually(t, 20*time.Second, "the landed session's worktree is still there", func() bool { return !there(landed.Worktree) })
	if tip := testutil.Git(t, repo, "rev-parse", "--verify", "refs/heads/"+landed.Branch); tip != live {
		t.Errorf("its branch is at %s, want %s", tip, live)
	}
	evs := c.treeEvents(landed.ID)
	if len(evs) != 1 || evs[0].Kind != event.WorktreeDropped || evs[0].Source != event.SourceIrie || evs[0].Str("path") != landed.Worktree ||
		evs[0].Str("branch") != landed.Branch || evs[0].Str("commit") != live || evs[0].Str("land") != "live" || !sameDir(evs[0].Str("repo"), repo) {
		t.Errorf("the journal's record = %+v", evs)
	}
	// What has not landed keeps its worktree: work not committed, or a
	// commit live lacks.
	time.Sleep(dropSettle + time.Second)
	for _, s := range []ipc.Session{dirty, ahead} {
		if !there(s.Worktree) || len(c.treeEvents(s.ID)) != 0 {
			t.Errorf("%s: worktree there %v, events %+v", s.Name, there(s.Worktree), c.treeEvents(s.ID))
		}
	}

	// Undoing the close brings it back as it was: the same path on the same
	// branch, the repo's hook run again, its local files copied in, and
	// its agent carrying on there.
	c.step(dirUndo)
	back, ok := c.row(landed.ID)
	if !ok {
		t.Fatal("undo did not reopen it")
	}
	sameSession(t, back, landed)
	if b := testutil.Git(t, landed.Worktree, "rev-parse", "--abbrev-ref", "HEAD"); b != landed.Branch {
		t.Errorf("back on %q", b)
	}
	if !there(filepath.Join(landed.Worktree, "build", "hooked")) {
		t.Error("the repo's hook did not run for the worktree that came back")
	}
	for _, f := range landed.Copied {
		want, _ := os.ReadFile(filepath.Join(repo, f))
		if got, err := os.ReadFile(filepath.Join(landed.Worktree, f)); err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s came back as %q, %v", f, got, err)
		}
	}
	runs := c.runs(landed.ID)
	if log := c.agentLog(landed.ID); len(runs) != 2 || !sameDir(lastStartCwd(log), landed.Worktree) {
		t.Errorf("runs %+v, last start in %s", runs, lastStartCwd(log))
	}
	if evs := c.treeEvents(landed.ID); len(evs) != 2 || evs[1].Kind != event.WorktreeRestored || evs[1].Str("path") != landed.Worktree || evs[1].Str("commit") != live {
		t.Errorf("after the undo, the journal's records = %+v", evs)
	}

	// A close and a reopen do it again.
	c.closeSession(landed.ID)
	testutil.Eventually(t, 20*time.Second, "the worktree did not go again", func() bool { return !there(landed.Worktree) })
	if _, err := c.reopen(landed.ID); err != nil {
		t.Fatal(err)
	}
	if !there(landed.Worktree) {
		t.Error("reopen did not add the worktree back")
	}
	if n := len(c.treeEvents(landed.ID)); n != 4 {
		t.Errorf("%d records, want 4", n)
	}
}

// lastStartCwd is where the fake agent last started.
func lastStartCwd(log []map[string]any) string {
	cwd := ""
	for _, l := range log {
		if l["t"] == "start" {
			cwd, _ = l["cwd"].(string)
		}
	}
	return cwd
}

func TestTheSweepDropsOnlyWhatCanGo(t *testing.T) {
	// With worktree.drop off, a close keeps the worktree, as before.
	c, repo := dropSetup(t, "\n[worktree]\ndrop = false\n")
	a := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,stop", Repo: repo, Name: "a"})
	busy := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,stop", Repo: repo, Name: "busy"})
	dirty := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,write:notes.md,stop", Repo: repo, Name: "dirty"})
	for _, s := range []ipc.Session{a, busy, dirty} {
		c.waitState(s.ID, ipc.StateDone)
	}
	// Something that is not its agent's runs in one, and outlives it.
	sleeper := exec.Command("sleep", "60")
	sleeper.Dir = busy.Worktree
	if err := sleeper.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sleeper.Process.Kill(); sleeper.Wait() })
	for _, s := range []ipc.Session{a, busy, dirty} {
		c.closeSession(s.ID)
		c.waitGone(s.Pane, 10*time.Second)
	}
	time.Sleep(dropSettle + time.Second)
	if !there(a.Worktree) {
		t.Fatal("a close dropped a worktree with worktree.drop off")
	}

	// The dry run says what could go and why the rest stays, and removes
	// nothing.
	sweep := func(drop bool) ipc.WorktreesResult {
		t.Helper()
		var r ipc.WorktreesResult
		if err := c.call(ipc.OpWorktrees, ipc.WorktreesArgs{Drop: drop}, &r); err != nil {
			t.Fatalf("worktrees: %v", err)
		}
		return r
	}
	keep := func(r ipc.WorktreesResult, id string) ipc.ClosedTree {
		for _, tr := range r.Trees {
			if tr.Session == id {
				return tr
			}
		}
		t.Fatalf("%s is not in %+v", id, r.Trees)
		return ipc.ClosedTree{}
	}
	r := sweep(false)
	if len(r.Trees) != 3 || r.Can != 1 || r.Trees[0].Session != a.ID || r.Trees[0].Keep != "" || r.Trees[0].Bytes <= 0 || r.Bytes != r.Trees[0].Bytes || r.Dropped != 0 {
		t.Errorf("dry run = %+v", r)
	}
	if k := keep(r, busy.ID); !k.Busy || !strings.HasPrefix(k.Keep, "sleep (pid ") || !strings.Contains(k.Keep, " works in it") {
		t.Errorf("busy = %+v", k)
	}
	if k := keep(r, dirty.ID); k.Busy || k.Keep != "1 file is not committed: notes.md" {
		t.Errorf("dirty = %+v", k)
	}
	if !there(a.Worktree) {
		t.Fatal("the dry run removed a worktree")
	}

	// The sweep removes it, and only it.
	r = sweep(true)
	if r.Dropped != 1 || r.Can != 1 || !r.Trees[0].Dropped || r.Trees[0].Session != a.ID {
		t.Errorf("sweep = %+v", r)
	}
	if there(a.Worktree) || !there(busy.Worktree) || !there(dirty.Worktree) {
		t.Errorf("after the sweep: a %v, busy %v, dirty %v", there(a.Worktree), there(busy.Worktree), there(dirty.Worktree))
	}
	if evs := c.treeEvents(a.ID); len(evs) != 1 || evs[0].Kind != event.WorktreeDropped {
		t.Errorf("the journal's records = %+v", evs)
	}
	// Once nothing runs there, the busy one can go too; a's is gone.
	sleeper.Process.Kill()
	sleeper.Wait()
	if r := sweep(false); len(r.Trees) != 2 || r.Can != 1 || r.Trees[0].Session != busy.ID {
		t.Errorf("dry run once the sleeper ended = %+v", r)
	}
	if _, err := c.reopen(a.ID); err != nil || !there(a.Worktree) {
		t.Errorf("reopen after the sweep: %v, worktree there %v", err, there(a.Worktree))
	}
}

func TestTheDogReadsADroppedWorktreeFromItsBranch(t *testing.T) {
	testutil.IsolateGit(t)
	repo := testutil.NewRepo(t, filepath.Join(t.TempDir(), "website"), map[string]string{"README.md": "# website\n"})
	testutil.Git(t, repo, "branch", "live")
	c := &Core{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	c.useConfig(config.Default())
	c.cfg().Dog.Land = map[string]string{"website": "live"}
	res, err := worktree.Create(worktree.Request{Repo: repo, Name: "website", Slug: "fix"}, worktree.Options{Root: t.TempDir(), BranchPrefix: "irie/"})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(-time.Minute)
	testutil.WriteFiles(t, res.Dir, map[string]string{"fix.md": "fixed\n"})
	testutil.Git(t, res.Dir, "add", "fix.md")
	testutil.Git(t, res.Dir, "commit", "-q", "-m", "Fix it")
	testutil.Git(t, repo, "branch", "-f", "live", res.Branch)
	if err := worktree.Drop(res.Dir, repo, nil, time.Minute); err != nil {
		t.Fatal(err)
	}
	l := jotLook{id: "s38aaaaaaaaaa", cwd: res.Dir, repo: repo, worktree: res.Dir, branch: res.Branch, dropped: true, created: start, fresh: true}
	if _, loose := c.readJot(l, nil, time.Minute); len(loose) != 0 {
		t.Errorf("landed: %+v", loose)
	}
	// The land branch moves back: the dog finds the commit on the branch.
	testutil.Git(t, repo, "branch", "-f", "live", "main")
	if _, loose := c.readJot(l, nil, time.Minute); len(loose) != 1 || loose[0].Ahead != 1 || loose[0].path != res.Dir {
		t.Errorf("after live moved back: %+v", loose)
	}
	// A worktree Irie did not drop and that is gone, its agent removed.
	l.dropped = false
	if _, loose := c.readJot(l, nil, time.Minute); len(loose) != 0 {
		t.Errorf("one the agent removed: %+v", loose)
	}
}

func TestANewWorktreeNeverTakesADroppedOnesPath(t *testing.T) {
	testutil.IsolateGit(t)
	repo := testutil.NewRepo(t, filepath.Join(t.TempDir(), "website"), map[string]string{"README.md": "# website\n"})
	root := t.TempDir()
	c := &Core{}
	c.noteDropped(filepath.Join(root, "website", "fix"), droppedTree{session: "s38aaaaaaaaaa", branch: "gone/fix"})
	res, err := worktree.Create(worktree.Request{Repo: repo, Name: "website", Slug: "fix", Taken: c.dropTaken(root, "website")}, worktree.Options{Root: root, BranchPrefix: "irie/"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Slug != "fix-2" || res.Dir != filepath.Join(root, "website", "fix-2") {
		t.Errorf("the new worktree = %+v, want fix-2", res)
	}
}
