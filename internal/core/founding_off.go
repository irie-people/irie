//go:build nofounding

package core

import "github.com/irie-people/irie/internal/ipc"

// This build leaves the founding plugin out (go build -tags nofounding,
// DESIGN §4.17): none of its code or texts are in the binary, and this
// stands in for founding.go, answering that it isn't in this build.

const foundingBuilt = false

func (c *Core) foundingOp(int, ipc.FoundingArgs) (ipc.FoundingResult, error) {
	return ipc.FoundingResult{}, notBuilt("The Charters plugin")
}

func (c *Core) recordFoundingAtStart() {}
