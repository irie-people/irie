package anon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The services that say where a request leaves from: Mullvad's, which
// knows its own exits, and the Tor Project's. Tests point them elsewhere.
var (
	MullvadCheck = "https://am.i.mullvad.net/json"
	TorCheck     = "https://check.torproject.org/api/ip"
)

// Client is an HTTP client whose requests leave through proxy, a
// socks5h:// address (or http:// for tests), or straight out through the
// Mac's own tunnel when proxy is "". Names are looked up at the proxy. A
// redirect follows the same way, at most 10, and only to http or https.
func Client(proxy string, timeout time.Duration) (*http.Client, error) {
	tr := &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          8,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   30 * time.Second,
		ResponseHeaderTimeout: timeout,
	}
	if proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil || u.Host == "" {
			return nil, fmt.Errorf("proxy %q: not an address", proxy)
		}
		tr.Proxy = http.ProxyURL(u)
	}
	return &http.Client{
		Transport: tr,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("more than 10 redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("a redirect to %s, which is not http or https", req.URL.Scheme)
			}
			return nil
		},
	}, nil
}

// proxyOf is a compartment's proxy, which tests point at a proxy of
// their own.
var proxyOf = func(c Compartment) string { return c.Proxy() }

// Exit is what a check found of where requests leave from: never an
// address.
type Exit struct {
	Mullvad bool   // Mullvad says it is one of its exits
	Server  string // the exit Mullvad names, such as se-got-wg-socks5-001
	Country string
	City    string
	Tor     bool // the Tor Project says it is a Tor exit
}

func getJSON(ctx context.Context, c *http.Client, address string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", address, resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(v)
}

// Where asks Mullvad where a request through proxy leaves from ("" = the
// Mac's own tunnel).
func Where(ctx context.Context, proxy string) (Exit, error) {
	c, err := Client(proxy, 30*time.Second)
	if err != nil {
		return Exit{}, err
	}
	var r struct {
		Mullvad bool   `json:"mullvad_exit_ip"`
		Server  string `json:"mullvad_exit_ip_hostname"`
		Country string `json:"country"`
		City    string `json:"city"`
	}
	if err := getJSON(ctx, c, MullvadCheck, &r); err != nil {
		return Exit{}, err
	}
	return Exit{Mullvad: r.Mullvad, Server: r.Server, Country: r.Country, City: r.City}, nil
}

// TorWhere asks the Tor Project whether a request through proxy leaves
// from Tor.
func TorWhere(ctx context.Context, proxy string) (Exit, error) {
	c, err := Client(proxy, 60*time.Second)
	if err != nil {
		return Exit{}, err
	}
	var r struct {
		IsTor bool `json:"IsTor"`
	}
	if err := getJSON(ctx, c, TorCheck, &r); err != nil {
		return Exit{}, err
	}
	return Exit{Tor: r.IsTor}, nil
}

// Verify checks that compartment c leaves where it should: a Mullvad
// compartment from its own relay, which is not where the Mac's own tunnel
// leaves from; a Tor compartment from Tor. Its words name no address or
// relay, so an agent may read them. An exit that fails the check is an
// error: use it for nothing.
func Verify(ctx context.Context, c Compartment) (string, error) {
	if c.Route == Tor {
		e, err := TorWhere(ctx, proxyOf(c))
		if err != nil {
			return "", fmt.Errorf("Tor did not answer: %w", err)
		}
		if !e.Tor {
			return "", errors.New("the Tor Project says the exit is not Tor's: use it for nothing")
		}
		return "Checked just now: it leaves from Tor.", nil
	}
	e, err := Where(ctx, proxyOf(c))
	if err != nil {
		return "", fmt.Errorf("its Mullvad exit did not answer: %w (is Mullvad VPN connected?)", err)
	}
	if !e.Mullvad {
		return "", errors.New("Mullvad says the exit is not one of its own: use it for nothing")
	}
	if e.Server != "" && e.Server != SocksName(c.Relay) {
		return "", errors.New("Mullvad says the exit is another relay than the compartment's: use it for nothing")
	}
	// Mullvad names a relay's proxy se-got-wg-socks5-001 and its tunnel's
	// exit se-got-wg-001: the same server either way.
	server := func(s string) string { return strings.Replace(s, "-wg-socks5-", "-wg-", 1) }
	mac, err := Where(ctx, "")
	if err == nil && mac.Server != "" && server(mac.Server) == server(e.Server) {
		return "", errors.New("the exit is the Mac's own: use it for nothing")
	}
	where := e.Country
	if e.City != "" {
		where = e.City + ", " + e.Country
	}
	return "Checked just now: it leaves from a Mullvad exit of its own in " + where + ", not the Mac's.", nil
}
