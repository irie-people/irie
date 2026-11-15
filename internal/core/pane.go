package core

// The panes beside a session (docs/proposals/workspaces.md §4), which the Mac
// app shows as a rolodex to the right of the agent's terminal. A pane's own
// shell is the user's login shell in the session's folder, in a tmux session
// of its own on the dock server, as a Dock command is (dock.go): it never
// resizes an agent, it outlives a restart of the app or the core, and the
// Dock's sweep ends it with its session. When the shell exits, its last
// screen stays, as an ended Dock command's does, until the pane restarts it
// or closes. Beside a session on a peer, the shell is ssh to that machine
// and a login shell there, in the session's folder (remoteShell).

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/tmux"
)

// paneID is the shape of a pane's id, which names its tmux session.
var paneID = regexp.MustCompile(`^[a-z0-9]{1,24}$`)

// paneMu makes each pane op's look and act one step: a client asks to start
// a card's shell again as the card comes to the front, and an end may race
// a start, which would otherwise leave a shell with no card.
var paneMu sync.Mutex

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
	var dir string
	// What the shell runs beside a session on a peer, from home here.
	var remote []string
	switch {
	case x != nil:
		dir = cmp.Or(x.worktree, x.cwd)
	default:
		p, rest, ok := c.splitPeerID(a.Session)
		var row ipc.Session
		if ok {
			row, ok = p.rows[rest]
		}
		if !ok {
			c.mu.Unlock()
			return ipc.PaneResult{}, ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Session)
		}
		ssh := c.peerSSH(p)
		if ssh == "" {
			c.mu.Unlock()
			return ipc.PaneResult{}, ipc.Errorf(ipc.CodeBadArgs, "%s runs on %s, and a shell beside it reaches %s over ssh: set peers.%s.ssh in the config", row.Name, p.name, p.name, p.name)
		}
		dir = cmp.Or(row.Worktree, row.Cwd)
		remote = remoteShell(ssh, dir)
	}
	c.mu.Unlock()
	start := dir
	if st, err := os.Stat(dir); remote != nil || dir == "" || err != nil || !st.IsDir() {
		// A session whose folder has gone opens its shell at home, as one
		// on a peer starts its ssh from home.
		start, _ = os.UserHomeDir()
		if remote == nil {
			dir = start
		}
	}

	name := paneSession(a.Session, a.Pane)
	paneMu.Lock()
	defer paneMu.Unlock()
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
		if err := c.paneStart(name, a.Session, a.Pane, start, remote); err != nil {
			return ipc.PaneResult{}, err
		}
	case a.Start:
		if !exists {
			if err := c.paneStart(name, a.Session, a.Pane, start, remote); err != nil {
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

// paneStart starts a pane's shell, a login shell in dir, or remote in its
// place, and marks its tmux session as its session's, so the Dock's sweep
// ends it with that session.
func (c *Core) paneStart(name, session, pane, dir string, remote []string) error {
	conf, err := c.dockConf()
	if err != nil {
		return err
	}
	run := []string{loginShell(), "-l"}
	if remote != nil {
		run = remote
	}
	args := append([]string{"new-session", "-d", "-s", name, "-x", "100", "-y", "30", "-c", dir, "--"}, run...)
	if _, err := c.dock.StartWith(conf, args...); err != nil {
		// Another core's start won: the shell is there.
		if strings.Contains(err.Error(), "duplicate session") {
			return nil
		}
		return fmt.Errorf("start the pane's shell: %w", err)
	}
	c.log.Info("pane started", "session", session, "pane", pane, "cwd", dir)
	if _, err := c.dock.Run("set-option", "-t", "="+name+":", "@irie_dock_session", session); err != nil {
		// A shell the sweep can't tie to its session would outlive it.
		_, _ = c.dock.Run("kill-session", "-t", "="+name)
		return fmt.Errorf("mark the pane's shell as its session's: %w", err)
	}
	return nil
}

// remoteShell is what a pane's shell runs beside a session on a peer: ssh
// to that machine with a terminal, and there a login shell in the session's
// folder, or at home when the folder has gone. A link gone quiet ends it
// within a minute, keeping its last screen, rather than freezing it.
func remoteShell(ssh, dir string) []string {
	cd := ""
	if dir != "" {
		cd = "cd " + quoteForShell(dir) + " 2>/dev/null; "
	}
	return []string{"ssh", "-t", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3", "--", ssh, cd + `exec "$SHELL" -l`}
}

// quoteForShell quotes s as one word for a POSIX shell.
func quoteForShell(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
