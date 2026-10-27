package core

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/irie-hq/irie/internal/event"
	"github.com/irie-hq/irie/internal/ipc"
)

// track folds a session's events into its state (DESIGN §5.3). Nothing in
// it is stored: a restarted core replays the journal into a new track.
type track struct {
	requested time.Time              // spawn.requested; zero when Irie did not see the spawn
	started   bool                   // session.started
	prompted  bool                   // prompt.submitted
	failed    *event.Event           // spawn.failed
	exited    *event.Event           // pane.exited
	ended     *event.Event           // session.ended, until the agent shows life again
	stalled   *event.Event           // the readiness timer's input.needed, until a prompt or tool event
	needs     map[string]event.Event // what each agent needs from the user
	activity  string                 // "", working or done
	viewed    bool                   // shown since the turn ended
	tools     map[string]event.Event // running tools' tool.started, by tool_use_id
	subs      map[string]event.Event // running subagents' subagent.started, by agent
}

const (
	working = "working"
	done    = "done"
)

func (t *track) apply(e event.Event) {
	if e.Source == event.SourceHook && e.Kind != event.SessionEnded {
		// After /clear, Claude Code ends one session and starts the next.
		t.ended = nil
	}
	switch e.Kind {
	case event.SpawnRequested:
		t.requested = e.Time()
	case event.SpawnFailed:
		t.failed = &e
	case event.PaneExited:
		t.exited = &e
	case event.SessionStarted:
		t.started = true
	case event.PromptSubmitted:
		t.prompted, t.activity, t.needs, t.stalled = true, working, nil, nil
	case event.ToolStarted:
		t.busy(e)
		if t.tools == nil {
			t.tools = map[string]event.Event{}
		}
		t.tools[toolUseID(e)] = e
	case event.ToolFinished, event.ToolFailed:
		t.busy(e)
		delete(t.tools, toolUseID(e))
	case event.InputNeeded:
		if e.Str("reason") == event.ReasonStalled {
			t.stalled = &e
			break
		}
		// A later notice of the same need, such as the permission_prompt
		// notification after a PermissionRequest, adds nothing.
		if old, ok := t.needs[e.Agent]; ok && old.Kind == event.InputNeeded && old.Str("reason") == e.Str("reason") {
			break
		}
		t.need(e)
	case event.TurnEnded:
		t.activity, t.viewed = done, false
		delete(t.needs, event.Main)
		t.stopTools(event.Main)
	case event.TurnFailed:
		t.activity, t.viewed = done, false
		t.stopTools(event.Main)
		t.need(e)
	case event.SubagentStarted:
		t.activity = working
		if t.subs == nil {
			t.subs = map[string]event.Event{}
		}
		t.subs[e.Agent] = e
	case event.SubagentFinished:
		delete(t.needs, e.Agent)
		t.stopTools(e.Agent)
		delete(t.subs, e.Agent)
	case event.SessionEnded:
		t.ended = &e
		t.subs = nil
	case event.UIShown:
		t.viewed = true
	}
}

// busy notes that an agent is at work, which answers whatever it needed.
func (t *track) busy(e event.Event) {
	t.activity, t.stalled = working, nil
	delete(t.needs, e.Agent)
}

func (t *track) need(e event.Event) {
	if t.needs == nil {
		t.needs = map[string]event.Event{}
	}
	t.needs[e.Agent] = e
}

func (t *track) stopTools(agent string) {
	for id, e := range t.tools {
		if e.Agent == agent {
			delete(t.tools, id)
		}
	}
}

func toolUseID(e event.Event) string {
	if e.Vendor != nil {
		return e.Vendor.ToolUseID
	}
	return ""
}

// ready reports whether the spawn became a live agent: SessionStart and
// UserPromptSubmit arrived, or the agent is plainly at work.
func (t *track) ready() bool { return t.started && t.prompted || t.activity != "" }

// awaiting reports whether Irie saw this session spawned and still waits for
// it to become ready.
func (t *track) awaiting() bool {
	return !t.requested.IsZero() && !t.ready() && t.failed == nil && t.exited == nil
}

// latestNeed is the newest of the agents' needs.
func (t *track) latestNeed() event.Event {
	var n event.Event
	for _, e := range t.needs {
		if e.Seq >= n.Seq {
			n = e
		}
	}
	return n
}

// latestTool is the newest running tool call.
func (t *track) latestTool() (event.Event, bool) { return t.latestToolOf("") }

// latestToolOf is an agent's newest running tool call; any agent's when
// agent is empty.
func (t *track) latestToolOf(agent string) (event.Event, bool) {
	var n event.Event
	for _, e := range t.tools {
		if (agent == "" || e.Agent == agent) && e.Seq >= n.Seq {
			n = e
		}
	}
	return n, n.Kind != ""
}

// subagents lists the running subagents, oldest first, each with what it
// needs from the user or else its newest running tool.
func (t *track) subagents() []ipc.Subagent {
	if len(t.subs) == 0 {
		return nil
	}
	out := make([]ipc.Subagent, 0, len(t.subs))
	for agent, e := range t.subs {
		sa := ipc.Subagent{Agent: agent, Type: e.Str("agent_type"), Started: e.Time()}
		if n, ok := t.needs[agent]; ok {
			sa.NeedsYou, sa.Detail = true, needDetail(n)
		} else if tool, ok := t.latestToolOf(agent); ok {
			sa.Detail = joinDetail(tool.Str("tool"), tool.Str("summary"))
		}
		out = append(out, sa)
	}
	slices.SortFunc(out, func(a, b ipc.Subagent) int {
		if c := a.Started.Compare(b.Started); c != 0 {
			return c
		}
		return strings.Compare(a.Agent, b.Agent)
	})
	return out
}

// state is the first state of DESIGN §5.3 that applies. dead is tmux's word
// that the pane died, before Irie has recorded it.
func (t *track) state(dead bool) string {
	switch {
	case t.failed != nil:
		return ipc.StateFailed
	case t.exited != nil || dead || t.ended != nil:
		return ipc.StateExited
	case len(t.needs) > 0:
		return ipc.StateNeedsYou
	case t.stalled != nil:
		return ipc.StateStalled
	case t.activity == working:
		return ipc.StateWorking
	case t.activity == done && t.viewed:
		return ipc.StateIdle
	case t.activity == done:
		return ipc.StateDone
	case !t.requested.IsZero():
		return ipc.StateStarting
	}
	// A session Irie has no events for, such as one started before it read
	// hooks, is alive and so taken to be working.
	return ipc.StateWorking
}

// describe fills a row's state, reason, detail, tail and exit code.
func (t *track) describe(s *ipc.Session, dead bool, deadStatus int) {
	s.State = t.state(dead)
	code := func(e *event.Event, key string) *int {
		if n, ok := e.Int(key); ok {
			return &n
		}
		return nil
	}
	switch s.State {
	case ipc.StateFailed:
		s.Reason, s.ExitCode, s.Tail = t.failed.Str("reason"), code(t.failed, "exit"), lines(t.failed.Str("tail"))
		s.Detail = "failed"
		if s.ExitCode != nil {
			s.Detail += fmt.Sprintf(": exit %d", *s.ExitCode)
		}
		s.Detail += lastLine(s.Tail)
	case ipc.StateExited:
		switch {
		case t.exited != nil:
			s.ExitCode = code(t.exited, "code")
		case dead:
			s.ExitCode = &deadStatus
		}
		s.Detail = "exited"
		if s.ExitCode != nil {
			s.Detail += fmt.Sprintf(" %d", *s.ExitCode)
		}
		if t.ended != nil {
			s.Reason = t.ended.Str("reason")
			if s.Reason != "" {
				s.Detail += " · " + s.Reason
			}
		}
	case ipc.StateNeedsYou:
		n := t.latestNeed()
		if n.Kind == event.TurnFailed {
			s.Reason = n.Str("error")
			s.Detail = "turn failed: " + s.Reason
			break
		}
		s.Reason = n.Str("reason")
		s.Detail = needDetail(n)
	case ipc.StateStalled:
		s.Reason, s.Tail = event.ReasonStalled, lines(t.stalled.Str("tail"))
		s.Detail = "stalled: no hooks within " + t.stalled.Str("wait") + lastLine(s.Tail)
	case ipc.StateWorking:
		if tool, ok := t.latestTool(); ok {
			s.Detail = joinDetail(tool.Str("tool"), tool.Str("summary"))
		}
	}
	if s.State != ipc.StateExited && s.State != ipc.StateFailed {
		s.Subagents = t.subagents()
	}
}

func needDetail(n event.Event) string {
	switch n.Str("reason") {
	case event.ReasonPermission:
		if tool := n.Str("tool"); tool != "" {
			return "needs permission: " + joinDetail(tool, n.Str("summary"))
		}
		return "needs permission: " + n.Str("message")
	case event.ReasonQuestion:
		if q := n.Str("question"); q != "" {
			return "asks: " + q
		}
		return "needs input: " + n.Str("message")
	case event.ReasonPlan:
		return "wants the plan approved"
	case event.ReasonElicitation:
		return strings.TrimSpace(n.Str("server")+" asks: ") + " " + n.Str("message")
	}
	return "needs you"
}

func joinDetail(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func lastLine(tail []string) string {
	if len(tail) == 0 {
		return ""
	}
	return " · " + tail[len(tail)-1]
}
