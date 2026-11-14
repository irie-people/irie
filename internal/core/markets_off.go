//go:build nomarkets

package core

import "github.com/irie-people/irie/internal/ipc"

// This build leaves the markets plugin out (go build -tags nomarkets,
// DESIGN §4.17): this stands in for markets.go, answering that it isn't in
// this build.

const marketsBuilt = false

func (c *Core) marketsOp(ipc.MarketsArgs) (ipc.MarketsResult, error) {
	return ipc.MarketsResult{}, notBuilt("The markets plugin")
}
