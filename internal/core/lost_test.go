package core

import (
	"slices"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/testutil"
)

// lostOf lists the closed sessions marked lost, by id.
func lostOf(r ipc.ListResult) map[string]ipc.ClosedSession {
	out := map[string]ipc.ClosedSession{}
	for _, c := range r.Closed {
		if c.Lost {
			out[c.ID] = c
		}
	}
	return out
}

// A session whose agent ended with Irie's tmux server, as when the Mac
// restarts, is lost: the next core records it as closed on its own, with
// lost set, and reopen brings it back as it was: its conversation carried
// on, parked or frozen again. A session closed before is not lost, and a
// core that starts again records nothing twice.
func TestSessionsLostWithTheServerComeBackAsTheyWere(t *testing.T) {
	c := setup(t)
	// Named three ways: by the title Claude Code gave it, at spawn, and by
	// a rename.
	plain := c.spawn("look into the flaky login test")
	parked := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,stop", Name: "Login fix"})
	frozen := c.spawn("tidy the docs folder")
	closed := c.spawn("prompt,stop")
	c.waitState(plain.ID, ipc.StateWorking)
	c.waitState(parked.ID, ipc.StateDone)
	c.waitState(frozen.ID, ipc.StateWorking)
	c.waitState(closed.ID, ipc.StateDone)
	c.readUp(plain.ID, aiTitle("Flaky login test"))
	if _, err := c.rename(frozen.ID, "Docs tidy"); err != nil {
		t.Fatal(err)
	}
	c.park(parked.ID, true)
	c.freeze(frozen.ID, true)
	c.closeSession(closed.ID)
	c.waitGone(closed.Pane, 10*time.Second)

	// The server ends with the agents in it, and Irie starts again.
	if _, err := c.TryTmux("kill-server"); err != nil {
		t.Fatal(err)
	}
	c.up()
	if ls := c.list(); len(ls) != 0 {
		t.Fatalf("rows after the restart = %+v", ls)
	}
	r := c.listAll()
	lost := lostOf(r)
	if len(lost) != 3 || lost[plain.ID].Name != "Flaky Login Test" || lost[parked.ID].Name != "Login fix" || lost[frozen.ID].Name != "Docs tidy" || !lost[plain.ID].Auto {
		t.Fatalf("lost = %+v, want the three sessions that were open, closed on their own", lost)
	}
	if r.Closed[0].ID != frozen.ID {
		t.Errorf("closed = %+v, want the one started last first", r.Closed)
	}
	if i := slices.IndexFunc(r.Closed, func(s ipc.ClosedSession) bool { return s.ID == closed.ID }); i < 0 || r.Closed[i].Lost {
		t.Errorf("the session closed before the restart = %+v", r.Closed)
	}
	var marks []event.Event
	for _, e := range c.events(frozen.ID) {
		if e.Kind == event.UIClosed {
			marks = append(marks, e)
		}
	}
	if len(marks) != 1 || marks[0].Source != event.SourceIrie || marks[0].Data["lost"] != true || marks[0].Data["frozen"] != true || marks[0].Data["named"] != true ||
		marks[0].Str("vendor_id") != frozen.VendorID || marks[0].Data["resume"] != true {
		t.Errorf("the frozen session's ui.closed = %+v", marks)
	}

	// Back as it was: the same row and conversation, parked or frozen.
	s, err := c.reopen(parked.ID)
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != parked.ID || s.Name != parked.Name || !s.Created.Equal(parked.Created) || s.VendorID != parked.VendorID || s.Cwd != parked.Cwd {
		t.Errorf("reopened %+v, lost %+v", s, parked)
	}
	spec, err := host.ReadSpec(c.paths.SpecFile(parked.ID))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(spec.Argv); n < 2 || spec.Argv[n-2] != "--resume" || spec.Argv[n-1] != parked.VendorID {
		t.Errorf("argv = %q, want it to resume %s", spec.Argv, parked.VendorID)
	}
	if row, ok := c.row(parked.ID); !ok || !row.Parked {
		t.Errorf("the parked session came back as %+v", row)
	}
	if _, err := c.reopen(frozen.ID); err != nil {
		t.Fatal(err)
	}
	testutil.Eventually(t, 10*time.Second, "the frozen session did not come back frozen", func() bool {
		row, ok := c.row(frozen.ID)
		return ok && row.Frozen != nil
	})
	if row, _ := c.row(frozen.ID); row.Parked || row.Name != "Docs tidy" {
		t.Errorf("the frozen session came back as %+v", row)
	}

	// A core that starts again finds nothing more lost.
	c.restartCore()
	if lost := lostOf(c.listAll()); len(lost) != 1 || lost[plain.ID].ID == "" {
		t.Errorf("lost after another restart = %+v, want only the one not reopened", lost)
	}
	n := 0
	for _, e := range c.events(plain.ID) {
		if e.Kind == event.UIClosed {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the lost session was closed %d times, want once", n)
	}
}
