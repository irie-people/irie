package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Irie's theme of Claude Code's takes the palette's dark grounds for the
// surfaces Claude Code paints itself, and is written only when it changes.
func TestIriesThemeIsThePalettesDarkGrounds(t *testing.T) {
	dir := t.TempDir()
	dark := map[string]string{"panel": "#18221C", "card": "#1C2620", "hover": "#25322A", "selection": "#2A3D31", "ink": "#ECF0E6"}
	wrote, err := WriteTheme(dir, dark)
	if err != nil || !wrote {
		t.Fatalf("first write: %v, %v", wrote, err)
	}
	b, err := os.ReadFile(ThemeFile(dir))
	if err != nil {
		t.Fatal(err)
	}
	var theme struct {
		Name      string            `json:"name"`
		Base      string            `json:"base"`
		Overrides map[string]string `json:"overrides"`
	}
	if err := json.Unmarshal(b, &theme); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"composerSidebarBackground": "#18221C", "userMessageBackground": "#1C2620",
		"userMessageBackgroundHover": "#25322A", "selectionBg": "#2A3D31"}
	if theme.Name != "Irie" || theme.Base != "dark" || len(theme.Overrides) != len(want) {
		t.Fatalf("theme %+v", theme)
	}
	for k, v := range want {
		if theme.Overrides[k] != v {
			t.Errorf("%s = %q, want %q", k, theme.Overrides[k], v)
		}
	}
	if wrote, err := WriteTheme(dir, dark); err != nil || wrote {
		t.Errorf("the same palette again: wrote %v, %v", wrote, err)
	}
	dark["panel"] = "#203040"
	if wrote, err := WriteTheme(dir, dark); err != nil || !wrote {
		t.Errorf("another palette: wrote %v, %v", wrote, err)
	}
}

// A session takes Irie's theme while its file is there and the user's own
// theme is Claude Code's default; any other theme of theirs stands.
func TestASessionTakesIriesThemeOverTheDefault(t *testing.T) {
	dir := t.TempDir()
	if got := SessionTheme(dir); got != "" {
		t.Errorf("with no theme file: %q", got)
	}
	if _, err := WriteTheme(dir, map[string]string{"panel": "#18221C"}); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(dir, "settings.json")
	for _, c := range []struct {
		file string
		want string
	}{
		{"", "custom:irie"},
		{`{"model": "opus"}`, "custom:irie"},
		{`{"theme": "dark"}`, "custom:irie"},
		{`{"theme": "auto"}`, "custom:irie"},
		{`{"theme": "light"}`, ""},
		{`{"theme": "dark-daltonized"}`, ""},
		{`{"theme": "custom:dracula"}`, ""},
	} {
		if c.file == "" {
			os.Remove(settings)
		} else if err := os.WriteFile(settings, []byte(c.file), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := SessionTheme(dir); got != c.want {
			t.Errorf("settings %s: %q, want %q", c.file, got, c.want)
		}
	}
	if got := ConfigDir("/home/alice", []string{"CLAUDE_CONFIG_DIR=/home/alice/work-claude"}); got != "/home/alice/work-claude" {
		t.Errorf("CLAUDE_CONFIG_DIR: %q", got)
	}
	if got := ConfigDir("/home/alice", []string{"CLAUDE_CONFIG_DIR=relative"}); got != "/home/alice/.claude" {
		t.Errorf("a relative CLAUDE_CONFIG_DIR: %q", got)
	}
}

// The session's settings name the theme it takes, and nothing without one.
func TestTheSettingsNameTheTheme(t *testing.T) {
	with, err := Settings("/usr/local/bin/irie", time.Hour, true, Extra{Theme: "custom:irie"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(with), `"theme": "custom:irie"`) {
		t.Errorf("no theme in:\n%s", with)
	}
	without, err := Settings("/usr/local/bin/irie", time.Hour, true, Extra{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(without), `"theme"`) {
		t.Errorf("a theme with none asked:\n%s", without)
	}
}
