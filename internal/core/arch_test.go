package core

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/harness"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/proc"
	"github.com/irie-people/irie/internal/testutil"
)

func TestArchForFollowsTheConfig(t *testing.T) {
	// Gabriel's part is below.
	t.Setenv("IRIE_GABRIEL", "off")
	cfg := config.Default()
	cfg.Arch.Mikael.Good = []string{"Give credit where it is due."}
	c := &Core{}
	c.useConfig(cfg)
	claude, local, mikael := cfg.Profiles["claude"], cfg.Profiles["local"], cfg.Profiles["mikael"]
	for _, tc := range []struct {
		name    string
		p       config.Profile
		harness string
		want    harness.Arch
	}{
		{"every session", claude, "claude-code", harness.Arch{With: []string{"mikael"}, Good: cfg.Arch.Mikael.Good, Door: true}},
		// The small model that would weigh its calls is the local one.
		{"a local model", local, "claude-code", harness.Arch{With: []string{"mikael"}, Good: cfg.Arch.Mikael.Good}},
		{"Mikael himself", mikael, "claude-code", harness.Arch{With: []string{"mikael"}, As: "mikael", Good: cfg.Arch.Mikael.Good}},
		{"another harness", claude, "generic", harness.Arch{}},
	} {
		if got := c.archFor(tc.p, tc.harness); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
	cfg.Arch.Mikael.Door = false
	c.useConfig(cfg)
	if got := c.archFor(claude, "claude-code"); got.Door || len(got.With) != 1 {
		t.Errorf("door off: %+v", got)
	}
	cfg.Arch.Mikael.On = false
	c.useConfig(cfg)
	if got := c.archFor(claude, "claude-code"); len(got.With) != 0 || got.Door {
		t.Errorf("Mikael off: %+v", got)
	}
	// Off, he still answers a session started to talk to him.
	if got := c.archFor(mikael, "claude-code"); got.As != "mikael" || len(got.With) != 0 {
		t.Errorf("Mikael off, +mikael: %+v", got)
	}
	// Gabriel goes with every session where he can work.
	t.Setenv("IRIE_GABRIEL", "")
	cfg = config.Default()
	c.useConfig(cfg)
	got := c.archFor(claude, "claude-code")
	if want := []string{"mikael", "gabriel"}; proc.CanBackground && !slices.Equal(got.With, want) {
		t.Errorf("with Gabriel: %v, want %v", got.With, want)
	}
	cfg.Arch.Gabriel.On = false
	c.useConfig(cfg)
	if got := c.archFor(claude, "claude-code"); slices.Contains(got.With, "gabriel") {
		t.Errorf("Gabriel off: %v", got.With)
	}
}

func TestArchWordGoesToSessionsAndSubagentsThatStart(t *testing.T) {
	c := &Core{paths: paths.Paths{State: t.TempDir()}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	took := func(id string, a harness.Arch) {
		t.Helper()
		dir := c.paths.Session(id)
		os.MkdirAll(dir, 0o700)
		b, _ := json.Marshal(a)
		if err := os.WriteFile(filepath.Join(dir, harness.ArchFile), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	start := []event.Event{{Kind: event.SessionStarted}}
	sub := func(kind string) []event.Event {
		return []event.Event{{Kind: event.SubagentStarted, Data: map[string]any{"agent_type": kind}}}
	}
	took("s1", harness.Arch{With: []string{"mikael"}, OnCall: true, Door: true, Good: []string{"Be honest with clients."}})
	w := c.archWord("s1", start)
	for _, want := range []string{"[Irie] Mikael, the first of Irie's arch agents, is with you", "ask the mikael agent", "never work around him", "- Be honest with clients."} {
		if !strings.Contains(w, want) {
			t.Errorf("the word to a session does not say %q:\n%s", want, w)
		}
	}
	// A subagent hears it too, but cannot call on him.
	if w := c.archWord("s1", sub("Explore")); !strings.Contains(w, "Mikael, the first of Irie's arch agents, is with you") || strings.Contains(w, "ask the mikael agent") || !strings.Contains(w, "never work around him") {
		t.Errorf("the word to a subagent:\n%s", w)
	}
	// Mikael called as a subagent hears nothing from himself.
	if w := c.archWord("s1", sub("mikael")); w != "" {
		t.Errorf("Mikael heard his own word: %q", w)
	}
	// Nor does a session that runs as him.
	took("s2", harness.Arch{With: []string{"mikael"}, As: "mikael", OnCall: true})
	if w := c.archWord("s2", start); w != "" {
		t.Errorf("+mikael heard: %q", w)
	}
	// A session that took no door is not told of one.
	took("s3", harness.Arch{With: []string{"mikael"}, OnCall: true})
	if w := c.archWord("s3", start); strings.Contains(w, "never work around him") || !strings.Contains(w, "ask the mikael agent") {
		t.Errorf("no door:\n%s", w)
	}
	// Other events, and sessions that took nothing, hear nothing.
	if w := c.archWord("s1", []event.Event{{Kind: event.ToolStarted}}); w != "" {
		t.Errorf("a tool call heard: %q", w)
	}
	if w := c.archWord("s4", start); w != "" {
		t.Errorf("a session with no arch file heard: %q", w)
	}
}

// settingsDoors returns the prompt hooks of a session's settings file.
func settingsDoors(t *testing.T, c *coreEnv, id string) []map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(c.paths.Session(id), "claude-settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Hooks map[string][]struct {
			Matcher string           `json:"matcher"`
			Hooks   []map[string]any `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, g := range s.Hooks["PreToolUse"] {
		for _, h := range g.Hooks {
			if h["type"] == "prompt" {
				h["matcher"] = g.Matcher
				out = append(out, h)
			}
		}
	}
	return out
}

// hookStdout is what Irie answered the agent's hooks of event with.
func hookStdout(c *coreEnv, id, ev string) string {
	var out []string
	for _, r := range c.agentLog(id) {
		if r["event"] == ev {
			out = append(out, fmt.Sprint(r["stdout"]))
		}
	}
	return strings.Join(out, "\n")
}

func TestMikaelGoesWithEverySession(t *testing.T) {
	c := setupWith(t, agentConfig{extra: fmt.Sprintf(`[arch.mikael]
good = ["Give credit where it is due."]

[profiles.helper]
command = %q
arch = "mikael"
`, testutil.BuildFakeAgent(t))})
	s := c.spawn("prompt,sub:a1:tool:Read,stop")
	c.waitState(s.ID, ipc.StateDone)

	// He is on call by name, his charge ending with the user's good.
	spec, err := host.ReadSpec(c.paths.SpecFile(s.ID))
	if err != nil {
		t.Fatal(err)
	}
	i := slices.Index(spec.Argv, "--agents")
	if i < 0 || i+1 >= len(spec.Argv) || slices.Contains(spec.Argv, "--agent") {
		t.Fatalf("argv = %q", spec.Argv)
	}
	var defs map[string]map[string]any
	if err := json.Unmarshal([]byte(spec.Argv[i+1]), &defs); err != nil || defs["mikael"] == nil || !strings.HasSuffix(fmt.Sprint(defs["mikael"]["prompt"]), "- Give credit where it is due.") {
		t.Errorf("--agents %s: %v", spec.Argv[i+1], err)
	}

	// He keeps the door: a prompt hook for each rule, which gives the
	// reason back to the agent and lets its turn go on.
	doors := settingsDoors(t, c, s.ID)
	var curl map[string]any
	for _, d := range doors {
		if d["if"] == "Bash(curl *)" {
			curl = d
		}
	}
	if curl == nil || curl["matcher"] != "Bash" || curl["continueOnBlock"] != true || !strings.Contains(fmt.Sprint(curl["prompt"]), "$ARGUMENTS") {
		t.Errorf("no door for curl among %d doors: %v", len(doors), curl)
	}

	// The session and its subagent hear his word as they start.
	if w := hookStdout(c, s.ID, "SessionStart"); !strings.Contains(w, "Mikael, the first of Irie's arch agents, is with you") || !strings.Contains(w, "ask the mikael agent") || !strings.Contains(w, "Give credit where it is due.") {
		t.Errorf("SessionStart answered %q", w)
	}
	if w := hookStdout(c, s.ID, "SubagentStart"); !strings.Contains(w, "Mikael, the first of Irie's arch agents, is with you") || strings.Contains(w, "ask the mikael agent") {
		t.Errorf("SubagentStart answered %q", w)
	}

	// A session started to talk to him runs as him, with no word and no
	// door of his own.
	helper := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,stop", Profile: "helper"})
	c.waitState(helper.ID, ipc.StateDone)
	spec, _ = host.ReadSpec(c.paths.SpecFile(helper.ID))
	if i := slices.Index(spec.Argv, "--agent"); i < 0 || i+1 >= len(spec.Argv) || spec.Argv[i+1] != "mikael" {
		t.Errorf("+helper argv = %q", spec.Argv)
	}
	if w := hookStdout(c, helper.ID, "SessionStart"); strings.Contains(w, "Mikael, the first of Irie's arch agents, is with you") {
		t.Errorf("+helper heard Mikael's word: %q", w)
	}
	if d := settingsDoors(t, c, helper.ID); len(d) != 0 {
		t.Errorf("+helper has %d doors", len(d))
	}
}

func TestMikaelOffLeavesSessionsAsTheyWere(t *testing.T) {
	c := setupWith(t, agentConfig{extra: "[arch.mikael]\non = false\n"})
	s := c.spawn("prompt,stop")
	c.waitState(s.ID, ipc.StateDone)
	spec, err := host.ReadSpec(c.paths.SpecFile(s.ID))
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(spec.Argv, "--agents") {
		t.Errorf("argv = %q", spec.Argv)
	}
	if d := settingsDoors(t, c, s.ID); len(d) != 0 {
		t.Errorf("%d doors", len(d))
	}
	if _, err := os.Stat(filepath.Join(c.paths.Session(s.ID), harness.ArchFile)); err == nil {
		t.Error("an arch file with Mikael off")
	}
	if w := hookStdout(c, s.ID, "SessionStart"); strings.Contains(w, "Mikael") {
		t.Errorf("SessionStart answered %q", w)
	}
}
