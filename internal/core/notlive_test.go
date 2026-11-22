package core

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/testutil"
)

// A card whose commits have not landed says so (DESIGN §4.5): unlanded
// names them before a close and closes nothing, the close goes ahead and
// its reply names them, its ui.closed keeps how many, and the dog lists the
// card, closed, until they land, across a restart too. A cherry-pick lands
// them. A card in a plain folder never has any.
func TestClosingACardWithWorkNotLiveSaysSoAndLeavesATrail(t *testing.T) {
	c, repo := dogSetup(t, "nudges = 0\n\n[worktree]\ndrop = false\n")
	s := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,write:notes.md,commit,stop", Repo: repo, Name: "spaces"})
	plain := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,stop", Cwd: t.TempDir(), Name: "plain"})
	c.waitState(s.ID, ipc.StateDone)
	c.waitState(plain.ID, ipc.StateDone)

	var u ipc.UnlandedResult
	if err := c.call(ipc.OpUnlanded, ipc.CloseArgs{Sessions: []string{plain.ID, s.ID}}, &u); err != nil {
		t.Fatal(err)
	}
	if len(u.Sessions) != 2 || len(u.NotLive) != 1 {
		t.Fatalf("unlanded = %+v, want both sessions and one card not live", u)
	}
	n := u.NotLive[0]
	if n.Session != s.ID || n.Name != "spaces" || n.Ahead != 1 || n.Land != "live" || n.Branch != s.Branch || !sameDir(n.Path, s.Worktree) ||
		len(n.Commits) != 1 || !strings.HasSuffix(n.Commits[0], " Fake work") {
		t.Errorf("not live = %+v", n)
	}
	if _, ok := c.row(s.ID); !ok {
		t.Fatal("unlanded closed the card")
	}

	// The close never waits on it: it closes, and says so.
	var r ipc.CloseResult
	if err := c.call(ipc.OpClose, ipc.CloseArgs{Session: s.ID}, &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Sessions) != 1 || len(r.NotLive) != 1 || r.NotLive[0].Session != s.ID || r.NotLive[0].Ahead != 1 {
		t.Errorf("close = %+v", r)
	}
	if _, ok := c.row(s.ID); ok {
		t.Error("the card is still open")
	}
	var kept bool
	for _, e := range c.events(s.ID) {
		if e.Kind == event.UIClosed {
			kept = e.Source == event.SourceUser && e.Data["not_live"] == float64(1)
		}
	}
	if !kept {
		t.Error("its ui.closed does not keep the commit not live")
	}
	// A card in a plain folder has none.
	r = ipc.CloseResult{}
	if err := c.call(ipc.OpClose, ipc.CloseArgs{Session: plain.ID}, &r); err != nil || len(r.NotLive) != 0 {
		t.Errorf("the plain folder's close = %+v, %v", r, err)
	}

	// The dog lists the card you closed with its work not live, and only it.
	listed := func(when string) {
		t.Helper()
		jots := c.dog()
		if len(jots) != 1 || jots[0].ID != s.ID || jots[0].State != "closed" || !jots[0].NotLive || jots[0].Closed == nil ||
			jots[0].Loose.Ahead != 1 || jots[0].Loose.Land != "live" || len(jots[0].Loose.Where) != 1 || jots[0].Loose.Where[0].Branch != s.Branch {
			t.Errorf("%s: dog = %+v", when, jots)
		}
	}
	listed("closed")
	c.restartCore()
	listed("after a restart")

	// It lands as a cherry-pick: the dog lets it go.
	lw := filepath.Join(t.TempDir(), "live")
	testutil.Git(t, repo, "worktree", "add", "-q", lw, "live")
	testutil.Git(t, lw, "cherry-pick", s.Branch)
	if jots := c.dog(); len(jots) != 0 {
		t.Errorf("dog after a cherry-pick landed it = %+v", jots)
	}
}

// A close of several, as Close Others, says what of each card's work has
// not landed, in the list's order; so does unlanded beforehand, for the
// sessions the close would take.
func TestClosingSeveralSaysWhatOfEachHasNotLanded(t *testing.T) {
	c, repo := dogSetup(t, "nudges = 0\n\n[worktree]\ndrop = false\n")
	a := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,write:a.md,commit,stop", Repo: repo, Name: "alpha"})
	b := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,stop", Repo: repo, Name: "beta"})
	g := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,write:g.md,commit,stop", Repo: repo, Name: "gamma"})
	ids := []string{g.ID, b.ID, a.ID}
	for _, s := range []ipc.Session{a, b, g} {
		c.waitState(s.ID, ipc.StateDone)
	}
	var u ipc.UnlandedResult
	if err := c.call(ipc.OpUnlanded, ipc.CloseArgs{Sessions: ids}, &u); err != nil {
		t.Fatal(err)
	}
	if len(u.Sessions) != 3 || len(u.NotLive) != 2 || u.NotLive[0].Session != a.ID || u.NotLive[1].Session != g.ID {
		t.Errorf("unlanded = %+v, want alpha's and gamma's, in the list's order", u)
	}
	var r ipc.CloseResult
	if err := c.call(ipc.OpClose, ipc.CloseArgs{Sessions: ids}, &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Sessions) != 3 || len(r.NotLive) != 2 || r.NotLive[0].Name != "alpha" || r.NotLive[1].Name != "gamma" {
		t.Errorf("close = %+v", r)
	}
	listed := map[string]bool{}
	for _, j := range c.dog() {
		listed[j.ID] = j.NotLive
	}
	if len(listed) != 2 || !listed[a.ID] || !listed[g.ID] {
		t.Errorf("dog lists %v, want alpha and gamma", listed)
	}
}
