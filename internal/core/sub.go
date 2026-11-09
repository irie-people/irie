package core

// Stopping one of a session's subs at your word (DESIGN §4.4): the sidebars'
// Stop on a helper's or a teammate's row. A helper runs inside its
// session's agent, so nothing of its own can be ended: from then on its
// tool calls are turned away, each with word to end at once, until it
// finishes. A teammate is a Claude Code session of its own, in a terminal
// on the team server (team.go), which ends.

import (
	"slices"

	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/team"
)

// stoppedNote is what a stopped helper hears instead of its tool's result.
const stoppedNote = "[Irie] The user stopped you from Irie. Make no more tool calls: end now, and say in a line or two what you found so far."

// stopSub stops the helper or teammate a names, and records it in the
// journal (ui.sub_stopped).
func (c *Core) stopSub(a ipc.StopSubArgs) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	x := c.sessions[a.Session]
	if x == nil {
		return ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Session)
	}
	switch {
	case a.Agent != "" && a.Teammate == "":
		if _, ok := x.track.subs[a.Agent]; !ok {
			return ipc.Errorf(ipc.CodeNotFound, "%s has no helper %q at work", x.name, a.Agent)
		}
		if x.stopped == nil {
			x.stopped = map[string]bool{}
		}
		x.stopped[a.Agent] = true
		c.record(x, event.Event{Source: event.SourceUser, Kind: event.UISubStopped, Data: map[string]any{"agent": a.Agent}})
	case a.Teammate != "" && a.Agent == "":
		if !slices.ContainsFunc(x.team, func(m team.Teammate) bool { return m.Session == a.Teammate }) {
			return ipc.Errorf(ipc.CodeNotFound, "%s has no teammate %q", x.name, a.Teammate)
		}
		if _, err := c.team.Run("kill-session", "-t", "="+a.Teammate); err != nil {
			return err
		}
		c.record(x, event.Event{Source: event.SourceUser, Kind: event.UISubStopped, Data: map[string]any{"teammate": a.Teammate}})
	default:
		return ipc.Errorf(ipc.CodeBadArgs, "stop_sub needs a helper's agent or a teammate, not both")
	}
	c.log.Info("sub stopped", "session", x.id, "agent", a.Agent, "teammate", a.Teammate)
	return nil
}
