//go:build !novpn

package config

// VPNBuilt says whether this build has the VPN plugin (DESIGN §4.18); go
// build -tags novpn leaves it out.
const VPNBuilt = true
