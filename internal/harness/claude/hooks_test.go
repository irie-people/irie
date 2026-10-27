package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/irie-hq/irie/internal/config"
	"github.com/irie-hq/irie/internal/event"
	"github.com/irie-hq/irie/internal/harness"
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
	check(t, "PostToolUse", payload("PostToolUse", bashCall, `"tool_response":{"stdout":"ok","stderr":"","interrupted":false}`, `"duration_ms":1200`),
		want{event.ToolFinished, "main", map[string]any{"ms": 1200, "tool": "Bash"}})
	check(t, "PostToolUseFailure", payload("PostToolUseFailure", bashCall, `"error":"Exit code 1\nFAIL"`, `"duration_ms":4187`),
		want{event.ToolFailed, "main", map[string]any{"ms": 4187, "tool": "Bash"}})
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
	check(t, "StopFailure", payload("StopFailure", `"error":"rate_limit"`, `"error_details":"429 Too Many Requests"`),
		want{event.TurnFailed, "main", map[string]any{"error": "rate_limit"}})
	check(t, "SubagentStart", payload("SubagentStart", inSub),
		want{event.SubagentStarted, "sub:a1b2c3", map[string]any{"agent_type": "Explore"}})
	subTranscript := "/home/alice/transcripts/" + vendorID + "/subagents/agent-a1b2c3.jsonl"
	check(t, "SubagentStop", payload("SubagentStop", inSub, `"agent_transcript_path":"`+subTranscript+`"`),
		want{event.SubagentFinished, "sub:a1b2c3", map[string]any{"agent_type": "Explore", "transcript": subTranscript}})
	check(t, "SessionEnd", payload("SessionEnd", `"reason":"prompt_input_exit"`),
		want{event.SessionEnded, "main", map[string]any{"reason": "prompt_input_exit"}})
	check(t, "an event Irie does not use", payload("PostToolBatch"))
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

const helpWithName = "  -n, --name <name>   Set a display name for this session"

func TestProbeReadsTheVersionAndFlags(t *testing.T) {
	path, _ := fakeCLI(t, "2.1.310 (Claude Code)", helpWithName)
	p := Harness{}.Probe(config.Profile{Command: path, Args: []string{"--verbose"}}, os.Environ())
	want := harness.Probe{Version: "2.1.310", Flags: map[string]bool{"--name": true}}
	if !reflect.DeepEqual(p, want) {
		t.Errorf("probe = %+v, want %+v", p, want)
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
