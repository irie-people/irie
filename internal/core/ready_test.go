package core

import (
	"testing"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/ready"
)

// The ready op checks the computer as the agents see it, for the Mac app's
// welcome: here the claude profile runs the fake agent.
func TestReadyAnswersWhatTheAgentsHave(t *testing.T) {
	c := prepare(t, agentConfig{})
	c.WriteConfig(t, agentConfig{extra: "[profiles.claude]\ncommand = \"" + c.fake + "\"\n"}.toml(c.fake))
	t.Setenv("IRIE_FAKE_SIGNED_OUT", "1")
	c.up()
	var r ready.Report
	if err := c.call(ipc.OpReady, nil, &r); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]ready.Status{ready.TmuxID: ready.OK, ready.ClaudeID: ready.OK, ready.SignInID: ready.Missing} {
		if it, ok := r.Item(id); !ok || it.Status != want {
			t.Errorf("%s = %+v, want %s", id, it, want)
		}
	}
	if it, _ := r.Item(ready.ClaudeID); it.Version != "2.1.300" {
		t.Errorf("Claude Code's version = %q", it.Version)
	}
	if _, ok := r.Item(ready.PathID); ok {
		t.Error("the core checked a terminal's PATH")
	}
	if r.Ready() {
		t.Error("ready while signed out")
	}
}
