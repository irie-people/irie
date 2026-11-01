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
func (c *Core) input(a ipc.InputArgs) (ipc.InputResult, error) {
	// :move names a session outside Irie, so that list must be fresh.
	if first, _, _ := strings.Cut(strings.TrimSpace(a.Text), "\n"); strings.HasPrefix(first, ":move") {
		c.refreshOutside(outsideEvery, false)
	}
	home, _ := os.UserHomeDir()
	c.mu.Lock()
	// ^name starts the task on that machine (peers.go).
	p, text, _ := c.placeOn(a.Text)
	a.Text = text
	v := irie.View{Sessions: c.list(), Outside: c.outsideList(), Home: home}
	c.mu.Unlock()
	if p != nil {
		return c.inputOn(p, a)
	}
	st, err := irie.Read(a, v)
	if err != nil {
		return ipc.InputResult{}, err
	}
	if st.Op == "" {
		return irie.Said(st, nil, nil)
	}
	raw, err := json.Marshal(st.Args)
	if err != nil {
		return ipc.InputResult{}, err
	}
	res, err := c.Call(st.Op, raw)
	return irie.Said(st, res, err)
}
