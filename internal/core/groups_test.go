package core

import (
	"slices"
	"testing"

	"github.com/irie-people/irie/internal/ipc"
)

func (c *coreEnv) group(a ipc.GroupArgs) ipc.GroupResult {
	c.t.Helper()
	var r ipc.GroupResult
	if err := c.call(ipc.OpGroup, a, &r); err != nil {
		c.t.Fatalf("group %+v: %v", a, err)
	}
	return r
}

func groupNames(r ipc.GroupResult) []string {
	out := []string{}
	for _, g := range r.Groups {
		out = append(out, g.Name)
	}
	return out
}

// inGroup are the names of the rows in group id, in the list's order.
func inGroup(rows []ipc.Session, id string) []string {
	var out []string
	for _, s := range rows {
		if s.Group == id {
			out = append(out, s.Name)
		}
	}
	return out
}

// spawnNamed spawns a session for each name, in turn, and returns their ids
// by name.
func (c *coreEnv) spawnNamed(names ...string) map[string]string {
	c.t.Helper()
	id := map[string]string{}
	for _, n := range names {
		id[n] = c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Name: n}).ID
	}
	return id
}

func TestANewGroupGathersItsSessionsAndOneUndoTakesItBack(t *testing.T) {
	c := setup(t)
	id := c.spawnNamed("alpha", "beta", "gamma", "delta")
	r := c.group(ipc.GroupArgs{Do: ipc.GroupNew, Name: " Steak\tcards ", Sessions: []string{id["delta"], id["beta"]}})
	if !slices.Equal(groupNames(r), []string{"Steak cards"}) || r.Group == "" || len(r.Sessions) != 2 {
		t.Fatalf("new = %+v", r)
	}
	steak := r.Group
	// The group is where the first of its sessions was, with the other
	// just after it.
	rows := c.list()
	if got := names(rows); !slices.Equal(got, []string{"alpha", "beta", "delta", "gamma"}) {
		t.Errorf("order = %v, want delta gathered after beta", got)
	}
	if got := inGroup(rows, steak); !slices.Equal(got, []string{"beta", "delta"}) {
		t.Errorf("in the group: %v", got)
	}
	if h := c.history(); !slices.Equal(labels(h.Undo)[4:], []string{`group "beta"`, `group "delta"`}) || h.Undo[4].Batch == "" {
		t.Errorf("history %v", labels(h.Undo))
	}
	c.restartCore()
	if got := inGroup(c.list(), steak); !slices.Equal(got, []string{"beta", "delta"}) {
		t.Errorf("after a restart, in the group: %v", got)
	}
	if r := c.group(ipc.GroupArgs{}); !slices.Equal(groupNames(r), []string{"Steak cards"}) {
		t.Errorf("after a restart, groups = %v", groupNames(r))
	}

	// One undo takes both out, back where they were, and the group goes
	// with its last session; redo makes it again, with its id and name.
	if u := c.step(dirUndo); u.Label != "group 2 sessions" {
		t.Errorf("undo label %q", u.Label)
	}
	rows = c.list()
	if got := names(rows); !slices.Equal(got, []string{"alpha", "beta", "gamma", "delta"}) {
		t.Errorf("after undo, order = %v", got)
	}
	if got := inGroup(rows, steak); len(got) != 0 {
		t.Errorf("after undo, in the group: %v", got)
	}
	if r := c.group(ipc.GroupArgs{}); len(r.Groups) != 0 {
		t.Errorf("after undo, groups = %v", groupNames(r))
	}
	c.step(dirRedo)
	rows = c.list()
	if got := names(rows); !slices.Equal(got, []string{"alpha", "beta", "delta", "gamma"}) {
		t.Errorf("after redo, order = %v", got)
	}
	if got := inGroup(rows, steak); !slices.Equal(got, []string{"beta", "delta"}) {
		t.Errorf("after redo, in the group: %v", got)
	}
	if r := c.group(ipc.GroupArgs{}); !slices.Equal(groupNames(r), []string{"Steak cards"}) {
		t.Errorf("after redo, groups = %v", groupNames(r))
	}
}

func TestSessionsJoinAndLeaveAGroupAndUngroupKeepsThemInPlace(t *testing.T) {
	c := setup(t)
	id := c.spawnNamed("alpha", "beta", "gamma", "delta", "epsilon")
	g := c.group(ipc.GroupArgs{Do: ipc.GroupNew, Sessions: []string{id["alpha"]}}).Group
	// A group made without a name gets one of its own; names are unique in
	// any case.
	if r := c.group(ipc.GroupArgs{Do: ipc.GroupRename, Group: g, Name: "steak"}); !slices.Equal(groupNames(r), []string{"steak"}) {
		t.Errorf("renamed: %v", groupNames(r))
	}
	other := c.group(ipc.GroupArgs{Do: ipc.GroupNew, Name: "STEAK", Sessions: []string{id["epsilon"]}})
	if !slices.Equal(groupNames(other), []string{"steak", "STEAK 2"}) {
		t.Errorf("groups = %v", groupNames(other))
	}

	// One that joins goes last in the group; by name, add finds it in any
	// case.
	if r := c.group(ipc.GroupArgs{Do: ipc.GroupAdd, Name: "Steak", Sessions: []string{id["delta"]}}); r.Group != g {
		t.Errorf("add by name went to %q, want %q", r.Group, g)
	}
	c.group(ipc.GroupArgs{Do: ipc.GroupAdd, Group: g, Sessions: []string{id["gamma"]}})
	rows := c.list()
	if got := names(rows); !slices.Equal(got, []string{"alpha", "delta", "gamma", "beta", "epsilon"}) {
		t.Errorf("order = %v", got)
	}
	if got := inGroup(rows, g); !slices.Equal(got, []string{"alpha", "delta", "gamma"}) {
		t.Errorf("in the group: %v", got)
	}
	// Dropped before one of it, it goes there.
	c.group(ipc.GroupArgs{Do: ipc.GroupAdd, Group: g, Sessions: []string{id["beta"]}, Before: id["delta"]})
	if got := inGroup(c.list(), g); !slices.Equal(got, []string{"alpha", "beta", "delta", "gamma"}) {
		t.Errorf("in the group after a drop: %v", got)
	}

	// Taken out, a session goes just after the rest of its group.
	if r := c.group(ipc.GroupArgs{Do: ipc.GroupRemove, Sessions: []string{id["alpha"]}}); !slices.Equal(r.Sessions, []string{id["alpha"]}) {
		t.Errorf("remove moved %v", r.Sessions)
	}
	rows = c.list()
	if got := names(rows); !slices.Equal(got, []string{"beta", "delta", "gamma", "alpha", "epsilon"}) {
		t.Errorf("after remove, order = %v", got)
	}
	if got := inGroup(rows, ""); !slices.Equal(got, []string{"alpha"}) {
		t.Errorf("in no group: %v", got)
	}
	if h := c.history(); labels(h.Undo)[len(h.Undo)-1] != `ungroup "alpha"` {
		t.Errorf("history %v", labels(h.Undo))
	}

	// Ungroup removes the group and leaves its sessions where they are;
	// undo makes it again.
	r := c.group(ipc.GroupArgs{Do: ipc.GroupUngroup, Group: g})
	if !slices.Equal(groupNames(r), []string{"STEAK 2"}) || len(r.Sessions) != 3 {
		t.Errorf("ungroup = %+v", r)
	}
	rows = c.list()
	if got := names(rows); !slices.Equal(got, []string{"beta", "delta", "gamma", "alpha", "epsilon"}) {
		t.Errorf("after ungroup, order = %v", got)
	}
	if got := inGroup(rows, ""); !slices.Equal(got, []string{"beta", "delta", "gamma", "alpha"}) {
		t.Errorf("after ungroup, in no group: %v", got)
	}
	if u := c.step(dirUndo); u.Label != `ungroup "steak"` {
		t.Errorf("undo label %q", u.Label)
	}
	if got := inGroup(c.list(), g); !slices.Equal(got, []string{"beta", "delta", "gamma"}) {
		t.Errorf("after undoing ungroup, in the group: %v", got)
	}
	if r := c.group(ipc.GroupArgs{}); !slices.Equal(groupNames(r), []string{"STEAK 2", "steak"}) {
		t.Errorf("after undoing ungroup, groups = %v", groupNames(r))
	}
}

func TestJoiningAGroupUnpinsAndUnparksAndUndoPutsThemBack(t *testing.T) {
	c := setup(t)
	id := c.spawnNamed("alpha", "beta", "gamma", "delta")
	c.pin(id["gamma"], true)
	c.park(id["delta"], true)
	g := c.group(ipc.GroupArgs{Do: ipc.GroupNew, Sessions: []string{id["beta"], id["gamma"], id["delta"]}}).Group
	rows := c.list()
	if got := pinned(rows); len(got) != 0 {
		t.Errorf("pinned = %v, want none", got)
	}
	for _, s := range rows {
		if s.Parked {
			t.Errorf("%s is still parked", s.Name)
		}
	}
	if got := inGroup(rows, g); len(got) != 3 {
		t.Errorf("in the group: %v", got)
	}
	c.step(dirUndo)
	rows = c.list()
	if got := pinned(rows); !slices.Equal(got, []string{"gamma"}) {
		t.Errorf("after undo, pinned = %v", got)
	}
	if r, _ := c.row(id["delta"]); !r.Parked {
		t.Error("after undo, delta is not parked again")
	}
	if got := names(rows); !slices.Equal(got, []string{"alpha", "beta", "gamma", "delta"}) {
		t.Errorf("after undo, order = %v", got)
	}

	// Parking one of a group leaves it in the group, so it comes back
	// there when unparked.
	c.step(dirRedo)
	c.park(id["beta"], true)
	if r, _ := c.row(id["beta"]); !r.Parked || r.Group != g {
		t.Errorf("parked beta = %+v", r)
	}
	c.park(id["beta"], false)
	if r, _ := c.row(id["beta"]); r.Parked || r.Group != g {
		t.Errorf("unparked beta = %+v", r)
	}
}

func TestEachSpaceHasItsOwnGroups(t *testing.T) {
	c := setup(t)
	id := c.spawnNamed("alpha", "beta", "gamma")
	g := c.group(ipc.GroupArgs{Do: ipc.GroupNew, Name: "Steak", Sessions: []string{id["alpha"], id["beta"]}}).Group
	other := c.spaces(ipc.SpacesArgs{Do: ipc.SpacesNew, Name: "Gigbot"}).Space
	// One that goes to another space leaves its group, which stays here.
	if err := c.call(ipc.OpSpace, ipc.SpaceArgs{Sessions: []string{id["beta"]}, Space: other}, nil); err != nil {
		t.Fatal(err)
	}
	if r, _ := c.row(id["beta"]); r.Group != "" {
		t.Errorf("beta moved to another space but is still in group %q", r.Group)
	}
	// Undo puts it back in its space and its group.
	c.step(dirUndo)
	if r, _ := c.row(id["beta"]); r.Group != g || r.Space != "" {
		t.Errorf("after undo, beta = space %q, group %q", r.Space, r.Group)
	}
	// The whole group goes with its sessions.
	if err := c.call(ipc.OpSpace, ipc.SpaceArgs{Sessions: []string{id["alpha"], id["beta"]}, Space: other}, nil); err != nil {
		t.Fatal(err)
	}
	if got := inGroup(c.list(), g); !slices.Equal(got, []string{"alpha", "beta"}) {
		t.Errorf("after moving the whole group, in it: %v", got)
	}
}

func TestANewSessionGoesInItsOpenersGroup(t *testing.T) {
	c := setup(t)
	id := c.spawnNamed("alpha", "beta")
	g := c.group(ipc.GroupArgs{Do: ipc.GroupNew, Name: "Steak", Sessions: []string{id["alpha"]}}).Group
	groupOfRow := func(a ipc.SpawnArgs) string {
		t.Helper()
		r, _ := c.row(c.spawnArgs(a).ID)
		return r.Group
	}
	// An agent's spawn goes in its own session's group.
	if got := groupOfRow(ipc.SpawnArgs{Task: "prompt", Name: "from-alpha", After: id["alpha"]}); got != g {
		t.Errorf("from-alpha is in group %q, want %q", got, g)
	}
	// A typed task beside a card goes in its group only with new_tabs =
	// next.
	if got := groupOfRow(ipc.SpawnArgs{Task: "prompt", Name: "typed", After: id["alpha"], Jot: true}); got != "" {
		t.Errorf("a typed task went in group %q", got)
	}
	// A group by name, made when none has it.
	if got := groupOfRow(ipc.SpawnArgs{Task: "prompt", Name: "named", Group: "steak"}); got != g {
		t.Errorf("named is in group %q, want %q", got, g)
	}
	fresh := groupOfRow(ipc.SpawnArgs{Task: "prompt", Name: "fresh", Group: "Gigbot"})
	if r := c.group(ipc.GroupArgs{}); !slices.Equal(groupNames(r), []string{"Steak", "Gigbot"}) || fresh == "" || fresh == g {
		t.Errorf("groups %v, fresh in %q", groupNames(r), fresh)
	}
	// Each joins its group's cards, last in the group.
	rows := c.list()
	if got := inGroup(rows, g); !slices.Equal(got, []string{"alpha", "from-alpha", "named"}) {
		t.Errorf("in the group: %v", got)
	}
	if got := names(rows); !slices.Equal(got, []string{"alpha", "from-alpha", "named", "beta", "typed", "fresh"}) {
		t.Errorf("order = %v, want the group's together", got)
	}
}

func TestANewSessionOfAGroupGoesInItWithNewTabsFirst(t *testing.T) {
	c := setupWith(t, agentConfig{extra: "[ui]\nnew_tabs = \"first\"\n"})
	id := c.spawnNamed("x", "y", "alpha")
	// new_tabs = first: alpha, y, x.
	g := c.group(ipc.GroupArgs{Do: ipc.GroupNew, Sessions: []string{id["x"]}}).Group
	c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Name: "kid", After: id["x"]})
	rows := c.list()
	if got := names(rows); !slices.Equal(got, []string{"alpha", "y", "x", "kid"}) {
		t.Errorf("order = %v, want kid last in x's group, where the group is", got)
	}
	if got := inGroup(rows, g); !slices.Equal(got, []string{"x", "kid"}) {
		t.Errorf("in the group: %v", got)
	}
}

func TestAGroupStaysTogetherAndInPlace(t *testing.T) {
	c := setup(t)
	id := c.spawnNamed("x", "y", "alpha", "beta", "gamma", "delta", "eps")
	g := c.group(ipc.GroupArgs{Do: ipc.GroupNew, Name: "Steak", Sessions: []string{id["beta"], id["gamma"], id["delta"]}}).Group

	// Taking two out at once, then undo and redo, puts them back in their
	// order.
	c.group(ipc.GroupArgs{Do: ipc.GroupRemove, Sessions: []string{id["beta"], id["gamma"]}})
	want := []string{"x", "y", "alpha", "delta", "beta", "gamma", "eps"}
	if got := names(c.list()); !slices.Equal(got, want) {
		t.Errorf("after remove, order = %v", got)
	}
	c.step(dirUndo)
	if got := names(c.list()); !slices.Equal(got, []string{"x", "y", "alpha", "beta", "gamma", "delta", "eps"}) {
		t.Errorf("after undo, order = %v", got)
	}
	c.step(dirRedo)
	if got := names(c.list()); !slices.Equal(got, want) {
		t.Errorf("after redo, order = %v, want %v", got, want)
	}
	c.step(dirUndo)

	// Unpinned, unparked or promoted, a card of a group comes back into
	// it, and the group stays where it is.
	c.pin(id["gamma"], true)
	c.pin(id["gamma"], false)
	c.park(id["beta"], true)
	c.park(id["beta"], false)
	if err := c.call(ipc.OpPriority, ipc.PriorityArgs{Session: id["delta"], Level: 1}, nil); err != nil {
		t.Fatal(err)
	}
	rows := c.list()
	if got := names(rows); !slices.Equal(got, []string{"x", "y", "alpha", "delta", "gamma", "beta", "eps"}) {
		t.Errorf("after unpin, unpark and promote, order = %v", got)
	}
	if got := inGroup(rows, g); !slices.Equal(got, []string{"delta", "gamma", "beta"}) {
		t.Errorf("in the group: %v", got)
	}

	// A new group of one card of another group goes inside that group, as
	// a new folder of a file goes in the file's folder, in the card's
	// place.
	r := c.group(ipc.GroupArgs{Do: ipc.GroupNew, Sessions: []string{id["gamma"]}})
	other := r.Group
	rows = c.list()
	if got := names(rows); !slices.Equal(got, []string{"x", "y", "alpha", "delta", "gamma", "beta", "eps"}) {
		t.Errorf("after a new group of gamma, order = %v", got)
	}
	if got := inGroup(rows, other); !slices.Equal(got, []string{"gamma"}) {
		t.Errorf("in the new group: %v", got)
	}
	if p, _ := parentIn(r, other); p != g {
		t.Errorf("the new group is in %q, want Steak %q", p, g)
	}

	// A renamed group made again by redo keeps its new name.
	c.group(ipc.GroupArgs{Do: ipc.GroupRename, Group: other, Name: "Gigbot"})
	c.step(dirUndo)
	c.step(dirRedo)
	if r := c.group(ipc.GroupArgs{}); !slices.Contains(groupNames(r), "Gigbot") {
		t.Errorf("after undo and redo, groups = %v, want Gigbot kept", groupNames(r))
	}
}

func TestUndoPinsAgainACardThatJoinedItsOwnGroup(t *testing.T) {
	c := setup(t)
	id := c.spawnNamed("alpha", "beta")
	g := c.group(ipc.GroupArgs{Do: ipc.GroupNew, Name: "Steak", Sessions: []string{id["alpha"], id["beta"]}}).Group
	c.pin(id["beta"], true)
	// Dropped on its own group's header, it comes out of Pinned.
	c.group(ipc.GroupArgs{Do: ipc.GroupAdd, Group: g, Sessions: []string{id["beta"]}})
	if r, _ := c.row(id["beta"]); r.Pinned || r.Group != g {
		t.Errorf("after joining its group, beta = pinned %v, group %q", r.Pinned, r.Group)
	}
	c.step(dirUndo)
	if r, _ := c.row(id["beta"]); !r.Pinned || r.Group != g {
		t.Errorf("after undo, beta = pinned %v, group %q", r.Pinned, r.Group)
	}
}

func TestAGroupByNameIsOneOfTheSessionsSpace(t *testing.T) {
	c := setup(t)
	id := c.spawnNamed("alpha", "beta")
	docs := c.group(ipc.GroupArgs{Do: ipc.GroupNew, Name: "Docs", Sessions: []string{id["alpha"]}}).Group
	other := c.spaces(ipc.SpacesArgs{Do: ipc.SpacesNew, Name: "Gigbot"}).Space
	if err := c.call(ipc.OpSpace, ipc.SpaceArgs{Sessions: []string{id["beta"]}, Space: other}, nil); err != nil {
		t.Fatal(err)
	}
	// Docs is the first space's: in the other, the name makes a group of
	// its own, of the same name, as each space names its groups on its
	// own, so the next "docs" there finds it.
	r := c.group(ipc.GroupArgs{Do: ipc.GroupAdd, Name: "docs", Sessions: []string{id["beta"]}})
	if r.Group == docs || !slices.Equal(groupNames(r), []string{"Docs", "docs"}) {
		t.Errorf("beta went in %q; groups %v", r.Group, groupNames(r))
	}
	// Nor can a session join another space's group by its id.
	if err := c.call(ipc.OpGroup, ipc.GroupArgs{Do: ipc.GroupAdd, Group: docs, Sessions: []string{id["beta"]}}, nil); err == nil {
		t.Error("beta joined the other space's group")
	}
}

func TestAReopenedSessionComesBackInItsGroupAndSpace(t *testing.T) {
	c := setup(t)
	id := c.spawnNamed("alpha", "beta", "gamma")
	other := c.spaces(ipc.SpacesArgs{Do: ipc.SpacesNew, Name: "Gigbot"}).Space
	if err := c.call(ipc.OpSpace, ipc.SpaceArgs{Sessions: []string{id["alpha"], id["beta"]}, Space: other}, nil); err != nil {
		t.Fatal(err)
	}
	g := c.group(ipc.GroupArgs{Do: ipc.GroupNew, Name: "Steak", Sessions: []string{id["alpha"], id["beta"]}}).Group
	// Closed, its sessions keep the group for their reopen.
	if err := c.call(ipc.OpClose, ipc.CloseArgs{Sessions: []string{id["alpha"], id["beta"]}}, nil); err != nil {
		t.Fatal(err)
	}
	if r := c.group(ipc.GroupArgs{}); !slices.Equal(groupNames(r), []string{"Steak"}) {
		t.Errorf("with its sessions closed, groups = %v", groupNames(r))
	}
	c.restartCore()
	s, err := c.reopen(id["beta"])
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := c.row(s.ID); r.Group != g || r.Space != other {
		t.Errorf("reopened beta = space %q, group %q; want %q, %q", r.Space, r.Group, other, g)
	}
}

func TestGroupRefusesWhatItCannotDo(t *testing.T) {
	c := setup(t)
	id := c.spawnNamed("alpha")
	for _, a := range []ipc.GroupArgs{
		{Do: ipc.GroupNew},
		{Do: ipc.GroupNew, Sessions: []string{"s000000000000"}},
		{Do: ipc.GroupAdd, Group: "g000000000000", Sessions: []string{id["alpha"]}},
		{Do: ipc.GroupAdd, Sessions: []string{id["alpha"]}},
		{Do: ipc.GroupRename, Group: "g000000000000", Name: "x"},
		{Do: ipc.GroupUngroup, Group: "g000000000000"},
		{Do: "fold"},
	} {
		if err := c.call(ipc.OpGroup, a, nil); err == nil {
			t.Errorf("group %+v worked", a)
		}
	}
	g := c.group(ipc.GroupArgs{Do: ipc.GroupNew, Sessions: []string{id["alpha"]}}).Group
	if err := c.call(ipc.OpGroup, ipc.GroupArgs{Do: ipc.GroupRename, Group: g, Name: "  "}, nil); err == nil {
		t.Error("an empty name worked")
	}
	// Taking out one in no group changes nothing, and is no step.
	n := len(c.history().Undo)
	c.group(ipc.GroupArgs{Do: ipc.GroupRemove, Sessions: []string{id["alpha"]}})
	if r := c.group(ipc.GroupArgs{Do: ipc.GroupRemove, Sessions: []string{id["alpha"]}}); len(r.Sessions) != 0 {
		t.Errorf("a second remove moved %v", r.Sessions)
	}
	if got := len(c.history().Undo); got != n+1 {
		t.Errorf("history grew by %d, want 1", got-n)
	}
}
