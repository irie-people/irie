package core

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

func TestPreferencesNote(t *testing.T) {
	if n := preferencesNote([]string{}); n != "" {
		t.Errorf("no preferences said %q", n)
	}
	want := "[Irie] The user’s preferences, which always apply:\n- Text me in one line.\n- Ask before you push."
	if n := preferencesNote([]string{" Text me in one line. ", "Ask before you push."}); n != want {
		t.Errorf("note = %q, want %q", n, want)
	}
}

// A session on a local model in a scratch folder is told to keep what it
// finds in notes.md, after its playbook and before the user's preferences.
// One in a worktree, where the file would be work left to land, and one on
// Claude are told nothing of it.
func TestALocalSessionKeepsNotesInItsScratchFolder(t *testing.T) {
	c := bareCore(t, config.Default())
	dir := filepath.Join(c.paths.Scratch(), "find-the-best-model")
	note := c.startNote(&session{harness: "claude-code", profile: "dory", local: true, cwd: dir, out: &outcome{}})
	told := strings.Index(note, "Keep what you find in notes.md in this folder")
	if told < 0 || strings.Index(note, "The user’s preferences") < told {
		t.Errorf("a local session in a scratch folder is told %q", note)
	}
	tree := "/home/alice/.local/share/irie/worktrees/web/fix-it"
	for _, x := range []*session{
		{harness: "claude-code", profile: "dory", local: true, cwd: tree, worktree: tree, out: &outcome{}},
		{harness: "claude-code", profile: "claude", cwd: dir, out: &outcome{}},
	} {
		if n := c.notesNote(x); n != "" {
			t.Errorf("+%s in %s is told %q", x.profile, x.cwd, n)
		}
	}
}

// TestASessionHearsYourOwnPreferences: a session is told the user's own
// preferences as it starts, in place of the built-in ones.
func TestASessionHearsYourOwnPreferences(t *testing.T) {
	c := setupWith(t, agentConfig{spawn: "preferences = [\"Text me in one line.\"]\n"})
	s := c.spawn("prompt")
	c.waitState(s.ID, ipc.StateWorking)
	told := ""
	for _, r := range c.agentLog(s.ID) {
		if r["t"] != "hook" || r["event"] != "SessionStart" {
			continue
		}
		var out struct {
			HookSpecificOutput struct {
				AdditionalContext string `json:"additionalContext"`
			} `json:"hookSpecificOutput"`
		}
		if err := json.Unmarshal([]byte(fmt.Sprint(r["stdout"])), &out); err != nil {
			t.Fatalf("SessionStart printed %v: %v", r["stdout"], err)
		}
		told = out.HookSpecificOutput.AdditionalContext
	}
	if !strings.Contains(told, preferencesNote([]string{"Text me in one line."})) || strings.Contains(told, config.Preferences()[0]) {
		t.Errorf("as it started, the session was told %q", told)
	}
}
