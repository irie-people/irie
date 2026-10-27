package core

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/irie-hq/irie/internal/event"
	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/testutil"
)

func (c *coreEnv) undoOp(dir string, a ipc.UndoArgs) (ipc.UndoResult, error) {
	var r ipc.UndoResult
	err := c.call(dir, a, &r)
	return r, err
}

// step undoes or redoes, by dir, and fails the test unless it went ahead.
func (c *coreEnv) step(dir string) ipc.UndoResult {
	c.t.Helper()
	r, err := c.undoOp(dir, ipc.UndoArgs{})
	if err != nil || r.Did != map[string]string{dirUndo: ipc.DidUndo, dirRedo: ipc.DidRedo}[dir] {
		c.t.Fatalf("%s = %+v, %v; asked %+v", dir, r, err, r.Choice)
	}
	return r
}

// ask undoes or redoes, by dir, and fails the test unless it asked.
func (c *coreEnv) ask(dir string) ipc.Choice {
	c.t.Helper()
	r, err := c.undoOp(dir, ipc.UndoArgs{})
	if err != nil || r.Did != ipc.DidAsk || r.Choice == nil {
		c.t.Fatalf("%s = %+v, %v; want a question", dir, r, err)
	}
	return *r.Choice
}

// answer answers a question with key.
func (c *coreEnv) answer(dir string, ch ipc.Choice, key string) ipc.UndoResult {
	c.t.Helper()
	r, err := c.undoOp(dir, ipc.UndoArgs{ChoiceID: ch.ID, Choice: key})
	if err != nil {
		c.t.Fatalf("%s %s: %v", dir, key, err)
	}
	return r
}

// history reads the history from state.json.
func (c *coreEnv) history() history {
	c.t.Helper()
	st, err := readState(c.paths.StateFile())
	if err != nil {
		c.t.Fatal(err)
	}
	return st.hist
}

// editHistory changes state.json, as something other than Irie's own
// actions might leave it, and restarts the core to read it.
func (c *coreEnv) editHistory(f func(h *history)) {
	c.t.Helper()
	st, err := readState(c.paths.StateFile())
	if err != nil {
		c.t.Fatal(err)
	}
	f(&st.hist)
	if err := writeState(c.paths.StateFile(), st); err != nil {
		c.t.Fatal(err)
	}
	c.restartCore()
}

func labels(list []action) []string {
	out := []string{}
	for _, a := range list {
		out = append(out, a.Label)
	}
	return out
}

func keysOf(ch ipc.Choice) []string {
	var out []string
	for _, o := range ch.Options {
		out = append(out, o.Key)
	}
	return out
}

// sameSession reports whether a reopened row is the row that went: the same
// id, name, age, place, profile and conversation.
func sameSession(t *testing.T, got, want ipc.Session) {
	t.Helper()
	if got.ID != want.ID || got.Name != want.Name || !got.Created.Equal(want.Created) || got.Cwd != want.Cwd ||
		got.Worktree != want.Worktree || got.Branch != want.Branch || got.Profile != want.Profile || got.VendorID != want.VendorID {
		t.Errorf("came back as %+v, want %+v", got, want)
	}
}

func (c *coreEnv) uiSteps(id string) []string {
	var out []string
	for _, e := range c.events(id) {
		if e.Kind == event.UIUndo || e.Kind == event.UIRedo {
			out = append(out, fmt.Sprintf("%s %s %s>%s", e.Kind, e.Str("action"), e.Str("before"), e.Str("after")))
		}
	}
	return out
}

func TestUndoAndRedoOfAStartRoundTrip(t *testing.T) {
	c := setup(t)
	a, b := c.spawn("prompt,stop"), c.spawn("prompt,stop")
	c.waitState(a.ID, ipc.StateDone)
	c.waitState(b.ID, ipc.StateDone)
	if h := c.history(); !slices.Equal(labels(h.Undo), []string{`start "Prompt,stop"`, `start "Prompt,stop-2"`}) || len(h.Redo) != 0 {
		t.Fatalf("history %+v", h)
	}

	// Undo closes the newest first, by id.
	if r := c.step(dirUndo); r.Label != `start "Prompt,stop-2"` {
		t.Errorf("undo %+v", r)
	}
	c.step(dirUndo)
	if ls := c.list(); len(ls) != 0 {
		t.Errorf("rows after two undos: %+v", ls)
	}
	if r := c.listAll(); len(r.Closed) != 2 || r.Closed[0].ID != a.ID {
		t.Errorf("closed %+v", r.Closed)
	}
	// Undo and redo record nothing new and clear nothing.
	if h := c.history(); len(h.Undo) != 0 || !slices.Equal(labels(h.Redo), []string{`start "Prompt,stop-2"`, `start "Prompt,stop"`}) {
		t.Errorf("history after two undos %+v", h)
	}
	if _, err := c.undoOp(dirUndo, ipc.UndoArgs{}); code(err) != ipc.CodeNotFound {
		t.Errorf("undo with nothing to undo: %v", err)
	}

	// Redo reopens them as they were, in their own rows, once each old agent
	// has ended.
	c.step(dirRedo)
	c.step(dirRedo)
	ls := c.list()
	if len(ls) != 2 {
		t.Fatalf("rows after two redos: %+v", ls)
	}
	sameSession(t, ls[0], a)
	sameSession(t, ls[1], b)
	for _, s := range []ipc.Session{a, b} {
		c.waitState(s.ID, ipc.StateIdle)
		var kinds []string
		for _, l := range c.agentLog(s.ID) {
			if l["t"] == "start" || l["t"] == "exit" {
				kinds = append(kinds, fmt.Sprint(l["t"]))
			}
		}
		if !slices.Equal(kinds, []string{"start", "exit", "start"}) {
			t.Errorf("%s's agent ran %v: two must never share a conversation", s.Name, kinds)
		}
	}
	if h := c.history(); !slices.Equal(labels(h.Undo), []string{`start "Prompt,stop"`, `start "Prompt,stop-2"`}) || len(h.Redo) != 0 {
		t.Errorf("history after two redos %+v", h)
	}
	if got, want := c.uiSteps(a.ID), []string{"ui.undo spawn open>closed", "ui.redo spawn closed>open"}; !slices.Equal(got, want) {
		t.Errorf("journal %v, want %v", got, want)
	}

	// And round again: the state is the same each time.
	c.step(dirUndo)
	c.step(dirRedo)
	if s, ok := c.row(b.ID); !ok {
		t.Error("the second round lost the row")
	} else {
		sameSession(t, s, b)
	}
}

func TestUndoAndRedoOfACloseRoundTripAcrossARestart(t *testing.T) {
	c := setup(t)
	a := c.spawn("prompt,stop")
	c.waitState(a.ID, ipc.StateDone)
	c.closeSession(a.ID)
	if h := c.history(); !slices.Equal(labels(h.Undo), []string{`start "Prompt,stop"`, `close "Prompt,stop"`}) {
		t.Fatalf("history %+v", h)
	}
	c.restartCore()

	if r := c.step(dirUndo); r.Label != `close "Prompt,stop"` {
		t.Errorf("undo %+v", r)
	}
	s, ok := c.row(a.ID)
	if !ok {
		t.Fatal("undoing the close did not reopen it")
	}
	sameSession(t, s, a)
	c.step(dirRedo)
	if _, ok := c.row(a.ID); ok {
		t.Error("redoing the close left the row")
	}
	if r := c.listAll(); len(r.Closed) != 1 || r.Closed[0].ID != a.ID || r.Closed[0].Name != a.Name {
		t.Errorf("closed %+v", r.Closed)
	}
	if h := c.history(); !slices.Equal(labels(h.Undo), []string{`start "Prompt,stop"`, `close "Prompt,stop"`}) || len(h.Redo) != 0 {
		t.Errorf("history %+v", h)
	}
	if got, want := c.uiSteps(a.ID), []string{"ui.undo close closed>open", "ui.redo close open>closed"}; !slices.Equal(got, want) {
		t.Errorf("journal %v, want %v", got, want)
	}

	// A reopen by hand, as :reopen does, is a step of its own: it clears
	// what could be redone, and undo closes the session again.
	c.step(dirUndo)
	c.closeSession(a.ID)
	if h := c.history(); len(h.Redo) != 0 || h.Undo[len(h.Undo)-1].Label != `close "Prompt,stop"` {
		t.Errorf("after a close by hand %+v", h)
	}
	if _, err := c.reopen(""); err != nil {
		t.Fatal(err)
	}
	if h := c.history(); !slices.Equal(labels(h.Undo), []string{`start "Prompt,stop"`, `close "Prompt,stop"`, `reopen "Prompt,stop"`}) || len(h.Redo) != 0 {
		t.Errorf("after a reopen by hand %v / %v", labels(h.Undo), labels(h.Redo))
	}
	c.waitState(a.ID, ipc.StateIdle)
	c.step(dirUndo)
	if _, ok := c.row(a.ID); ok {
		t.Error("undoing the reopen left the row")
	}
}

func TestUndoAsksBeforeStoppingAnAgentAtWork(t *testing.T) {
	c := setup(t)
	a := c.spawn("prompt")
	c.waitState(a.ID, ipc.StateWorking)
	ch := c.ask(dirUndo)
	if ch.Question != `"Prompt" is still at work.` || !slices.Equal(keysOf(ch), []string{ipc.ChoiceCancel, ipc.ChoiceApply, ipc.ChoiceSkip}) {
		t.Errorf("question %+v", ch)
	}
	if _, ok := c.row(a.ID); !ok {
		t.Fatal("asking closed it")
	}
	if r := c.answer(dirUndo, ch, ipc.ChoiceCancel); r.Did != ipc.DidCancel || len(c.history().Undo) != 1 {
		t.Errorf("cancel %+v, history %+v", r, c.history())
	}
	ch = c.ask(dirUndo)
	if r := c.answer(dirUndo, ch, ipc.ChoiceApply); r.Did != ipc.DidUndo {
		t.Fatalf("apply %+v", r)
	}
	if _, ok := c.row(a.ID); ok {
		t.Error("going ahead left the row")
	}
	if h := c.history(); len(h.Undo) != 0 || len(h.Redo) != 1 {
		t.Errorf("history %+v", h)
	}
}

// A frozen session is judged by what it was doing when it froze: closing it
// still stops its agent mid-task.
func TestUndoAsksBeforeStoppingAFrozenAgent(t *testing.T) {
	c := setup(t)
	a := c.spawn("prompt")
	c.waitState(a.ID, ipc.StateWorking)
	c.freeze(a.ID, true)
	c.waitState(a.ID, ipc.StateFrozen)
	ch := c.ask(dirUndo)
	if ch.Question != `"Prompt" is frozen in the middle of its work.` || !slices.Equal(keysOf(ch), []string{ipc.ChoiceCancel, ipc.ChoiceApply, ipc.ChoiceSkip}) {
		t.Errorf("question %+v", ch)
	}
	if r := c.answer(dirUndo, ch, ipc.ChoiceApply); r.Did != ipc.DidUndo {
		t.Fatalf("apply %+v", r)
	}
	if _, ok := c.row(a.ID); ok {
		t.Error("going ahead left the row")
	}
}

func TestUndoAsksWhenTheSessionChanged(t *testing.T) {
	c := setup(t)
	a := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,stop", Name: "twin"})
	c.waitState(a.ID, ipc.StateDone)
	c.closeSession(a.ID)
	b := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,stop", Name: "twin"})
	if b.Name != "twin" {
		t.Fatalf("the second is %q", b.Name)
	}
	// Leave the second's start out of the history, so the close is next
	// while the name is taken.
	c.editHistory(func(h *history) { h.Undo = h.Undo[:2] })

	ch := c.ask(dirUndo)
	if ch.Question != `Another session is called "twin" now.` || ch.Options[0].Key != ipc.ChoiceApply || ch.Options[0].Label != `Reopen it as "twin-2"` || !ch.Options[0].Recommended {
		t.Errorf("question %+v", ch)
	}
	if r := c.answer(dirUndo, ch, ipc.ChoiceApply); r.Did != ipc.DidUndo || r.Label != `close "twin-2"` {
		t.Errorf("apply %+v", r)
	}
	if s, ok := c.row(a.ID); !ok || s.Name != "twin-2" {
		t.Fatalf("reopened %+v", s)
	}
	if h := c.history(); !slices.Equal(labels(h.Redo), []string{`close "twin-2"`}) {
		t.Errorf("redo %v", labels(h.Redo))
	}

	// Its start is next, but it is called twin-2 now.
	c.waitState(a.ID, ipc.StateIdle)
	ch = c.ask(dirUndo)
	if ch.Question != `"twin" is called "twin-2" now.` || ch.Options[0].Key != ipc.ChoiceApply {
		t.Errorf("question %+v", ch)
	}
	if r := c.answer(dirUndo, ch, ipc.ChoiceApply); r.Did != ipc.DidUndo {
		t.Errorf("apply %+v", r)
	}
	if _, ok := c.row(a.ID); ok {
		t.Error("the row is still there")
	}
	if _, ok := c.row(b.ID); !ok {
		t.Error("the other twin went too")
	}
}

func TestUndoOfWhatIsAlreadySoSettlesOrSkips(t *testing.T) {
	c := setup(t)
	a := c.spawn("prompt,stop")
	c.waitState(a.ID, ipc.StateDone)
	c.closeSession(a.ID)
	// The close is not in the history, and a session Irie never had is.
	c.editHistory(func(h *history) {
		h.Undo = append(h.Undo[:1], action{ID: "aghost", Kind: actSpawn, Session: "s000000000000", Name: "ghost", Label: `start "ghost"`})
	})

	ch := c.ask(dirUndo)
	if ch.Question != `"ghost" is gone: Irie no longer has it, open or closed.` || !slices.Equal(keysOf(ch), []string{ipc.ChoiceSkip, ipc.ChoiceCancel}) || !ch.Options[0].Recommended {
		t.Errorf("question %+v", ch)
	}
	if r := c.answer(dirUndo, ch, ipc.ChoiceSkip); r.Did != ipc.DidSkip {
		t.Errorf("skip %+v", r)
	}
	if h := c.history(); !slices.Equal(labels(h.Undo), []string{`start "Prompt,stop"`}) || len(h.Redo) != 0 {
		t.Errorf("history after skip %+v", h)
	}

	ch = c.ask(dirUndo)
	if ch.Question != `"Prompt,stop" is closed already.` || !slices.Equal(keysOf(ch), []string{ipc.ChoiceSettle, ipc.ChoiceSkip, ipc.ChoiceCancel}) {
		t.Errorf("question %+v", ch)
	}
	if r := c.answer(dirUndo, ch, ipc.ChoiceSettle); r.Did != ipc.DidSettle {
		t.Errorf("settle %+v", r)
	}
	if h := c.history(); len(h.Undo) != 0 || !slices.Equal(labels(h.Redo), []string{`start "Prompt,stop"`}) {
		t.Errorf("history after settle %+v", h)
	}
	// Redo carries it on again.
	c.step(dirRedo)
	if s, ok := c.row(a.ID); !ok {
		t.Error("redo did not reopen it")
	} else {
		sameSession(t, s, a)
	}
}

func TestRedoAsksBeforeStartingATaskAgain(t *testing.T) {
	// Its first run hangs before Claude Code would start a conversation, as
	// at a trust prompt; later runs are the fake agent.
	fake := testutil.BuildFakeAgent(t)
	dir := t.TempDir()
	script := filepath.Join(dir, "claude")
	body := fmt.Sprintf(`#!/bin/sh
case "$1" in --version|--help) exec %[1]q "$@" ;; esac
if [ ! -e %[2]q ]; then : > %[2]q; exec sleep 600; fi
exec %[1]q "$@"
`, fake, filepath.Join(dir, "ran"))
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	c := setupWith(t, agentConfig{command: script})
	a := c.spawn("prompt")
	testutil.Eventually(t, 5*time.Second, "the first run did not start", func() bool {
		_, err := os.Stat(filepath.Join(dir, "ran"))
		return err == nil
	})
	// Starting, it has nothing to lose: undo closes it at once.
	c.step(dirUndo)
	ch := c.ask(dirRedo)
	if ch.Question != `"Prompt" never got going, so there is no conversation to carry on.` || ch.Options[0].Key != ipc.ChoiceApply || ch.Options[0].Label != "Start its task again" {
		t.Errorf("question %+v", ch)
	}
	if r := c.answer(dirRedo, ch, ipc.ChoiceApply); r.Did != ipc.DidRedo {
		t.Fatalf("apply %+v", r)
	}
	s := c.waitState(a.ID, ipc.StateWorking)
	if s.Name != a.Name || s.VendorID == a.VendorID {
		t.Errorf("started again as %+v", s)
	}
}

func TestAFailedUndoLeavesTheHistoryAsItWas(t *testing.T) {
	c := setup(t)
	a := c.spawn("prompt,stop")
	c.waitState(a.ID, ipc.StateDone)
	c.closeSession(a.ID)
	c.waitGone(a.Pane, 5*time.Second)
	before := c.history()
	dir := c.paths.Session(a.ID)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	if _, err := c.undoOp(dirUndo, ipc.UndoArgs{}); err == nil {
		t.Fatal("the reopen worked in a folder it cannot write")
	}
	if h := c.history(); !slices.Equal(labels(h.Undo), labels(before.Undo)) || len(h.Redo) != 0 {
		t.Errorf("history after a failed undo %+v, before %+v", h, before)
	}
	if r := c.listAll(); len(r.Closed) != 1 || len(r.Sessions) != 0 {
		t.Errorf("after a failed undo: %+v", r)
	}
	os.Chmod(dir, 0o700)
	c.step(dirUndo)
	if _, ok := c.row(a.ID); !ok {
		t.Error("the undo did not work once it could")
	}
}
