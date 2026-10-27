package claude

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLocalEnvPinsEveryModel(t *testing.T) {
	env := LocalEnv("qwen3.5:9b", "http://127.0.0.1:11435", 65536)
	want := map[string]string{
		"ANTHROPIC_BASE_URL":                       "http://127.0.0.1:11435",
		"ANTHROPIC_AUTH_TOKEN":                     "ollama",
		"ANTHROPIC_API_KEY":                        "",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":             "qwen3.5:9b",
		"ANTHROPIC_DEFAULT_SONNET_MODEL":           "qwen3.5:9b",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":            "qwen3.5:9b",
		"ANTHROPIC_DEFAULT_FABLE_MODEL":            "qwen3.5:9b",
		"CLAUDE_CODE_SUBAGENT_MODEL":               "qwen3.5:9b",
		"CLAUDE_CODE_AUTO_MODE_SERVER":             "0",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
		"CLAUDE_CODE_MAX_CONTEXT_TOKENS":           "65536",
	}
	if !reflect.DeepEqual(env, want) {
		t.Errorf("env = %v\nwant %v", env, want)
	}
	// The auto-compact window takes 100k tokens at least.
	if w := LocalEnv("m", "u", 131072)["CLAUDE_CODE_AUTO_COMPACT_WINDOW"]; w != "131072" {
		t.Errorf("auto-compact window at 128k = %q", w)
	}
	// An unknown context sets neither.
	if e := LocalEnv("m", "u", 0); e["CLAUDE_CODE_MAX_CONTEXT_TOKENS"] != "" || e["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] != "" {
		t.Errorf("unknown context: %v", e)
	}
}

func writeSettings(t *testing.T, path string, statusLine map[string]any) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"statusLine": statusLine, "theme": "dark"})
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestUserStatusLineFollowsPrecedence(t *testing.T) {
	root := t.TempDir()
	home, repo := filepath.Join(root, "home"), filepath.Join(root, "repo")
	cwd := filepath.Join(root, "tree", "sub")
	user := filepath.Join(home, ".claude", "settings.json")
	shared := filepath.Join(cwd, ".claude", "settings.json")
	oldLocal := filepath.Join(cwd, ".claude", "settings.local.json")
	rootLocal := filepath.Join(repo, ".claude", "settings.local.json")

	if _, ok := UserStatusLine(cwd, repo, home, nil); ok {
		t.Fatal("a status line with no settings files")
	}
	writeSettings(t, user, map[string]any{"type": "command", "command": "~/.claude/statusline.sh", "padding": 2, "refreshInterval": 5})
	if l, ok := UserStatusLine(cwd, repo, home, nil); !ok || l.Command != "~/.claude/statusline.sh" || l.Padding != 2 || l.RefreshInterval != 5 {
		t.Errorf("user = %+v, %v", l, ok)
	}
	writeSettings(t, shared, map[string]any{"type": "command", "command": "shared"})
	if l, _ := UserStatusLine(cwd, repo, home, nil); l.Command != "shared" {
		t.Errorf("shared project = %+v", l)
	}
	writeSettings(t, oldLocal, map[string]any{"command": "old local"})
	if l, _ := UserStatusLine(cwd, repo, home, nil); l.Command != "old local" || l.Type != "command" {
		t.Errorf("local in cwd = %+v", l)
	}
	writeSettings(t, rootLocal, map[string]any{"type": "command", "command": "root local"})
	if l, _ := UserStatusLine(cwd, repo, home, nil); l.Command != "root local" {
		t.Errorf("local at the repo root = %+v", l)
	}
	// CLAUDE_CONFIG_DIR moves the user's settings.
	other := filepath.Join(root, "config")
	writeSettings(t, filepath.Join(other, "settings.json"), map[string]any{"type": "command", "command": "elsewhere"})
	if l, _ := UserStatusLine(filepath.Join(root, "plain"), "", home, []string{"CLAUDE_CONFIG_DIR=" + other}); l.Command != "elsewhere" {
		t.Errorf("CLAUDE_CONFIG_DIR = %+v", l)
	}
	// A status line of another type, or with no command, is skipped.
	writeSettings(t, rootLocal, map[string]any{"type": "other", "command": "x"})
	writeSettings(t, oldLocal, map[string]any{"type": "command", "command": " "})
	if l, _ := UserStatusLine(cwd, repo, home, nil); l.Command != "shared" {
		t.Errorf("after skipping = %+v", l)
	}
}

func TestSettingsMakeIrieTheStatusLine(t *testing.T) {
	home := t.TempDir()
	s := spawnOf("x")
	s.StatusLine, s.Home, s.Cwd = true, home, t.TempDir()
	_, _, files := launchSpawn(t, defaultProfile(), s)
	var got struct {
		StatusLine StatusLine `json:"statusLine"`
	}
	if err := json.Unmarshal(files[SettingsFile], &got); err != nil {
		t.Fatal(err)
	}
	// Irie's own line re-runs on a timer, to follow the session while it
	// is idle.
	if want := (StatusLine{Type: "command", Command: "'" + irieBin + "' statusline", RefreshInterval: 2}); got.StatusLine != want {
		t.Errorf("statusLine = %+v, want %+v", got.StatusLine, want)
	}

	// The user's own status line runs after Irie's, quoted for sh, and
	// keeps its padding and refresh.
	writeSettings(t, filepath.Join(home, ".claude", "settings.json"), map[string]any{"type": "command", "command": `jq -r '.model.display_name'`, "padding": 1, "refreshInterval": 10, "hideVimModeIndicator": true})
	_, _, files = launchSpawn(t, defaultProfile(), s)
	if err := json.Unmarshal(files[SettingsFile], &got); err != nil {
		t.Fatal(err)
	}
	want := StatusLine{Type: "command", Command: "'" + irieBin + `' statusline --then 'jq -r '\''.model.display_name'\'''`, Padding: 1, RefreshInterval: 10, HideVimModeIndicator: true}
	if got.StatusLine != want {
		t.Errorf("statusLine = %+v\nwant %+v", got.StatusLine, want)
	}
	// sh hands the helper the user's command intact.
	out, err := exec.Command("sh", "-c", "printf '%s' "+want.Command[len("'"+irieBin+"' statusline --then "):]).Output()
	if err != nil || string(out) != `jq -r '.model.display_name'` {
		t.Errorf("sh reads the command as %q (%v)", out, err)
	}

	// Without usage.status_line the settings file has no status line.
	s.StatusLine = false
	_, _, files = launchSpawn(t, defaultProfile(), s)
	var none map[string]any
	json.Unmarshal(files[SettingsFile], &none)
	if _, ok := none["statusLine"]; ok {
		t.Error("a status line without usage.status_line")
	}
}
