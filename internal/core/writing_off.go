//go:build nowriting

package core

import (
	"encoding/json"

	"github.com/irie-people/irie/internal/harness"
	"github.com/irie-people/irie/internal/ipc"
)

// This build leaves the writing plugin out (go build -tags nowriting,
// DESIGN §4.17): none of its code is in the binary, and these stand in for
// variorum.go, answering that it isn't in this build.

type variorumState struct{}

func (c *Core) variorumMCP(string) map[string]harness.MCPServer { return nil }

func (c *Core) noteVariorum(*session, string, json.RawMessage) {}

func (c *Core) variorumOp(int, ipc.VariorumArgs) (ipc.VariorumResult, error) {
	return ipc.VariorumResult{State: ipc.VariorumOff, Text: "The Variorum tab isn't in this build."}, nil
}
