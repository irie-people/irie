package core

import (
	"slices"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/ipc"
)

// parentOf is the group that group id is in, as r lists it, and whether r
// lists id at all.
func parentIn(r ipc.GroupResult, id string) (string, bool) {
	for _, g := range r.Groups {
		if g.ID == id {
			return g.Parent, true
		}
	}
	return "", false
}

// groupCall runs a group op that should fail, and says how.
func (c *coreEnv) groupFails(a ipc.GroupArgs) string {
	c.t.Helper()
	err := c.call(ipc.OpGroup, a, nil)
	if err == nil {
		c.t.Fatalf("group %+v worked", a)
	}
	return err.Error()
}

func TestGroupsHoldGroupsAndKeepTheirSessionsTogether(t *testing.T) {
	c := setup(t)
	id := c.spawnNamed("alpha", "beta", "gamma", "delta", "epsilon")
	irie := c.group(ipc.GroupArgs{Do: ipc.GroupNew, Name: "Irie", Sessions: []string{id["beta"], id["gamma"]}}).Group

	// A new group around a group holds it where it is, as one step.
	r := c.group(ipc.GroupArgs{Do: ipc.GroupNew, Name: "Work", Groups: []string{irie}})
	work := r.Group
	if p, _ := parentIn(r, irie); p != work {
		t.Fatalf("Irie is in %q, want Work %q", p, work)
	}
	if p, _ := parentIn(r, work); p != "" {
		t.Errorf("Work is in %q, want the top", p)
	}
	if got := names(c.list()); !slices.Equal(got, []string{"alpha", "beta", "gamma", "delta", "epsilon"}) {
		t.Errorf("order = %v, want it as it was", got)
	}
	if h := c.history(); labels(h.Undo)[len(h.Undo)-1] != `move "Irie" into "Work"` {
		t.Errorf("history %v", labels(h.Undo))
	}

	// A session that joins Work goes last in it, after Irie's sessions.
	c.group(ipc.GroupArgs{Do: ipc.GroupAdd, Group: work, Sessions: []string{id["alpha"]}})
	rows := c.list()
	if got := names(rows); !slices.Equal(got, []string{"beta", "gamma", "alpha", "delta", "epsilon"}) {
		t.Errorf("after alpha joined Work, order = %v", got)
	}
	// One dropped in Irie, before gamma, goes there.
	c.group(ipc.GroupArgs{Do: ipc.GroupMove, Into: irie, Before: id["gamma"], Sessions: []string{id["epsilon"]}})
	rows = c.list()
	if got := names(rows); !slices.Equal(got, []string{"beta", "epsilon", "gamma", "alpha", "delta"}) {
		t.Errorf("after epsilon went in Irie, order = %v", got)
	}
	if got := inGroup(rows, irie); !slices.Equal(got, []string{"beta", "epsilon", "gamma"}) {
		t.Errorf("in Irie: %v", got)
	}

	// A path makes each group of it that is missing, inside the one
	// before it, and the session goes in with the group's sessions.
	r = c.group(ipc.GroupArgs{Do: ipc.GroupAdd, Name: "work / irie/Deep", Sessions: []string{id["delta"]}})
	deep := r.Group
	if p, _ := parentIn(r, deep); p != irie {
		t.Fatalf("Deep is in %q, want Irie", p)
	}
	if got := names(c.list()); !slices.Equal(got, []string{"beta", "epsilon", "gamma", "delta", "alpha"}) {
		t.Errorf("after delta went in Deep, order = %v", got)
	}
	if got := ipc.GroupTitle(r.Groups, deep, "/"); got != "Work/Irie/Deep" {
		t.Errorf("Deep's path = %q", got)
	}

	// A restarted core keeps them.
	c.restartCore()
	r = c.group(ipc.GroupArgs{})
	if p, _ := parentIn(r, deep); p != irie {
		t.Errorf("after a restart, Deep is in %q", p)
	}
	if p, _ := parentIn(r, irie); p != work {
		t.Errorf("after a restart, Irie is in %q", p)
	}

	// Ungroup puts what Irie held in Work, where it is, and one undo
	// makes Irie again, around all of it.
	r = c.group(ipc.GroupArgs{Do: ipc.GroupUngroup, Group: irie})
	if _, ok := parentIn(r, irie); ok {
		t.Errorf("Irie is still there: %+v", r.Groups)
	}
	if p, _ := parentIn(r, deep); p != work {
		t.Errorf("after ungrouping Irie, Deep is in %q, want Work", p)
	}
	rows = c.list()
	if got := names(rows); !slices.Equal(got, []string{"beta", "epsilon", "gamma", "delta", "alpha"}) {
		t.Errorf("after ungrouping Irie, order = %v", got)
	}
	if got := inGroup(rows, work); !slices.Equal(got, []string{"beta", "epsilon", "gamma", "alpha"}) {
		t.Errorf("after ungrouping Irie, in Work: %v", got)
	}
	if u := c.step(dirUndo); u.Label != `ungroup "Irie"` {
		t.Errorf("undo label %q", u.Label)
	}
	r = c.group(ipc.GroupArgs{})
	if p, _ := parentIn(r, irie); p != work {
		t.Errorf("after undo, Irie is in %q", p)
	}
	if p, _ := parentIn(r, deep); p != irie {
		t.Errorf("after undo, Deep is in %q", p)
	}
	if got := inGroup(c.list(), irie); !slices.Equal(got, []string{"beta", "epsilon", "gamma"}) {
		t.Errorf("after undo, in Irie: %v", got)
	}
}

func TestADraggedGroupGoesWithItsSessionsAndUndoPutsItBack(t *testing.T) {
	c := setup(t)
	id := c.spawnNamed("a", "b", "c", "d", "e")
	x := c.group(ipc.GroupArgs{Do: ipc.GroupNew, Name: "X", Sessions: []string{id["b"], id["c"]}}).Group
	y := c.group(ipc.GroupArgs{Do: ipc.GroupNew, Name: "Y", Sessions: []string{id["e"]}}).Group

	// Dropped on Y's header, X goes last in Y, its sessions with it.
	r := c.group(ipc.GroupArgs{Do: ipc.GroupMove, Groups: []string{x}, Into: y})
	if p, _ := parentIn(r, x); p != y {
		t.Fatalf("X is in %q, want Y", p)
	}
	if got := names(c.list()); !slices.Equal(got, []string{"a", "d", "e", "b", "c"}) {
		t.Errorf("after X went in Y, order = %v", got)
	}
	if u := c.step(dirUndo); u.Label != `move "X" into "Y"` {
		t.Errorf("undo label %q", u.Label)
	}
	r = c.group(ipc.GroupArgs{})
	if p, _ := parentIn(r, x); p != "" {
		t.Errorf("after undo, X is in %q", p)
	}
	if got := names(c.list()); !slices.Equal(got, []string{"a", "b", "c", "d", "e"}) {
		t.Errorf("after undo, order = %v", got)
	}
	c.step(dirRedo)
	if got := names(c.list()); !slices.Equal(got, []string{"a", "d", "e", "b", "c"}) {
		t.Errorf("after redo, order = %v", got)
	}

	// Dropped on a card at the top of the list, it comes out of Y, in
	// that card's place.
	r = c.group(ipc.GroupArgs{Do: ipc.GroupMove, Groups: []string{x}, Before: id["a"]})
	if p, _ := parentIn(r, x); p != "" {
		t.Errorf("X is in %q, want the top", p)
	}
	if got := names(c.list()); !slices.Equal(got, []string{"b", "c", "a", "d", "e"}) {
		t.Errorf("after X went before a, order = %v", got)
	}
	// Among the groups beside it, a group moves as one: before Y's first
	// card means before Y.
	c.group(ipc.GroupArgs{Do: ipc.GroupMove, Groups: []string{x}, After: id["e"]})
	if got := names(c.list()); !slices.Equal(got, []string{"a", "d", "e", "b", "c"}) {
		t.Errorf("after X went after e, order = %v", got)
	}
	if r := c.group(ipc.GroupArgs{}); func() string { p, _ := parentIn(r, x); return p }() != "" {
		t.Error("X dropped after a top card of Y's went in Y; a drop's group is Into's")
	}

	// A group never goes inside itself.
	c.group(ipc.GroupArgs{Do: ipc.GroupMove, Groups: []string{x}, Into: y})
	if e := c.groupFails(ipc.GroupArgs{Do: ipc.GroupMove, Groups: []string{y}, Into: x}); !strings.Contains(e, "inside itself") {
		t.Errorf("Y into X: %s", e)
	}
	if e := c.groupFails(ipc.GroupArgs{Do: ipc.GroupMove, Groups: []string{x}, Into: x}); !strings.Contains(e, "inside itself") {
		t.Errorf("X into X: %s", e)
	}
	c.groupFails(ipc.GroupArgs{Do: ipc.GroupMove, Groups: []string{"nowhere"}})
	c.groupFails(ipc.GroupArgs{Do: ipc.GroupMove, Groups: []string{x}, Into: "nowhere"})
}

func TestRemoveTakesASessionOneGroupOut(t *testing.T) {
	c := setup(t)
	id := c.spawnNamed("a", "b", "c", "d")
	inner := c.group(ipc.GroupArgs{Do: ipc.GroupAdd, Name: "Outer/Inner", Sessions: []string{id["a"], id["b"]}}).Group
	r := c.group(ipc.GroupArgs{Do: ipc.GroupAdd, Name: "Outer", Sessions: []string{id["c"]}})
	outer := r.Group
	if p, _ := parentIn(r, inner); p != outer {
		t.Fatalf("Inner is in %q, want Outer", p)
	}
	// a leaves Inner for Outer, just after Inner.
	c.group(ipc.GroupArgs{Do: ipc.GroupRemove, Sessions: []string{id["a"]}})
	rows := c.list()
	if got := names(rows); !slices.Equal(got, []string{"b", "a", "c", "d"}) {
		t.Errorf("order = %v", got)
	}
	if row, _ := c.row(id["a"]); row.Group != outer {
		t.Errorf("a is in %q, want Outer", row.Group)
	}
	// Again, and it is in no group, just after Outer.
	c.group(ipc.GroupArgs{Do: ipc.GroupRemove, Sessions: []string{id["a"]}})
	if got := names(c.list()); !slices.Equal(got, []string{"b", "c", "a", "d"}) {
		t.Errorf("order = %v", got)
	}
	if row, _ := c.row(id["a"]); row.Group != "" {
		t.Errorf("a is in %q, want none", row.Group)
	}
	// Undo puts it back in Outer, then in Inner, where it was.
	c.step(dirUndo)
	c.step(dirUndo)
	if row, _ := c.row(id["a"]); row.Group != inner {
		t.Errorf("after two undos, a is in %q, want Inner", row.Group)
	}
	if got := names(c.list()); !slices.Equal(got, []string{"a", "b", "c", "d"}) {
		t.Errorf("after two undos, order = %v", got)
	}
}

func TestGroupNamesAreTheirFoldersOwn(t *testing.T) {
	c := setup(t)
	id := c.spawnNamed("a", "b", "c")
	notesA := c.group(ipc.GroupArgs{Do: ipc.GroupAdd, Name: "Work/Notes", Sessions: []string{id["a"]}}).Group
	notesB := c.group(ipc.GroupArgs{Do: ipc.GroupAdd, Name: "Home/Notes", Sessions: []string{id["b"]}}).Group
	r := c.group(ipc.GroupArgs{})
	if notesA == notesB || ipc.GroupTitle(r.Groups, notesB, "/") != "Home/Notes" {
		t.Fatalf("groups %+v", r.Groups)
	}
	// Moved beside another of its name, it is numbered.
	work := r.Groups[ipc.FindGroup(r.Groups, "Work")].ID
	r = c.group(ipc.GroupArgs{Do: ipc.GroupMove, Groups: []string{notesB}, Into: work})
	if got := ipc.GroupTitle(r.Groups, notesB, "/"); got != "Work/Notes 2" {
		t.Errorf("moved into Work, it is %q", got)
	}
	// A bare name finds the one nearest the top; a path finds any.
	c.group(ipc.GroupArgs{Do: ipc.GroupAdd, Name: "Notes", Sessions: []string{id["c"]}})
	if row, _ := c.row(id["c"]); row.Group != notesA {
		t.Errorf("c went in %q, want Work/Notes %q", row.Group, notesA)
	}
	if i := ipc.FindGroup(r.Groups, "work/notes 2"); i < 0 || r.Groups[i].ID != notesB {
		t.Errorf("work/notes 2 found %d", i)
	}
	// A spawn names its group by path.
	s := c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Name: "spawned", Group: "Work/Notes 2/Drafts"})
	r = c.group(ipc.GroupArgs{})
	if row, _ := c.row(s.ID); ipc.GroupTitle(r.Groups, row.Group, "/") != "Work/Notes 2/Drafts" {
		t.Errorf("spawned is in %q", ipc.GroupTitle(r.Groups, row.Group, "/"))
	}
}

func TestGroupsGoEightDeepAtMost(t *testing.T) {
	c := setup(t)
	id := c.spawnNamed("a", "b")
	if e := c.groupFails(ipc.GroupArgs{Do: ipc.GroupAdd, Name: "1/2/3/4/5/6/7/8/9", Sessions: []string{id["a"]}}); !strings.Contains(e, "deep") {
		t.Errorf("nine deep: %s", e)
	}
	deep := c.group(ipc.GroupArgs{Do: ipc.GroupAdd, Name: "1/2/3/4/5/6/7/8", Sessions: []string{id["a"]}}).Group
	other := c.group(ipc.GroupArgs{Do: ipc.GroupNew, Name: "Other", Sessions: []string{id["b"]}}).Group
	if e := c.groupFails(ipc.GroupArgs{Do: ipc.GroupMove, Groups: []string{other}, Into: deep}); !strings.Contains(e, "deep") {
		t.Errorf("Other into the eighth: %s", e)
	}
	if e := c.groupFails(ipc.GroupArgs{Do: ipc.GroupNew, Sessions: []string{id["a"]}}); !strings.Contains(e, "deep") {
		t.Errorf("a new group in the eighth: %s", e)
	}
}

func TestAWholeGroupSentToAnotherSpaceComesOutOfItsGroup(t *testing.T) {
	c := setup(t)
	id := c.spawnNamed("a", "b", "c")
	inner := c.group(ipc.GroupArgs{Do: ipc.GroupAdd, Name: "Outer/Inner", Sessions: []string{id["a"], id["b"]}}).Group
	outer := c.group(ipc.GroupArgs{Do: ipc.GroupAdd, Name: "Outer", Sessions: []string{id["c"]}}).Group
	other := c.spaces(ipc.SpacesArgs{Do: ipc.SpacesNew, Name: "Elsewhere"}).Space
	if err := c.call(ipc.OpSpace, ipc.SpaceArgs{Sessions: []string{id["a"], id["b"]}, Space: other}, nil); err != nil {
		t.Fatal(err)
	}
	r := c.group(ipc.GroupArgs{})
	if p, _ := parentIn(r, inner); p != "" {
		t.Errorf("Inner went to another space but is still in %q", p)
	}
	if got := inGroup(c.list(), inner); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("in Inner: %v", got)
	}
	// One undo puts both back, and Inner back in Outer.
	c.step(dirUndo)
	r = c.group(ipc.GroupArgs{})
	if p, _ := parentIn(r, inner); p != outer {
		t.Errorf("after undo, Inner is in %q, want Outer", p)
	}
	if row, _ := c.row(id["a"]); row.Space != "" || row.Group != inner {
		t.Errorf("after undo, a = space %q, group %q", row.Space, row.Group)
	}
}

func TestADropInItsOwnGroupMovesACardThereAndARenameRelabelsTheHistory(t *testing.T) {
	c := setup(t)
	id := c.spawnNamed("a", "b", "c")
	sub := c.group(ipc.GroupArgs{Do: ipc.GroupAdd, Name: "P/H", Sessions: []string{id["b"], id["c"]}}).Group
	p := c.group(ipc.GroupArgs{Do: ipc.GroupAdd, Name: "P", Sessions: []string{id["a"]}}).Group
	if got := names(c.list()); !slices.Equal(got, []string{"b", "c", "a"}) {
		t.Fatalf("order = %v", got)
	}
	// a, in P already, dropped on H's top edge goes just before H.
	c.group(ipc.GroupArgs{Do: ipc.GroupMove, Into: p, Before: id["b"], Sessions: []string{id["a"]}})
	if got := names(c.list()); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Errorf("after the drop, order = %v", got)
	}
	if u := c.step(dirUndo); u.Label != `reorder "a"` {
		t.Errorf("undo label %q", u.Label)
	}
	if got := names(c.list()); !slices.Equal(got, []string{"b", "c", "a"}) {
		t.Errorf("after undo, order = %v", got)
	}
	// A group's step names it as it is named now.
	c.group(ipc.GroupArgs{Do: ipc.GroupMove, Groups: []string{sub}})
	c.group(ipc.GroupArgs{Do: ipc.GroupRename, Group: sub, Name: "Renamed"})
	if h := c.history(); labels(h.Undo)[len(h.Undo)-1] != `move "Renamed" to the top` {
		t.Errorf("history %v", labels(h.Undo))
	}
}
