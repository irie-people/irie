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

// The founding documents (DESIGN §4.17): while the founding plugin is on,
// the founding op gives the Mac app's Founding tab the Declaration of
// Independence, the Constitution, the Bill of Rights and the Freedom of
// Information Act, the passages a search finds, and their local chain,
// which the core keeps: the texts this build carries go on it at start and
// at each look, as a new version only when a text has changed. go build
// -tags nofounding leaves this file out, and founding_off.go stands in.

const foundingBuilt = true

// foundingMu serializes the chain's writes: one process writes a chain,
// and in it one call at a time.
var foundingMu sync.Mutex

// recordFounding puts the texts this build carries on the local chain,
// unless its newest version holds them already, and checks every version.
func (c *Core) recordFounding() ipc.FoundingChain {
	foundingMu.Lock()
	defer foundingMu.Unlock()
	ch := founding.OpenChain(c.paths.FoundingChain())
	texts, err := founding.Texts()
	if err == nil {
		var made bool
		var name string
		if name, made, err = ch.Record(texts, time.Now()); err == nil && made {
			c.log.Info("founding: the texts are on the local chain", "version", name)
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
		c.log.Warn("founding: the local chain doesn't check", "problem", st.Problem)
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
		return ipc.FoundingResult{}, ipc.Errorf(ipc.CodeInternal, "the founding documents don't read: %v", err)
	}
	r := ipc.FoundingResult{Docs: docs}
	if q := strings.TrimSpace(a.Find); q != "" {
		r.Found = founding.Find(docs, q)
		return r, nil
	}
	st := c.recordFounding()
	r.Chain = &st
	if a.Log > 0 {
		if r.Versions, err = founding.OpenChain(c.paths.FoundingChain()).Log(a.Log); err != nil {
			return r, ipc.Errorf(ipc.CodeInternal, "the founding documents' chain: %v", err)
		}
	}
	return r, nil
}
