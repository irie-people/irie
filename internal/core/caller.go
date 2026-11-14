package core

import (
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
	if pid <= 0 {
		return false
	}
	c.mu.Lock()
	hosts := map[int]bool{}
	for _, p := range c.snap.ByRole(tmux.RoleAgent) {
		if p.PID > 0 {
			hosts[p.PID] = true
		}
	}
	c.mu.Unlock()
	t, err := proc.Read()
	if err != nil {
		c.log.Warn("caller", "err", err)
		return false
	}
	for range 64 {
		if hosts[pid] {
			return true
		}
		p, ok := t.Get(pid)
		if !ok || p.PPID <= 1 || p.PPID == pid {
			return false
		}
		pid = p.PPID
	}
	return false
}

// notBuilt is the refusal of a part that this build leaves out with its
// plugin (DESIGN §4.17), such as one built with -tags noprivacy.
func notBuilt(what string) error {
	return ipc.Errorf(ipc.CodeBadArgs, "%s isn't in this build.", what)
}

// builtFeatures leaves out of hello's features the ops of a plugin this
// build leaves out, so a client never offers them.
func builtFeatures(all []string) []string {
	if vpnBuilt && privacyBuilt && marketsBuilt && mailBuilt {
		return all
	}
	var out []string
	for _, f := range all {
		switch {
		case !vpnBuilt && f == ipc.OpVPN:
		case !privacyBuilt && f == ipc.OpAnon:
		case !marketsBuilt && f == ipc.OpMarkets:
		case !mailBuilt && f == ipc.OpMail:
		default:
			out = append(out, f)
		}
	}
	return out
}
