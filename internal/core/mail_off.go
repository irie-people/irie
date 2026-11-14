//go:build nomail

package core

import "github.com/irie-people/irie/internal/ipc"

// This build leaves the mail plugin out (go build -tags nomail, DESIGN
// §4.17): none of its code is in the binary, and these stand in for
// mail.go, answering that it isn't in this build.

const mailBuilt = false

type mailState struct{}

func (c *Core) mailClose() {}

func (c *Core) mailLive() []ipc.MailAccount { return nil }

func (c *Core) mailOp(int, ipc.MailArgs) (ipc.MailResult, error) {
	return ipc.MailResult{}, notBuilt("Mail")
}
