package anon

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// forwarder serves f on two ports of the test's own, SOCKS5 and HTTP.
func forwarder(t *testing.T, f Forwarder) (socks, web string) {
	t.Helper()
	listen := func(handle func(net.Conn)) string {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ln.Close() })
		go serveConns(ln, handle)
		return ln.Addr().String()
	}
	return listen(f.SOCKS), listen(f.HTTP)
}

func client(proxy string) *http.Client {
	u, _ := url.Parse(proxy)
	return &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(u)}}
}

func get(t *testing.T, c *http.Client, address string) (int, string) {
	t.Helper()
	resp, err := c.Get(address)
	if err != nil {
		t.Fatalf("GET %s: %v", address, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// The shell's programs reach a page through either of the forwarder's
// ports, and the name goes to the exit to be looked up there.
func TestForwarderCarriesTheShellThroughTheExit(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "filings of "+r.Host+r.URL.Path)
	}))
	defer site.Close()
	exit := newSocks5(t, strings.TrimPrefix(site.URL, "http://"))
	socks, web := forwarder(t, Forwarder{Exit: "127.0.0.1:" + strconv.Itoa(exit.port())})

	if code, body := get(t, client("socks5h://"+socks), "http://acme.example/a"); code != 200 || body != "filings of acme.example/a" {
		t.Errorf("through SOCKS5: %d %q", code, body)
	}
	if code, body := get(t, client("http://"+web), "http://acme.example/b"); code != 200 || body != "filings of acme.example/b" {
		t.Errorf("through the HTTP proxy: %d %q", code, body)
	}
	if h := exit.hosts(); len(h) != 2 || h[0] != "acme.example:80" || h[1] != "acme.example:80" {
		t.Errorf("the exit was asked for %v: names should be looked up there", h)
	}
}

// https goes by CONNECT, with TLS from end to end.
func TestForwarderConnectsHTTPS(t *testing.T) {
	site := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "secure "+r.URL.Path)
	}))
	defer site.Close()
	exit := newSocks5(t, strings.TrimPrefix(site.URL, "https://"))
	_, web := forwarder(t, Forwarder{Exit: "127.0.0.1:" + strconv.Itoa(exit.port())})
	u, _ := url.Parse("http://" + web)
	tr := site.Client().Transport.(*http.Transport).Clone()
	tr.Proxy = http.ProxyURL(u)
	tr.TLSClientConfig.ServerName = "example.com"
	c := &http.Client{Timeout: 10 * time.Second, Transport: tr}
	if code, body := get(t, c, "https://example.com/c"); code != 200 || body != "secure /c" {
		t.Errorf("CONNECT: %d %q", code, body)
	}
	if h := exit.hosts(); len(h) != 1 || h[0] != "example.com:443" {
		t.Errorf("the exit was asked for %v", h)
	}
}

// Signing in, sending mail and the machine's own addresses never go
// through the exit, by either port.
func TestForwarderRefusesSignInsAndPrivateAddresses(t *testing.T) {
	exit := newSocks5(t, "127.0.0.1:1")
	socks, web := forwarder(t, Forwarder{Exit: "127.0.0.1:" + strconv.Itoa(exit.port())})
	for _, target := range []string{"github.com:22", "smtp.example:587", "127.0.0.1:8888", "localhost:9334", "[::1]:80", "10.64.0.1:53"} {
		c, err := net.Dial("tcp", web)
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(c, "CONNECT "+target+" HTTP/1.1\r\nHost: "+target+"\r\n\r\n")
		resp, err := http.ReadResponse(bufio.NewReader(c), nil)
		c.Close()
		if err != nil || resp.StatusCode != http.StatusForbidden {
			t.Errorf("CONNECT %s: %v, %v", target, resp, err)
		}
	}
	// SOCKS5 answers 2, not allowed by its rules.
	c, err := net.Dial("tcp", socks)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Write([]byte{5, 1, 0})
	c.Write(append([]byte{5, 1, 0, 3, 10}, append([]byte("github.com"), 0, 22)...))
	var r [12]byte
	if _, err := io.ReadFull(c, r[:4]); err != nil || r[1] != 0 || r[3] != 2 {
		t.Errorf("SOCKS5 to port 22 answered %v, %v", r[:4], err)
	}
	if h := exit.hosts(); len(h) != 0 {
		t.Errorf("the exit was asked for %v", h)
	}
}

// When the exit is down or can't reach a site, the shell is told so, in
// words that never name the exit.
func TestForwarderErrorsDontNameTheExit(t *testing.T) {
	dead, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gone := dead.Addr().String()
	dead.Close()
	_, web := forwarder(t, Forwarder{Exit: gone})
	code, body := get(t, client("http://"+web), "http://acme.example/")
	if code != http.StatusBadGateway || !strings.Contains(body, "Mullvad VPN") || strings.Contains(body, gone) || strings.Contains(body, "127.0.0.1") {
		t.Errorf("an exit that is down: %d %q", code, body)
	}
	// The fake exit can't reach this site, and says so: SOCKS5's 1.
	exit := newSocks5(t, gone)
	_, web = forwarder(t, Forwarder{Exit: "127.0.0.1:" + strconv.Itoa(exit.port())})
	code, body = get(t, client("http://"+web), "http://acme.example/")
	if code != http.StatusBadGateway || !strings.Contains(body, "couldn't reach acme.example:80") || strings.Contains(body, strconv.Itoa(exit.port())) {
		t.Errorf("a site the exit can't reach: %d %q", code, body)
	}
}

func TestUpstreamIsTheExitsOwnProxy(t *testing.T) {
	if u := (Compartment{Route: Mullvad, Relay: "se-got-wg-001"}).Upstream(); u != "se-got-wg-socks5-001.relays.mullvad.net:1080" {
		t.Errorf("Mullvad upstream = %q", u)
	}
	if u := (Compartment{Route: Tor, TorPort: 9151}).Upstream(); u != "127.0.0.1:9151" {
		t.Errorf("Tor upstream = %q", u)
	}
}

// The shell is kept from the user's sign-ins: the usual variables, and
// any whose name says it holds a secret, but nothing else.
func TestSecretNamesKeepSignInsFromTheShell(t *testing.T) {
	got := SecretNames([]string{"PATH=/usr/bin", "ACME_API_KEY=x", "DB_PASSWORD=y", "SSH_AUTH_SOCK=/tmp/s", "GIT_AUTHOR_NAME=alice", "IRIE_SESSION=s1", "HOME=/home/alice", "MY_OAUTH_CLIENT=z"})
	for _, want := range []string{"GH_TOKEN", "ACME_API_KEY", "DB_PASSWORD", "SSH_AUTH_SOCK", "MY_OAUTH_CLIENT", "CLAUDE_CODE_MESSAGING_TOKEN"} {
		if !slices.Contains(got, want) {
			t.Errorf("SecretNames lacks %s: %v", want, got)
		}
	}
	for _, not := range []string{"PATH", "GIT_AUTHOR_NAME", "IRIE_SESSION", "HOME"} {
		if slices.Contains(got, not) {
			t.Errorf("SecretNames keeps %s from the shell: %v", not, got)
		}
	}
}
