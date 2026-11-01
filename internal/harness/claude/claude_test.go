package claude

import (
	"bytes"
	"encoding/json"
	"flag"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/harness"
	"github.com/irie-people/irie/internal/learn"
)

var update = flag.Bool("update", false, "rewrite the golden files")

const (
	sessionDir = "/home/alice/.local/state/irie/sessions/s0k3j9x2m4a7q"
	vendorID   = "6f1c2b4e-8a9d-4c3b-9e7f-0a1b2c3d4e5f"
	irieBin    = "/home/alice/.local/bin/irie"
)

// current is what probing a recent Claude Code finds.
var current = harness.Probe{Version: "2.1.310", Flags: map[string]bool{"--name": true}}

func spawnOf(task string) harness.Spawn {
	return harness.Spawn{
		Session:  "s0k3j9x2m4a7q",
		Name:     "describe-this-repo",
		Task:     task,
		Cwd:      "/home/alice/code/website",
		Dir:      sessionDir,
		VendorID: vendorID,
		Bin:      irieBin,
		HoldMax:  24 * time.Hour,
		Probe:    current,
	}
}

func defaultProfile() config.Profile { return config.Default().Profiles["claude"] }

func launchSpawn(t *testing.T, p config.Profile, s harness.Spawn) ([]string, map[string]string, map[string][]byte) {
	t.Helper()
	argv, env, files, err := Harness{}.Launch(p, s)
	if err != nil {
		t.Fatal(err)
	}
	return argv, env, files
}

func launch(t *testing.T, p config.Profile, task string) ([]string, map[string]string, map[string][]byte) {
	t.Helper()
	return launchSpawn(t, p, spawnOf(task))
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s:\n%s\nwant:\n%s", name, got, want)
	}
}

func TestArgvGoldenForDefaultProfile(t *testing.T) {
	const task = "describe this repo in one paragraph"
	argv, env, files := launch(t, defaultProfile(), task)
	got, err := json.MarshalIndent(argv, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "argv-default.golden", append(got, '\n'))
	if len(env) != 0 {
		t.Errorf("env = %v, want none", env)
	}
	if string(files["task.md"]) != task {
		t.Errorf("task.md = %q", files["task.md"])
	}
}

func TestSettingsFileGolden(t *testing.T) {
	_, _, files := launch(t, defaultProfile(), "x")
	golden(t, "settings-default.golden", files[SettingsFile])
}

// hookEntry is one handler of the settings file, flattened.
type hookEntry struct {
	matcher   string
	command   string
	timeout   int
	onFailure string
}

func settingsHooks(t *testing.T, b []byte) map[string][]hookEntry {
	t.Helper()
	var s struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type      string `json:"type"`
				Command   string `json:"command"`
				Timeout   int    `json:"timeout"`
				OnFailure string `json:"onFailure"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		t.Fatalf("settings file: %v\n%s", err, b)
	}
	out := map[string][]hookEntry{}
	for ev, groups := range s.Hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				if h.Type != "command" {
					t.Errorf("%s: handler type %q", ev, h.Type)
				}
				out[ev] = append(out[ev], hookEntry{g.Matcher, h.Command, h.Timeout, h.OnFailure})
			}
		}
	}
	return out
}

// TestSettingsHooksFollowTheDesign checks the hook table of DESIGN §3.3.
func TestSettingsHooksFollowTheDesign(t *testing.T) {
	s := spawnOf("x")
	s.HoldMax = 2 * time.Hour
	_, _, files := launchSpawn(t, defaultProfile(), s)
	got := settingsHooks(t, files[SettingsFile])
	held := int((2*time.Hour + 120*time.Second) / time.Second)
	want := map[string]hookEntry{
		"SessionStart":       {"", "", 10, ""},
		"UserPromptSubmit":   {"", "", 10, ""},
		"PreToolUse":         {"*", "", held, "block"},
		"PostToolUse":        {"*", "", held, ""},
		"PostToolUseFailure": {"*", "", held, ""},
		"PermissionRequest":  {"*", "", 10, ""},
		"Notification":       {"", "", 10, ""},
		"Elicitation":        {"", "", 10, ""},
		"Stop":               {"", "", 10, ""},
		"StopFailure":        {"", "", 10, ""},
		"SubagentStart":      {"", "", 10, ""},
		"SubagentStop":       {"", "", 10, ""},
		"SessionEnd":         {"", "", 1, ""},
	}
	if len(got) != len(want) {
		t.Errorf("events %v, want the %d of the design", slices.Sorted(maps.Keys(got)), len(want))
	}
	for ev, w := range want {
		hs := got[ev]
		if len(hs) != 1 {
			t.Errorf("%s: %d handlers, want 1", ev, len(hs))
			continue
		}
		h := hs[0]
		if h.matcher != w.matcher || h.timeout != w.timeout || h.onFailure != w.onFailure {
			t.Errorf("%s = %+v, want matcher %q timeout %d onFailure %q", ev, h, w.matcher, w.timeout, w.onFailure)
		}
		if h.command != "'"+irieBin+"' hook" {
			t.Errorf("%s command = %q", ev, h.command)
		}
	}
}

func TestHookCommandSurvivesTheShell(t *testing.T) {
	for _, bin := range []string{"/home/alice/My Tools/irie", "/home/alice/it's/irie", `/home/alice/$HOME/"x"/irie`} {
		s := spawnOf("x")
		s.Bin = bin
		_, _, files := launchSpawn(t, defaultProfile(), s)
		cmd := settingsHooks(t, files[SettingsFile])["Stop"][0].command
		// Claude Code runs a command hook with sh -c; printf stands in for irie.
		out, err := exec.Command("/bin/sh", "-c", "set -- "+strings.TrimSuffix(cmd, " hook")+`; printf '%s' "$1"`).Output()
		if err != nil {
			t.Fatalf("%q: %v", cmd, err)
		}
		if string(out) != bin || !strings.HasSuffix(cmd, " hook") {
			t.Errorf("command %q runs %q, want %q", cmd, out, bin)
		}
	}
}

func TestAnOldClaudeGetsNoOnFailure(t *testing.T) {
	for _, p := range []harness.Probe{{Version: "2.1.294"}, {}} {
		s := spawnOf("x")
		s.Probe = p
		_, _, files := launchSpawn(t, defaultProfile(), s)
		if bytes.Contains(files[SettingsFile], []byte("onFailure")) {
			t.Errorf("version %q: settings carry onFailure", p.Version)
		}
	}
	_, _, files := launch(t, defaultProfile(), "x")
	if !bytes.Contains(files[SettingsFile], []byte(`"onFailure": "block"`)) {
		t.Error("a recent Claude Code gets no onFailure")
	}
}

func TestNameNeedsTheFlag(t *testing.T) {
	argv, _, _ := launch(t, defaultProfile(), "x")
	if i := slices.Index(argv, "--name"); i < 0 || argv[i+1] != "describe-this-repo" {
		t.Errorf("argv = %q, want --name describe-this-repo", argv)
	}
	s := spawnOf("x")
	s.Probe = harness.Probe{Version: "2.1.310"}
	argv, _, _ = launchSpawn(t, defaultProfile(), s)
	if slices.Contains(argv, "--name") {
		t.Errorf("argv = %q: --name without the flag in --help", argv)
	}
	// No name the user chose: Claude Code titles the session itself.
	s = spawnOf("x")
	s.Name = ""
	argv, _, _ = launchSpawn(t, defaultProfile(), s)
	if slices.Contains(argv, "--name") {
		t.Errorf("argv = %q: --name with no name", argv)
	}
}

func TestPluginDirGoesOnlyToACLIThatLoadsIt(t *testing.T) {
	const skills = "/home/alice/.local/share/irie/skills/plugins/0123456789abcdef"
	s := spawnOf("x")
	s.PluginDir = skills
	s.Probe = harness.Probe{Version: "2.1.316", Flags: map[string]bool{"--name": true, "--plugin-dir": true}}
	argv, _, _ := launchSpawn(t, defaultProfile(), s)
	i := slices.Index(argv, "--plugin-dir")
	if i < 0 || argv[i+1] != skills || argv[i-2] != "--settings" {
		t.Errorf("argv = %q, want --plugin-dir %s after the settings", argv, skills)
	}
	resumed, _, _, err := Harness{}.Resume(defaultProfile(), s)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(resumed, skills) {
		t.Errorf("resume argv = %q, want the same skills", resumed)
	}
	s.Probe = current // the flag is not in its probe
	if argv, _, _ := launchSpawn(t, defaultProfile(), s); slices.Contains(argv, "--plugin-dir") {
		t.Errorf("argv = %q: --plugin-dir to a CLI that did not load a plugin", argv)
	}
	s.Probe.Flags = map[string]bool{"--plugin-dir": true}
	s.PluginDir = ""
	if argv, _, _ := launchSpawn(t, defaultProfile(), s); slices.Contains(argv, "--plugin-dir") {
		t.Errorf("argv = %q: --plugin-dir with no skills", argv)
	}
}

func TestSettingsArePassedWithTheSessionsFile(t *testing.T) {
	argv, _, files := launch(t, defaultProfile(), "x")
	i := slices.Index(argv, "--settings")
	if i < 0 || argv[i+1] != sessionDir+"/claude-settings.json" {
		t.Errorf("argv = %q", argv)
	}
	if _, ok := files["claude-settings.json"]; !ok {
		t.Errorf("files = %v", slices.Sorted(maps.Keys(files)))
	}
}

func TestLargeTaskGoesThroughTaskFile(t *testing.T) {
	task := strings.Repeat("line of a very long task\n", (2<<20)/25+1)
	if len(task) < 2<<20 {
		t.Fatalf("test setup: task is %d bytes", len(task))
	}
	argv, _, files := launch(t, defaultProfile(), task)
	want := "Your full task is in " + sessionDir + "/task.md. Read all of it before doing anything else."
	if got := argv[len(argv)-1]; got != want {
		t.Errorf("task argument = %q, want %q", got, want)
	}
	if !bytes.Equal(files["task.md"], []byte(task)) {
		t.Error("task.md is not byte-identical to the task")
	}
	for _, a := range argv {
		if len(a) > InlineMax {
			t.Errorf("an argument is %d bytes", len(a))
		}
	}
}

func TestInlineLimitIs100KiB(t *testing.T) {
	atLimit := strings.Repeat("a", 100<<10)
	argv, _, _ := launch(t, defaultProfile(), atLimit)
	if argv[len(argv)-1] != atLimit {
		t.Error("a 100 KiB task was not passed inline")
	}
	argv, _, _ = launch(t, defaultProfile(), atLimit+"b")
	if !strings.HasPrefix(argv[len(argv)-1], "Your full task is in ") {
		t.Error("a task over 100 KiB was passed inline")
	}
}

func TestNewlinesSurvive(t *testing.T) {
	const task = "line one\nline two\n\n    indented\r\n\ttabbed\nlast"
	argv, _, files := launch(t, defaultProfile(), task)
	if got := argv[len(argv)-1]; got != task {
		t.Errorf("task argument = %q, want %q", got, task)
	}
	if string(files["task.md"]) != task {
		t.Errorf("task.md = %q, want %q", files["task.md"], task)
	}
}

func TestLeadingDashGetsASpace(t *testing.T) {
	const task = "-v makes the build fail; find out why"
	argv, _, files := launch(t, defaultProfile(), task)
	if got := argv[len(argv)-1]; got != " "+task {
		t.Errorf("task argument = %q", got)
	}
	if string(files["task.md"]) != task {
		t.Errorf("task.md = %q", files["task.md"])
	}
}

func TestTaskWithNULGoesThroughTaskFile(t *testing.T) {
	argv, _, files := launch(t, defaultProfile(), "a\x00b")
	if !strings.HasPrefix(argv[len(argv)-1], "Your full task is in ") {
		t.Errorf("task argument = %q", argv[len(argv)-1])
	}
	if string(files["task.md"]) != "a\x00b" {
		t.Errorf("task.md = %q", files["task.md"])
	}
}

func TestProfileArgsFollowTheCommand(t *testing.T) {
	p := defaultProfile()
	p.Command = "/bin/sh"
	p.Args = []string{"-c", "echo ready; sleep 600"}
	argv, _, _ := launch(t, p, "x")
	if want := []string{"/bin/sh", "-c", "echo ready; sleep 600", "--model"}; !reflect.DeepEqual(argv[:4], want) {
		t.Errorf("argv = %q", argv)
	}
}

func TestEmptyValuesAreLeftOut(t *testing.T) {
	p := config.Profile{Harness: "claude-code"}
	argv, _, _ := launch(t, p, "x")
	want := []string{"claude", "--settings", sessionDir + "/claude-settings.json", "--session-id", vendorID, "--name", "describe-this-repo", "x"}
	if !reflect.DeepEqual(argv, want) {
		t.Errorf("argv = %q, want %q", argv, want)
	}
}

func TestProfileEnvIsPassed(t *testing.T) {
	p := defaultProfile()
	p.Env = map[string]string{"ANTHROPIC_BASE_URL": "http://127.0.0.1:11434"}
	_, env, _ := launch(t, p, "x")
	if env["ANTHROPIC_BASE_URL"] != "http://127.0.0.1:11434" {
		t.Errorf("env = %v", env)
	}
	env["ANTHROPIC_BASE_URL"] = "changed"
	if p.Env["ANTHROPIC_BASE_URL"] != "http://127.0.0.1:11434" {
		t.Error("Launch returned the profile's own env map")
	}
}

func TestVendorIDAndBinAreRequired(t *testing.T) {
	for _, edit := range []func(*harness.Spawn){
		func(s *harness.Spawn) { s.VendorID = "" },
		func(s *harness.Spawn) { s.Bin = "" },
	} {
		s := spawnOf("x")
		edit(&s)
		if _, _, _, err := (Harness{}).Launch(defaultProfile(), s); err == nil {
			t.Errorf("no error for %+v", s)
		}
	}
}

func TestSessionIDFlagCarriesTheVendorID(t *testing.T) {
	argv, _, _ := launch(t, defaultProfile(), "x")
	i := slices.Index(argv, "--session-id")
	if i < 0 || argv[i+1] != vendorID {
		t.Errorf("argv = %q", argv)
	}
}

func TestClaudeCodeHasEveryLevel(t *testing.T) {
	if l := (Harness{}).Levels(); l != harness.Generic|harness.Observed|harness.Held|harness.Subagents {
		t.Errorf("levels %v, want GOHS", l)
	}
}

// TestTutorModeSetsTheStyleAndTipsGoUnderTheSpinner checks the settings file
// of a session in a tutor mode, with Irie's tips (docs/proposals/learning.md).
func TestTutorModeSetsTheStyleAndTipsGoUnderTheSpinner(t *testing.T) {
	s := spawnOf("x")
	s.Tutor = config.TutorTeach
	s.Tips = []learn.Tip{{ID: "irie.park", Text: "Park a session you have checked"}, {ID: "irie.red", Text: "A red card needs you"}}
	_, _, files := launchSpawn(t, defaultProfile(), s)
	golden(t, "settings-tutor.golden", files[SettingsFile])

	var got struct {
		Style string `json:"outputStyle"`
		Tips  *struct {
			Label string `json:"label"`
			Tips  []struct {
				ID       string `json:"id"`
				Text     string `json:"text"`
				Cooldown int    `json:"cooldownSessions"`
			} `json:"tips"`
		} `json:"spinnerTipsOverride"`
	}
	if err := json.Unmarshal(files[SettingsFile], &got); err != nil {
		t.Fatal(err)
	}
	if got.Style != "Learning" {
		t.Errorf("outputStyle = %q, want Learning", got.Style)
	}
	if got.Tips == nil || got.Tips.Label != "Irie tip" || len(got.Tips.Tips) != 2 || got.Tips.Tips[0].ID != "irie.park" || got.Tips.Tips[0].Cooldown < 1 {
		t.Errorf("spinnerTipsOverride = %+v", got.Tips)
	}

	// A resume keeps the style: its settings file is written afresh.
	s.Tutor = config.TutorExplain
	_, _, files, err := Harness{}.Resume(defaultProfile(), s)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(files[SettingsFile], []byte(`"outputStyle": "Explanatory"`)) {
		t.Errorf("resume settings:\n%s", files[SettingsFile])
	}
}

func TestOffLeavesTheUsersStyleAndOldVersionsGetNoTips(t *testing.T) {
	s := spawnOf("x")
	s.Tips = []learn.Tip{{ID: "irie.park", Text: "Park a session you have checked"}}
	s.Probe.Version = "2.1.200 (Claude Code)"
	_, _, files := launchSpawn(t, defaultProfile(), s)
	for _, key := range []string{"outputStyle", "spinnerTipsOverride"} {
		if bytes.Contains(files[SettingsFile], []byte(key)) {
			t.Errorf("%s in the settings file:\n%s", key, files[SettingsFile])
		}
	}
}

// TestVisualAsksForDiagramsInTutorModeOnly checks the append flag that asks a
// tutor-mode session for diagrams (learn.visual).
func TestVisualAsksForDiagramsInTutorModeOnly(t *testing.T) {
	appends := func(s harness.Spawn) bool {
		argv, _, _ := launchSpawn(t, defaultProfile(), s)
		i := slices.Index(argv, "--append-system-prompt")
		return i >= 0 && i+1 < len(argv) && argv[i+1] == harness.VisualNote
	}
	s := spawnOf("x")
	s.Probe = harness.Probe{Version: "2.1.316", Flags: map[string]bool{"--name": true, "--append-system-prompt": true}}
	s.Visual = true
	if appends(s) {
		t.Error("no tutor mode, yet it asks for diagrams")
	}
	s.Tutor = config.TutorExplain
	if !appends(s) {
		t.Error("explain mode with visual on asks for no diagrams")
	}
	s.Visual = false
	if appends(s) {
		t.Error("visual off, yet it asks for diagrams")
	}
	s.Visual = true
	s.Probe.Flags = map[string]bool{"--name": true}
	if appends(s) {
		t.Error("a CLI without the flag gets it")
	}
}
