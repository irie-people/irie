package core

import (
	"fmt"
	"strings"

	"github.com/irie-people/irie/internal/tmux"
)

// remoteWindow names the window of the phone app's gateway.
const remoteWindow = "irie-remote"

// remoteSpec is what the gateway runs with: the version and the settings
// it reads only as it starts. Its pane keeps it in @irie_version, so a
// gateway of another version or settings is started again.
func (c *Core) remoteSpec() string {
	r := c.cfg().Remote
	return strings.Join([]string{c.version, fmt.Sprint(r.Port), r.Listen, r.Name}, " ")
}

// keepRemote keeps the phone app's gateway, `irie remote serve`, running
// in its own window while remote.on is set, and stops it once it is not
// (docs/PHONE.md). Its own supervisor restarts a gateway that exits; this
// starts a new one after an install, or after its settings changed. Like
// every pane of Irie's own, it is never an agent's, so starting it again
// disturbs none. The caller holds c.mu.
func (c *Core) keepRemote() {
	on := c.cfg().Remote.On
	panes := c.snap.ByRole(tmux.RoleRemote)
	var cmds [][]string
	for i, p := range panes {
		if i > 0 || !on {
			cmds = append(cmds, []string{"kill-pane", "-t", p.ID})
		}
	}
	spec := c.remoteSpec()
	serve := []string{"--", c.bin, "remote", "serve", "--supervise"}
	switch {
	case !on:
	case len(panes) == 0:
		out, err := c.tmux.Run(append([]string{"new-window", "-d", "-t", tmux.AgentsSession + ":", "-n", remoteWindow, "-P", "-F", "#{pane_id}"}, serve...)...)
		if err != nil {
			c.log.Warn("remote: start", "err", err)
			return
		}
		pane := strings.TrimSpace(out)
		cmds = append(cmds, []string{"set-option", "-p", "-t", pane, tmux.OptRole, tmux.RoleRemote},
			[]string{"set-option", "-p", "-t", pane, tmux.OptVersion, spec})
		c.log.Info("remote: started", "pane", pane, "spec", spec)
	case panes[0].Dead || panes[0].Version != spec:
		p := panes[0].ID
		cmds = append(cmds, append([]string{"respawn-pane", "-k", "-t", p}, serve...),
			[]string{"set-option", "-p", "-t", p, tmux.OptVersion, spec})
		c.log.Info("remote: restarted", "pane", p, "spec", spec, "was", panes[0].Version)
	}
	if len(cmds) > 0 {
		if _, err := c.tmux.RunList(cmds); err != nil {
			c.log.Warn("remote", "err", err)
		}
	}
}
