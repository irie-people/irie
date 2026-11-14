//go:build novpn

package config

// VPNBuilt says whether this build has the VPN plugin (DESIGN §4.18); this
// one, built with -tags novpn, has not: vpn.on reads as off whatever
// config.toml says.
const VPNBuilt = false
