package config

import (
	"net"
	"net/url"
	"os"
	"strings"
)

// LoadsAt says whether Irie may call a model on the server at raw, and so
// have the server load it: any server while local.here is true; with it
// false, only one on another computer (DESIGN §6.3). "" stands for Irie's
// own servers, which run on this one.
func (l Local) LoadsAt(raw string) bool {
	return l.Here || (raw != "" && !OnThisComputer(raw))
}

// OnThisComputer says whether raw names a server on this computer: a
// loopback or unspecified address, localhost, or this computer's own host
// name. An address that does not parse counts as this computer's, so that
// with local.here false it loads nothing.
func OnThisComputer(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return true
	}
	h := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if h == "" || h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback() || ip.IsUnspecified()
	}
	name, err := os.Hostname()
	if err != nil {
		return false
	}
	name = strings.ToLower(strings.TrimSuffix(name, ".local"))
	return h == name || h == name+".local"
}
