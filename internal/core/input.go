package core

import (
	"encoding/json"
	"os"
	"strings"

	irie "github.com/irie-people/irie/core"
	"github.com/irie-people/irie/internal/ipc"
)

// input reads what a person typed and does it (DESIGN §4.2, docs/CORE.md).
// The words come to one op, which runs through Call as if a client had sent
// it, so a typed :park is a park in every way: its checks, its journal and
// its step of the history.
//
// peer is the process that sent the words, so that a change only the person
// may make, the VPN's, is refused when an agent types it (vpn.go); 0 is
// unknown.
func (c *Core) input(a ipc.InputArgs, peer int) (ipc.InputResult, error) {
	// :move names a session outside Irie, so that list must be fresh.
	if first, _, _ := strings.Cut(strings.TrimSpace(a.Text), "\n"); strings.HasPrefix(first, ":move") {
		c.refreshOutside(outsideEvery, false)
	}
	home, _ := os.UserHomeDir()
	c.mu.Lock()
	// ^name starts the task on that machine (peers.go), when that machine
	// runs every Claude Code hook this one does (place.go).
	p, text, _ := c.placeOn(a.Text)
	a.Text = text
	why := ""
	if p != nil {
		why = c.hooksDiffer(p)
	}
	v := irie.View{Sessions: c.list(), Outside: c.outsideList(), Home: home}
	c.mu.Unlock()
	if p != nil {
		if why != "" {
			return ipc.InputResult{}, ipc.Errorf(ipc.CodeBadArgs, "Not started on %s: %s.", p.name, why)
		}
		return c.inputOn(p, a)
	}
	st, err := irie.Read(a, v)
	if err != nil {
		return ipc.InputResult{}, err
	}
	if st.Op == "" {
		return irie.Said(st, nil, nil)
	}
	if st.Op == ipc.OpVPN {
		res, err := c.vpnOp(peer, st.Args.(ipc.VPNArgs))
		return irie.Said(st, res, err)
	}
	// With place.mode = auto, a new task may go to a peer with more room.
	if sa, ok := st.Args.(ipc.SpawnArgs); ok && st.Op == ipc.OpSpawn {
		if r, placed, err := c.placeTask(a, sa); placed {
			return r, err
		}
	}
	raw, err := json.Marshal(st.Args)
	if err != nil {
		return ipc.InputResult{}, err
	}
	res, err := c.Call(st.Op, raw)
	return irie.Said(st, res, err)
}
