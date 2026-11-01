package core

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
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
	vendorID  string                 // the conversation the last SessionStart named, which /clear changes
	resumed   bool                   // spawn.requested carried a conversation on, as a reopen or a move does
	// away is the conversation its agent handed to Claude Code's
	// background (←, /background, or an exit with work still running):
	// a copy under a new id, which the supervisor keeps going and whose
	// hooks still report here. Close ends it too (close.go).
	away string

	// How its last turn ended, for the jot dog (dog.go): how often the dog
	// sent it back since the user's last prompt, and the work of its own
	// in the background, or wakeups, that will wake it again.
	nudged    int
	waiting   int
	waitingOn string

	// For the trod bot (trod.go): when its last turn ended or failed,
	// and whether its last message asked the user something; what the
	// shift sent it, oldest first; when the shift last typed it a message
	// that has not come in as a prompt yet; and whether the shift told its
	// agent to decide a question itself and the agent has not worked since,
	// so that a question asked again waits for the user.
	stopped      time.Time
	asked        bool
	trod         []trodSend
	trodTyped    time.Time
	trodAnswered bool

	// What the user did to the session as a whole (freeze.go, priority.go,
	// order.go).
	frozen   *event.Event // freeze.on, until freeze.off
	held     bool         // a frozen session's running agent came to a tool call, where the shim holds it
	priority int          // the level of the last priority.set
	parked   bool         // ui.parked, until ui.unparked

	// The session's transcripts, then its finished subagents', in the
	// order the hooks named them: what the token tally reads. own are the
	// session's own, whose titles name it (title.go).
	transcripts []string
	own         []string
}

const (
	working = "working"
	done    = "done"
)

func (t *track) apply(e event.Event) {
	// Its agent is gone: its pane died, or its session ended other than
	// by /clear or /resume, after which the same process goes on.
	gone := t.exited != nil || t.ended != nil && t.ended.Str("reason") != "clear" && t.ended.Str("reason") != "resume"
	if e.Source == event.SourceHook && e.Kind != event.SessionEnded {
		// After /clear, Claude Code ends one session and starts the next.
		t.ended = nil
	}
	switch e.Kind {
	case event.SpawnRequested:
		// A reopened session starts over, but its tokens still count.
		*t = track{requested: e.Time(), transcripts: t.transcripts, own: t.own, resumed: e.Str("resume") != ""}
	case event.SpawnFailed:
		t.failed = &e
	case event.PaneExited:
		t.exited = &e
	case event.SessionStarted:
		t.started = true
		t.addTranscript(e.Str("transcript"))
		if p := e.Str("transcript"); p != "" && !slices.Contains(t.own, p) {
			t.own = append(t.own, p)
		}
		if e.Vendor != nil && e.Vendor.SessionID != "" {
			t.vendorID = e.Vendor.SessionID
			// A fork that starts once the agent is gone is the
			// conversation carried on in Claude Code's background.
			if gone && e.Str("source") == "fork" {
				t.away = e.Vendor.SessionID
			}
		}
		if e.Str("source") == "resume" && t.activity == "" {
			// A resumed conversation, such as one moved into Irie, waits at
			// its prompt: it is ready without one, and idle.
			t.activity, t.viewed = done, true
		}
	case event.PromptSubmitted:
		// A message the trod bot typed is not the user's prompt: what
		// the dog and the shift did since the user's last one still counts.
		shifts := !t.trodTyped.IsZero() && e.Time().Sub(t.trodTyped) < trodTypedFor
		t.prompted, t.activity, t.needs, t.stalled = true, working, nil, nil
		t.waiting, t.waitingOn, t.asked, t.trodTyped = 0, "", false, time.Time{}
		if !shifts {
			t.nudged, t.trodAnswered = 0, false
		}
	case event.ToolStarted:
		t.busy(e)
		if e.Str("tool") != "AskUserQuestion" {
			t.trodAnswered = false
		}
		t.held = t.held || t.frozen != nil
		if t.tools == nil {
			t.tools = map[string]event.Event{}
		}
		t.tools[toolUseID(e)] = e
	case event.ToolFinished, event.ToolFailed:
		t.busy(e)
		t.held = t.held || t.frozen != nil
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
		if e.Agent == event.Main {
			t.waiting, _ = e.Int("waiting")
			t.waitingOn = e.Str("waiting_on")
			t.stopped, t.asked = e.Time(), e.Data["asks"] == true
		}
	case event.DogNudged:
		// The turn goes on: the dog sent the agent back to finish.
		t.nudged++
		t.busy(e)
	case event.TrodSent:
		t.trod = append(t.trod, trodSend{at: e.Time(), reason: e.Str("reason"), how: e.Str("how")})
		if len(t.trod) > maxTrodSends {
			t.trod = t.trod[len(t.trod)-maxTrodSends:]
		}
		t.trodAnswered = t.trodAnswered || e.Str("reason") == trodQuestion
		if e.Str("how") == trodTyped {
			// Its prompt.submitted follows.
			t.trodTyped = e.Time()
			break
		}
		// The turn goes on: at its end, or past a question turned away.
		t.busy(e)
	case event.TurnFailed:
		t.activity, t.viewed = done, false
		t.stopTools(event.Main)
		t.need(e)
		t.stopped, t.asked = e.Time(), false
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
		t.addTranscript(e.Str("transcript"))
	case event.SessionEnded:
		t.ended = &e
		t.subs = nil
	case event.UIShown:
		t.viewed = true
	case event.FreezeOn:
		t.frozen, t.held = &e, false
	case event.FreezeOff:
		t.frozen, t.held = nil, false
	case event.PrioritySet:
		t.priority, _ = e.Int("level")
	case event.UIParked:
		t.parked = true
	case event.UIUnparked:
		t.parked = false
	}
}

func (t *track) addTranscript(path string) {
	if path != "" && !slices.Contains(t.transcripts, path) {
		t.transcripts = append(t.transcripts, path)
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

// conversed reports whether the agent got as far as a conversation that can
// be carried on: its prompt went in, or it showed life, or it carries one on
// already, even before it shows life, as a reopen does.
func (t *track) conversed() bool { return t.prompted || t.activity != "" || t.resumed }

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
			sa.NeedsYou = true
			sa.Detail, sa.Command = needLine(n)
		} else if tool, ok := t.latestToolOf(agent); ok {
			sa.Detail, sa.Command = callLine(tool)
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
	case t.frozen != nil:
		return ipc.StateFrozen
	}
	return t.live()
}

// live is the state of a session that has not ended, as if it were not
// frozen: what a frozen row still says it was doing.
func (t *track) live() string {
	switch {
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

// describe fills a row's state, reason, detail, tail and exit code, and
// what the user did to it. A frozen row keeps the reason and detail of what
// it was doing.
func (t *track) describe(s *ipc.Session, dead bool, deadStatus int) {
	s.State = t.state(dead)
	s.Priority, s.Parked = t.priority, t.parked
	code := func(e *event.Event, key string) *int {
		if n, ok := e.Int(key); ok {
			return &n
		}
		return nil
	}
	shown := s.State
	if s.State == ipc.StateFrozen {
		shown = t.live()
		at := t.frozen.Time()
		whole, _ := t.frozen.Data["whole"].(bool)
		s.Frozen, s.Freezing = &at, !whole && !t.held && t.activity == working
	}
	switch shown {
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
		if t.away != "" {
			s.Detail += " · moved to Claude Code's background"
		}
	case ipc.StateNeedsYou:
		n := t.latestNeed()
		if n.Kind == event.TurnFailed {
			s.Reason = n.Str("error")
			s.Detail = "turn failed: " + s.Reason
			break
		}
		s.Reason = n.Str("reason")
		s.Detail, s.Command = needLine(n)
	case ipc.StateStalled:
		s.Reason, s.Tail = event.ReasonStalled, lines(t.stalled.Str("tail"))
		s.Detail = "stalled: no hooks within " + t.stalled.Str("wait") + lastLine(s.Tail)
	case ipc.StateWorking:
		if tool, ok := t.latestTool(); ok {
			s.Detail, s.Command = callLine(tool)
		}
	case ipc.StateDone, ipc.StateIdle:
		// Work of its own in the background, or a wakeup, will wake it.
		if t.waiting > 0 {
			s.Detail = joinDetail("Waiting on its work in the background", t.waitingOn)
		}
	}
	if s.State == ipc.StateFrozen && t.frozen.Str("by") == trodBy {
		s.Detail = joinDetail("Frozen by the trod bot: "+t.frozen.Str("reason"), s.Detail)
	}
	if s.State != ipc.StateExited && s.State != ipc.StateFailed {
		s.Subagents = t.subagents()
	}
}

// callLine is what a running tool call shows: its plain words for the row,
// such as "Running the tests", and the call itself for the details, such as
// "Bash · go test ./...". The core records the words on the event (Phrase);
// an event from before it did has only the call, which is then the row's.
func callLine(e event.Event) (detail, command string) {
	call := joinDetail(e.Str("tool"), e.Str("summary"))
	if p := e.Str("phrase"); p != "" {
		return p, call
	}
	return call, ""
}

// needLine is what a need shows: its plain words, such as "Asks: may it run
// rm -rf build?", and for a permission the call it asks about. An event from
// before the core recorded words says it the older way.
func needLine(n event.Event) (detail, command string) {
	p := n.Str("phrase")
	if p == "" {
		return needDetail(n), ""
	}
	if n.Str("reason") == event.ReasonPermission && n.Str("tool") != "" {
		command = joinDetail(n.Str("tool"), n.Str("summary"))
	}
	return p, command
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
