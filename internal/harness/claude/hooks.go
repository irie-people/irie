package claude

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/irie-hq/irie/internal/event"
	"github.com/irie-hq/irie/internal/harness"
)

// object is a JSON object such as a hook's input, read leniently: a field of
// an unexpected type reads as empty rather than failing the whole event.
type object map[string]json.RawMessage

func (p object) str(key string) string {
	var s string
	json.Unmarshal(p[key], &s)
	return s
}

// number reads a whole number, such as duration_ms.
func (p object) number(key string) (int, bool) {
	var f float64
	if json.Unmarshal(p[key], &f) != nil {
		return 0, false
	}
	return int(f), true
}

// length is a string's length before the shim cut it, in characters.
func (p object) length(key string) (int, bool) {
	var l map[string]float64
	json.Unmarshal(p["irie_lengths"], &l)
	n, ok := l[key]
	return int(n), ok
}

// Normalize maps Claude Code's hook events to envelope kinds (DESIGN §3.3,
// §5.2). An event Irie does not use gives no envelopes.
func (h Harness) Normalize(raw []byte) ([]event.Event, error) {
	var p object
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("claude: hook payload: %w", err)
	}
	hook, agentID, tool := p.str("hook_event_name"), p.str("agent_id"), p.str("tool_name")
	vendor := event.Vendor{
		SessionID: p.str("session_id"), PromptID: p.str("prompt_id"), ToolUseID: p.str("tool_use_id"),
		AgentID: agentID, AgentType: p.str("agent_type"), HookEvent: hook,
	}
	ev := func(kind string, data map[string]any) event.Event {
		v := vendor
		return event.Event{Source: event.SourceHook, Harness: h.ID(), Kind: kind, Agent: event.Agent(agentID), Vendor: &v, Data: data}
	}
	// with returns data with the non-empty values among key, value pairs.
	with := func(data map[string]any, kv ...string) map[string]any {
		for i := 0; i+1 < len(kv); i += 2 {
			if kv[i+1] != "" {
				data[kv[i]] = kv[i+1]
			}
		}
		return data
	}

	switch hook {
	case "SessionStart":
		return []event.Event{ev(event.SessionStarted, with(map[string]any{},
			"transcript", p.str("transcript_path"), "model", p.str("model"),
			"permission_mode", p.str("permission_mode"), "source", p.str("source")))}, nil
	case "UserPromptSubmit":
		chars := utf8.RuneCountInString(p.str("prompt"))
		if n, ok := p.length("prompt"); ok {
			chars = n
		}
		return []event.Event{ev(event.PromptSubmitted, map[string]any{"chars": chars})}, nil
	case "PreToolUse":
		out := []event.Event{ev(event.ToolStarted, with(map[string]any{"tool": tool}, "summary", summary(tool, p["tool_input"])))}
		switch tool {
		case "AskUserQuestion":
			out = append(out, ev(event.InputNeeded, with(map[string]any{"reason": event.ReasonQuestion}, "question", firstQuestion(p["tool_input"]))))
		case "ExitPlanMode":
			out = append(out, ev(event.InputNeeded, map[string]any{"reason": event.ReasonPlan}))
		}
		return out, nil
	case "PostToolUse", "PostToolUseFailure":
		kind := event.ToolFinished
		if hook == "PostToolUseFailure" {
			kind = event.ToolFailed
		}
		data := map[string]any{"tool": tool}
		if ms, ok := p.number("duration_ms"); ok {
			data["ms"] = ms
		}
		return []event.Event{ev(kind, data)}, nil
	case "PermissionRequest":
		return []event.Event{ev(event.InputNeeded, with(map[string]any{"reason": event.ReasonPermission},
			"tool", tool, "summary", summary(tool, p["tool_input"])))}, nil
	case "Notification":
		reason := map[string]string{"permission_prompt": event.ReasonPermission, "agent_needs_input": event.ReasonQuestion}[p.str("notification_type")]
		if reason == "" {
			return nil, nil
		}
		return []event.Event{ev(event.InputNeeded, with(map[string]any{"reason": reason}, "message", p.str("message")))}, nil
	case "Elicitation":
		return []event.Event{ev(event.InputNeeded, with(map[string]any{"reason": event.ReasonElicitation},
			"message", p.str("message"), "server", p.str("mcp_server_name")))}, nil
	case "Stop":
		// The turn's last message is what a delegated session reports back
		// (DESIGN §6.4).
		return []event.Event{ev(event.TurnEnded, with(map[string]any{}, "message", p.str("last_assistant_message")))}, nil
	case "StopFailure":
		return []event.Event{ev(event.TurnFailed, with(map[string]any{}, "error", p.str("error")))}, nil
	case "SubagentStart":
		return []event.Event{ev(event.SubagentStarted, with(map[string]any{}, "agent_type", p.str("agent_type")))}, nil
	case "SubagentStop":
		return []event.Event{ev(event.SubagentFinished, with(map[string]any{},
			"agent_type", p.str("agent_type"), "transcript", p.str("agent_transcript_path")))}, nil
	case "SessionEnd":
		return []event.Event{ev(event.SessionEnded, with(map[string]any{}, "reason", p.str("reason")))}, nil
	}
	return nil, nil
}

// summary is a one-line description of a tool call's input, for the detail
// line: the command, path, pattern, URL or query it works on.
func summary(tool string, input json.RawMessage) string {
	var in object
	if json.Unmarshal(input, &in) != nil {
		return ""
	}
	var s string
	switch tool {
	case "Bash", "PowerShell":
		s = in.str("command")
	case "Read", "Write", "Edit", "MultiEdit":
		s = in.str("file_path")
	case "NotebookEdit":
		s = in.str("notebook_path")
	case "Glob", "Grep":
		s = in.str("pattern")
	case "WebFetch":
		s = in.str("url")
	case "WebSearch":
		s = in.str("query")
	case "Agent", "Task":
		s = in.str("description")
	case "AskUserQuestion":
		s = firstQuestion(input)
	}
	return strings.Join(strings.Fields(s), " ")
}

// firstQuestion is the first question of an AskUserQuestion call.
func firstQuestion(input json.RawMessage) string {
	var in struct {
		Questions []struct {
			Question string `json:"question"`
		} `json:"questions"`
	}
	if json.Unmarshal(input, &in) != nil || len(in.Questions) == 0 {
		return ""
	}
	return strings.Join(strings.Fields(in.Questions[0].Question), " ")
}

type hookSpecific struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
	AdditionalContext        string `json:"additionalContext,omitempty"`
}

// Respond renders a reply as Claude Code's hook output (DESIGN §3.3): context
// on the events that take it, and a deny on PreToolUse. It never allows a
// call, which would bypass the user's permission rules, and it says nothing
// on other events.
func (Harness) Respond(e event.Event, r harness.HookReply) []byte {
	name := ""
	if e.Vendor != nil {
		name = e.Vendor.HookEvent
	}
	out := hookSpecific{HookEventName: name}
	switch name {
	case "PreToolUse":
		if r.Deny != "" {
			out.PermissionDecision, out.PermissionDecisionReason = "deny", r.Deny
		}
		out.AdditionalContext = r.Context
	case "PostToolUse", "PostToolUseFailure", "UserPromptSubmit", "SessionStart":
		out.AdditionalContext = r.Context
	default:
		return nil
	}
	if out.PermissionDecision == "" && out.AdditionalContext == "" {
		return nil
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.Encode(struct {
		HookSpecificOutput hookSpecific `json:"hookSpecificOutput"`
	}{out})
	return b.Bytes()
}
