package core

import (
	"cmp"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/statusline"
	"github.com/irie-people/irie/internal/ui"
)

// status takes one status line update of a Claude Code session (DESIGN
// §6.3): what the session has cost, how full its context is, and what it
// last saw of the plan's use, which is the account's (takePlan). The
// session's last update is kept in status.json, for a restarted core. It
// answers with the session's state and running subagents, whether it is
// parked, and while it works its own colour.
func (c *Core) status(a ipc.StatusArgs) (*ipc.StatusReply, error) {
	u, err := statusline.Parse(a.Payload)
	if err != nil {
		return nil, ipc.Errorf(ipc.CodeBadArgs, "%v", err)
	}
	c.mu.Lock()
	x := c.sessions[a.Session]
	if x == nil {
		c.mu.Unlock()
		return nil, ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Session)
	}
	x.statusN++
	if !reflect.DeepEqual(x.usage, u) {
		x.usage = u
		if err := writeJSON(c.paths.StatusFile(x.id), u); err != nil {
			c.log.Warn("status.json", "session", x.id, "err", err)
		}
	} else {
		c.perf.Add("status.unchanged", 1)
	}
	c.takePlan(u.FiveHour, u.SevenDay)
	row := x.wire()
	reply := &ipc.StatusReply{State: row.State, Subagents: row.Subagents, Parked: row.Parked}
	// Its colour is the one its row has, given by the time changes lists it.
	frames := c.changes()
	if row.State == ipc.StateWorking {
		reply.Color = ui.AgentColor(cmp.Or(c.appTheme, c.cfg().UI.Theme), x.hue)
	}
	c.mu.Unlock()
	c.publish(frames)
	c.publishHeader()
	return reply, nil
}

// theme takes the Mac app's palette, so the agents' colours on the status
// line match their cards; until the app says, they follow ui.theme.
func (c *Core) theme(a ipc.ThemeArgs) error {
	if !slices.Contains(config.Themes, a.Theme) {
		return ipc.Errorf(ipc.CodeBadArgs, "theme %q: must be one of %s", a.Theme, strings.Join(config.Themes, ", "))
	}
	c.mu.Lock()
	c.appTheme = a.Theme
	c.mu.Unlock()
	return nil
}

// loadStatus reads a session's last status update and folds what it saw of
// the plan's use into the plan's (takePlan). The caller holds c.mu.
func (c *Core) loadStatus(x *session) {
	b, err := os.ReadFile(c.paths.StatusFile(x.id))
	if err != nil {
		return
	}
	if json.Unmarshal(b, &x.usage) != nil {
		return
	}
	c.takePlan(x.usage.FiveHour, x.usage.SevenDay)
}

// takePlan folds what one session saw of the plan's two windows into the
// plan's use, and says whether that changed it. Each session sends the
// figures of its own last request, so an idle session goes on sending old
// ones: a window that has since reset, or a lower use of the window that
// runs, from before the others' work. So each window keeps the figure of
// the window that resets last and, within it, the highest use, since a
// window's use only grows until it resets. A figure that does not say when
// its window resets is taken as it comes. The caller holds c.mu.
func (c *Core) takePlan(five, week *ipc.Window) bool {
	p := ipc.Plan{FiveHour: newerWindow(c.plan.FiveHour, five), SevenDay: newerWindow(c.plan.SevenDay, week)}
	if reflect.DeepEqual(p, c.plan) {
		return false
	}
	c.plan = p
	return true
}

// newerWindow is the newer of what the core has of a window and what a
// session saw of it, nil when neither says.
func newerWindow(have, got *ipc.Window) *ipc.Window {
	switch {
	case got == nil:
		return have
	case have == nil, have.ResetsAt.IsZero(), got.ResetsAt.IsZero(), got.ResetsAt.After(have.ResetsAt):
		return got
	case got.ResetsAt.Equal(have.ResetsAt) && got.Used > have.Used:
		return got
	}
	return have
}
