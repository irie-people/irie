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
	UIClosed         = "ui.closed"
	UIReopened       = "ui.reopened"
	UIUndo           = "ui.undo"
	UIRedo           = "ui.redo"
	// UIShown records that the user showed a session, which turns a done
	// session idle (DESIGN §5.3). It is an addition to the table of §5.2.
	UIShown = "ui.shown"
	// UIReordered records that the user put a session elsewhere in the
	// list, by dragging it or by promoting it to the top (DESIGN §4.1).
	UIReordered = "ui.reordered"
	// Outcomes teach the router how accurate each profile is (DESIGN §6.4):
	// the user's rating of a session, and a task run again on a more
	// accurate profile, which says the first session fell short. A
	// failover carries a task on after a usage limit.
	OutcomeRated     = "outcome.rated"
	OutcomeEscalated = "outcome.escalated"
	RouteFailover    = "route.failover"
)

var kinds = map[string]bool{}

func init() {
	for _, k := range []string{
		SpawnRequested, SpawnFailed, SessionStarted, PromptSubmitted, ToolStarted, ToolFinished, ToolFailed,
		InputNeeded, TurnEnded, TurnFailed, SubagentStarted, SubagentFinished, SessionEnded, PaneExited,
		GateSet, GateHeld, GateReleased, FreezeOn, FreezeOff, PrioritySet, NoteQueued, NoteDelivered,
		UIRenamed, UIParked, UIUnparked, UIClosed, UIReopened, UIUndo, UIRedo, UIShown, UIReordered,
		OutcomeRated, OutcomeEscalated, RouteFailover,
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
		bad("session %q is not an Irie session id", e.Session)
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
