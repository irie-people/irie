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
	v := irie.View{Sessions: c.list(), Outside: c.outsideList(), Home: home}
	c.mu.Unlock()
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
	raw, err := json.Marshal(st.Args)
	if err != nil {
		return ipc.InputResult{}, err
	}
	res, err := c.Call(st.Op, raw)
	return irie.Said(st, res, err)
}
