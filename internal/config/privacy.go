package config

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"time"
)

// The privacy plugin's defaults and checks (DESIGN §4.17) live here, so the
// config needs none of the plugin's packages: a build without the plugin
// (go build -tags noprivacy) leaves them out of the binary, and still reads
// a config.toml that sets them. internal/browser and internal/anon take
// these from here.

// BrowserIdle is how long Irie's browser waits for a call before it closes.
const BrowserIdle = 10 * time.Minute

// BrowserSearch is DuckDuckGo's page for browsers without JavaScript,
// which answers with plain links.
const BrowserSearch = "https://html.duckduckgo.com/html/?q=%s"

// The routes an anonymous research compartment leaves by.
const (
	AnonMullvad = "mullvad" // a Mullvad relay's SOCKS5 proxy, inside the Mac's own Mullvad tunnel
	AnonTor     = "tor"     // Irie's own Tor, on a SocksPort of the compartment's own
)

// AnonRoutes are the routes, in the order the config names them.
var AnonRoutes = []string{AnonMullvad, AnonTor}

// ParseProxy checks a SOCKS5 proxy URL, socks5://host:port (socks5h://
// too), and returns its host and port.
func ParseProxy(s string) (string, int, error) {
	u, err := url.Parse(s)
	if err != nil {
		return "", 0, err
	}
	if u.Scheme != "socks5" && u.Scheme != "socks5h" {
		return "", 0, errors.New("must be socks5://host:port")
	}
	if u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" {
		return "", 0, errors.New("must be socks5://host:port, with nothing more")
	}
	host := u.Hostname()
	port, err := strconv.Atoi(u.Port())
	if host == "" || err != nil || port < 1 || port > 65535 {
		return "", 0, errors.New("must be socks5://host:port")
	}
	if !proxyHost.MatchString(host) {
		return "", 0, fmt.Errorf("%q is not a host name or address", host)
	}
	return host, port, nil
}

var proxyHost = regexp.MustCompile(`^[A-Za-z0-9.:-]+$`)
