package core

// The panes beside a session (docs/proposals/workspaces.md §4), which the Mac
// app shows as a rolodex to the right of the agent's terminal. A pane's own
// shell is the user's login shell in the session's folder, in a tmux session
// of its own on the dock server, as a Dock command is (dock.go): it never
// resizes an agent, it outlives a restart of the app or the core, and the
// Dock's sweep ends it with its session. When the shell exits, its last
// screen stays, as an ended Dock command's does, until the pane restarts it
// or closes.

import (
	"errors"
	"fmt"
	"os"
	"regexp"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/tmux"
)

// paneID is the shape of a pane's id, which names its tmux session.
var paneID = regexp.MustCompile(`^[a-z0-9]{1,24}$`)

// paneSession names the tmux session of a session's pane on the dock server.
func paneSession(session, pane string) string {
	return "pane-" + tmuxSafe(session) + "-" + pane
}

// tmuxSafe keeps the letters, digits, - and _ of s, which a tmux session's
// name may hold.
func tmuxSafe(s string) string {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_' {
			b = append(b, ch)
		}
	}
	return string(b)
}

// paneOp answers ipc.OpPane.
func (c *Core) paneOp(a ipc.PaneArgs) (ipc.PaneResult, error) {
	if !paneID.MatchString(a.Pane) {
		return ipc.PaneResult{}, ipc.Errorf(ipc.CodeBadArgs, "a pane's id is 1 to 24 lower-case letters and digits, not %q", a.Pane)
	}
	c.mu.Lock()
	x := c.sessions[a.Session]
	if x == nil {
		c.mu.Unlock()
		return ipc.PaneResult{}, ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Session)
	}
	dir := x.worktree
	if dir == "" {
		dir = x.cwd
	}
	c.mu.Unlock()
	if st, err := os.Stat(dir); dir == "" || err != nil || !st.IsDir() {
		// A session whose folder has gone opens its shell at home.
		dir, _ = os.UserHomeDir()
	}

	name := paneSession(a.Session, a.Pane)
	_, exists := c.dockCopies()[name]
	end := func() error {
		if _, err := c.dock.Run("kill-session", "-t", "="+name); err != nil && !errors.Is(err, tmux.ErrNoServer) {
			return err
		}
		return nil
	}
	switch {
	case a.End:
		if exists {
			if err := end(); err != nil {
				return ipc.PaneResult{}, err
			}
			c.log.Info("pane ended", "session", a.Session, "pane", a.Pane)
		}
	case a.Restart:
		if exists {
			_ = end()
		}
		if err := c.paneStart(name, a.Session, a.Pane, dir); err != nil {
			return ipc.PaneResult{}, err
		}
	case a.Start:
		if !exists {
			if err := c.paneStart(name, a.Session, a.Pane, dir); err != nil {
				return ipc.PaneResult{}, err
			}
		}
	}

	res := ipc.PaneResult{State: ipc.DockStateStopped, Cwd: dir}
	if cp, ok := c.dockCopies()[name]; ok {
		res.State = ipc.DockStateRunning
		res.Target = name
		res.Tmux = c.tmux.Bin
		res.Socket = c.dockSocketPath()
		if cp.dead {
			res.State = ipc.DockStateExited
			code := cp.status
			res.Exit = &code
		}
	}
	return res, nil
}

// paneStart starts a pane's shell, a login shell in dir, and marks its tmux
// session as its session's, so the Dock's sweep ends it with that session.
func (c *Core) paneStart(name, session, pane, dir string) error {
	conf, err := c.dockConf()
	if err != nil {
		return err
	}
	if _, err := c.dock.StartWith(conf, "new-session", "-d", "-s", name, "-x", "100", "-y", "30", "-c", dir,
		"--", loginShell(), "-l"); err != nil {
		return fmt.Errorf("start the pane's shell: %w", err)
	}
	c.log.Info("pane started", "session", session, "pane", pane, "cwd", dir)
	_, err = c.dock.Run("set-option", "-t", "="+name+":", "@irie_dock_session", session)
	return err
}
