package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/journal"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/tmux"
)

func steps(ids ...string) []action {
	var out []action
	for _, id := range ids {
		out = append(out, action{ID: id, Kind: "test", Session: "s" + id, Name: id, Label: "test " + id})
	}
	return out
}

func ids(list []action) []string {
	out := []string{}
	for _, a := range list {
		out = append(out, a.ID)
	}
	return out
}

func TestHistoryUndoesAndRedoesInOrder(t *testing.T) {
	var h history
	for _, a := range steps("a", "b", "c") {
		h.push(a)
	}
	if u, r := h.labels(); u != "test c" || r != "" {
		t.Errorf("labels %q %q", u, r)
	}
	for _, id := range []string{"c", "b"} {
		a, _ := h.top(dirUndo)
		if a.ID != id || !h.finish(dirUndo, id, a) {
			t.Fatalf("undo took %s, want %s", a.ID, id)
		}
	}
	if !reflect.DeepEqual(ids(h.Undo), []string{"a"}) || !reflect.DeepEqual(ids(h.Redo), []string{"c", "b"}) {
		t.Errorf("after two undos: undo %v redo %v", ids(h.Undo), ids(h.Redo))
	}
	if u, r := h.labels(); u != "test a" || r != "test b" {
		t.Errorf("labels %q %q", u, r)
	}
	a, _ := h.top(dirRedo)
	h.finish(dirRedo, a.ID, a)
	if !reflect.DeepEqual(ids(h.Undo), []string{"a", "b"}) || !reflect.DeepEqual(ids(h.Redo), []string{"c"}) {
		t.Errorf("after a redo: undo %v redo %v", ids(h.Undo), ids(h.Redo))
	}
	// A new step clears what could be redone.
	h.push(steps("d")[0])
	if !reflect.DeepEqual(ids(h.Undo), []string{"a", "b", "d"}) || len(h.Redo) != 0 {
		t.Errorf("after a new step: undo %v redo %v", ids(h.Undo), ids(h.Redo))
	}
	// A step that is no longer next, because a new one came in while it was
	// undone, leaves the history rather than going on the redo stack.
	if h.finish(dirUndo, "b", steps("b")[0]) || !reflect.DeepEqual(ids(h.Undo), []string{"a", "d"}) || len(h.Redo) != 0 {
		t.Errorf("a stale finish: undo %v redo %v", ids(h.Undo), ids(h.Redo))
	}
	if !h.drop(dirUndo, "a") || h.drop(dirUndo, "a") || !reflect.DeepEqual(ids(h.Undo), []string{"d"}) {
		t.Errorf("drop: undo %v", ids(h.Undo))
	}
	if _, ok := (&history{}).top(dirRedo); ok {
		t.Error("an empty history has a step to redo")
	}
}

func TestHistoryKeepsTheLatestHundred(t *testing.T) {
	var h history
	for i := range maxHistory + 5 {
		h.push(steps(fmt.Sprint(i))[0])
	}
	if len(h.Undo) != maxHistory || h.Undo[0].ID != "5" || h.Undo[maxHistory-1].ID != fmt.Sprint(maxHistory+4) {
		t.Errorf("%d steps, from %s", len(h.Undo), h.Undo[0].ID)
	}
	for range 10 {
		a, _ := h.top(dirUndo)
		h.finish(dirUndo, a.ID, a)
	}
	if len(h.Undo)+len(h.Redo) != maxHistory {
		t.Errorf("undo %d + redo %d", len(h.Undo), len(h.Redo))
	}
}

func TestStateFileRoundTripsAndMigrates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// No file yet: an empty history.
	st, err := readState(path)
	if err != nil || len(st.hist.Undo)+len(st.hist.Redo) != 0 {
		t.Fatalf("no file: %+v, %v", st, err)
	}

	// A file from before undo has no stacks; what else it holds stays.
	os.WriteFile(path, []byte(`{"sessions":{"s0k3j9x2m4a7q":{"parked":true}}}`), 0o600)
	st, err = readState(path)
	if err != nil || len(st.hist.Undo)+len(st.hist.Redo) != 0 {
		t.Fatalf("an old file: %+v, %v", st, err)
	}
	at := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	st.hist.push(action{ID: "a1", Kind: actClose, Session: "s0k3j9x2m4a7q", Name: "api-fix", Label: `close "api-fix"`, At: at, Data: map[string]any{"closed": "2026-10-27T09:00:00Z"}})
	st.hist.push(action{ID: "a2", Kind: actSpawn, Session: "s0k3j9x2m4a7r", Name: "web", Label: `start "web"`, At: at})
	a, _ := st.hist.top(dirUndo)
	st.hist.finish(dirUndo, a.ID, a)
	if err := writeState(path, st); err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &raw); err != nil || string(raw["version"]) != "1" || !strings.Contains(string(raw["sessions"]), `"parked": true`) {
		t.Errorf("written:\n%s", b)
	}
	back, err := readState(path)
	if err != nil || !reflect.DeepEqual(back.hist, st.hist) {
		t.Errorf("read back %+v, %v; wrote %+v", back.hist, err, st.hist)
	}

	// A stack that does not parse starts empty and says so; the other stays.
	os.WriteFile(path, []byte(`{"version":1,"undo":"oops","redo":[{"id":"a3","kind":"spawn","session":"s1","name":"x","label":"start \"x\""}]}`), 0o600)
	st, err = readState(path)
	if err == nil || len(st.hist.Undo) != 0 || !reflect.DeepEqual(ids(st.hist.Redo), []string{"a3"}) {
		t.Errorf("a bad stack: %+v, %v", st.hist, err)
	}

	// A file that is not JSON is kept aside, and the history starts empty.
	os.WriteFile(path, []byte(`{"undo":[`), 0o600)
	st, err = readState(path)
	if err == nil || len(st.hist.Undo) != 0 {
		t.Errorf("not JSON: %+v, %v", st.hist, err)
	}
	if b, _ := os.ReadFile(path + ".bad"); string(b) != `{"undo":[` {
		t.Errorf("state.json.bad = %q", b)
	}
}

// fakeKind is the "test" action kind: what its check finds and what its do
// does are the test's.
var fakeKind struct {
	mu    sync.Mutex
	probs []problem
	err   error
	done  []string // dir:id, each effect taken
	hook  func()   // runs inside do
	in    atomic.Int32
	over  atomic.Bool // two effects ran at once
}

func init() {
	eff := func(dir string) effect {
		return effect{
			check: func(c *Core, a action) []problem {
				fakeKind.mu.Lock()
				defer fakeKind.mu.Unlock()
				return fakeKind.probs
			},
			do: func(c *Core, a action) (action, error) {
				if fakeKind.in.Add(1) > 1 {
					fakeKind.over.Store(true)
				}
				defer fakeKind.in.Add(-1)
				fakeKind.mu.Lock()
				err, hook := fakeKind.err, fakeKind.hook
				fakeKind.mu.Unlock()
				if hook != nil {
					hook()
				}
				if err != nil {
					return a, err
				}
				fakeKind.mu.Lock()
				fakeKind.done = append(fakeKind.done, dir+":"+a.ID)
				fakeKind.mu.Unlock()
				return a, nil
			},
		}
	}
	registerAction("test", actionKind{verb: "test", undo: eff(dirUndo), redo: eff(dirRedo)})
}

func resetFake(probs []problem, err error) {
	fakeKind.mu.Lock()
	defer fakeKind.mu.Unlock()
	fakeKind.probs, fakeKind.err, fakeKind.done, fakeKind.hook = probs, err, nil, nil
	fakeKind.over.Store(false)
}

// historyCore is a core without tmux whose state and journal live in a
// temporary directory.
func historyCore(t *testing.T) *Core {
	t.Helper()
	home := t.TempDir()
	p, err := paths.New(func(k string) string { return map[string]string{"HOME": home}[k] }, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.EnsureState(); err != nil {
		t.Fatal(err)
	}
	c := newCore(Options{Config: config.Default(), Paths: p, Version: "test", Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, &tmux.Runner{Bin: filepath.Join(home, "no-tmux")})
	if c.journal, err = journal.Open(p.Journal(), journal.Options{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.journal.Close() })
	c.loadState()
	return c
}

func (c *Core) stacks() (undo, redo []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return ids(c.steps.hist.Undo), ids(c.steps.hist.Redo)
}

func busyProblem() problem {
	return problem{code: "busy", question: `"x" is still at work.`, recommend: ipc.ChoiceCancel,
		options: []ipc.ChoiceOption{{Key: ipc.ChoiceApply, Label: "Close it now", Detail: "Stops its agent."}}}
}

func TestUndoAndRedoTakeTheEffectsAndRecordNothingNew(t *testing.T) {
	resetFake(nil, nil)
	c := historyCore(t)
	c.recordAction("test", "s0000000000s1", nil)
	c.recordAction("test", "s0000000000s2", nil)
	u, _ := c.stacks()
	first, second := u[0], u[1]

	r, err := c.undo(dirUndo, ipc.UndoArgs{})
	if err != nil || r.Did != ipc.DidUndo || r.Label != `test "s0000000000s2"` {
		t.Fatalf("undo = %+v, %v", r, err)
	}
	if u, rd := c.stacks(); !reflect.DeepEqual(u, []string{first}) || !reflect.DeepEqual(rd, []string{second}) {
		t.Errorf("after undo: %v %v", u, rd)
	}
	r, err = c.undo(dirRedo, ipc.UndoArgs{})
	if err != nil || r.Did != ipc.DidRedo {
		t.Fatalf("redo = %+v, %v", r, err)
	}
	if u, rd := c.stacks(); !reflect.DeepEqual(u, []string{first, second}) || len(rd) != 0 {
		t.Errorf("after redo: %v %v", u, rd)
	}
	if want := []string{"undo:" + second, "redo:" + second}; !reflect.DeepEqual(fakeKind.done, want) {
		t.Errorf("effects %v, want %v", fakeKind.done, want)
	}
	if _, err := c.undo(dirRedo, ipc.UndoArgs{}); code(err) != ipc.CodeNotFound || err.Error() != "nothing to redo" {
		t.Errorf("redo with nothing to redo: %v", err)
	}
	// Both made it into the journal, before and after.
	evs, _ := journal.ReadKinds(c.paths.Journal(), time.Time{}, event.UIUndo, event.UIRedo)
	if len(evs) != 2 || evs[0].Kind != event.UIUndo || evs[1].Kind != event.UIRedo || evs[0].Session != "s0000000000s2" || evs[0].Harness != "irie" || evs[0].Str("label") != `test "s0000000000s2"` || evs[0].Str("before") != "gone" || evs[0].Str("after") != "gone" {
		t.Errorf("journal %+v", evs)
	}

	// The history survives a restart.
	again := newCore(Options{Config: config.Default(), Paths: c.paths, Log: c.log}, c.tmux)
	again.loadState()
	if u, rd := again.stacks(); !reflect.DeepEqual(u, []string{first, second}) || len(rd) != 0 {
		t.Errorf("after a restart: %v %v", u, rd)
	}
}

func TestAFailedEffectLeavesTheHistoryAsItWas(t *testing.T) {
	resetFake(nil, errors.New("tmux is gone"))
	c := historyCore(t)
	c.recordAction("test", "s1", nil)
	before, _ := c.stacks()
	if _, err := c.undo(dirUndo, ipc.UndoArgs{}); err == nil || err.Error() != "tmux is gone" {
		t.Errorf("undo = %v", err)
	}
	if u, r := c.stacks(); !reflect.DeepEqual(u, before) || len(r) != 0 {
		t.Errorf("after a failed undo: %v %v", u, r)
	}
	b, _ := os.ReadFile(c.paths.StateFile())
	if !strings.Contains(string(b), before[0]) {
		t.Errorf("state.json lost the step:\n%s", b)
	}
}

func TestAnUncertainUndoAsksAndChangesNothing(t *testing.T) {
	resetFake([]problem{busyProblem()}, nil)
	c := historyCore(t)
	c.recordAction("test", "s0000000000s1", nil)
	c.recordAction("test", "s0000000000s2", nil)
	before, _ := c.stacks()

	r, err := c.undo(dirUndo, ipc.UndoArgs{})
	if err != nil || r.Did != ipc.DidAsk || r.Choice == nil || r.Label != `test "s0000000000s2"` {
		t.Fatalf("undo = %+v, %v", r, err)
	}
	ch := *r.Choice
	var keys []string
	for _, o := range ch.Options {
		keys = append(keys, o.Key)
		if o.Label == "" || o.Detail == "" {
			t.Errorf("an option says too little: %+v", o)
		}
	}
	// The safest first and marked; skip and cancel always.
	if want := []string{ipc.ChoiceCancel, ipc.ChoiceApply, ipc.ChoiceSkip}; !reflect.DeepEqual(keys, want) || !ch.Options[0].Recommended || ch.Options[1].Recommended {
		t.Errorf("options %+v, want keys %v with the first recommended", ch.Options, want)
	}
	if ch.Question != `"x" is still at work.` || ch.Options[2].Label != "Skip this one (drop it from history)" || ch.Options[0].Label != "Cancel (change nothing)" {
		t.Errorf("choice %+v", ch)
	}
	if u, rd := c.stacks(); !reflect.DeepEqual(u, before) || len(rd) != 0 || len(fakeKind.done) != 0 {
		t.Errorf("asking changed things: %v %v %v", u, rd, fakeKind.done)
	}

	// Cancel changes nothing, the history included.
	if r, err := c.undo(dirUndo, ipc.UndoArgs{ChoiceID: ch.ID, Choice: ipc.ChoiceCancel}); err != nil || r.Did != ipc.DidCancel {
		t.Errorf("cancel = %+v, %v", r, err)
	}
	if u, _ := c.stacks(); !reflect.DeepEqual(u, before) || len(fakeKind.done) != 0 {
		t.Errorf("after cancel: %v %v", u, fakeKind.done)
	}
	// An answer to another step, another direction, or no question at all
	// is refused, and so is a choice without its question.
	for _, id := range []string{strings.Replace(ch.ID, before[1], before[0], 1), strings.Replace(ch.ID, ":undo:", ":redo:", 1), "nonsense"} {
		if _, err := c.undo(dirUndo, ipc.UndoArgs{ChoiceID: id, Choice: ipc.ChoiceApply}); code(err) != ipc.CodeStale {
			t.Errorf("answer to %q: %v", id, err)
		}
	}
	if _, err := c.undo(dirUndo, ipc.UndoArgs{Choice: ipc.ChoiceApply}); code(err) != ipc.CodeBadArgs {
		t.Errorf("a choice without its question: %v", err)
	}
	if _, err := c.undo(dirUndo, ipc.UndoArgs{ChoiceID: ch.ID, Choice: "maybe"}); code(err) != ipc.CodeBadArgs {
		t.Errorf("an unknown choice: %v", err)
	}
	// Going ahead takes the effect.
	r, err = c.undo(dirUndo, ipc.UndoArgs{ChoiceID: ch.ID, Choice: ipc.ChoiceApply})
	if err != nil || r.Did != ipc.DidUndo || !reflect.DeepEqual(fakeKind.done, []string{"undo:" + before[1]}) {
		t.Fatalf("apply = %+v, %v; effects %v", r, err, fakeKind.done)
	}
	evs, _ := journal.ReadKinds(c.paths.Journal(), time.Time{}, event.UIUndo)
	if len(evs) != 1 || evs[0].Str("choice") != ipc.ChoiceApply {
		t.Errorf("journal %+v", evs)
	}
	// Now that the step was undone, its old question is stale.
	if _, err := c.undo(dirUndo, ipc.UndoArgs{ChoiceID: ch.ID, Choice: ipc.ChoiceApply}); code(err) != ipc.CodeStale {
		t.Errorf("a stale answer: %v", err)
	}
}

func TestQuestionsComeOneAtATime(t *testing.T) {
	renamed := problem{code: "renamed", question: `"x" is called "y" now.`, recommend: ipc.ChoiceApply,
		options: []ipc.ChoiceOption{{Key: ipc.ChoiceApply, Label: `Close "y" anyway`, Detail: "Ends its agent."}}}
	resetFake([]problem{renamed, busyProblem()}, nil)
	c := historyCore(t)
	c.recordAction("test", "s1", nil)
	r, _ := c.undo(dirUndo, ipc.UndoArgs{})
	if r.Choice == nil || !strings.HasSuffix(r.Choice.ID, ":renamed") || r.Choice.Options[0].Key != ipc.ChoiceApply {
		t.Fatalf("first question %+v", r.Choice)
	}
	r, _ = c.undo(dirUndo, ipc.UndoArgs{ChoiceID: r.Choice.ID, Choice: ipc.ChoiceApply})
	if r.Did != ipc.DidAsk || r.Choice == nil || !strings.HasSuffix(r.Choice.ID, ":renamed+busy") || len(fakeKind.done) != 0 {
		t.Fatalf("second question %+v, effects %v", r, fakeKind.done)
	}
	// An id that claims a problem was accepted cannot skip one that offers
	// no going ahead.
	gone := problem{code: "gone", question: "gone", recommend: ipc.ChoiceSkip}
	resetFake([]problem{gone}, nil)
	u, _ := c.stacks()
	if r, err := c.undo(dirUndo, ipc.UndoArgs{ChoiceID: u[0] + ":undo:gone+x", Choice: ipc.ChoiceApply}); err != nil || r.Did != ipc.DidAsk || len(fakeKind.done) != 0 {
		t.Errorf("forged id = %+v, %v, effects %v", r, err, fakeKind.done)
	}
	resetFake([]problem{renamed, busyProblem()}, nil)
	r, _ = c.undo(dirUndo, ipc.UndoArgs{})
	r, _ = c.undo(dirUndo, ipc.UndoArgs{ChoiceID: r.Choice.ID, Choice: ipc.ChoiceApply})
	r, err := c.undo(dirUndo, ipc.UndoArgs{ChoiceID: r.Choice.ID, Choice: ipc.ChoiceApply})
	if err != nil || r.Did != ipc.DidUndo || len(fakeKind.done) != 1 {
		t.Errorf("both accepted = %+v, %v, effects %v", r, err, fakeKind.done)
	}
}

func TestSkipAndSettle(t *testing.T) {
	settle := problem{code: "closed", question: `"x" is closed already.`, recommend: ipc.ChoiceSettle,
		options: []ipc.ChoiceOption{{Key: ipc.ChoiceSettle, Label: "Leave it closed and move on", Detail: "…"}}}
	resetFake([]problem{settle}, nil)
	c := historyCore(t)
	c.recordAction("test", "s1", nil)
	c.recordAction("test", "s2", nil)
	u, _ := c.stacks()

	r, _ := c.undo(dirUndo, ipc.UndoArgs{})
	if r.Choice == nil || r.Choice.Options[0].Key != ipc.ChoiceSettle || !r.Choice.Options[0].Recommended {
		t.Fatalf("question %+v", r.Choice)
	}
	// Settling moves the step over without taking its effect.
	r, err := c.undo(dirUndo, ipc.UndoArgs{ChoiceID: r.Choice.ID, Choice: ipc.ChoiceSettle})
	if err != nil || r.Did != ipc.DidSettle || len(fakeKind.done) != 0 {
		t.Fatalf("settle = %+v, %v", r, err)
	}
	if uu, rd := c.stacks(); !reflect.DeepEqual(uu, u[:1]) || !reflect.DeepEqual(rd, u[1:]) {
		t.Errorf("after settle: %v %v", uu, rd)
	}
	// Settling is only for a question that offers it.
	resetFake([]problem{busyProblem()}, nil)
	r, _ = c.undo(dirUndo, ipc.UndoArgs{})
	if r, err := c.undo(dirUndo, ipc.UndoArgs{ChoiceID: r.Choice.ID, Choice: ipc.ChoiceSettle}); err != nil || r.Did != ipc.DidAsk {
		t.Errorf("settle where it is not offered = %+v, %v", r, err)
	}
	// Skipping drops the step and nothing else.
	r, err = c.undo(dirUndo, ipc.UndoArgs{ChoiceID: r.Choice.ID, Choice: ipc.ChoiceSkip})
	if err != nil || r.Did != ipc.DidSkip || len(fakeKind.done) != 0 {
		t.Fatalf("skip = %+v, %v", r, err)
	}
	if uu, rd := c.stacks(); len(uu) != 0 || !reflect.DeepEqual(rd, u[1:]) {
		t.Errorf("after skip: %v %v", uu, rd)
	}
}

func TestAStepOfAnUnknownKindCanOnlyBeSkipped(t *testing.T) {
	resetFake(nil, nil)
	c := historyCore(t)
	c.steps.hist.push(action{ID: "a1", Kind: "freeze", Session: "s1", Name: "x", Label: `freeze "x"`})
	r, err := c.undo(dirUndo, ipc.UndoArgs{})
	if err != nil || r.Choice == nil || r.Choice.Question != `This version of Irie cannot undo freeze "x".` {
		t.Fatalf("undo = %+v, %v", r, err)
	}
	if len(r.Choice.Options) != 2 || r.Choice.Options[0].Key != ipc.ChoiceSkip || r.Choice.Options[1].Key != ipc.ChoiceCancel {
		t.Errorf("options %+v", r.Choice.Options)
	}
	if r, err := c.undo(dirUndo, ipc.UndoArgs{ChoiceID: "a1:undo:unknown+x", Choice: ipc.ChoiceApply}); err != nil || r.Did != ipc.DidAsk {
		t.Errorf("going ahead = %+v, %v", r, err)
	}
}

func TestUndosRunOneAtATime(t *testing.T) {
	resetFake(nil, nil)
	fakeKind.hook = func() { time.Sleep(50 * time.Millisecond) }
	c := historyCore(t)
	for i := range 4 {
		c.recordAction("test", fmt.Sprintf("s%d", i), nil)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.undo(dirUndo, ipc.UndoArgs{})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	if fakeKind.over.Load() {
		t.Error("two undos ran at once")
	}
	if u, r := c.stacks(); len(u) != 0 || len(r) != 4 || len(fakeKind.done) != 4 {
		t.Errorf("undo %v redo %v effects %v", u, r, fakeKind.done)
	}
}

func TestANewActionDuringAnUndoKeepsTheHistoryLinear(t *testing.T) {
	resetFake(nil, nil)
	c := historyCore(t)
	c.recordAction("test", "s1", nil)
	c.recordAction("test", "s2", nil)
	u, _ := c.stacks()
	fakeKind.hook = func() { c.recordAction("test", "s3", nil) }
	if _, err := c.undo(dirUndo, ipc.UndoArgs{}); err != nil {
		t.Fatal(err)
	}
	after, r := c.stacks()
	if len(after) != 2 || after[0] != u[0] || after[1] == u[1] || len(r) != 0 {
		t.Errorf("undo %v redo %v, from %v", after, r, u)
	}
}

func TestHistoryFrames(t *testing.T) {
	resetFake(nil, nil)
	c := historyCore(t)
	next := frames(t, c)
	var got []string
	for range 4 {
		f, ok := next(time.Second)
		if !ok {
			t.Fatal("no frame")
		}
		got = append(got, f.T)
		if f.T == ipc.FrameHistory && (f.Undo != "" || f.Redo != "") {
			t.Errorf("first history frame %+v", f)
		}
	}
	if want := []string{ipc.FrameSnapshot, ipc.FrameOutside, ipc.FrameClosed, ipc.FrameHistory}; !reflect.DeepEqual(got, want) {
		t.Errorf("first frames %v, want %v", got, want)
	}
	c.recordAction("test", "s1", nil)
	if f, ok := next(time.Second); !ok || f.T != ipc.FrameHistory || f.Undo != `test "s1"` || f.Redo != "" {
		t.Errorf("after an action: %+v", f)
	}
	c.undo(dirUndo, ipc.UndoArgs{})
	if f, ok := next(time.Second); !ok || f.T != ipc.FrameHistory || f.Undo != "" || f.Redo != `test "s1"` {
		t.Errorf("after an undo: %+v", f)
	}
	b, _ := json.Marshal(ipc.Frame{T: ipc.FrameHistory})
	if string(b) != `{"t":"history","undo":"","redo":""}` {
		t.Errorf("an empty history frame is %s", b)
	}
}
