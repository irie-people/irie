package claude

// Irie's Claude Code theme. Claude Code paints a few surfaces itself: the
// panel it docks beside the conversation (its diff panel, a mod's pane),
// your messages and a selection, grey in its dark preset, apart from the
// palette the rest of Irie draws in. So Irie writes a custom theme of Claude
// Code's, themes/irie.json in its config folder, from the Mac app's palette:
// its dark set, as Irie's terminal is dark. Claude Code watches that folder
// and repaints a session that uses the theme as the file changes. A session
// Irie starts uses it while the user's own theme is Claude Code's default.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// ThemeSlug is Irie's theme's name: "custom:irie" as a theme setting.
const ThemeSlug = "irie"

// ThemeVersion is the first Claude Code with custom themes.
const ThemeVersion = "2.1.118"

// themeTokens are the colours of Claude Code's that Irie's theme sets, and
// the role of the palette's dark set each takes: the docked panel on the
// details panel's ground, your messages on a card's, a hovered one on the
// hover's, and a selection on the selection's. Its text, marks and diffs
// keep Claude Code's dark preset, which they read on.
var themeTokens = []struct{ token, role string }{
	{"composerSidebarBackground", "panel"},
	{"userMessageBackground", "card"},
	{"userMessageBackgroundHover", "hover"},
	{"selectionBg", "selection"},
}

// ConfigDir is Claude Code's config folder for an agent with this
// environment: CLAUDE_CONFIG_DIR when it is absolute, else ~/.claude.
func ConfigDir(home string, environ []string) string {
	dir := filepath.Join(home, ".claude")
	for _, kv := range environ {
		if v, ok := strings.CutPrefix(kv, "CLAUDE_CONFIG_DIR="); ok && filepath.IsAbs(v) {
			dir = v
		}
	}
	return dir
}

// ThemeFile is where Irie's theme is kept in a config folder.
func ThemeFile(dir string) string { return filepath.Join(dir, "themes", ThemeSlug+".json") }

// WriteTheme writes Irie's theme from a palette's dark set, each colour
// "#RRGGBB" by its role, unless the file already says the same. It says
// whether it wrote.
func WriteTheme(dir string, dark map[string]string) (bool, error) {
	overrides := map[string]string{}
	for _, t := range themeTokens {
		if v := dark[t.role]; v != "" {
			overrides[t.token] = v
		}
	}
	b, err := json.MarshalIndent(struct {
		Name      string            `json:"name"`
		Base      string            `json:"base"`
		Overrides map[string]string `json:"overrides"`
	}{"Irie", "dark", overrides}, "", "  ")
	if err != nil {
		return false, err
	}
	b = append(b, '\n')
	path := ThemeFile(dir)
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, b) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return false, err
	}
	return true, os.Rename(tmp, path)
}

// SessionTheme is the theme a session's settings name: Irie's, while its
// file is in the config folder and the user's own theme, in their
// settings.json, is Claude Code's default (none, dark or auto, which is
// dark on Irie's terminal); "" leaves the user's own.
func SessionTheme(dir string) string {
	if _, err := os.Stat(ThemeFile(dir)); err != nil {
		return ""
	}
	if b, err := os.ReadFile(filepath.Join(dir, "settings.json")); err == nil {
		var s struct {
			Theme string `json:"theme"`
		}
		if json.Unmarshal(b, &s) == nil && s.Theme != "" && s.Theme != "dark" && s.Theme != "auto" {
			return ""
		}
	}
	return "custom:" + ThemeSlug
}
