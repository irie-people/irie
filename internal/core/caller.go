package core

import (
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/proc"
	"github.com/irie-people/irie/internal/tmux"
)

// fromAgent reports whether the process pid runs inside one of Irie's
// agents: whether an agent pane's first process, its host, is among its
// parents. It stops an agent that goes through Irie, not one that runs
// mullvad itself, which lockdown mode holds against. An unknown pid is not
// an agent's.
func (c *Core) fromAgent(pid int) bool {
	_, ok := c.agentPane(pid)
	return ok
}

// peerAgent is the session whose agent's process tree process pid runs
// in, as fromAgent walks it, and whether pid runs inside an agent at all:
// a pane that is no session's is an agent's all the same.
func (c *Core) peerAgent(pid int) (session string, inAgent bool) {
	p, ok := c.agentPane(pid)
	if !ok {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, x := range c.sessions {
		if x.pane == p.ID {
			return x.id, true
		}
	}
	return "", true
}

// agentPane is the agent pane whose host is among the parents of process
// pid, and whether there is one.
func (c *Core) agentPane(pid int) (tmux.Pane, bool) {
	if pid <= 0 {
		return tmux.Pane{}, false
	}
	c.mu.Lock()
	hosts := map[int]tmux.Pane{}
	for _, p := range c.snap.ByRole(tmux.RoleAgent) {
		if p.PID > 0 {
			hosts[p.PID] = p
		}
	}
	c.mu.Unlock()
	t, err := proc.Read()
	if err != nil {
		c.log.Warn("caller", "err", err)
		return tmux.Pane{}, false
	}
	for range 64 {
		if p, ok := hosts[pid]; ok {
			return p, true
		}
		p, ok := t.Get(pid)
		if !ok || p.PPID <= 1 || p.PPID == pid {
			return tmux.Pane{}, false
		}
		pid = p.PPID
	}
	return tmux.Pane{}, false
}

// notBuilt is the refusal of a part that this build leaves out with its
// plugin (DESIGN §4.17), such as one built with -tags noprivacy.
func notBuilt(what string) error {
	return ipc.Errorf(ipc.CodeBadArgs, "%s isn't in this build.", what)
}

// builtFeatures leaves out of hello's features the ops of a plugin this
// build leaves out, so a client never offers them.
func builtFeatures(all []string) []string {
	if vpnBuilt && privacyBuilt && marketsBuilt && mailBuilt && yoinkBuilt && config.WritingBuilt {
		return all
	}
	var out []string
	for _, f := range all {
		switch {
		case !vpnBuilt && f == ipc.OpVPN:
		case !privacyBuilt && f == ipc.OpAnon:
		case !marketsBuilt && f == ipc.OpMarkets:
		case !mailBuilt && f == ipc.OpMail:
		case !yoinkBuilt && f == ipc.OpYoink:
		case !config.WritingBuilt && f == ipc.OpVariorum:
		default:
			out = append(out, f)
		}
	}
	return out
}
