//go:build nochat

package core

import (
	"context"

	"github.com/irie-people/irie/internal/ipc"
)

// This build leaves the chat plugin out (go build -tags nochat, DESIGN
// §4.17): none of its code is in the binary, and these stand in for
// chat.go, answering that it isn't in this build.

const chatBuilt = false

type chatState struct{}

func (c *Core) chatLive() []ipc.ChatAccount { return nil }

func (c *Core) chatListen(context.Context) {}

func (c *Core) chatOp(int, ipc.ChatArgs) (ipc.ChatResult, error) {
	return ipc.ChatResult{}, notBuilt("Chat")
}
