package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/harness/claude"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/testutil"
)

// fallbackConfig adds +fab, a profile on Fable that falls back to Opus,
// whose agent keeps its Claude Code settings in dir, never the user's own.
// A failover at the plan's limit would carry a task on to a profile of the
// real claude, so it is off.
func fallbackConfig(t *testing.T, dir string) agentConfig {
	return agentConfig{extra: fmt.Sprintf(`[profiles.fab]
command = %q
model = "claude-fable-5-1"
fallback_model = "claude-opus-5-5"
permission_mode = "auto"
env = { CLAUDE_CONFIG_DIR = %q }

[route]
failover = false
`, testutil.BuildFakeAgent(t), dir)}
}

// The user's settings before the switch: their own default model, which
// /model replaces and the switch puts back.
const userSettings = "{\n  \"theme\": \"dark\",\n  \"model\": \"fable[1m]\"\n}\n"

// Fable's own usage limit stops a turn: Irie types /model into the agent,
// which switches without asking although its prompt cache is warm, puts
// the user's default back, and tells the agent to carry on, on Opus, in
// the same session. New sessions of the profile then start on Opus. The
// plan's own limit, which a switch does not lift, switches nothing.
func TestAModelsOwnLimitSwitchesTheSessionToItsFallback(t *testing.T) {
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(settings, []byte(userSettings), 0o600); err != nil {
		t.Fatal(err)
	}
	c := setupWith(t, fallbackConfig(t, dir))

	// The plan's limit: no switch.
	plan := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,box,stopfail:rate_limit", Profile: "fab"})
	c.waitState(plan.ID, ipc.StateNeedsYou)

	// box: its idle prompt shows, which /model and the note are typed into.
	s := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,box,limit:fable", Profile: "fab"})
	var fell event.Event
	testutil.Eventually(t, 15*time.Second, "no route.fallback", func() bool {
		for _, e := range c.events(s.ID) {
			if e.Kind == event.RouteFallback {
				fell = e
				return true
			}
		}
		return false
	})
	if fell.Str("from") != "claude-fable-5-1" || fell.Str("to") != "claude-opus-5-5" || !strings.Contains(fell.Str("limit"), "Fable limit") || fell.Str("reason") != "" {
		t.Errorf("route.fallback %v", fell.Data)
	}
	testutil.Eventually(t, 20*time.Second, "the agent was never told to carry on", func() bool {
		evs := c.events(s.ID)
		at := slices.IndexFunc(evs, func(e event.Event) bool { return e.Kind == event.ModelSwitched })
		return at >= 0 && slices.ContainsFunc(evs[at:], func(e event.Event) bool { return e.Kind == event.PromptSubmitted })
	})
	var switched map[string]any
	for _, l := range c.agentLog(s.ID) {
		if l["t"] == "model" {
			switched = l
		}
	}
	if switched["to"] != "claude-opus-5-5" || switched["switched"] != true || switched["decision"] != "allow" || switched["asked"] != false {
		t.Errorf("the agent's switch: %v", switched)
	}
	if typed := c.typedTo(s.ID); len(typed) != 2 || typed[0] != "/model claude-opus-5-5" || typed[1] != fallbackNote("claude-fable-5-1", "claude-opus-5-5") {
		t.Errorf("typed %q", typed)
	}
	if r, _ := c.row(s.ID); r.Model != "claude-opus-5-5" {
		t.Errorf("the card's model %q", r.Model)
	}
	var got struct{ Model, Theme string }
	b, _ := os.ReadFile(settings)
	if err := json.Unmarshal(b, &got); err != nil || got.Model != "fable[1m]" || got.Theme != "dark" {
		t.Errorf("the user's settings after the switch: %s", b)
	}

	// Nothing typed to the session the plan's limit stopped.
	if slices.Contains(c.kinds(plan.ID), event.RouteFallback) || len(c.typedTo(plan.ID)) > 0 {
		t.Errorf("the plan's limit switched: %v, typed %q", c.kinds(plan.ID), c.typedTo(plan.ID))
	}

	// A new session of the profile starts on Opus while the limit holds.
	n := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,stop", Profile: "fab"})
	c.waitState(n.ID, ipc.StateDone)
	argv := c.agentArgv(n.ID)
	if i := slices.Index(argv, "--model"); i < 0 || argv[i+1] != "claude-opus-5-5" {
		t.Errorf("a new session's argv %q", argv)
	}
	if r, _ := c.row(n.ID); !slices.ContainsFunc(r.Warnings, func(w string) bool { return strings.HasPrefix(w, "starts on claude-opus-5-5") }) {
		t.Errorf("a new session's warnings %q", r.Warnings)
	}
}

// agentArgv is the argv the fake agent of a session started with.
func (c *coreEnv) agentArgv(id string) []string {
	c.t.Helper()
	for _, l := range c.agentLog(id) {
		if l["t"] == "start" {
			var argv []string
			for _, a := range l["argv"].([]any) {
				argv = append(argv, a.(string))
			}
			return argv
		}
	}
	c.t.Fatalf("no start in %s's agent log", id)
	return nil
}

func TestFamilies(t *testing.T) {
	for model, want := range map[string]string{
		"claude-fable-5-1": "fable", "fable": "fable", "fable[1m]": "fable", "Claude-Opus-5-5": "opus",
		"opus": "opus", "claude-sonnet-5-5": "sonnet", "claude-haiku-5-5": "haiku", "dory-4b": "", "": "",
		"opusplan": "", "claude-3-fable": "",
	} {
		if got := family(model); got != want {
			t.Errorf("family(%q) = %q, want %q", model, got, want)
		}
	}
}

func TestOnlyAModelsOwnLimitIsItsOwn(t *testing.T) {
	for _, c := range []struct {
		message, model string
		own            bool
	}{
		{"You've reached your Fable limit. Run /usage-credits to continue or switch models with /model.", "claude-fable-5-1", true},
		{"Fable limit reached · continuing on Fable 5.1 uses usage credits, and the prompt to confirm went unanswered", "claude-fable-5-1", true},
		{"You've hit your Opus limit · resets 3:45pm", "claude-opus-5-5", true},
		{"You've hit your Opus limit · resets 3:45pm", "claude-fable-5-1", false},
		{"You've hit your session limit · resets 3:45pm", "claude-fable-5-1", false},
		{"You've hit your weekly limit · resets Mon 12:00am", "claude-opus-5-5", false},
		{"API Error: Rate limit reached", "claude-fable-5-1", false},
		{"You've reached your Fable limit.", "dory-4b", false},
	} {
		if got := ownLimit(c.message, c.model); got != c.own {
			t.Errorf("ownLimit(%q, %q) = %v", c.message, c.model, got)
		}
	}
}

// While a model's own limit holds, a profile with a fallback starts on it,
// for an hour after the limit last stopped a turn; one without stays.
func TestNewSessionsStartOnTheFallbackWhileTheLimitHolds(t *testing.T) {
	c := historyCore(t)
	now := time.Now()
	fab := config.Profile{Model: "claude-fable-5-1", FallbackModel: "claude-opus-5-5"}
	c.mu.Lock()
	defer c.mu.Unlock()
	if m, why := c.startsOn(fab, now); m != fab.Model || why != "" {
		t.Errorf("with no limit: %q, %q", m, why)
	}
	c.noteLimit("fable", now.Add(-10*time.Minute))
	if m, why := c.startsOn(fab, now); m != "claude-opus-5-5" || !strings.Contains(why, "claude-fable-5-1's own usage limit") {
		t.Errorf("while the limit holds: %q, %q", m, why)
	}
	if m, _ := c.startsOn(config.Profile{Model: "claude-fable-5-1"}, now); m != "claude-fable-5-1" {
		t.Errorf("a profile without a fallback starts on %q", m)
	}
	if m, _ := c.startsOn(fab, now.Add(limitHold)); m != fab.Model {
		t.Errorf("after the hold: %q", m)
	}
	// An older limit never moves the newest back.
	c.noteLimit("fable", now.Add(-time.Hour*3))
	if m, _ := c.startsOn(fab, now); m != "claude-opus-5-5" {
		t.Errorf("after an older limit: %q", m)
	}
}

// Only the switch Irie makes skips Claude Code's confirmation; the user's
// own /model goes as Claude Code takes it.
func TestOnlyIriesSwitchIsAllowed(t *testing.T) {
	payload := func(to, requested string) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"hook_event_name": "PreModelSwitch", "to_model": to, "requested_model": requested})
		return b
	}
	x := &session{}
	if modelSwitchAsk(x, payload("claude-opus-5-5", "claude-opus-5-5")) {
		t.Error("a switch with none under way was allowed")
	}
	x.switching = &modelSwitch{to: "claude-opus-5-5", done: make(chan struct{})}
	for _, p := range []json.RawMessage{payload("claude-opus-5-5", "claude-opus-5-5"), payload("claude-opus-5-5[1m]", "")} {
		if !modelSwitchAsk(x, p) {
			t.Errorf("Irie's switch %s was not allowed", p)
		}
	}
	if modelSwitchAsk(x, payload("claude-sonnet-5-5", "sonnet")) {
		t.Error("another switch was allowed while Irie's is under way")
	}
	if modelSwitchAsk(x, json.RawMessage(`{"hook_event_name":"PostModelSwitch","to_model":"claude-opus-5-5"}`)) {
		t.Error("a PostModelSwitch was answered")
	}
	modelSwitched(x, event.Event{Kind: event.ModelSwitched, Data: map[string]any{"model": "claude-opus-5-5", "requested": "claude-opus-5-5"}})
	select {
	case <-x.switching.done:
	default:
		t.Error("model.switched did not finish the switch")
	}
	// Twice is harmless.
	modelSwitched(x, event.Event{Kind: event.ModelSwitched, Data: map[string]any{"model": "claude-opus-5-5"}})
}

// A restarted core still starts new sessions on the fallback while the
// limit holds: it reads the limits from the journal's route.fallback.
func TestARestartKeepsTheLimits(t *testing.T) {
	c := historyCore(t)
	c.mu.Lock()
	x := &session{id: "s0000000000f1", name: "fab", harness: "claude-code"}
	c.sessions[x.id] = x
	c.record(x, event.Event{Source: event.SourceIrie, Agent: event.Main, Kind: event.RouteFallback,
		Data: map[string]any{"from": "claude-fable-5-1", "to": "claude-opus-5-5"}})
	c.mu.Unlock()
	c.limits = map[string]time.Time{}
	c.loadOutcomes()
	if at, ok := c.limits["fable"]; !ok || time.Since(at) > time.Minute {
		t.Errorf("limits after a restart %v", c.limits)
	}
}

// A session that started without the hooks a switch needs, as one from
// before Irie had them, or one with its slash commands off, waits for the
// reset, and its route.fallback says why: nothing is typed to it.
func TestASessionThatCannotSwitchWaits(t *testing.T) {
	c := historyCore(t)
	for _, tc := range []struct {
		argv  []string
		hooks bool
		want  string
	}{
		{[]string{"claude", "--model", "claude-fable-5-1"}, false, "without the hooks a model switch needs"},
		{[]string{"claude", "--disable-slash-commands", "--model", "claude-fable-5-1"}, true, "--disable-slash-commands"},
	} {
		id := newID(time.Now())
		dir := c.paths.Session(id)
		os.MkdirAll(dir, 0o700)
		if err := host.WriteSpec(c.paths.SpecFile(id), host.Spec{Session: id, Profile: "fable", Harness: "claude-code", Argv: tc.argv, Cwd: dir}); err != nil {
			t.Fatal(err)
		}
		settings, _ := claude.Settings("/home/alice/.local/bin/irie", 0, tc.hooks, claude.Extra{})
		os.WriteFile(filepath.Join(dir, claude.SettingsFile), settings, 0o600)
		sw := &modelSwitch{from: "claude-fable-5-1", to: "claude-opus-5-5", limit: "You've reached your Fable limit.", done: make(chan struct{})}
		c.mu.Lock()
		x := &session{id: id, name: id, harness: "claude-code", profile: "fable", switching: sw}
		c.sessions[id] = x
		c.mu.Unlock()
		c.fallBack(id, sw)
		var reason string
		for _, e := range c.journalOf(t, id) {
			if e.Kind == event.RouteFallback {
				reason = e.Str("reason")
			}
		}
		if !strings.Contains(reason, tc.want) || !strings.HasPrefix(reason, "it waits for the limit to reset") {
			t.Errorf("%q: reason %q, want %q", tc.argv, reason, tc.want)
		}
		c.mu.Lock()
		if x.switching != nil {
			t.Errorf("%q: the switch is still under way", tc.argv)
		}
		c.mu.Unlock()
	}
}
