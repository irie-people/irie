package core

import (
	"cmp"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"

	"github.com/irie-hq/irie/internal/config"
	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/statusline"
	"github.com/irie-hq/irie/internal/ui"
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
	if u.FiveHour != nil || u.SevenDay != nil {
		c.plan = ipc.Plan{FiveHour: u.FiveHour, SevenDay: u.SevenDay}
	}
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

// loadStatus reads a session's last status update, and takes its plan use
// when it is the newest the core has seen. The caller holds c.mu.
func (c *Core) loadStatus(x *session) {
	path := c.paths.StatusFile(x.id)
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	if json.Unmarshal(b, &x.usage) != nil {
		return
	}
	if fi, err := os.Stat(path); err == nil && (x.usage.FiveHour != nil || x.usage.SevenDay != nil) && fi.ModTime().After(c.planAt) {
		c.plan, c.planAt = ipc.Plan{FiveHour: x.usage.FiveHour, SevenDay: x.usage.SevenDay}, fi.ModTime()
	}
}
