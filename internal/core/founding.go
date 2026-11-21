//go:build !nofounding

package core

import (
	"strings"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/founding"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/plugins"
)

// The charters (DESIGN §4.17): while the founding plugin is on, the
// founding op gives the Mac app's Charters tab the texts of rights, from
// Magna Carta to the Privacy Act, the passages a search finds, the links
// that say how the texts bear on one another, and their local chain, which
// the core keeps: the texts and links this build carries go on it at start
// and at each look, as a new version only when one has changed. go build
// -tags nofounding leaves this file out, and founding_off.go stands in.

const foundingBuilt = true

// foundingMu serializes the chain's writes: one process writes a chain,
// and in it one call at a time.
var foundingMu sync.Mutex

// chartersChain is the chain in its folder, with the first build's chain,
// the founding documents' at chain/founding, moved in once when it is
// there and this folder has nothing yet.
func (c *Core) chartersChain() founding.Chain {
	ch := founding.OpenChain(c.paths.Charters())
	if err := ch.Adopt(c.paths.FoundingChainFirst()); err != nil {
		c.log.Warn("charters: the first build's chain didn't move in", "err", err)
	}
	return ch
}

// recordFounding puts the texts and links this build carries on the local
// chain, unless its newest version holds them already, and checks every
// version.
func (c *Core) recordFounding() ipc.FoundingChain {
	foundingMu.Lock()
	defer foundingMu.Unlock()
	ch := c.chartersChain()
	texts, err := founding.Texts()
	if err == nil {
		var made bool
		var name string
		if name, made, err = ch.Record(texts, founding.Links, time.Now()); err == nil && made {
			c.log.Info("charters: the texts are on the local chain", "version", name)
		}
	}
	st := ch.Check()
	if err != nil && st.Problem == "" {
		st.Problem = err.Error()
		st.Verified = false
	}
	return st
}

// recordFoundingAtStart puts the texts on the chain when the core starts,
// while the plugin is on, so they are there before the tab first looks.
func (c *Core) recordFoundingAtStart() {
	if !c.cfg().FoundingOn() {
		return
	}
	if st := c.recordFounding(); !st.Verified {
		c.log.Warn("charters: the local chain doesn't check", "problem", st.Problem)
	}
}

// foundingOp answers anyone, an agent too: the texts are public, and the
// op only reads them and keeps their chain.
func (c *Core) foundingOp(_ int, a ipc.FoundingArgs) (ipc.FoundingResult, error) {
	if why := plugins.Why(*c.cfg(), plugins.Founding); why != "" {
		return ipc.FoundingResult{}, ipc.Errorf(ipc.CodeBadArgs, "%s", why)
	}
	docs, err := founding.Load()
	if err != nil {
		return ipc.FoundingResult{}, ipc.Errorf(ipc.CodeInternal, "the charters don't read: %v", err)
	}
	r := ipc.FoundingResult{Docs: docs}
	if q := strings.TrimSpace(a.Find); q != "" {
		r.Found = founding.Find(docs, q)
		return r, nil
	}
	r.Links = founding.Links
	st := c.recordFounding()
	r.Chain = &st
	if a.Log > 0 {
		if r.Versions, err = c.chartersChain().Log(a.Log); err != nil {
			return r, ipc.Errorf(ipc.CodeInternal, "the charters' chain: %v", err)
		}
	}
	if a.Export {
		if r.Export, err = c.chartersChain().Export(); err != nil {
			return r, ipc.Errorf(ipc.CodeInternal, "the charters' chain: %v", err)
		}
	}
	return r, nil
}
