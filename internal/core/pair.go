package core

import (
	"net"
	"strconv"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/remote"
)

// maxPairMinutes bounds a pairing code's life: a week.
const maxPairMinutes = 7 * 24 * 60

// pair opens a pairing code for a phone (ipc.OpPair), which the gateway
// takes from the state folder, and says where the phone sets up: the
// machine's Bonjour name first, then its address on the local network.
func (c *Core) pair(a ipc.PairArgs) (ipc.PairResult, error) {
	if a.Minutes < 0 || a.Minutes > maxPairMinutes {
		return ipc.PairResult{}, ipc.Errorf(ipc.CodeBadArgs, "a code works from 1 minute to a week")
	}
	life := time.Duration(a.Minutes) * time.Minute
	if life == 0 {
		life = remote.CodeLife
	}
	cfg := c.cfg().Remote
	code, err := (&remote.Store{Dir: c.paths.Remote()}).NewCode(life)
	if err != nil {
		return ipc.PairResult{}, err
	}
	name := cfg.Name
	if name == "" {
		name = remote.MachineName()
	}
	setup := strconv.Itoa(cfg.Port - 1)
	r := ipc.PairResult{On: cfg.On, Name: name, Code: code, Until: time.Now().Add(life).UTC(), URLs: []string{}}
	for _, n := range remote.LocalNames() {
		r.URLs = append(r.URLs, "http://"+net.JoinHostPort(n, setup)+"/")
	}
	if ip := remote.LANAddr(); ip != nil {
		r.URLs = append(r.URLs, "http://"+net.JoinHostPort(ip.String(), setup)+"/")
	}
	return r, nil
}
