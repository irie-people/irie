package core

import (
	"slices"
	"testing"

	"github.com/irie-people/irie/internal/ipc"
)

// pinned are the names of the pinned rows, in the list's order.
func pinned(rows []ipc.Session) []string {
	var out []string
	for _, s := range rows {
		if s.Pinned {
			out = append(out, s.Name)
		}
	}
	return out
}

// unpinnedHere are the names of the rows neither pinned nor parked, in the
// list's order: the rest, as the sidebars draw them between Pinned and
// Parked.
func unpinnedHere(rows []ipc.Session) []string {
	var out []string
	for _, s := range rows {
		if !s.Pinned && !s.Parked {
			out = append(out, s.Name)
		}
	}
	return out
}

func (c *coreEnv) pin(id string, on bool) {
	c.t.Helper()
	op := ipc.OpUnpin
	if on {
		op = ipc.OpPin
	}
	if err := c.call(op, ipc.ParkArgs{Session: id}, nil); err != nil {
		c.t.Fatalf("%s %s: %v", op, id, err)
	}
}

func TestPinPutsASessionInPinnedAndParkingItUnpinsIt(t *testing.T) {
	c := setup(t)
	id := map[string]string{}
	for _, n := range []string{"alpha", "beta", "gamma", "delta"} {
		id[n] = c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Name: n}).ID
	}
	// Pinned in turn, the last pinned last; pinning twice is no step.
	c.pin(id["gamma"], true)
	c.pin(id["alpha"], true)
	c.pin(id["alpha"], true)
	rows := c.list()
	if got := pinned(rows); !slices.Equal(got, []string{"gamma", "alpha"}) {
		t.Errorf("pinned = %v, want gamma then alpha", got)
	}
	if got := unpinnedHere(rows); !slices.Equal(got, []string{"beta", "delta"}) {
		t.Errorf("the rest = %v", got)
	}
	if h := c.history(); !slices.Equal(labels(h.Undo)[4:], []string{`pin "gamma"`, `pin "alpha"`}) {
		t.Errorf("history %v", labels(h.Undo))
	}
	c.restartCore()
	if got := pinned(c.list()); !slices.Equal(got, []string{"gamma", "alpha"}) {
		t.Errorf("after a restart, pinned = %v", got)
	}

	// Unpinned, alpha comes first among the rest, just under Pinned.
	c.pin(id["alpha"], false)
	if got := unpinnedHere(c.list()); !slices.Equal(got, []string{"alpha", "beta", "delta"}) {
		t.Errorf("after unpinning alpha, the rest = %v", got)
	}

	// Parking a pinned session unpins it, and undoing the park pins it
	// again.
	c.park(id["gamma"], true)
	rows = c.list()
	if got := pinned(rows); len(got) != 0 {
		t.Errorf("after parking gamma, pinned = %v", got)
	}
	if got := parked(rows); !slices.Equal(got, []string{"gamma"}) {
		t.Errorf("after parking gamma, parked = %v", got)
	}
	c.step(dirUndo)
	rows = c.list()
	if got, p := pinned(rows), parked(rows); !slices.Equal(got, []string{"gamma"}) || len(p) != 0 {
		t.Errorf("after undoing the park, pinned = %v, parked = %v", got, p)
	}

	// Pinning a parked session unparks it, and undoing the pin parks it
	// again.
	c.park(id["delta"], true)
	c.pin(id["delta"], true)
	rows = c.list()
	if got, p := pinned(rows), parked(rows); !slices.Equal(got, []string{"gamma", "delta"}) || len(p) != 0 {
		t.Errorf("after pinning a parked delta, pinned = %v, parked = %v", got, p)
	}
	if r := c.step(dirUndo); r.Label != `pin "delta"` {
		t.Errorf("undo %+v", r)
	}
	rows = c.list()
	if got, p := pinned(rows), parked(rows); !slices.Equal(got, []string{"gamma"}) || !slices.Equal(p, []string{"delta"}) {
		t.Errorf("after undoing the pin, pinned = %v, parked = %v", got, p)
	}
	for _, op := range []string{ipc.OpPin, ipc.OpUnpin} {
		if err := c.call(op, ipc.ParkArgs{Session: "s000000000000"}, nil); err == nil {
			t.Errorf("%s of no session went through", op)
		}
	}
}

func TestClosingSeveralIsOneStepThatOneUndoTakesBack(t *testing.T) {
	c := setup(t)
	id := map[string]string{}
	for _, n := range []string{"alpha", "beta", "gamma"} {
		id[n] = c.spawnArgs(ipc.SpawnArgs{Task: "prompt,stop", Name: n}).ID
	}
	// Each got going, so reopen carries its conversation on.
	for _, n := range []string{"alpha", "beta", "gamma"} {
		c.waitState(id[n], ipc.StateDone)
	}
	var r ipc.CloseResult
	if err := c.call(ipc.OpClose, ipc.CloseArgs{Sessions: []string{id["gamma"], id["alpha"]}}, &r); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.Sessions, []string{id["alpha"], id["gamma"]}) {
		t.Errorf("closed %v, want alpha then gamma, in the list's order", r.Sessions)
	}
	if got := names(c.list()); !slices.Equal(got, []string{"beta"}) {
		t.Errorf("open after the close: %v", got)
	}
	h := c.history()
	u, rd := h.labels()
	if u != "close 2 sessions" || rd != "" {
		t.Errorf("labels %q / %q, want close 2 sessions", u, rd)
	}
	// One undo reopens both, in their places.
	if res := c.step(dirUndo); res.Label != "close 2 sessions" || res.Did != ipc.DidUndo {
		t.Errorf("undo %+v", res)
	}
	if got := names(c.list()); !slices.Equal(got, []string{"alpha", "beta", "gamma"}) {
		t.Errorf("open after the undo: %v", got)
	}
	h = c.history()
	if u, rd := h.labels(); rd != "close 2 sessions" || u != `start "gamma"` {
		t.Errorf("labels after the undo %q / %q", u, rd)
	}
	// One redo closes both again.
	if res := c.step(dirRedo); res.Label != "close 2 sessions" || res.Did != ipc.DidRedo {
		t.Errorf("redo %+v", res)
	}
	if got := names(c.list()); !slices.Equal(got, []string{"beta"}) {
		t.Errorf("open after the redo: %v", got)
	}
}

func TestCloseFinishedLeavesPinnedParkedAndWorkingSessions(t *testing.T) {
	c := setup(t)
	id := map[string]string{}
	for _, n := range []string{"done1", "done2", "kept", "put-by"} {
		id[n] = c.spawnArgs(ipc.SpawnArgs{Task: "prompt,stop", Name: n}).ID
	}
	working := c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Name: "busy"})
	for _, n := range []string{"done1", "done2", "kept", "put-by"} {
		c.waitState(id[n], ipc.StateDone)
	}
	c.pin(id["kept"], true)
	c.park(id["put-by"], true)
	var r ipc.CloseResult
	if err := c.call(ipc.OpClose, ipc.CloseArgs{Finished: true}, &r); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.Sessions, []string{id["done1"], id["done2"]}) {
		t.Errorf("closed %v, want done1 and done2", r.Sessions)
	}
	rows := c.list()
	if got := names(rows); !slices.Contains(got, "kept") || !slices.Contains(got, "put-by") || !slices.Contains(got, working.Name) {
		t.Errorf("open after closing the finished: %v", got)
	}
	// Nothing left finished: a close of none is no step.
	before := len(c.history().Undo)
	if err := c.call(ipc.OpClose, ipc.CloseArgs{Finished: true}, &r); err != nil || len(r.Sessions) != 0 {
		t.Errorf("a second close of the finished: %v, %v", r.Sessions, err)
	}
	if n := len(c.history().Undo); n != before {
		t.Errorf("a close of none recorded %d steps", n-before)
	}
}

func TestNewTabsPutsANewSessionNextToTheOneThatStartedIt(t *testing.T) {
	c := setupWith(t, agentConfig{extra: "[ui]\nnew_tabs = \"next\"\n"})
	id := map[string]string{}
	for _, n := range []string{"alpha", "beta", "gamma"} {
		id[n] = c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Name: n}).ID
	}
	c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Name: "from-alpha", After: id["alpha"]})
	if got := names(c.list()); !slices.Equal(got, []string{"alpha", "from-alpha", "beta", "gamma"}) {
		t.Errorf("order = %v, want from-alpha just after alpha", got)
	}
	// From a pinned session, a new one goes first among the rest.
	c.pin(id["gamma"], true)
	c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Name: "from-gamma", After: id["gamma"]})
	if got := unpinnedHere(c.list()); !slices.Equal(got, []string{"from-gamma", "alpha", "from-alpha", "beta"}) {
		t.Errorf("the rest = %v, want from-gamma first", got)
	}
	// With nothing that started it, it goes last.
	c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Name: "on-its-own"})
	if got := unpinnedHere(c.list()); got[len(got)-1] != "on-its-own" {
		t.Errorf("the rest = %v, want on-its-own last", got)
	}
}

func TestNewTabsFirstPutsANewSessionJustUnderPinned(t *testing.T) {
	c := setupWith(t, agentConfig{extra: "[ui]\nnew_tabs = \"first\"\n"})
	a := c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Name: "alpha"})
	c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Name: "beta"})
	c.pin(a.ID, true)
	c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Name: "gamma"})
	rows := c.list()
	if got := unpinnedHere(rows); !slices.Equal(got, []string{"gamma", "beta"}) {
		t.Errorf("the rest = %v, want gamma first", got)
	}
	if got := pinned(rows); !slices.Equal(got, []string{"alpha"}) {
		t.Errorf("pinned = %v", got)
	}
}
