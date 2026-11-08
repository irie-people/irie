package core

import (
	"testing"

	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/team"
	"github.com/irie-people/irie/internal/worktree"
)

// An orchestrator whose own turn is over while its helpers or teammates
// still work waits on them (DESIGN §5.3): its row stays done or idle, says
// what it waits on, and carries Subs, which the sidebars draw.

func newWaiter() *session { return &session{id: "s0k3j9x2m4a7q", name: "Patents"} }

func (x *session) apply(evs ...event.Event) {
	for _, e := range evs {
		x.track.apply(e)
	}
}

func TestATurnOverWaitsOnItsHelpersInTheBackground(t *testing.T) {
	s := newScript()
	x := newWaiter()
	x.apply(
		s.hook(event.PromptSubmitted),
		s.sub(event.SubagentStarted, "agent_type", "Explore"),
		s.hook(event.TurnEnded, "waiting", 1, "waiting_on", "Find the seed patents", "subagents", 1),
		// The helper's work does not make the turn its session's again.
		s.sub(event.ToolStarted, "tool", "WebSearch", "summary", "expired patents", "phrase", "Searching the web", "tool_use_id", "t1"),
	)
	row := x.wire()
	if row.State != ipc.StateDone || row.Subs == nil || *row.Subs != (ipc.Subs{Helpers: 1}) || row.Detail != "Waiting on 1 helper" ||
		len(row.Subagents) != 1 || row.Subagents[0].Detail != "Searching the web" {
		t.Fatalf("row: state %s, subs %+v, detail %q, helpers %+v", row.State, row.Subs, row.Detail, row.Subagents)
	}

	// Seen, it is idle and still waits.
	x.apply(s.ev(event.UIShown, "", event.SourceUser))
	if row := x.wire(); row.State != ipc.StateIdle || row.Subs == nil {
		t.Errorf("seen: state %s, subs %+v", row.State, row.Subs)
	}

	// What the helper needs is the session's to answer, and the answer
	// leaves it waiting again.
	x.apply(s.sub(event.InputNeeded, "reason", event.ReasonPermission, "tool", "Bash", "summary", "rm -rf build"))
	if row := x.wire(); row.State != ipc.StateNeedsYou || row.Subs != nil {
		t.Errorf("a helper's need: state %s, subs %+v", row.State, row.Subs)
	}
	x.apply(s.sub(event.ToolFinished, "tool", "WebSearch", "tool_use_id", "t1"))
	if row := x.wire(); row.State != ipc.StateIdle || row.Subs == nil {
		t.Errorf("answered: state %s, subs %+v", row.State, row.Subs)
	}

	// Once the helper stops nothing is at work, and its turn is over as
	// the Stop left it.
	x.apply(s.sub(event.SubagentFinished))
	if row := x.wire(); row.State != ipc.StateIdle || row.Subs != nil || row.Detail != "Waiting on its work in the background · Find the seed patents" {
		t.Errorf("after the helper: state %s, subs %+v, detail %q", row.State, row.Subs, row.Detail)
	}

	// What it found wakes the orchestrator, whose turn is its own.
	x.apply(s.hook(event.ToolStarted, "tool", "Read", "tool_use_id", "t2"))
	if row := x.wire(); row.State != ipc.StateWorking || row.Subs != nil {
		t.Errorf("woken: state %s, subs %+v", row.State, row.Subs)
	}
}

// A helper in the foreground runs inside its session's turn, which stays
// working however the helper's tool calls go.
func TestAHelperInTheForegroundKeepsTheTurnWorking(t *testing.T) {
	s := newScript()
	x := newWaiter()
	got := states(&x.track,
		s.hook(event.PromptSubmitted),
		s.sub(event.SubagentStarted, "agent_type", "Explore"),
		s.sub(event.ToolStarted, "tool", "Grep", "tool_use_id", "t1"),
		s.sub(event.ToolFinished, "tool", "Grep", "tool_use_id", "t1"),
		s.sub(event.SubagentFinished),
		s.hook(event.TurnEnded, "subagents", 0),
	)
	want := []string{"working", "working", "working", "working", "working", "done"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("states %v, want %v", got, want)
		}
	}
	if row := x.wire(); row.Subs != nil {
		t.Errorf("subs %+v once nothing runs", row.Subs)
	}
}

// Only a helper in the background outlives its session's turn, so a Stop
// that lists none there tells the core that every helper it still has has
// stopped, though their SubagentStop never came.
func TestAStopWithNoHelperInTheBackgroundDropsTheOnesThatNeverStopped(t *testing.T) {
	s := newScript()
	x := newWaiter()
	other := func(kind string, data ...any) event.Event { return s.ev(kind, "sub:d4e5f6", event.SourceHook, data...) }
	x.apply(
		s.hook(event.PromptSubmitted),
		s.sub(event.SubagentStarted, "agent_type", "Explore"),
		s.sub(event.ToolStarted, "tool", "Grep", "tool_use_id", "t1"),
		other(event.SubagentStarted, "agent_type", "Plan"),
		other(event.InputNeeded, "reason", event.ReasonPermission, "tool", "Bash"),
	)
	// A Stop whose task registry was out of reach says nothing of them.
	x.apply(s.hook(event.TurnEnded))
	if len(x.track.subs) != 2 {
		t.Fatalf("a Stop without background_tasks dropped helpers: %v", x.track.subs)
	}
	// Nor does one that lists a helper in the background, which may be
	// either of them.
	x.apply(s.hook(event.PromptSubmitted), s.hook(event.TurnEnded, "subagents", 1))
	if len(x.track.subs) != 2 {
		t.Fatalf("a Stop with a helper in the background dropped helpers: %v", x.track.subs)
	}
	x.apply(s.hook(event.PromptSubmitted), s.hook(event.TurnEnded, "subagents", 0))
	row := x.wire()
	if len(x.track.subs) != 0 || len(x.track.tools) != 0 || len(x.track.needs) != 0 || row.State != ipc.StateDone || row.Subs != nil {
		t.Errorf("after a Stop with none in the background: subs %v, tools %v, needs %v, row %s %+v",
			x.track.subs, x.track.tools, x.track.needs, row.State, row.Subs)
	}
}

func TestATurnOverWaitsOnItsTeammatesAtWork(t *testing.T) {
	s := newScript()
	x := newWaiter()
	x.apply(s.hook(event.PromptSubmitted), s.hook(event.TurnEnded, "waiting", 4, "waiting_on", "Find seed patents", "subagents", 0))
	x.team = []team.Teammate{
		{Session: x.id + "-1", Name: "seed-finder", Working: true},
		{Session: x.id + "-2", Name: "expand-a", Working: true},
		{Session: x.id + "-3", Name: "expand-b"},                            // waits for a message
		{Session: x.id + "-4", Name: "expand-c", Working: true, Dead: true}, // its pane died
	}
	row := x.wire()
	if row.State != ipc.StateDone || row.Subs == nil || *row.Subs != (ipc.Subs{Teammates: 2}) || row.Detail != "Waiting on 2 teammates" {
		t.Fatalf("row: state %s, subs %+v, detail %q", row.State, row.Subs, row.Detail)
	}
	// A helper of its own counts beside them.
	x.apply(s.sub(event.SubagentStarted, "agent_type", "Explore"))
	if row := x.wire(); row.State != ipc.StateDone || row.Detail != "Waiting on 1 helper and 2 teammates" {
		t.Errorf("with a helper: state %s, detail %q", row.State, row.Detail)
	}
	x.apply(s.sub(event.SubagentFinished))
	// Once they all wait for a message, nothing of its is at work.
	for i := range x.team {
		x.team[i].Working = false
	}
	if row := x.wire(); row.Subs != nil || row.Detail != "Waiting on its work in the background · Find seed patents" {
		t.Errorf("all waiting: subs %+v, detail %q", row.Subs, row.Detail)
	}
}

// What a jot left that has not landed waits until its helpers are through:
// they may be the ones writing it.
func TestAJotWaitingOnItsHelpersSaysNothingOfWhatIsLeft(t *testing.T) {
	s := newScript()
	x := newWaiter()
	x.dog.loose = []looseIn{{jotTree: jotTree{path: "/home/alice/code/irie"}, land: "live", Loose: worktree.Loose{Dirty: 2}}}
	x.apply(s.hook(event.PromptSubmitted), s.sub(event.SubagentStarted, "agent_type", "Explore"), s.hook(event.TurnEnded, "subagents", 1))
	if row := x.wire(); row.Loose != nil || row.Detail != "Waiting on 1 helper" {
		t.Errorf("waiting: loose %+v, detail %q", row.Loose, row.Detail)
	}
	x.apply(s.sub(event.SubagentFinished))
	if row := x.wire(); row.Loose == nil || row.Detail != "Not through: 2 files not committed" {
		t.Errorf("through waiting: loose %+v, detail %q", row.Loose, row.Detail)
	}
}

// A session frozen while a helper of its works after its turn is over
// still has something running on to its next tool call.
func TestFreezingWhileAHelperWorksAfterTheTurn(t *testing.T) {
	s := newScript()
	x := newWaiter()
	x.apply(
		s.hook(event.PromptSubmitted),
		s.sub(event.SubagentStarted, "agent_type", "Explore"),
		s.hook(event.TurnEnded, "subagents", 1),
		s.ev(event.FreezeOn, "", event.SourceUser, "whole", false),
	)
	if row := x.wire(); row.State != ipc.StateFrozen || !row.Freezing || row.Subs != nil {
		t.Errorf("frozen: state %s, freezing %v, subs %+v", row.State, row.Freezing, row.Subs)
	}
}

// Closing a session whose turn is over still ends the helpers it waits on,
// so undoing its start asks first.
func TestUndoAsksBeforeEndingTheHelpersASessionWaitsOn(t *testing.T) {
	s := newScript()
	x := newWaiter()
	x.apply(s.hook(event.PromptSubmitted), s.hook(event.TurnEnded, "subagents", 0))
	c := &Core{sessions: map[string]*session{x.id: x}}
	a := action{Kind: actSpawn, Session: x.id, Name: x.name}
	if ps := checkClose(c, a); len(ps) != 0 {
		t.Fatalf("a finished turn with nothing at work: %+v", ps)
	}
	x.team = []team.Teammate{{Session: x.id + "-1", Name: "seed-finder", Working: true}}
	ps := checkClose(c, a)
	if len(ps) != 1 || ps[0].code != "busy" || ps[0].question != `"Patents" waits on 1 teammate still at work.` || ps[0].recommend != ipc.ChoiceCancel {
		t.Fatalf("problems %+v", ps)
	}
	x.apply(s.ev(event.FreezeOn, "", event.SourceUser, "whole", true))
	if ps := checkClose(c, a); len(ps) != 1 || ps[0].question != `"Patents" is frozen while it waits on 1 teammate.` {
		t.Errorf("frozen: problems %+v", ps)
	}
}
