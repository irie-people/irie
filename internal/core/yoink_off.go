//go:build noyoink

package core

import "github.com/irie-people/irie/internal/ipc"

// This build leaves the yoink plugin out (go build -tags noyoink, DESIGN
// §4.17): none of its code is in the binary, and these stand in for
// yoink.go, answering that it isn't in this build.

const yoinkBuilt = false

type yoinkState struct{}

func (c *Core) yoinkOp(int, ipc.YoinkArgs) (ipc.YoinkResult, error) {
	return ipc.YoinkResult{}, notBuilt("Yoink")
}
