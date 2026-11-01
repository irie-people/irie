package core

import (
	"encoding/json"
	"fmt"
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
