//go:build nojammin

package core

import "github.com/irie-people/irie/internal/ipc"

// This build leaves the jammin plugin out (go build -tags nojammin, DESIGN
// §4.17): none of its code is in the binary, and these stand in for
// jammin.go, answering that it isn't in this build.

const jamminBuilt = false

type jamminState struct{}

func (c *Core) jamminOp(int, ipc.JamminArgs) (ipc.JamminResult, error) {
	return ipc.JamminResult{}, notBuilt("Jammin")
}

func (c *Core) jamminLive() []ipc.JamSource { return nil }
