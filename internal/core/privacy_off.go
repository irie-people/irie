//go:build noprivacy

package core

import (
	"github.com/irie-people/irie/internal/harness"
	"github.com/irie-people/irie/internal/ipc"
)

// This build leaves the privacy plugin out (go build -tags noprivacy,
// DESIGN §4.17): none of its code is in the binary, and these stand in for
// anon.go and browser.go, answering that it isn't in this build. The VPN
// is a plugin of its own (vpn.go, vpn_off.go).

const privacyBuilt = false

// compartment stands in for an anonymous research compartment, of which
// this build makes none.
type compartment struct{ Name string }

func (c *Core) anonName(string, string) (string, error) {
	return "", notBuilt("Anonymous research")
}

func (c *Core) prepareAnon(string, string) (compartment, []string, error) {
	return compartment{}, nil, notBuilt("Anonymous research")
}

func (c *Core) anonLaunch(*harness.Spawn, *session, compartment) error {
	return notBuilt("Anonymous research")
}

func anonNote(*session) string { return "" }

func (c *Core) anonOp(ipc.AnonArgs) (ipc.AnonResult, error) {
	return ipc.AnonResult{}, notBuilt("Anonymous research")
}

func (c *Core) browserMCP(string, []string) (map[string]harness.MCPServer, string) {
	return nil, ""
}

func (c *Core) keepBrowser() {}
