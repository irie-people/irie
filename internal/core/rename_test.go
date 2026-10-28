package core

import (
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/journal"
	"github.com/irie-people/irie/internal/testutil"
	"github.com/irie-people/irie/internal/tmux"
)

func (c *coreEnv) rename(id, name string) (ipc.Session, error) {
	var r ipc.RenameResult
	err := c.call(ipc.OpRename, ipc.RenameArgs{Session: id, Name: name}, &r)
	return r.Session, err
}

// renames returns session id's ui.renamed events, oldest first.
func (c *coreEnv) renames(id string) []event.Event {
	c.t.Helper()
	evs, err := journal.ReadKind(c.paths.Journal(), time.Time{}, event.UIRenamed)
	if err != nil {
		c.t.Fatal(err)
	}
	var out []event.Event
	for _, e := range evs {
		if e.Session == id {
			out = append(out, e)
		}
	}
	return out
}

func TestRenameSurvivesACoreRestart(t *testing.T) {
	// The terminal view, so the session's own pane is in the slot.
	c := setupWith(t, agentConfig{terminal: true})
	a := c.spawn("task a")
	c.waitState(a.ID, ipc.StateWorking)
	c.show(a.ID, false)

	// Any text: capitals, spaces, other scripts, and what tmux would expand
	// as a format. Runs of whitespace become one space.
	name := "Fix the login #{session_name} ☀"
	s, err := c.rename(a.ID, "  Fix the\tlogin #{session_name}\n☀ ")
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != name || s.State != ipc.StateWorking {
		t.Errorf("renamed row = %+v, want the name %q and the state working", s, name)
	}
	if row, _ := c.row(a.ID); row.Name != name {
		t.Errorf("listed name = %q, want %q", row.Name, name)
	}
	// The name is on the pane, here in the slot; irie:0 keeps its own name.
	if p := c.slot(); p["@irie_session"] != a.ID || p["@irie_name"] != name {
		t.Errorf("slot = %v, want the session with the name %q", p, name)
	}
	if w := c.Tmux(t, "display-message", "-p", "-t", tmux.MainSession+":0", "#{window_name}"); strings.Contains(w, "Fix") {
		t.Errorf("irie:0 is named %q", w)
	}
	if evs := c.renames(a.ID); len(evs) != 1 || evs[0].Source != event.SourceUser || evs[0].Str("before") != "Task A" || evs[0].Str("after") != name || evs[0].Data["before_named"] != false {
		t.Errorf("ui.renamed = %+v, want one from Task A, not pinned, to %q", evs, name)
	}

	// A new core reads the name back from the pane.
	pid := c.supervised(tmux.AgentsSession + ":core")
	if pid == 0 {
		t.Fatal("core process not found under its supervisor")
	}
	kill(t, pid)
	testutil.Eventually(t, 10*time.Second, "the core did not come back with the new name", func() bool {
		var r ipc.ListResult
		return c.call(ipc.OpList, nil, &r) == nil && len(r.Sessions) == 1 && r.Sessions[0].Name == name
	})
}

// Close keeps the new name, through a restart, and reopen brings the
// session back under it, with the name as typed on its new window too.
func TestRenameSurvivesCloseAndReopen(t *testing.T) {
	c := setupWith(t, agentConfig{readyTimeout: "1s"})
	a := c.spawn("prompt")
	c.waitState(a.ID, ipc.StateWorking)
	name := "Fix the login #{session_name}"
	if _, err := c.rename(a.ID, name); err != nil {
		t.Fatal(err)
	}
	c.closeSession(a.ID)
	c.waitGone(a.Pane, 3*time.Second)
	c.restartCore()
	if r := c.listAll(); len(r.Closed) != 1 || r.Closed[0].Name != name {
		t.Fatalf("closed = %+v, want %q", r.Closed, name)
	}
	s, err := c.reopen(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != name || c.pane(s.Pane)["@irie_name"] != name {
		t.Errorf("reopened as %q, pane %v; want %q", s.Name, c.pane(s.Pane), name)
	}
	if w := c.Tmux(t, "display-message", "-p", "-t", s.Pane, "#{window_name}"); w != name {
		t.Errorf("the reopened window is named %q, want %q", w, name)
	}
}

func TestRenameNumbersATakenNameAndRefusesNone(t *testing.T) {
	c := setup(t)
	a := c.spawn("task a")
	b := c.spawn("task b")
	c.waitState(a.ID, ipc.StateWorking)
	c.waitState(b.ID, ipc.StateWorking)

	// Another row's name is numbered, as a spawn's is.
	if s, err := c.rename(b.ID, "Task A"); err != nil || s.Name != "Task A-2" {
		t.Errorf("rename to a taken name = %q, %v; want Task A-2", s.Name, err)
	}
	// Its own name changes nothing and is not journaled, but the name is
	// the user's now.
	if s, err := c.rename(a.ID, " Task A "); err != nil || s.Name != "Task A" || c.pane(s.Pane)["@irie_named"] != "1" {
		t.Errorf("rename to its own name = %q, %v; pane %v", s.Name, err, c.pane(s.Pane))
	}
	if evs := c.renames(a.ID); len(evs) != 0 {
		t.Errorf("a rename that changed nothing was journaled: %+v", evs)
	}
	for _, x := range []struct{ id, name, code string }{
		{a.ID, "", ipc.CodeBadArgs},
		{a.ID, " \t\n", ipc.CodeBadArgs},
		{"s000000000000", "x", ipc.CodeNotFound},
	} {
		if _, err := c.rename(x.id, x.name); code(err) != x.code {
			t.Errorf("rename %s to %q: %v, want %s", x.id, x.name, err, x.code)
		}
	}
	// A long name is cut at 200 bytes, on a character's edge.
	if s, err := c.rename(a.ID, strings.Repeat("é", 150)); err != nil || s.Name != strings.Repeat("é", 100) {
		t.Errorf("rename to 300 bytes = %d bytes, %v; want 200", len(s.Name), err)
	}
}
