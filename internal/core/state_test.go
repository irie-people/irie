package core

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-hq/irie/internal/event"
	"github.com/irie-hq/irie/internal/ipc"
)

// script builds events the way the core records them, with rising seqs.
type script struct {
	seq uint64
	at  time.Time
}

func (s *script) ev(kind, agent, source string, data ...any) event.Event {
	s.seq++
	s.at = s.at.Add(time.Second)
	e := event.Event{V: 1, Seq: s.seq, TS: event.Stamp(s.at), Session: "s0k3j9x2m4a7q", Agent: agent, Source: source, Harness: "claude-code", Kind: kind, Data: map[string]any{}}
	for i := 0; i+1 < len(data); i += 2 {
		if data[i] == "tool_use_id" {
			e.Vendor = &event.Vendor{ToolUseID: data[i+1].(string)}
			continue
		}
		e.Data[data[i].(string)] = data[i+1]
	}
	return e
}

func (s *script) hook(kind string, data ...any) event.Event {
	return s.ev(kind, event.Main, event.SourceHook, data...)
}

func (s *script) sub(kind string, data ...any) event.Event {
	return s.ev(kind, "sub:a1b2c3", event.SourceHook, data...)
}

func (s *script) irie(kind string, data ...any) event.Event {
	return s.ev(kind, "", event.SourceIrie, data...)
}

// states applies each event and returns the state after each one.
func states(t *track, evs ...event.Event) []string {
	var out []string
	for _, e := range evs {
		t.apply(e)
		out = append(out, t.state(false))
	}
	return out
}

func newScript() *script { return &script{at: time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)} }

func TestTheStatesOfATurn(t *testing.T) {
	s := newScript()
	var tr track
	got := states(&tr,
		s.irie(event.SpawnRequested),
		s.hook(event.SessionStarted),
		s.hook(event.PromptSubmitted),
		s.hook(event.ToolStarted, "tool", "Bash", "tool_use_id", "t1"),
		s.hook(event.ToolFinished, "tool", "Bash", "tool_use_id", "t1"),
		s.hook(event.TurnEnded),
		s.ev(event.UIShown, "", event.SourceUser),
		s.hook(event.PromptSubmitted),
		s.hook(event.TurnEnded),
	)
	want := []string{"starting", "starting", "working", "working", "working", "done", "idle", "working", "done"}
	if !slices.Equal(got, want) {
		t.Errorf("states %v, want %v", got, want)
	}
}

func TestRunningSubagentsAreListedUntilTheyStop(t *testing.T) {
	s := newScript()
	var tr track
	describe := func() ipc.Session {
		var row ipc.Session
		tr.describe(&row, false, 0)
		return row
	}
	for _, e := range []event.Event{
		s.hook(event.SessionStarted),
		s.hook(event.PromptSubmitted),
		s.sub(event.SubagentStarted, "agent_type", "Explore"),
		s.sub(event.ToolStarted, "tool", "Grep", "summary", "TODO", "tool_use_id", "t1"),
	} {
		tr.apply(e)
	}
	if got := describe().Subagents; len(got) != 1 || got[0].Agent != "sub:a1b2c3" || got[0].Type != "Explore" ||
		got[0].Detail != "Grep · TODO" || got[0].NeedsYou || got[0].Started.IsZero() {
		t.Fatalf("subagents = %+v", got)
	}

	// What a subagent needs shows on the subagent as well as on the row.
	tr.apply(s.sub(event.InputNeeded, "reason", event.ReasonPermission, "tool", "Bash", "summary", "rm -rf build"))
	row := describe()
	if row.State != ipc.StateNeedsYou || len(row.Subagents) != 1 || !row.Subagents[0].NeedsYou ||
		row.Subagents[0].Detail != "needs permission: Bash · rm -rf build" {
		t.Fatalf("after a need: state %s, subagents %+v", row.State, row.Subagents)
	}

	tr.apply(s.sub(event.SubagentFinished))
	if got := describe().Subagents; len(got) != 0 {
		t.Errorf("after the stop: subagents = %+v", got)
	}

	// A session that ends lists none.
	tr.apply(s.sub(event.SubagentStarted, "agent_type", "Plan"))
	tr.apply(s.hook(event.SessionEnded, "reason", "other"))
	if got := describe().Subagents; len(got) != 0 {
		t.Errorf("after the session ended: subagents = %+v", got)
	}
}

func TestTheTranscriptsAreRecordedForTheTokenTally(t *testing.T) {
	s := newScript()
	var tr track
	tr.apply(s.hook(event.SessionStarted, "transcript", "/home/alice/transcripts/main.jsonl"))
	tr.apply(s.sub(event.SubagentStarted, "agent_type", "Explore"))
	tr.apply(s.sub(event.SubagentFinished, "transcript", "/home/alice/transcripts/main/agent-a1b2c3.jsonl"))
	tr.apply(s.hook(event.SessionStarted, "transcript", "/home/alice/transcripts/main.jsonl")) // resumed: named again
	want := []string{"/home/alice/transcripts/main.jsonl", "/home/alice/transcripts/main/agent-a1b2c3.jsonl"}
	if !slices.Equal(tr.transcripts, want) {
		t.Errorf("transcripts %q, want %q", tr.transcripts, want)
	}
}

func TestNeedsYouIsClearedByTheNextPromptOrToolEvent(t *testing.T) {
	s := newScript()
	var tr track
	got := states(&tr,
		s.hook(event.PromptSubmitted),
		s.hook(event.InputNeeded, "reason", "permission", "tool", "Bash", "summary", "rm -rf build"),
		s.hook(event.InputNeeded, "reason", "permission", "message", "Claude needs your permission to use Bash"),
		s.hook(event.ToolFinished, "tool", "Bash"),
		s.hook(event.ToolStarted, "tool", "AskUserQuestion", "tool_use_id", "t2"),
		s.hook(event.InputNeeded, "reason", "question", "question", "Which colour?"),
		s.hook(event.ToolFinished, "tool", "AskUserQuestion", "tool_use_id", "t2"),
		s.hook(event.TurnFailed, "error", "rate_limit"),
		s.hook(event.PromptSubmitted),
	)
	want := []string{"working", "needs_you", "needs_you", "working", "working", "needs_you", "working", "needs_you", "working"}
	if !slices.Equal(got, want) {
		t.Errorf("states %v, want %v", got, want)
	}

	// The permission row names the tool, not the later notification.
	tr = track{}
	tr.apply(s.hook(event.InputNeeded, "reason", "permission", "tool", "Bash", "summary", "rm -rf build"))
	tr.apply(s.hook(event.InputNeeded, "reason", "permission", "message", "Claude needs your permission to use Bash"))
	var row ipc.Session
	tr.describe(&row, false, 0)
	if row.Reason != "permission" || row.Detail != "needs permission: Bash · rm -rf build" {
		t.Errorf("row = %+v", row)
	}
	tr.apply(s.hook(event.TurnFailed, "error", "rate_limit"))
	tr.describe(&row, false, 0)
	if row.Reason != "rate_limit" || !strings.Contains(row.Detail, "rate_limit") {
		t.Errorf("row = %+v", row)
	}
}

func TestEachAgentAnswersForItself(t *testing.T) {
	s := newScript()
	var tr track
	got := states(&tr,
		s.hook(event.PromptSubmitted),
		s.sub(event.SubagentStarted),
		s.sub(event.InputNeeded, "reason", "permission", "tool", "Bash"),
		s.hook(event.ToolStarted, "tool", "Read", "tool_use_id", "t1"), // the orchestrator's work answers nothing for the subagent
		s.sub(event.SubagentFinished),
		s.hook(event.InputNeeded, "reason", "plan"),
		s.sub(event.ToolStarted, "tool", "Grep", "tool_use_id", "t3"),
	)
	want := []string{"working", "working", "needs_you", "needs_you", "working", "needs_you", "needs_you"}
	if !slices.Equal(got, want) {
		t.Errorf("states %v, want %v", got, want)
	}
}

func TestReadiness(t *testing.T) {
	s := newScript()
	var tr track
	tr.apply(s.irie(event.SpawnRequested))
	if !tr.awaiting() {
		t.Error("a new spawn is not awaited")
	}
	tr.apply(s.hook(event.SessionStarted))
	if !tr.awaiting() {
		t.Error("SessionStart alone made the spawn ready")
	}
	got := states(&tr, s.irie(event.InputNeeded, "reason", "stalled", "tail", "Do you trust the files in this folder?", "wait", "1s"), s.hook(event.PromptSubmitted))
	if !slices.Equal(got, []string{"stalled", "working"}) || tr.awaiting() {
		t.Errorf("states %v, awaiting %v", got, tr.awaiting())
	}

	tr = track{}
	tr.apply(s.irie(event.SpawnRequested))
	tr.apply(s.ev(event.PaneExited, "", event.SourceHost, "code", 1))
	tr.apply(s.irie(event.SpawnFailed, "reason", "exited before it was ready", "exit", 1, "tail", "error: unknown option\nfakeagent: exit 1"))
	var row ipc.Session
	tr.describe(&row, true, 1)
	if row.State != ipc.StateFailed || row.ExitCode == nil || *row.ExitCode != 1 || len(row.Tail) != 2 || row.Detail != "failed: exit 1 · fakeagent: exit 1" {
		t.Errorf("failed row = %+v", row)
	}
	if tr.awaiting() {
		t.Error("a failed spawn is still awaited")
	}
}

func TestProcessTruthWins(t *testing.T) {
	s := newScript()
	var tr track
	got := states(&tr,
		s.hook(event.PromptSubmitted),
		s.hook(event.InputNeeded, "reason", "question"),
		s.ev(event.PaneExited, "", event.SourceTmux, "code", 0),
		s.hook(event.PromptSubmitted), // late, from a shim that outlived the agent
	)
	if !slices.Equal(got, []string{"working", "needs_you", "exited", "exited"}) {
		t.Errorf("states %v", got)
	}

	// session.ended is the agent's word, and /clear starts it again.
	tr = track{}
	got = states(&tr, s.hook(event.PromptSubmitted), s.hook(event.SessionEnded, "reason", "clear"), s.hook(event.SessionStarted, "source", "clear"))
	if !slices.Equal(got, []string{"working", "exited", "working"}) {
		t.Errorf("states %v", got)
	}

	// With no events at all, a live pane is working and a dead one exited.
	tr = track{}
	if tr.state(false) != ipc.StateWorking || tr.state(true) != ipc.StateExited || tr.awaiting() {
		t.Errorf("an unknown session: %s / %s", tr.state(false), tr.state(true))
	}
	var row ipc.Session
	tr.describe(&row, true, 3)
	if row.ExitCode == nil || *row.ExitCode != 3 || row.Detail != "exited 3" {
		t.Errorf("row = %+v", row)
	}
}

func TestWorkingShowsTheRunningTool(t *testing.T) {
	s := newScript()
	var tr track
	tr.apply(s.hook(event.PromptSubmitted))
	tr.apply(s.hook(event.ToolStarted, "tool", "Bash", "summary", "go test ./...", "tool_use_id", "t1"))
	var row ipc.Session
	tr.describe(&row, false, 0)
	// An event from before the core recorded plain words shows the call.
	if row.Detail != "Bash · go test ./..." || row.Command != "" {
		t.Errorf("detail %q, command %q", row.Detail, row.Command)
	}
	tr.apply(s.hook(event.ToolFinished, "tool", "Bash", "tool_use_id", "t1"))
	row = ipc.Session{}
	tr.describe(&row, false, 0)
	if row.Detail != "" {
		t.Errorf("detail %q after the tool finished", row.Detail)
	}
}

func TestRowsSayPlainWordsAndKeepTheCall(t *testing.T) {
	s := newScript()
	var tr track
	describe := func() ipc.Session {
		var row ipc.Session
		tr.describe(&row, false, 0)
		return row
	}
	tr.apply(s.hook(event.PromptSubmitted))
	tr.apply(s.hook(event.ToolStarted, "tool", "Bash", "summary", "go test ./...", "phrase", "Running the tests", "tool_use_id", "t1"))
	if row := describe(); row.Detail != "Running the tests" || row.Command != "Bash · go test ./..." {
		t.Errorf("working: detail %q, command %q", row.Detail, row.Command)
	}
	tr.apply(s.hook(event.InputNeeded, "reason", event.ReasonPermission, "tool", "Bash", "summary", "rm -rf build",
		"phrase", "Asks: may it run rm -rf build?"))
	if row := describe(); row.Detail != "Asks: may it run rm -rf build?" || row.Command != "Bash · rm -rf build" {
		t.Errorf("permission: detail %q, command %q", row.Detail, row.Command)
	}

	// A question asks in its own words and names no call.
	tr = track{}
	tr.apply(s.hook(event.PromptSubmitted))
	tr.apply(s.hook(event.InputNeeded, "reason", event.ReasonQuestion, "question", "Which colour?", "tool", "AskUserQuestion",
		"phrase", "Asks: which colour?"))
	if row := describe(); row.Detail != "Asks: which colour?" || row.Command != "" {
		t.Errorf("question: detail %q, command %q", row.Detail, row.Command)
	}

	// A helper says what it does the same way.
	tr = track{}
	tr.apply(s.hook(event.PromptSubmitted))
	tr.apply(s.sub(event.SubagentStarted, "agent_type", "Explore"))
	tr.apply(s.sub(event.ToolStarted, "tool", "Read", "summary", "docs/DESIGN.md", "phrase", "Reading DESIGN.md", "tool_use_id", "t2"))
	if subs := describe().Subagents; len(subs) != 1 || subs[0].Detail != "Reading DESIGN.md" || subs[0].Command != "Read · docs/DESIGN.md" {
		t.Errorf("helper: %+v", subs)
	}
}
