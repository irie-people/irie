package core

// Agent teams (DESIGN §2.3, §6.3). With spawn.teammates = "panes", a Claude
// Code session starts with TMUX naming the team server, at an anchor of its
// own there, and with the tmux shim first on its PATH. The teammates Claude
// Code starts then run in terminal panes of their own on the team server,
// one tmux session each, which the session's row lists and the Mac app
// shows beside its terminal. The core never stops the team server, so a
// restart of Irie leaves every teammate running; a session's teammates end
// once the session is gone.

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/irie-hq/irie/internal/config"
	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/team"
	"github.com/irie-hq/irie/internal/tmux"
)

// teamAnchor is a session's anchor on the team server.
type teamAnchor struct {
	pane, socket, pid string
}

// teams reports whether a session of harness h starts with agent teams.
func (c *Core) teams(h string) bool {
	return c.cfg().Spawn.Teammates == "panes" && h == "claude-code"
}

// anchor makes session id's anchor, a dead pane alone in a tmux session
// named after it, and starts the team server first when it is not running.
// A session that comes back finds the anchor it had. The caller holds c.mu.
func (c *Core) anchor(id string) (teamAnchor, error) {
	conf := c.paths.TeamConf()
	if old, _ := os.ReadFile(conf); string(old) != tmux.TeamConf() {
		if err := os.WriteFile(conf, []byte(tmux.TeamConf()), 0o600); err != nil {
			return teamAnchor{}, err
		}
	}
	const format = "#{pane_id} #{socket_path} #{pid}"
	out, err := c.team.StartWith(conf, "new-session", "-d", "-s", id, "-x", "80", "-y", "24", "-P", "-F", format, "--", "true")
	if err != nil && strings.Contains(err.Error(), "duplicate session") {
		out, err = c.team.Run("display-message", "-p", "-t", "="+id+":", format)
	}
	if err != nil {
		return teamAnchor{}, err
	}
	f := strings.Fields(out)
	if len(f) != 3 {
		return teamAnchor{}, fmt.Errorf("team server: anchor %q", out)
	}
	c.teamSocket = f[1]
	return teamAnchor{pane: f[0], socket: f[1], pid: f[2]}, nil
}

// armTeam readies session id's agent for its team: the shim in its dir,
// first on its PATH, the environment its teammates get, and TMUX at its
// anchor. It returns the dir to put first on the agent's PATH. The
// teammates' environment is the agent's own, without what ties a process
// to the session: Irie's hooks and status line, which the teammates' Claude
// Code runs too, never speak for the session. They only hold a teammate's
// tool calls while the session is frozen (IRIE_TEAM_OF). The caller holds
// c.mu.
func (c *Core) armTeam(id, dir string, a teamAnchor, profile config.Profile, env map[string]string) (string, error) {
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		return "", err
	}
	shim := team.Shim{Tmux: c.tmux.Bin, Socket: a.socket, Session: id, EnvFile: filepath.Join(dir, "team.env")}
	if err := os.WriteFile(filepath.Join(bin, "tmux"), []byte(shim.Script(c.bin)), 0o700); err != nil {
		return "", err
	}
	if _, ok := env["CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS"]; !ok {
		env["CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS"] = "1"
	}
	mates := map[string]string{}
	for _, kv := range c.agentEnv(profile) {
		if k, v, ok := strings.Cut(kv, "="); ok {
			mates[k] = v
		}
	}
	maps.Copy(mates, env)
	for _, k := range []string{"IRIE_SESSION", "TMUX", "TMUX_PANE"} {
		delete(mates, k)
	}
	// What the hooks of a teammate read to wait while the session is frozen.
	mates["IRIE_TEAM_OF"] = id
	if p := mates["PATH"]; p != "" {
		mates["PATH"] = bin + string(os.PathListSeparator) + p
	} else {
		mates["PATH"] = bin
	}
	var lines []string
	for _, k := range slices.Sorted(maps.Keys(mates)) {
		lines = append(lines, k+"="+mates[k])
	}
	if err := team.WriteEnv(shim.EnvFile, lines); err != nil {
		return "", err
	}
	env["TMUX"] = a.socket + "," + a.pid + ",0"
	env["TMUX_PANE"] = a.pane
	return bin, nil
}

// followTeams reads every session's teammates from the team server, and
// ends the teams of sessions that are gone: their anchors and teammates'
// sessions go, which ends what runs in them. The caller holds c.mu.
func (c *Core) followTeams() {
	clear := func() {
		for _, x := range c.sessions {
			x.team = nil
		}
	}
	if c.teamSocket == "" {
		if sock, err := c.tmux.SocketPath(); err == nil {
			c.teamSocket = filepath.Join(filepath.Dir(sock), c.team.Socket)
		}
	}
	if _, err := os.Stat(c.teamSocket); c.teamSocket == "" || err != nil {
		clear()
		return
	}
	out, err := c.team.Run("list-panes", "-a", "-F", team.Format)
	if err != nil {
		if !errors.Is(err, tmux.ErrNoServer) {
			c.log.Warn("teams", "err", err)
		}
		clear()
		return
	}
	teams := team.Parse(out)
	var gone [][]string
	for id, mates := range teams {
		if c.sessions[id] != nil {
			continue
		}
		gone = append(gone, []string{"kill-session", "-t", "=" + id})
		for _, m := range mates {
			gone = append(gone, []string{"kill-session", "-t", "=" + m.Session})
		}
	}
	if len(gone) > 0 {
		c.log.Info("teams end", "cmds", gone)
		if _, err := c.team.RunList(gone); err != nil {
			c.log.Warn("teams end", "err", err)
		}
	}
	for id, x := range c.sessions {
		x.team = teams[id]
	}
}

// wireTeam is a session's teammates for its row.
func wireTeam(mates []team.Teammate) []ipc.Teammate {
	var out []ipc.Teammate
	for _, m := range mates {
		t := ipc.Teammate{Session: m.Session, Name: m.Name, Color: m.Color, Type: m.Type, Title: m.Title, Working: m.Working, Dead: m.Dead}
		if m.Dead {
			t.ExitCode = &m.Exit
		}
		out = append(out, t)
	}
	return out
}

// teamServerEnv is what the team server starts with: only what tmux and a
// login need. A teammate's environment is the one armTeam writes, and
// nothing of the core's own, such as another session's IRIE_SESSION, gets
// in under it.
func teamServerEnv(env []string) []string {
	keep := []string{"HOME", "USER", "LOGNAME", "SHELL", "PATH", "TMPDIR", "TMUX_TMPDIR", "LANG", "LC_ALL", "LC_CTYPE"}
	var out []string
	for _, kv := range env {
		if k, _, ok := strings.Cut(kv, "="); ok && slices.Contains(keep, k) {
			out = append(out, kv)
		}
	}
	return out
}
