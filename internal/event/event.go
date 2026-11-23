// Package event is Irie's normalised event envelope (DESIGN §5.1) and its
// kinds (§5.2). Every journal line and every event frame is one envelope.
package event

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/irie-people/irie/internal/brand"
)

// Version is the envelope version.
const Version = 1

// MaxString caps every string in an event's data, in bytes.
const MaxString = 2 << 10

// Sources.
const (
	SourceHook       = "hook"
	SourceHost       = "host"
	SourceTmux       = "tmux"
	SourceTranscript = "transcript"
	SourceIrie       = "irie"
	SourceUser       = "user"
)

var sources = []string{SourceHook, SourceHost, SourceTmux, SourceTranscript, SourceIrie, SourceUser}

// Kinds (DESIGN §5.2).
const (
	SpawnRequested   = "spawn.requested"
	SpawnFailed      = "spawn.failed"
	SessionStarted   = "session.started"
	PromptSubmitted  = "prompt.submitted"
	ToolStarted      = "tool.started"
	ToolFinished     = "tool.finished"
	ToolFailed       = "tool.failed"
	InputNeeded      = "input.needed"
	TurnEnded        = "turn.ended"
	TurnFailed       = "turn.failed"
	SubagentStarted  = "subagent.started"
	SubagentFinished = "subagent.finished"
	SessionEnded     = "session.ended"
	PaneExited       = "pane.exited"
	GateSet          = "gate.set"
	GateHeld         = "gate.held"
	GateReleased     = "gate.released"
	FreezeOn         = "freeze.on"
	FreezeOff        = "freeze.off"
	PrioritySet      = "priority.set"
	NoteQueued       = "note.queued"
	NoteDelivered    = "note.delivered"
	UIRenamed        = "ui.renamed"
	UIParked         = "ui.parked"
	UIUnparked       = "ui.unparked"
	UIPinned         = "ui.pinned"
	UIUnpinned       = "ui.unpinned"
	UISubStopped     = "ui.sub_stopped"
	UISpaced         = "ui.spaced"
	UIGrouped        = "ui.grouped"
	UIClosed         = "ui.closed"
	UIReopened       = "ui.reopened"
	UIUndo           = "ui.undo"
	UIRedo           = "ui.redo"
	// UIShown records that the user showed a session, which turns a done
	// session idle (DESIGN §5.3). It is an addition to the table of §5.2.
	UIShown = "ui.shown"
	// UIReordered records that the user put a session elsewhere in the
	// list, by dragging it or moving it up or down (DESIGN §4.1).
	UIReordered = "ui.reordered"
	// Outcomes teach the router how accurate each profile is (DESIGN §6.4):
	// the user's rating of a session, and a task run again on a more
	// accurate profile, which says the first session fell short. A
	// failover carries a task on after a usage limit.
	OutcomeRated     = "outcome.rated"
	OutcomeEscalated = "outcome.escalated"
	RouteFailover    = "route.failover"
	// DelegateTimedOut records that a delegated session was still at work
	// when its caller's wait ran out (wait_ms), so Irie stopped it and handed
	// the subtask on (to_profile): the router counts it as an answer that took at
	// least the wait (DESIGN §6.4).
	DelegateTimedOut = "delegate.timed_out"
	// RouteFallback records that Irie switched a session to its profile's
	// fallback model, in place, after its own model's usage limit stopped
	// a turn (DESIGN §6.4).
	RouteFallback = "route.fallback"
	// ModelSwitched records that a session's model changed while it ran:
	// by /model, at a fallback, or as a resume restored it. Its model is
	// the one it runs from then on.
	ModelSwitched = "model.switched"
	// DogNudged records that the jot dog sent a finished turn back to work,
	// because the jot's work had not landed (DESIGN §4.8). It stands in for
	// the turn.ended of that stop.
	DogNudged = "dog.nudged"
	// TrodSent records that the trod bot sent a session on while you
	// were away (DESIGN §4.10): at the end of a turn, where it stands in
	// for that stop's turn.ended; at a question it turned away, in place of
	// the question's tool.started and input.needed; or by a message it
	// typed, whose prompt.submitted follows.
	TrodSent = "trod.sent"
	// BoostOn records that Irie boosted a session whose turns failed on an
	// API error twice in a row (DESIGN §4.3): its whole process tree comes
	// out of the background until BoostOff, which a turn that ends without
	// an API error records, or the end of the boost's time.
	BoostOn  = "boost.on"
	BoostOff = "boost.off"
	// RestOn records that a session's agent ended to free memory while its
	// card stays (DESIGN §4.3): why, and the memory it took; RestOff, that
	// its agent started again, carrying its conversation on.
	RestOn  = "rest.on"
	RestOff = "rest.off"
	// CacheRebuilt records a request that wrote most of its conversation's
	// context into the prompt cache again instead of reading it, from the
	// session's transcript (DESIGN §6.3): what it wrote again, what that
	// cost and, when Irie knows, what woke the session.
	CacheRebuilt = "cache.rebuilt"
	// WorktreeDropped records that Irie removed a closed session's worktree
	// once its branch was in its repo's land branch, keeping the branch, so
	// it can be put back by hand; WorktreeRestored, that reopen added it
	// back from the branch (DESIGN §4.5).
	WorktreeDropped  = "worktree.dropped"
	WorktreeRestored = "worktree.restored"
)

var kinds = map[string]bool{}

func init() {
	for _, k := range []string{
		SpawnRequested, SpawnFailed, SessionStarted, PromptSubmitted, ToolStarted, ToolFinished, ToolFailed,
		InputNeeded, TurnEnded, TurnFailed, SubagentStarted, SubagentFinished, SessionEnded, PaneExited,
		GateSet, GateHeld, GateReleased, FreezeOn, FreezeOff, PrioritySet, NoteQueued, NoteDelivered,
		UIRenamed, UIParked, UIUnparked, UIPinned, UIUnpinned, UISubStopped, UISpaced, UIGrouped, UIClosed, UIReopened, UIUndo, UIRedo, UIShown, UIReordered,
		OutcomeRated, OutcomeEscalated, RouteFailover, DelegateTimedOut, RouteFallback, ModelSwitched, DogNudged, TrodSent, BoostOn, BoostOff, RestOn, RestOff, CacheRebuilt,
		WorktreeDropped, WorktreeRestored,
	} {
		kinds[k] = true
	}
}

// Known reports whether kind is an event kind.
func Known(kind string) bool { return kinds[kind] }

// Reasons an agent needs the user (input.needed).
const (
	ReasonPermission  = "permission"
	ReasonQuestion    = "question"
	ReasonPlan        = "plan"
	ReasonElicitation = "elicitation"
	ReasonStalled     = "stalled"
	// ReasonTurn is a teach-mode turn that ended on a request for the user
	// to write a part, at a TODO(human) (docs/proposals/learning.md): the
	// agent waits at its prompt for them.
	ReasonTurn = "turn"
)

// Event is one envelope.
type Event struct {
	V       int            `json:"v"`
	Seq     uint64         `json:"seq"`     // assigned by the journal; strictly increasing
	TS      string         `json:"ts"`      // RFC 3339 UTC with milliseconds
	Session string         `json:"session"` // Irie session id
	Agent   string         `json:"agent"`   // main or sub:<vendor agent id>; empty for session-wide events
	Source  string         `json:"source"`
	Harness string         `json:"harness"`
	Kind    string         `json:"kind"`
	Vendor  *Vendor        `json:"vendor,omitempty"`
	Data    map[string]any `json:"data"`
}

// Vendor holds the harness's own ids, verbatim.
type Vendor struct {
	SessionID string `json:"session_id,omitempty"`
	PromptID  string `json:"prompt_id,omitempty"`
	ToolUseID string `json:"tool_use_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	AgentType string `json:"agent_type,omitempty"`
	HookEvent string `json:"hook_event,omitempty"`
}

// Main is the agent name of an orchestrator.
const Main = "main"

// Agent names the agent of a vendor agent id: main without one.
func Agent(agentID string) string {
	if agentID == "" {
		return Main
	}
	return "sub:" + agentID
}

const stampLayout = "2006-01-02T15:04:05.000Z"

// Stamp formats t as an envelope ts: RFC 3339 in UTC with milliseconds.
func Stamp(t time.Time) string { return t.UTC().Format(stampLayout) }

// Time parses the event's ts; it is zero when ts is not a stamp.
func (e Event) Time() time.Time {
	t, _ := time.Parse(stampLayout, e.TS)
	return t
}

// Str returns a string from the event's data, or "".
func (e Event) Str(key string) string {
	s, _ := e.Data[key].(string)
	return s
}

// Int returns a whole number from the event's data. Data read back from JSON
// holds float64s.
func (e Event) Int(key string) (int, bool) {
	switch n := e.Data[key].(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), n == float64(int(n))
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	}
	return 0, false
}

// Cap cuts s to at most MaxString bytes without splitting a rune.
func Cap(s string) string {
	if len(s) <= MaxString {
		return s
	}
	cut := MaxString
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// capGap stands where CapEnds cut a string's middle out.
const capGap = "\n…\n"

// CapEnds caps s at MaxString bytes as Cap does, but by cutting out its
// middle: it keeps a quarter for the head and the rest for the tail. A
// command's output says how it went at its end, as a test run's FAIL lines
// do, and a failed call's error starts with its exit code.
func CapEnds(s string) string {
	if len(s) <= MaxString {
		return s
	}
	head := MaxString / 4
	for head > 0 && !utf8.RuneStart(s[head]) {
		head--
	}
	tail := len(s) - (MaxString - head - len(capGap))
	for tail < len(s) && !utf8.RuneStart(s[tail]) {
		tail++
	}
	return s[:head] + capGap + s[tail:]
}

// CapAll caps every string inside v, which holds decoded JSON (maps, slices
// and scalars), in place where it can, and returns the result.
func CapAll(v any) any {
	switch x := v.(type) {
	case string:
		return Cap(x)
	case map[string]any:
		for k, item := range x {
			x[k] = CapAll(item)
		}
	case []any:
		for i, item := range x {
			x[i] = CapAll(item)
		}
	}
	return v
}

var sessionID = regexp.MustCompile(`^s[0-9a-hjkmnp-tv-z]{12}$`)

// Validate checks an envelope against DESIGN §5.1.
func Validate(e Event) error {
	var errs []error
	bad := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }
	if e.V != Version {
		bad("v = %d, want %d", e.V, Version)
	}
	if e.Seq == 0 {
		bad("seq is 0")
	}
	if t, err := time.Parse(stampLayout, e.TS); err != nil || Stamp(t) != e.TS {
		bad("ts %q is not RFC 3339 UTC with milliseconds", e.TS)
	}
	if !sessionID.MatchString(e.Session) {
		bad("session %q is not an "+brand.Name+" session id", e.Session)
	}
	if e.Agent != "" && e.Agent != Main && (!strings.HasPrefix(e.Agent, "sub:") || e.Agent == "sub:") {
		bad("agent %q is not main or sub:<id>", e.Agent)
	}
	if !slices.Contains(sources, e.Source) {
		bad("source %q", e.Source)
	}
	if e.Harness == "" {
		bad("no harness")
	}
	if !Known(e.Kind) {
		bad("kind %q", e.Kind)
	}
	if e.Data == nil {
		bad("no data")
	} else if err := capped(e.Data, "data"); err != nil {
		errs = append(errs, err)
	}
	if v := e.Vendor; v != nil {
		for _, s := range []string{v.SessionID, v.PromptID, v.ToolUseID, v.AgentID, v.AgentType, v.HookEvent} {
			if len(s) > MaxString {
				bad("a vendor id is %d bytes", len(s))
			}
		}
	}
	return errors.Join(errs...)
}

func capped(v any, where string) error {
	switch x := v.(type) {
	case string:
		if len(x) > MaxString {
			return fmt.Errorf("%s is %d bytes, over %d", where, len(x), MaxString)
		}
	case map[string]any:
		for k, item := range x {
			if err := capped(item, where+"."+k); err != nil {
				return err
			}
		}
	case []any:
		for i, item := range x {
			if err := capped(item, fmt.Sprintf("%s[%d]", where, i)); err != nil {
				return err
			}
		}
	}
	return nil
}
