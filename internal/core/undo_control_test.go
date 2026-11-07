package core

import (
	"slices"
	"testing"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/tmux"
)

// Rename, freeze, thaw, promote, putting back and reorder are steps of the
// history (DESIGN §4.5), and each round-trips: undo leaves the session as
// it was before the action, and redo as the action left it.

// dropLast leaves the history's newest step out, as an action Irie did not
// record would, and keeps the redo stack as it was before that action.
func (c *coreEnv) dropLast(redo []action) {
	c.t.Helper()
	c.editHistory(func(h *history) { h.Undo, h.Redo = h.Undo[:len(h.Undo)-1], redo })
}

func TestUndoAndRedoOfARename(t *testing.T) {
	c := setup(t)
	a := c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Name: "alpha"})
	if _, err := c.rename(a.ID, "beta"); err != nil {
		t.Fatal(err)
	}
	// Its own name again only pins it, so it is no step.
	if _, err := c.rename(a.ID, "beta"); err != nil {
		t.Fatal(err)
	}
	if h := c.history(); !slices.Equal(labels(h.Undo), []string{`start "alpha"`, `rename "alpha" to "beta"`}) || len(h.Redo) != 0 {
		t.Fatalf("history %v / %v", labels(h.Undo), labels(h.Redo))
	}
	if r := c.step(dirUndo); r.Label != `rename "alpha" to "beta"` {
		t.Errorf("undo %+v", r)
	}
	if s, _ := c.row(a.ID); s.Name != "alpha" || c.pane(s.Pane)[tmux.OptNamed] != "1" {
		t.Errorf("after undo: %q, pane %v; want alpha, the user's", s.Name, c.pane(s.Pane))
	}
	if h := c.history(); !slices.Equal(labels(h.Redo), []string{`rename "alpha" to "beta"`}) {
		t.Errorf("redo stack %v", labels(h.Redo))
	}
	c.step(dirRedo)
	if s, _ := c.row(a.ID); s.Name != "beta" {
		t.Errorf("after redo: %q", s.Name)
	}
	if got, want := c.uiSteps(a.ID), []string{"ui.undo rename open>open", "ui.redo rename open>open"}; !slices.Equal(got, want) {
		t.Errorf("journal %v, want %v", got, want)
	}

	// Undoing the rename of a name Claude Code gave hands the name back to
	// its titles.
	b := c.spawn("prompt")
	if _, err := c.rename(b.ID, "gamma"); err != nil {
		t.Fatal(err)
	}
	c.step(dirUndo)
	if s, _ := c.row(b.ID); s.Name != b.Name || c.pane(s.Pane)[tmux.OptNamed] != "0" {
		t.Errorf("after undo: %q, pane %v; want %q, not pinned", s.Name, c.pane(s.Pane), b.Name)
	}
	c.step(dirRedo)
	if s, _ := c.row(b.ID); s.Name != "gamma" || c.pane(s.Pane)[tmux.OptNamed] != "1" {
		t.Errorf("after redo: %q, pane %v; want gamma, pinned", s.Name, c.pane(s.Pane))
	}
}

// A rename undo asks when the session was renamed since, or when another
// session took the name, and settles when the name is back already.
func TestUndoOfARenameAsksWhenTheNamesChanged(t *testing.T) {
	c := setup(t)
	a := c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Name: "alpha"})
	if _, err := c.rename(a.ID, "beta"); err != nil {
		t.Fatal(err)
	}
	redo := c.history().Redo
	if _, err := c.rename(a.ID, "delta"); err != nil {
		t.Fatal(err)
	}
	c.dropLast(redo)
	b := c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Name: "alpha"})
	c.dropLast(redo)

	ch := c.ask(dirUndo)
	if ch.Question != `"beta" is called "delta" now.` || ch.Options[0].Key != ipc.ChoiceApply || ch.Options[0].Label != `Rename it "alpha" anyway` || !ch.Options[0].Recommended {
		t.Errorf("first question %+v", ch)
	}
	next := c.answer(dirUndo, ch, ipc.ChoiceApply).Choice
	if next == nil || next.Question != `Another session is called "alpha" now.` || next.Options[0].Label != `Rename it "alpha-2"` {
		t.Fatalf("second question %+v", next)
	}
	if r := c.answer(dirUndo, *next, ipc.ChoiceApply); r.Did != ipc.DidUndo {
		t.Errorf("apply %+v", r)
	}
	if s, _ := c.row(a.ID); s.Name != "alpha-2" {
		t.Errorf("after undo: %q", s.Name)
	}
	if s, _ := c.row(b.ID); s.Name != "alpha" {
		t.Errorf("the other row: %q", s.Name)
	}

	// Renamed back by hand, a redo has nothing to do but move on.
	redo = c.history().Redo
	if _, err := c.rename(a.ID, "beta"); err != nil {
		t.Fatal(err)
	}
	c.dropLast(redo)
	ch1 := c.ask(dirRedo)
	if ch1.Question != `"beta" has that name already.` || !slices.Equal(keysOf(ch1), []string{ipc.ChoiceSettle, ipc.ChoiceSkip, ipc.ChoiceCancel}) {
		t.Errorf("question %+v", ch1)
	}
	if r := c.answer(dirRedo, ch1, ipc.ChoiceSettle); r.Did != ipc.DidSettle {
		t.Errorf("settle %+v", r)
	}
}

func TestUndoAndRedoOfAFreezeAndAThaw(t *testing.T) {
	c := setup(t)
	a := c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Name: "alpha"})
	c.waitState(a.ID, ipc.StateWorking)
	c.freeze(a.ID, true)
	c.waitState(a.ID, ipc.StateFrozen)
	// Freezing it again changes nothing, so it is no step.
	c.freeze(a.ID, true)
	if h := c.history(); !slices.Equal(labels(h.Undo), []string{`start "alpha"`, `freeze "alpha"`}) {
		t.Fatalf("history %v", labels(h.Undo))
	}
	if r := c.step(dirUndo); r.Label != `freeze "alpha"` {
		t.Errorf("undo %+v", r)
	}
	c.waitState(a.ID, ipc.StateWorking)
	c.step(dirRedo)
	c.waitState(a.ID, ipc.StateFrozen)

	// A thaw by hand is a step of its own: undoing it freezes again.
	c.freeze(a.ID, false)
	c.waitState(a.ID, ipc.StateWorking)
	if h := c.history(); !slices.Equal(labels(h.Undo), []string{`start "alpha"`, `freeze "alpha"`, `thaw "alpha"`}) {
		t.Fatalf("history %v", labels(h.Undo))
	}
	c.step(dirUndo)
	c.waitState(a.ID, ipc.StateFrozen)
	if got, want := c.uiSteps(a.ID), []string{"ui.undo freeze open>open", "ui.redo freeze open>open", "ui.undo thaw open>open"}; !slices.Equal(got, want) {
		t.Errorf("journal %v, want %v", got, want)
	}

	// Thawed by some other hand, redoing the thaw only moves on.
	redo := c.history().Redo
	var r ipc.FreezeResult
	if err := c.call(ipc.OpThaw, ipc.FreezeArgs{All: true}, &r); err != nil || !slices.Equal(r.Sessions, []string{a.ID}) {
		t.Fatalf("thaw all: %v, %v", r.Sessions, err)
	}
	if h := c.history(); !slices.Equal(labels(h.Redo), labels(redo)) || len(h.Undo) != 2 {
		t.Errorf("thawing them all made a step: %v / %v", labels(h.Undo), labels(h.Redo))
	}
	ch := c.ask(dirRedo)
	if ch.Question != `"alpha" is not frozen any more.` || ch.Options[0].Key != ipc.ChoiceSettle {
		t.Errorf("question %+v", ch)
	}
}

// Undoing a promotion puts the session back where it was, parked if it
// was, and undoing a putting back promotes it again where it is.
func TestUndoOfAPromotionPutsTheSessionBack(t *testing.T) {
	c := setup(t)
	id := map[string]string{}
	for _, n := range []string{"alpha", "beta", "gamma"} {
		id[n] = c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Name: n}).ID
	}
	c.park(id["beta"], true)
	promote := func(n string, level int) {
		t.Helper()
		if err := c.call(ipc.OpPriority, ipc.PriorityArgs{Session: id[n], Level: level}, nil); err != nil {
			t.Fatal(err)
		}
	}
	promote("beta", 1)
	// Promoting it again changes no level, so it is no step.
	promote("beta", 1)
	if h := c.history(); !slices.Equal(labels(h.Undo)[3:], []string{`park "beta"`, `promote "beta"`}) {
		t.Fatalf("history %v", labels(h.Undo))
	}
	rows := c.list()
	if got := names(rows); !slices.Equal(got, []string{"beta", "alpha", "gamma"}) || len(parked(rows)) != 0 {
		t.Fatalf("promoted: %v, parked %v", got, parked(rows))
	}

	c.step(dirUndo)
	rows = c.list()
	if got := names(rows); !slices.Equal(got, []string{"alpha", "beta", "gamma"}) || !slices.Equal(parked(rows), []string{"beta"}) {
		t.Errorf("after undo: %v, parked %v", got, parked(rows))
	}
	if s, _ := c.row(id["beta"]); s.Priority != 0 {
		t.Errorf("after undo, priority %d", s.Priority)
	}
	c.step(dirRedo)
	rows = c.list()
	if got := names(rows); !slices.Equal(got, []string{"beta", "alpha", "gamma"}) || len(parked(rows)) != 0 {
		t.Errorf("after redo: %v, parked %v", got, parked(rows))
	}
	if s, _ := c.row(id["beta"]); s.Priority != 1 {
		t.Errorf("after redo, priority %d", s.Priority)
	}

	// Putting it back moves nothing, and nor does undoing that.
	if err := c.call(ipc.OpReorder, ipc.ReorderArgs{Session: id["beta"], Before: id["gamma"]}, nil); err != nil {
		t.Fatal(err)
	}
	promote("beta", 0)
	if h := c.history(); !slices.Equal(labels(h.Undo)[5:], []string{`reorder "beta"`, `stop promoting "beta"`}) {
		t.Fatalf("history %v", labels(h.Undo))
	}
	c.step(dirUndo)
	if s, _ := c.row(id["beta"]); s.Priority != 1 {
		t.Errorf("after undoing the putting back, priority %d", s.Priority)
	}
	if got := names(c.list()); !slices.Equal(got, []string{"alpha", "beta", "gamma"}) {
		t.Errorf("after undoing the putting back: %v", got)
	}
	c.step(dirRedo)
	if s, _ := c.row(id["beta"]); s.Priority != 0 {
		t.Errorf("after redoing the putting back, priority %d", s.Priority)
	}
}

func TestUndoAndRedoOfAReorder(t *testing.T) {
	c := setup(t)
	id := map[string]string{}
	for _, n := range []string{"alpha", "beta", "gamma"} {
		id[n] = c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Name: n}).ID
	}
	reorder := func(n, before string) {
		t.Helper()
		if err := c.call(ipc.OpReorder, ipc.ReorderArgs{Session: id[n], Before: id[before]}, nil); err != nil {
			t.Fatal(err)
		}
	}
	reorder("alpha", "")
	// Last already, so the same move again moves nothing and is no step.
	reorder("alpha", "")
	if h := c.history(); len(h.Undo) != 4 || h.Undo[3].Label != `reorder "alpha"` {
		t.Fatalf("history %v", labels(h.Undo))
	}
	c.step(dirUndo)
	if got := names(c.list()); !slices.Equal(got, []string{"alpha", "beta", "gamma"}) {
		t.Errorf("after undo: %v", got)
	}
	c.step(dirRedo)
	if got := names(c.list()); !slices.Equal(got, []string{"beta", "gamma", "alpha"}) {
		t.Errorf("after redo: %v", got)
	}

	// The place survives a restart, and when the place was got back by
	// hand, undo only moves on.
	redo := c.history().Redo
	reorder("alpha", "beta")
	c.dropLast(redo)
	ch := c.ask(dirUndo)
	if ch.Question != `"alpha" is in that place already.` || ch.Options[0].Key != ipc.ChoiceSettle {
		t.Errorf("question %+v", ch)
	}
	if r := c.answer(dirUndo, ch, ipc.ChoiceSettle); r.Did != ipc.DidSettle {
		t.Errorf("settle %+v", r)
	}
	c.step(dirRedo)
	if got := names(c.list()); !slices.Equal(got, []string{"beta", "gamma", "alpha"}) {
		t.Errorf("after the last redo: %v", got)
	}

	// A closed session has no place to be put in.
	c.closeSession(id["alpha"])
	c.editHistory(func(h *history) { h.Undo = h.Undo[:4] })
	ch = c.ask(dirUndo)
	if ch.Question != `"alpha" is closed, so there is nothing to reorder.` || ch.Options[0].Key != ipc.ChoiceSkip {
		t.Errorf("question %+v", ch)
	}
}
