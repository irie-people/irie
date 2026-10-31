package core

import (
	"cmp"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/statusline"
	"github.com/irie-people/irie/internal/ui"
)

// status takes one status line update of a Claude Code session (DESIGN
// §6.3): what the session has cost, how full its context is, and the
// plan's use, which is the account's and so the newest update's. The
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
	if !reflect.DeepEqual(x.usage, u) {
		x.usage = u
		if err := writeJSON(c.paths.StatusFile(x.id), u); err != nil {
			c.log.Warn("status.json", "session", x.id, "err", err)
		}
	}
	c.plan = mergePlan(c.plan, ipc.Plan{FiveHour: u.FiveHour, SevenDay: u.SevenDay})
	row := x.wire()
	reply := &ipc.StatusReply{State: row.State, Subagents: row.Subagents, Parked: row.Parked}
	if row.State == ipc.StateWorking {
		reply.Color = ui.AgentColor(cmp.Or(c.appTheme, c.cfg().UI.Theme), x.id)
	}
	frames := c.changes()
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

// loadStatus reads a session's last status update, and takes in its plan
// use as a new one would (mergePlan). The caller holds c.mu.
func (c *Core) loadStatus(x *session) {
	b, err := os.ReadFile(c.paths.StatusFile(x.id))
	if err != nil {
		return
	}
	if json.Unmarshal(b, &x.usage) != nil {
		return
	}
	c.plan = mergePlan(c.plan, ipc.Plan{FiveHour: x.usage.FiveHour, SevenDay: x.usage.SevenDay})
}

// sameWindow is how far apart two readings of one window's reset may be:
// Claude Code rounds the time, and two sessions may round it apart.
const sameWindow = 10 * time.Minute

// mergePlan is the plan's use from what the core had and one session's
// reading, window by window (mergeWindow).
func mergePlan(have, got ipc.Plan) ipc.Plan {
	return ipc.Plan{FiveHour: mergeWindow(have.FiveHour, got.FiveHour), SevenDay: mergeWindow(have.SevenDay, got.SevenDay)}
}

// mergeWindow is one window of the plan's use from what the core had and
// one session's reading. The use is the account's, but each session reads
// it when one of its own requests comes back, so a session that has been
// quiet reports an older, smaller figure; with many sessions the newest
// reading would flip between them every few updates, and every client
// would draw the header again each time. Within one window the use only
// grows, so the larger figure is the newer. A reading of a later window
// replaces the one the core had, and a reading of an earlier one is old
// news.
func mergeWindow(have, got *ipc.Window) *ipc.Window {
	switch {
	case got == nil:
		return have
	case have == nil, got.ResetsAt.After(have.ResetsAt.Add(sameWindow)):
		return got
	case got.ResetsAt.Before(have.ResetsAt.Add(-sameWindow)), got.Used <= have.Used:
		return have
	}
	return got
}
