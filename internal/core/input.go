package core

import (
	"encoding/json"
	"maps"
	"os"
	"slices"
	"strings"

	irie "github.com/irie-people/irie/core"
	"github.com/irie-people/irie/internal/config"
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
//
// Words sent from a note of the notepad mark the note sent once they went
// through, whatever they did but ask (notepad.go).
func (c *Core) input(a ipc.InputArgs, peer int) (out ipc.InputResult, err error) {
	if note, typed := a.Note, a.Text; note != "" {
		defer func() {
			if err == nil && out.Did != ipc.InputAsked {
				c.noteSent(note, typed, out)
			}
		}()
	}
	// :move names a session outside Irie, so that list must be fresh.
	if first, _, _ := strings.Cut(strings.TrimSpace(a.Text), "\n"); strings.HasPrefix(first, ":move") {
		c.refreshOutside("input", outsideEvery, false)
	}
	home, _ := os.UserHomeDir()
	c.mu.Lock()
	// ^name starts the task on that machine (peers.go), when that machine
	// runs every Claude Code hook this one does (place.go). A bench reads
	// its own: it runs its sessions there and judges them from here.
	var p *peerState
	if first, _, _ := strings.Cut(strings.TrimSpace(a.Text), "\n"); first != ":bench" && !strings.HasPrefix(first, ":bench ") {
		p, a.Text, _ = c.placeOn(a.Text)
	}
	why := ""
	if p != nil {
		why = c.hooksDiffer(p)
	}
	v := irie.View{Sessions: c.list(), Outside: c.outsideList(), Home: home, Exists: fileExists, Profiles: c.plusNames()}
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
	if st.Op == ipc.OpMail {
		res, err := c.mailOp(peer, st.Args.(ipc.MailArgs))
		return irie.Said(st, res, err)
	}
	if st.Op == ipc.OpYoink {
		res, err := c.yoinkOp(peer, st.Args.(ipc.YoinkArgs))
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

// plusNames are the names a typed + word picks a profile by (core.View):
// this machine's profiles and its peers', as a task placed on a peer may
// run on one of theirs (place.go), auto, and the local models' prefixes.
// The caller holds c.mu.
func (c *Core) plusNames() []string {
	names := slices.Collect(maps.Keys(c.cfg().Profiles))
	for _, p := range c.peers {
		if p.header != nil && p.header.Machine != nil {
			names = append(names, p.header.Machine.Profiles...)
		}
	}
	slices.Sort(names)
	return append(slices.Compact(names), config.Auto, localPrefix, llamaPrefix)
}

// fileExists says whether there is anything at a path, as :open asks.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
