package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// StatusLine is a settings file's statusLine.
type StatusLine struct {
	Type                 string `json:"type"`
	Command              string `json:"command"`
	Padding              int    `json:"padding,omitempty"`
	RefreshInterval      int    `json:"refreshInterval,omitempty"`
	HideVimModeIndicator bool   `json:"hideVimModeIndicator,omitempty"`
}

// UserStatusLine returns the status line the user's own settings give a
// session that starts in cwd, which Irie's --settings would otherwise
// hide: the first file that has one, highest precedence first. Those are
// the project's local settings, which Claude Code keeps at the root of the
// repo's main checkout (and, from older versions, in cwd), the project's
// shared settings in cwd, then the user's settings in CLAUDE_CONFIG_DIR or
// ~/.claude. repo is the main checkout, "" outside git; environ is the
// environment the agent starts with.
func UserStatusLine(cwd, repo, home string, environ []string) (StatusLine, bool) {
	var files []string
	if repo != "" && repo != home {
		files = append(files, filepath.Join(repo, ".claude", "settings.local.json"))
	}
	files = append(files, filepath.Join(cwd, ".claude", "settings.local.json"), filepath.Join(cwd, ".claude", "settings.json"))
	dir := filepath.Join(home, ".claude")
	for _, kv := range environ {
		if v, ok := strings.CutPrefix(kv, "CLAUDE_CONFIG_DIR="); ok && filepath.IsAbs(v) {
			dir = v
		}
	}
	files = append(files, filepath.Join(dir, "settings.json"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var s struct {
			StatusLine *StatusLine `json:"statusLine"`
		}
		if json.Unmarshal(b, &s) != nil || s.StatusLine == nil {
			continue
		}
		if l := *s.StatusLine; (l.Type == "" || l.Type == "command") && strings.TrimSpace(l.Command) != "" {
			l.Type = "command"
			return l, true
		}
	}
	return StatusLine{}, false
}

// statusLine is Irie's status line for a session (DESIGN §6.3): `irie
// statusline`, which gives the core the session's cost, context and plan
// use, then runs the user's own status line, keeping its padding and
// refresh, or prints a short line of its own, every ownRefresh seconds too.
func statusLine(bin string, user StatusLine, ok bool) StatusLine {
	l := StatusLine{Type: "command", Command: shellQuote(bin) + " statusline", RefreshInterval: ownRefresh}
	if ok {
		l.Command += " --then " + shellQuote(user.Command)
		l.Padding, l.RefreshInterval, l.HideVimModeIndicator = user.Padding, user.RefreshInterval, user.HideVimModeIndicator
	}
	return l
}

// ownRefresh is how often, in seconds, Claude Code re-runs Irie's own
// status line. Its updates stop while the session is idle, and so would the
// line's state while the session waits on helpers or its turn has just
// ended.
const ownRefresh = 2
