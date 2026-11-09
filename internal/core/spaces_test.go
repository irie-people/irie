package core

import (
	"slices"
	"testing"

	"github.com/irie-people/irie/internal/ipc"
)

func (c *coreEnv) spaces(a ipc.SpacesArgs) ipc.SpacesResult {
	c.t.Helper()
	var r ipc.SpacesResult
	if err := c.call(ipc.OpSpaces, a, &r); err != nil {
		c.t.Fatalf("spaces %+v: %v", a, err)
	}
	return r
}

func spaceNames(r ipc.SpacesResult) []string {
	var out []string
	for _, s := range r.Spaces {
		out = append(out, s.Name)
	}
	return out
}

// spaceOfRow is the space a session's row says it is in.
func (c *coreEnv) spaceOfRow(id string) string {
	c.t.Helper()
	r, ok := c.row(id)
	if !ok {
		c.t.Fatalf("no row %s", id)
	}
	return r.Space
}

func TestSpacesHoldSessionsAndOneUndoMovesThemBack(t *testing.T) {
	c := setup(t)
	if r := c.spaces(ipc.SpacesArgs{}); !slices.Equal(spaceNames(r), []string{"Main"}) {
		t.Fatalf("spaces at the start: %v", spaceNames(r))
	}
	steak := c.spaces(ipc.SpacesArgs{Do: ipc.SpacesNew, Name: "Steak"}).Space
	// Names are unique in any case, and an empty one gets a name of its own.
	c.spaces(ipc.SpacesArgs{Do: ipc.SpacesNew, Name: "steak"})
	r := c.spaces(ipc.SpacesArgs{Do: ipc.SpacesNew})
	if !slices.Equal(spaceNames(r), []string{"Main", "Steak", "steak 2", "Space 4"}) {
		t.Errorf("spaces = %v", spaceNames(r))
	}
	last := r.Space
	r = c.spaces(ipc.SpacesArgs{Do: ipc.SpacesRename, Space: last, Name: "  Gigbot\tdesk  "})
	r = c.spaces(ipc.SpacesArgs{Do: ipc.SpacesMove, Space: last, Before: steak})
	if !slices.Equal(spaceNames(r), []string{"Main", "Gigbot desk", "Steak", "steak 2"}) {
		t.Errorf("after a rename and a move: %v", spaceNames(r))
	}

	a := c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Name: "alpha"})
	b := c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Name: "beta"})
	var moved ipc.SpaceResult
	if err := c.call(ipc.OpSpace, ipc.SpaceArgs{Sessions: []string{a.ID, b.ID}, Space: steak}, &moved); err != nil {
		t.Fatal(err)
	}
	if len(moved.Sessions) != 2 || c.spaceOfRow(a.ID) != steak || c.spaceOfRow(b.ID) != steak {
		t.Errorf("moved %v; rows say %q, %q", moved.Sessions, c.spaceOfRow(a.ID), c.spaceOfRow(b.ID))
	}
	// One undo takes both back to the first space, and one redo moves them
	// again.
	if res := c.step(dirUndo); res.Label != "move 2 sessions" {
		t.Errorf("undo %+v", res)
	}
	if c.spaceOfRow(a.ID) != "" || c.spaceOfRow(b.ID) != "" {
		t.Errorf("after the undo, rows say %q, %q", c.spaceOfRow(a.ID), c.spaceOfRow(b.ID))
	}
	if res := c.step(dirRedo); res.Label != "move 2 sessions" || c.spaceOfRow(a.ID) != steak {
		t.Errorf("redo %+v; alpha in %q", res, c.spaceOfRow(a.ID))
	}
	// A session started from one in Steak starts there too.
	from := c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Name: "from-alpha", After: a.ID})
	if got := c.spaceOfRow(from.ID); got != steak {
		t.Errorf("a session started from alpha is in %q, want Steak", got)
	}
	c.restartCore()
	if got := spaceNames(c.spaces(ipc.SpacesArgs{})); len(got) != 4 || c.spaceOfRow(a.ID) != steak {
		t.Errorf("after a restart: spaces %v, alpha in %q", got, c.spaceOfRow(a.ID))
	}
	// Removed, a space's sessions are in the first.
	c.spaces(ipc.SpacesArgs{Do: ipc.SpacesRemove, Space: steak})
	if c.spaceOfRow(a.ID) != "" || c.spaceOfRow(from.ID) != "" {
		t.Errorf("after removing Steak, alpha is in %q", c.spaceOfRow(a.ID))
	}
	for _, bad := range []ipc.SpacesArgs{{Do: ipc.SpacesRename, Space: "pnope", Name: "x"}, {Do: "fold"}, {Do: ipc.SpacesRename, Space: last}} {
		if err := c.call(ipc.OpSpaces, bad, nil); err == nil {
			t.Errorf("spaces %+v went through", bad)
		}
	}
}

func TestTheLastSpaceStaysAndCloseFinishedKeepsToASpace(t *testing.T) {
	c := setup(t)
	first := c.spaces(ipc.SpacesArgs{}).Spaces[0].ID
	if err := c.call(ipc.OpSpaces, ipc.SpacesArgs{Do: ipc.SpacesRemove, Space: first}, nil); err == nil {
		t.Error("the last space was removed")
	}
	other := c.spaces(ipc.SpacesArgs{Do: ipc.SpacesNew, Name: "Other"}).Space
	here := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,stop", Name: "here"})
	there := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,stop", Name: "there", Space: other})
	c.waitState(here.ID, ipc.StateDone)
	c.waitState(there.ID, ipc.StateDone)
	if c.spaceOfRow(there.ID) != other {
		t.Fatalf("there is in %q, want Other", c.spaceOfRow(there.ID))
	}
	inFirst := ""
	var r ipc.CloseResult
	if err := c.call(ipc.OpClose, ipc.CloseArgs{Finished: true, Space: &inFirst}, &r); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.Sessions, []string{here.ID}) {
		t.Errorf("closed %v, want only the first space's finished session", r.Sessions)
	}
}
