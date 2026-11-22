package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/harness"
)

// Synthetic payloads shaped like the hook input of Claude Code's hooks
// reference, with the fields DESIGN §3.3 relies on.
const (
	transcript = "/home/alice/transcripts/" + vendorID + ".jsonl"
	common     = `"session_id":"` + vendorID + `","transcript_path":"` + transcript + `","cwd":"/home/alice/code/website","permission_mode":"auto"`
	inSub      = `"agent_id":"a1b2c3","agent_type":"Explore"`
	bashCall   = `"tool_name":"Bash","tool_input":{"command":"go test ./...","description":"Run the tests"},"tool_use_id":"toolu_01"`
)

func payload(event string, fields ...string) []byte {
	return []byte(`{` + common + `,"hook_event_name":"` + event + `"` + prefixed(fields) + `}`)
}

func prefixed(fields []string) string {
	var b strings.Builder
	for _, f := range fields {
		b.WriteString("," + f)
	}
	return b.String()
}

// want is one expected envelope, without the fields the core stamps.
type want struct {
	kind  string
	agent string
	data  map[string]any
}

func check(t *testing.T, name string, raw []byte, wants ...want) []event.Event {
	t.Helper()
	got, err := Harness{}.Normalize(raw)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if len(got) != len(wants) {
		t.Fatalf("%s: %d events %+v, want %d", name, len(got), got, len(wants))
	}
	for i, w := range wants {
		e := got[i]
		if e.Kind != w.kind || e.Agent != w.agent || e.Source != event.SourceHook || e.Harness != "claude-code" {
			t.Errorf("%s[%d] = %s/%s/%s/%s, want %s/%s/hook/claude-code", name, i, e.Kind, e.Agent, e.Source, e.Harness, w.kind, w.agent)
		}
		// Compare data through JSON, as the journal stores it.
		gb, _ := json.Marshal(e.Data)
		wb, _ := json.Marshal(w.data)
		if string(gb) != string(wb) {
			t.Errorf("%s[%d] data = %s, want %s", name, i, gb, wb)
		}
		if e.Vendor == nil || e.Vendor.SessionID != vendorID || e.Vendor.HookEvent == "" {
			t.Errorf("%s[%d] vendor = %+v", name, i, e.Vendor)
		}
	}
	return got
}

func TestNormalize(t *testing.T) {
	check(t, "SessionStart", payload("SessionStart", `"source":"startup"`, `"model":"claude-opus-5-5"`),
		want{event.SessionStarted, "main", map[string]any{"model": "claude-opus-5-5", "permission_mode": "auto", "source": "startup", "transcript": transcript}})
	check(t, "UserPromptSubmit", payload("UserPromptSubmit", `"prompt":"fix the flaky tést"`),
		want{event.PromptSubmitted, "main", map[string]any{"chars": 18}})
	pre := check(t, "PreToolUse", payload("PreToolUse", bashCall),
		want{event.ToolStarted, "main", map[string]any{"summary": "go test ./...", "tool": "Bash"}})
	if v := pre[0].Vendor; v.ToolUseID != "toolu_01" || v.HookEvent != "PreToolUse" {
		t.Errorf("vendor = %+v", v)
	}
	sub := check(t, "PreToolUse in a subagent", payload("PreToolUse", bashCall, inSub),
		want{event.ToolStarted, "sub:a1b2c3", map[string]any{"summary": "go test ./...", "tool": "Bash"}})
	if v := sub[0].Vendor; v.AgentID != "a1b2c3" || v.AgentType != "Explore" {
		t.Errorf("vendor = %+v", v)
	}
	check(t, "AskUserQuestion", payload("PreToolUse", `"tool_name":"AskUserQuestion"`, `"tool_use_id":"toolu_02"`,
		`"tool_input":{"questions":[{"question":"Which colour should the button be?","header":"Colour","options":[{"label":"Green"},{"label":"Gold"}],"multiSelect":false}]}`),
		want{event.ToolStarted, "main", map[string]any{"summary": "Which colour should the button be?", "tool": "AskUserQuestion"}},
		want{event.InputNeeded, "main", map[string]any{"question": "Which colour should the button be?", "reason": "question"}})
	check(t, "ExitPlanMode", payload("PreToolUse", `"tool_name":"ExitPlanMode"`, `"tool_use_id":"toolu_03"`, `"tool_input":{"plan":"## Plan\n1. Fix it"}`),
		want{event.ToolStarted, "main", map[string]any{"tool": "ExitPlanMode"}},
		want{event.InputNeeded, "main", map[string]any{"reason": "plan"}})
	check(t, "PostToolUse", payload("PostToolUse", bashCall, `"tool_response":{"stdout":"ok  \texample.com/x\t0.1s\n","stderr":"","interrupted":false}`, `"duration_ms":1200`),
		want{event.ToolFinished, "main", map[string]any{"ms": 1200, "tests": "passed", "tool": "Bash"}})
	check(t, "PostToolUseFailure", payload("PostToolUseFailure", bashCall, `"error":"Exit code 1\nFAIL"`, `"duration_ms":4187`),
		want{event.ToolFailed, "main", map[string]any{"ms": 4187, "test_line": "FAIL", "tests": "failed", "tool": "Bash"}})
	check(t, "PermissionRequest", payload("PermissionRequest", `"tool_name":"Bash"`, `"tool_input":{"command":"rm -rf node_modules"}`),
		want{event.InputNeeded, "main", map[string]any{"reason": "permission", "summary": "rm -rf node_modules", "tool": "Bash"}})
	check(t, "Notification permission_prompt", payload("Notification", `"notification_type":"permission_prompt"`, `"message":"Claude needs your permission to use Bash"`),
		want{event.InputNeeded, "main", map[string]any{"message": "Claude needs your permission to use Bash", "reason": "permission"}})
	check(t, "Notification agent_needs_input", payload("Notification", `"notification_type":"agent_needs_input"`, `"message":"Waiting for your input"`),
		want{event.InputNeeded, "main", map[string]any{"message": "Waiting for your input", "reason": "question"}})
	check(t, "Notification idle_prompt", payload("Notification", `"notification_type":"idle_prompt"`, `"message":"Claude is waiting for your input"`))
	check(t, "Elicitation", payload("Elicitation", `"mcp_server_name":"tracker"`, `"message":"Pick a project"`, `"mode":"form"`),
		want{event.InputNeeded, "main", map[string]any{"message": "Pick a project", "reason": "elicitation", "server": "tracker"}})
	check(t, "Stop", payload("Stop", `"stop_hook_active":false`, `"last_assistant_message":"Done."`),
		want{event.TurnEnded, "main", map[string]any{"message": "Done."}})
	check(t, "Stop without a message", payload("Stop", `"stop_hook_active":false`),
		want{event.TurnEnded, "main", map[string]any{}})
	check(t, "Stop that a stop hook sent on", payload("Stop", `"stop_hook_active":true`, `"last_assistant_message":"Done."`, `"background_tasks":[]`, `"session_crons":[]`),
		want{event.TurnEnded, "main", map[string]any{"message": "Done.", "stop_hook_active": true, "subagents": 0}})
	check(t, "Stop with work in the background", payload("Stop", `"stop_hook_active":false`, `"last_assistant_message":"Waiting on the install."`,
		`"background_tasks":[{"id":"task-001","type":"shell","status":"running","description":"Install the build\n  when the lock frees","command":"tools/live.sh"}]`,
		`"session_crons":[{"id":"cron-001","schedule":"*/5 * * * *","recurring":true,"prompt":"check the build"}]`),
		want{event.TurnEnded, "main", map[string]any{"message": "Waiting on the install.", "subagents": 0, "waiting": 2, "waiting_on": "Install the build when the lock frees"}})
	// Its helpers and teammates in the background: the subagents are
	// counted, so the core knows which helpers can still be at work.
	check(t, "Stop with helpers in the background", payload("Stop", `"stop_hook_active":false`, `"last_assistant_message":"Three helpers are on it."`,
		`"background_tasks":[{"id":"a1b2c3","type":"subagent","status":"running","description":"Find the seed patents","agent_type":"Explore"},`+
			`{"id":"a4d5e6","type":"subagent","status":"running","description":"Read the claims","agent_type":"general-purpose"},`+
			`{"id":"t-1","type":"teammate","status":"running","description":"Expand the citations"}]`,
		`"session_crons":[]`),
		want{event.TurnEnded, "main", map[string]any{"message": "Three helpers are on it.", "subagents": 2, "waiting": 3, "waiting_on": "Find the seed patents"}})
	check(t, "Stop whose task registry was out of reach", payload("Stop", `"stop_hook_active":false`, `"background_tasks":null`),
		want{event.TurnEnded, "main", map[string]any{}})
	check(t, "StopFailure", payload("StopFailure", `"error":"rate_limit"`, `"error_details":"429 Too Many Requests"`),
		want{event.TurnFailed, "main", map[string]any{"error": "rate_limit", "details": "429 Too Many Requests"}})
	check(t, "StopFailure with its message", payload("StopFailure", `"error":"server_error"`, `"last_assistant_message":"API Error: The response stopped arriving"`),
		want{event.TurnFailed, "main", map[string]any{"error": "server_error", "message": "API Error: The response stopped arriving"}})
	check(t, "SubagentStart", payload("SubagentStart", inSub),
		want{event.SubagentStarted, "sub:a1b2c3", map[string]any{"agent_type": "Explore"}})
	subTranscript := "/home/alice/transcripts/" + vendorID + "/subagents/agent-a1b2c3.jsonl"
	check(t, "SubagentStop", payload("SubagentStop", inSub, `"agent_transcript_path":"`+subTranscript+`"`),
		want{event.SubagentFinished, "sub:a1b2c3", map[string]any{"agent_type": "Explore", "transcript": subTranscript}})
	check(t, "SessionEnd", payload("SessionEnd", `"reason":"prompt_input_exit"`),
		want{event.SessionEnded, "main", map[string]any{"reason": "prompt_input_exit"}})
	// A model switch: the model it switched to, and who asked (DESIGN §6.4).
	check(t, "PostModelSwitch", payload("PostModelSwitch", `"from_model":"claude-fable-5-1"`, `"to_model":"claude-opus-5-5"`,
		`"requested_model":"claude-opus-5-5"`, `"source":"command"`, `"context_tokens":52624`, `"prompt_cache_warm":true`),
		want{event.ModelSwitched, "main", map[string]any{"model": "claude-opus-5-5", "from": "claude-fable-5-1", "requested": "claude-opus-5-5", "source": "command"}})
	check(t, "PostModelSwitch at a fallback", payload("PostModelSwitch", `"from_model":"claude-fable-5-1"`, `"to_model":"claude-opus-5-5"`,
		`"requested_model":null`, `"source":"auto"`),
		want{event.ModelSwitched, "main", map[string]any{"model": "claude-opus-5-5", "from": "claude-fable-5-1", "source": "auto"}})
	check(t, "a switch before it is made", payload("PreModelSwitch", `"to_model":"claude-opus-5-5"`))
	check(t, "an event Irie does not use", payload("PostToolBatch"))
}

// An `irie ab` run's verdict, from its report, the error it exits 1 with
// when the outputs differ, or its JSON (DESIGN §4.24).
func TestNormalizeReadsABRuns(t *testing.T) {
	call := func(more string) string {
		return `"tool_name":"Bash","tool_input":{"command":"` + brand.Slug + ` ab -n 10 ./gold ./new -- big.ldif"` + more + `},"tool_use_id":"toolu_10"`
	}
	slower := `"tool_response":{"stdout":"time   1.2s   1.5s   25% more\n\nnew is slower, with the same output\n","stderr":""}`
	check(t, "slower", payload("PostToolUse", call(""), slower),
		want{event.ToolFinished, "main", map[string]any{"ab": "slower", "ab_line": "new is slower, with the same output", "tool": "Bash"}})
	differs := `"error":"Exit code 1\noutput  differs at line 3:\n` + brand.Slug + `: ab: new's output differs from gold's, so its times are for different work"`
	check(t, "differs", payload("PostToolUseFailure", call(""), differs),
		want{event.ToolFailed, "main", map[string]any{"ab": "differs", "ab_line": brand.Slug + ": ab: new's output differs from gold's, so its times are for different work", "tool": "Bash"}})
	asJSON := `"tool_response":{"stdout":"{\"same\": true, \"time\": {\"change\": \"lower\"}}","stderr":""}`
	check(t, "as JSON", payload("PostToolUse", call(""), asJSON),
		want{event.ToolFinished, "main", map[string]any{"ab": "faster", "ab_line": `"time": {"change": "lower"}`, "tool": "Bash"}})
	check(t, "in the background", payload("PostToolUse", call(`,"run_in_background":true`), slower),
		want{event.ToolFinished, "main", map[string]any{"tool": "Bash"}})
}

// A shell command's tests, as the hooks reference gives a call's end: what
// it printed, or an error that starts with its exit code (DESIGN §4.8).
func TestNormalizeReadsTestRuns(t *testing.T) {
	call := func(cmd string, more string) string {
		return `"tool_name":"Bash","tool_input":{"command":"` + cmd + `"` + more + `},"tool_use_id":"toolu_09"`
	}
	failing := `"error":"Exit code 1\n--- FAIL: TestCards (0.00s)\n    cards_test.go:9: got 2\nFAIL\nFAIL\texample.com/x\t0.1s\nFAIL"`
	check(t, "a failure names its test", payload("PostToolUseFailure", call("cd /home/alice/code/website \u0026\u0026 go test ./...", ""), failing),
		want{event.ToolFailed, "main", map[string]any{"test_line": "--- FAIL: TestCards (0.00s)", "tests": "failed", "tool": "Bash"}})
	check(t, "a failure that tail lets through", payload("PostToolUse", call("go test ./... 2\u003e\u00261 | tail -3", ""), `"tool_response":{"stdout":"FAIL\texample.com/x\t0.1s\nFAIL\n","stderr":""}`),
		want{event.ToolFinished, "main", map[string]any{"test_line": "FAIL\texample.com/x\t0.1s", "tests": "failed", "tool": "Bash"}})
	check(t, "grep -v left nothing", payload("PostToolUseFailure", call("go test ./... 2\u003e\u00261 | grep -v ^ok", ""), `"error":"Exit code 1\n"`),
		want{event.ToolFailed, "main", map[string]any{"tests": "ran", "tool": "Bash"}})
	check(t, "in the background", payload("PostToolUse", call("go test ./...", `,"run_in_background":true`), `"tool_response":{"stdout":"","stderr":""}`),
		want{event.ToolFinished, "main", map[string]any{"tests": "ran", "tool": "Bash"}})
	check(t, "cut short", payload("PostToolUse", call("go test ./...", ""), `"tool_response":{"stdout":"FAIL","stderr":"","interrupted":true}`),
		want{event.ToolFinished, "main", map[string]any{"tests": "ran", "tool": "Bash"}})
	check(t, "aborted", payload("PostToolUseFailure", call("go test ./...", ""), `"error":"Exit code 1\nFAIL"`, `"is_interrupt":true`),
		want{event.ToolFailed, "main", map[string]any{"tests": "ran", "tool": "Bash"}})
	check(t, "timed out", payload("PostToolUseFailure", call("go test ./...", ""), `"error":"Command timed out after 2m 0s"`),
		want{event.ToolFailed, "main", map[string]any{"tests": "ran", "tool": "Bash"}})
	check(t, "no tests", payload("PostToolUseFailure", call("git status", ""), `"error":"Exit code 1\nFAIL"`),
		want{event.ToolFailed, "main", map[string]any{"tool": "Bash"}})
	check(t, "not a shell command", payload("PostToolUse", `"tool_name":"Read","tool_input":{"file_path":"/home/alice/go test"},"tool_use_id":"toolu_10"`, `"tool_response":"FAIL"`),
		want{event.ToolFinished, "main", map[string]any{"tool": "Read"}})
}

func TestNormalizeCountsTheWholePrompt(t *testing.T) {
	// The shim cuts long strings and records how long they were.
	raw := payload("UserPromptSubmit", `"prompt":"`+strings.Repeat("x", 2048)+`"`, `"irie_lengths":{"prompt":3000000}`)
	check(t, "a capped prompt", raw, want{event.PromptSubmitted, "main", map[string]any{"chars": 3000000}})
}

func TestNormalizeIsLenient(t *testing.T) {
	// A field of an unexpected type is ignored, not fatal.
	check(t, "odd types", payload("SessionStart", `"source":"startup"`, `"model":{"id":"claude-opus-5-5"}`),
		want{event.SessionStarted, "main", map[string]any{"permission_mode": "auto", "source": "startup", "transcript": transcript}})
	if _, err := (Harness{}).Normalize([]byte("not json")); err == nil {
		t.Error("no error for a payload that is not JSON")
	}
}

func TestToolSummaries(t *testing.T) {
	for _, c := range []struct{ tool, input, want string }{
		{"Bash", `{"command":"npm test\n  --watch=false"}`, "npm test --watch=false"},
		{"Read", `{"file_path":"/home/alice/code/website/main.go"}`, "/home/alice/code/website/main.go"},
		{"Edit", `{"file_path":"/home/alice/a.go","old_string":"x","new_string":"y"}`, "/home/alice/a.go"},
		{"Grep", `{"pattern":"TODO","path":"/home/alice"}`, "TODO"},
		{"WebFetch", `{"url":"https://example.com/docs","prompt":"summarise"}`, "https://example.com/docs"},
		{"WebSearch", `{"query":"tmux swap-pane"}`, "tmux swap-pane"},
		{"Agent", `{"prompt":"Find all API endpoints","description":"Find API endpoints","subagent_type":"Explore"}`, "Find API endpoints"},
		{"mcp__tracker__create", `{"title":"x"}`, ""},
		{"Bash", `"not an object"`, ""},
	} {
		raw := payload("PreToolUse", `"tool_name":"`+c.tool+`"`, `"tool_use_id":"toolu_09"`, `"tool_input":`+c.input)
		evs, err := Harness{}.Normalize(raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := evs[0].Str("summary"); got != c.want {
			t.Errorf("%s %s: summary %q, want %q", c.tool, c.input, got, c.want)
		}
	}
}

func ev(hook string) event.Event { return event.Event{Vendor: &event.Vendor{HookEvent: hook}} }

func TestRespond(t *testing.T) {
	note := harness.HookReply{Context: "[Irie] Note from the user: use the staging database"}
	for _, c := range []struct {
		hook  string
		reply harness.HookReply
		want  string
	}{
		{"PreToolUse", note, `{"hookSpecificOutput":{"hookEventName":"PreToolUse","additionalContext":"[Irie] Note from the user: use the staging database"}}`},
		{"PostToolUse", note, `{"hookSpecificOutput":{"hookEventName":"PostToolUse","additionalContext":"[Irie] Note from the user: use the staging database"}}`},
		{"PostToolUseFailure", note, `{"hookSpecificOutput":{"hookEventName":"PostToolUseFailure","additionalContext":"[Irie] Note from the user: use the staging database"}}`},
		{"UserPromptSubmit", note, `{"hookSpecificOutput":{"hookEventName":"UserPromptSubmit","additionalContext":"[Irie] Note from the user: use the staging database"}}`},
		{"SessionStart", note, `{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"[Irie] Note from the user: use the staging database"}}`},
		{"PreToolUse", harness.HookReply{Deny: "[Irie] Still paused by the user; this call did not run. Retry the same call to keep waiting."},
			`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"[Irie] Still paused by the user; this call did not run. Retry the same call to keep waiting."}}`},
		{"PreToolUse", harness.HookReply{}, ``},
		{"PostToolUse", harness.HookReply{Deny: "no"}, ``},
		{"Stop", note, ``},
		{"Stop", harness.HookReply{Unfinished: "[Irie] The jot dog: 2 commits are not in live yet."},
			`{"hookSpecificOutput":{"hookEventName":"Stop","additionalContext":"[Irie] The jot dog: 2 commits are not in live yet."}}`},
		{"SubagentStop", harness.HookReply{Unfinished: "x"}, ``},
		// Allow is for a model switch alone, never a tool call.
		{"PreToolUse", harness.HookReply{Allow: true}, ``},
		{"PermissionRequest", harness.HookReply{Allow: true}, ``},
		{"PostModelSwitch", harness.HookReply{Allow: true}, ``},
		{"PreModelSwitch", note, ``},
		{"Notification", note, ``},
		{"PermissionRequest", note, ``},
		{"SessionEnd", note, ``},
	} {
		got := strings.TrimSpace(string(Harness{}.Respond(ev(c.hook), c.reply)))
		if got != c.want {
			t.Errorf("%s %+v:\n got %s\nwant %s", c.hook, c.reply, got, c.want)
		}
		if strings.Contains(got, `"allow"`) {
			t.Errorf("%s: a reply allows the call: %s", c.hook, got)
		}
	}
}

func TestVersions(t *testing.T) {
	for in, want := range map[string]string{
		"2.1.310 (Claude Code)\n": "2.1.310",
		"claude 2.1.295":          "2.1.295",
		"v2.2.0-beta.1":           "2.2.0",
		"GNU bash, version 3.2":   "",
		"":                        "",
	} {
		if got := ParseVersion(in); got != want {
			t.Errorf("ParseVersion(%q) = %q, want %q", in, got, want)
		}
	}
	for _, c := range []struct {
		v  string
		ok bool
	}{{"2.1.295", true}, {"2.1.294", false}, {"2.1.1000", true}, {"2.2.0", true}, {"10.0.0", true}, {"1.9.999", false}, {"", false}} {
		if got := AtLeast(c.v, MinVersion); got != c.ok {
			t.Errorf("AtLeast(%q, %s) = %v", c.v, MinVersion, got)
		}
	}
}

// fakeCLI writes a shell script that answers --version and --help like
// Claude Code, and counts its runs in a file next to it.
func fakeCLI(t *testing.T, version, help string) (path, runs string) {
	t.Helper()
	// This script answers at once, but a loaded machine can be slow to start
	// it. Only TestProbeGivesUpOnAHungCLI, which writes its own, wants the
	// timeout to fire.
	old := probeTimeout
	probeTimeout = time.Minute
	t.Cleanup(func() { probeTimeout = old })
	dir := t.TempDir()
	path, runs = filepath.Join(dir, "claude"), filepath.Join(dir, "runs")
	script := "#!/bin/sh\necho run >> '" + runs + "'\nfor a; do case $a in\n" +
		"--version) printf '%s\\n' '" + version + "'; exit 0;;\n" +
		"--help) printf '%s\\n' '" + help + "'; exit 0;;\n" +
		"esac; done\nsleep 30\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, runs
}

func countRuns(t *testing.T, runs string) int {
	b, _ := os.ReadFile(runs)
	return strings.Count(string(b), "run")
}

// helpWithName is what --help of a recent Claude Code lists of the
// optional flags.
const helpWithName = "  --agent <agent>      Agent for the current session\n" +
	"  --agents <json>      JSON object defining custom agents\n" +
	"  -n, --name <name>   Set a display name for this session"

func TestProbeReadsTheVersionAndFlags(t *testing.T) {
	path, _ := fakeCLI(t, "2.1.310 (Claude Code)", helpWithName)
	p := Harness{}.Probe(config.Profile{Command: path, Args: []string{"--verbose"}}, os.Environ())
	want := harness.Probe{
		Version:  "2.1.310",
		Flags:    map[string]bool{"--name": true, "--agents": true, "--agent": true},
		Warnings: []string{"--plugin-dir left out: `" + path + " --help` does not list it"},
	}
	if !reflect.DeepEqual(p, want) {
		t.Errorf("probe = %+v, want %+v", p, want)
	}
}

// TestProbeFindsTheAppendFlagQuietly checks that --append-system-prompt, which
// only tutor mode's diagrams need, is found without a warning when missing.
func TestProbeFindsTheAppendFlagQuietly(t *testing.T) {
	path, _ := fakeCLI(t, "2.1.316 (Claude Code)", helpWithName+"\n  --append-system-prompt <prompt>   Append a system prompt")
	p := Harness{}.Probe(config.Profile{Command: path}, os.Environ())
	if !p.Flags["--append-system-prompt"] || !p.Flags["--name"] || strings.Contains(strings.Join(p.Warnings, "\n"), "append-system-prompt") {
		t.Errorf("probe = %+v", p)
	}
	path, _ = fakeCLI(t, "2.1.316 (Claude Code)", helpWithName)
	p = Harness{}.Probe(config.Profile{Command: path}, os.Environ())
	if p.Flags["--append-system-prompt"] || strings.Contains(strings.Join(p.Warnings, "\n"), "append-system-prompt") {
		t.Errorf("probe without the flag = %+v", p)
	}
}

// fakePluginCLI is a CLI whose --help lists --plugin-dir and whose
// `plugin list --json` prints list and exits with code, saying stderr.
func fakePluginCLI(t *testing.T, list string, code int, stderr string) string {
	t.Helper()
	old := pluginTimeout
	pluginTimeout = time.Minute
	t.Cleanup(func() { pluginTimeout = old })
	path, _ := fakeCLI(t, "2.1.316 (Claude Code)", helpWithName+"\n  --plugin-dir <path>   Load a plugin")
	dir := filepath.Dir(path)
	if err := os.WriteFile(filepath.Join(dir, "list.json"), []byte(list), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nfor a; do case $a in\n" +
		"--version) echo '2.1.316 (Claude Code)'; exit 0;;\n" +
		"--help) printf '%s\\n' '" + helpWithName + "' '  --plugin-dir <path>   Load a plugin'; exit 0;;\n" +
		"list) cat '" + filepath.Join(dir, "list.json") + "'; echo '" + stderr + "' >&2; exit " + strconv.Itoa(code) + ";;\n" +
		"esac; done\nsleep 30\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// slugged puts the brand's slug where a plugin list names irie: the plugin a
// session loads is called after the product.
func slugged(s string) string { return strings.ReplaceAll(s, "irie", brand.Slug) }

func TestProbeChecksThatAPluginLoads(t *testing.T) {
	loads := `[{"id": "swift-lsp@claude-plugins-official", "scope": "user", "enabled": true},
	           {"id": "irie@inline", "scope": "session", "enabled": true}]`
	for _, c := range []struct {
		name, list, stderr string
		code               int
		want               string // in the warnings; "" when the flag stays
	}{
		{"loads", loads, "", 0, ""},
		{"refused", "", "Error: --plugin-dir is disabled by managed settings", 1, "refused --plugin-dir: Error: --plugin-dir is disabled"},
		{"a plugin of yours named irie", `[{"id": "irie@my-market", "scope": "user", "enabled": true}, {"id": "irie@inline", "enabled": true}]`, "", 0, "a plugin of your own is named irie (irie@my-market)"},
		{"not loaded", `[{"id": "swift-lsp@claude-plugins-official", "enabled": true}]`, "", 0, "did not load a plugin"},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := fakePluginCLI(t, slugged(c.list), c.code, c.stderr)
			p := Harness{}.Probe(config.Profile{Command: path}, os.Environ())
			joined := strings.Join(p.Warnings, "\n")
			if c.want == "" {
				if !p.Flags["--plugin-dir"] || joined != "" {
					t.Errorf("probe = %+v, want --plugin-dir and no warnings", p)
				}
				return
			}
			if p.Flags["--plugin-dir"] || !strings.Contains(joined, brand.Name+"'s skills left out: ") || !strings.Contains(joined, slugged(c.want)) {
				t.Errorf("probe = %+v, want no --plugin-dir and a warning with %q", p, c.want)
			}
		})
	}
}

func TestPluginListSaysWhy(t *testing.T) {
	for _, c := range []struct{ out, want string }{
		{`[{"id": "irie@inline", "scope": "session", "enabled": true}]`, ""},
		{`[{"id": "irie@inline", "enabled": false}]`, "Claude Code did not load a plugin passed with --plugin-dir"},
		{`[]`, "Claude Code did not load a plugin passed with --plugin-dir"},
		{`[{"id": "irie-tools@market", "enabled": true}, {"id": "irie@inline", "enabled": true}]`, ""},
		{`[{"id": "irie@inline", "enabled": true}, {"id": "irie@skills-dir", "enabled": true}]`, "a plugin of your own is named irie (irie@skills-dir)"},
		{`Warning: something`, "Claude Code's list of plugins could not be read"},
	} {
		if got := pluginList(slugged(c.out)); got != slugged(c.want) {
			t.Errorf("pluginList(%s) = %q, want %q", c.out, got, slugged(c.want))
		}
	}
}

func TestProbeWarnsAboutAnOldClaude(t *testing.T) {
	path, _ := fakeCLI(t, "2.1.200 (Claude Code)", "  --model <model>")
	p := Harness{}.Probe(config.Profile{Command: path}, os.Environ())
	if p.Version != "2.1.200" || p.Flags["--name"] {
		t.Errorf("probe = %+v", p)
	}
	joined := strings.Join(p.Warnings, "\n")
	if !strings.Contains(joined, "2.1.200") || !strings.Contains(joined, MinVersion) || !strings.Contains(joined, "--name") {
		t.Errorf("warnings = %q", p.Warnings)
	}
}

func TestProbeRunsOncePerBinary(t *testing.T) {
	path, runs := fakeCLI(t, "2.1.310 (Claude Code)", helpWithName)
	prof := config.Profile{Command: path}
	Harness{}.Probe(prof, os.Environ())
	first := countRuns(t, runs)
	Harness{}.Probe(prof, os.Environ())
	if n := countRuns(t, runs); n != first {
		t.Errorf("a second probe of the same binary ran it again (%d → %d runs)", first, n)
	}
	// An update replaces the binary, which probes again.
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	Harness{}.Probe(prof, os.Environ())
	if n := countRuns(t, runs); n == first {
		t.Error("a changed binary was not probed again")
	}
}

func TestProbeGivesUpOnAHungCLI(t *testing.T) {
	defer func(d time.Duration) { probeTimeout = d }(probeTimeout)
	probeTimeout = 300 * time.Millisecond
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	p := Harness{}.Probe(config.Profile{Command: path}, os.Environ())
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("probe took %v", d)
	}
	if p.Version != "" || len(p.Warnings) == 0 {
		t.Errorf("probe = %+v, want an unknown version with a warning", p)
	}
}

func TestProbeOfAMissingCommandIsEmpty(t *testing.T) {
	p := Harness{}.Probe(config.Profile{Command: "/home/alice/no/such/claude"}, os.Environ())
	if p.Version != "" || len(p.Warnings) != 0 {
		t.Errorf("probe = %+v; the spawn fails on its own with the shell's message", p)
	}
}

// Irie's own model switch at a usage limit goes ahead without the
// confirmation Claude Code asks while the prompt cache is warm (DESIGN
// §6.4); with no allow, Claude Code decides as it would.
func TestRespondAllowsIriesModelSwitch(t *testing.T) {
	got := strings.TrimSpace(string(Harness{}.Respond(ev("PreModelSwitch"), harness.HookReply{Allow: true})))
	if want := `{"hookSpecificOutput":{"hookEventName":"PreModelSwitch","permissionDecision":"allow"}}`; got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
	if got := (Harness{}).Respond(ev("PreModelSwitch"), harness.HookReply{}); got != nil {
		t.Errorf("with no allow: %s", got)
	}
}
