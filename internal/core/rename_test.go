package core

import (
	"strings"
	"testing"
	"time"

	"github.com/irie-hq/irie/internal/event"
	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/journal"
	"github.com/irie-hq/irie/internal/testutil"
	"github.com/irie-hq/irie/internal/tmux"
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
	c := setup(t)
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
	if evs := c.renames(a.ID); len(evs) != 1 || evs[0].Source != event.SourceUser || evs[0].Str("before") != "task-a" || evs[0].Str("after") != name {
		t.Errorf("ui.renamed = %+v, want one from task-a to %q", evs, name)
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

func TestRenameNumbersATakenNameAndRefusesNone(t *testing.T) {
	c := setup(t)
	a := c.spawn("task a")
	b := c.spawn("task b")
	c.waitState(a.ID, ipc.StateWorking)
	c.waitState(b.ID, ipc.StateWorking)

	// Another row's name is numbered, as a spawn's is.
	if s, err := c.rename(b.ID, "task-a"); err != nil || s.Name != "task-a-2" {
		t.Errorf("rename to a taken name = %q, %v; want task-a-2", s.Name, err)
	}
	// Its own name changes nothing and is not journaled.
	if s, err := c.rename(a.ID, " task-a "); err != nil || s.Name != "task-a" {
		t.Errorf("rename to its own name = %q, %v", s.Name, err)
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
