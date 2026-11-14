//go:build novpn

package main

// This build leaves the VPN plugin out (go build -tags novpn, DESIGN
// §4.18), with irie setup vpn and its line in the usage.
const vpnSetupUsage = ""
