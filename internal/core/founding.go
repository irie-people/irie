//go:build !nofounding

package core

import (
	"strings"

	"github.com/irie-people/irie/internal/founding"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/plugins"
)

// The founding documents (DESIGN §4.17): while the founding plugin is on,
// the founding op gives the Mac app's Founding tab the Declaration of
// Independence, the Constitution and the Bill of Rights, and the passages a
// search finds. go build -tags nofounding leaves this file out, and
// founding_off.go stands in.

const foundingBuilt = true

// foundingOp answers anyone, an agent too: the texts are public, and the
// op only reads them.
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
	}
	return r, nil
}
