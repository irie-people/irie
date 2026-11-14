package core

import (
	"os"
	"slices"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/testutil"
)

// A turn that fails while no core listens, as for the seconds an install
// takes to restart the core, is not lost: the agent's hooks keep its
// events, and the core takes them once it is back, at the time they came,
// so the row says the turn failed rather than that it still works.
func TestATurnThatFailsWhileTheCoreIsDownIsNotLost(t *testing.T) {
	c := setupWith(t, agentConfig{env: map[string]string{"IRIE_FAKE_TURNS": "stopfail:server_error"}})
	s := c.spawn("prompt,stop")
	c.waitState(s.ID, ipc.StateDone)
	before := len(c.agentLog(s.ID))
	c.stopCore()
	down := time.Now()
	c.keys(s, "continue", "Enter")
	testutil.Eventually(t, 10*time.Second, "the agent's turn never failed", func() bool {
		return slices.ContainsFunc(c.agentLog(s.ID)[before:], func(r map[string]any) bool { return r["t"] == "hook" && r["event"] == "StopFailure" })
	})
	failed := time.Now()
	if _, err := os.Stat(c.paths.MissedFile(s.ID)); err != nil {
		t.Fatalf("the hooks kept nothing: %v", err)
	}
	c.up()
	if r := c.waitState(s.ID, ipc.StateNeedsYou); r.Detail != "turn failed: server_error" {
		t.Errorf("detail %q", r.Detail)
	}
	var late []string
	for _, e := range c.events(s.ID) {
		if e.Data["late"] != true {
			continue
		}
		late = append(late, e.Kind)
		// At the time it came, to the millisecond the journal keeps.
		if at := e.Time(); at.Before(down.Add(-time.Millisecond)) || at.After(failed) {
			t.Errorf("%s at %v, while the core was down from %v to %v", e.Kind, at, down, failed)
		}
	}
	if want := []string{event.PromptSubmitted, event.TurnFailed}; !slices.Equal(late, want) {
		t.Errorf("late events %v, want %v", late, want)
	}
	testutil.Eventually(t, 5*time.Second, "the core left the file", func() bool {
		_, err := os.Stat(c.paths.MissedFile(s.ID))
		return os.IsNotExist(err)
	})
}
