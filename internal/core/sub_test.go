package core

import (
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/testutil"
)

func TestStopTurnsAHelpersCallsAwayUntilItEnds(t *testing.T) {
	c := setup(t)
	// The helper waits, then calls two tools: by then it is stopped.
	s := c.spawn("prompt,sub:h1:sleep:3s+tool:Grep+tool:Read,stop")
	testutil.Eventually(t, 5*time.Second, "the helper did not start", func() bool {
		r, _ := c.row(s.ID)
		return len(r.Subagents) == 1
	})
	if err := c.call(ipc.OpStopSub, ipc.StopSubArgs{Session: s.ID, Agent: "sub:h1"}, nil); err != nil {
		t.Fatal(err)
	}
	c.waitState(s.ID, ipc.StateDone)
	var reasons []string
	for _, l := range c.agentLog(s.ID) {
		if l["t"] == "denied" {
			r, _ := l["reason"].(string)
			reasons = append(reasons, r)
		}
	}
	if len(reasons) != 2 || !strings.HasPrefix(reasons[0], "[Irie] The user stopped you from Irie.") {
		t.Errorf("the helper's calls were denied with %q, want both turned away", reasons)
	}
	stopped := false
	for _, e := range c.events(s.ID) {
		if e.Kind == event.UISubStopped && e.Str("agent") == "sub:h1" {
			stopped = true
		}
	}
	if !stopped {
		t.Error("the journal has no ui.sub_stopped")
	}
	// A helper not at work, or no sub named, is refused.
	for _, a := range []ipc.StopSubArgs{{Session: s.ID, Agent: "sub:h1"}, {Session: s.ID}, {Session: s.ID, Agent: "sub:x", Teammate: "t"}} {
		if err := c.call(ipc.OpStopSub, a, nil); err == nil {
			t.Errorf("stop_sub %+v went through", a)
		}
	}
}
