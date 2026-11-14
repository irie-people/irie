//go:build novpn

package core

import (
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

// This build leaves the VPN plugin out (go build -tags novpn, DESIGN
// §4.18): these stand in for vpn.go, answering that it isn't in this
// build.

const vpnBuilt = false

type vpnState struct{}

func (c *Core) keepVPN()                     {}
func (c *Core) vpnHeader(time.Time) *ipc.VPN { return nil }

func (c *Core) vpnOp(int, ipc.VPNArgs) (ipc.VPNResult, error) {
	return ipc.VPNResult{}, notBuilt("VPN control")
}
