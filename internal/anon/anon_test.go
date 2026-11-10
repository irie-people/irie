package anon

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// relayList is the daemon's relay list, cut down: one of each kind.
const relayList = `{
  "locations": {
    "se-got": {"city": "Gothenburg", "country": "Sweden"},
    "ch-zrh": {"city": "Zurich", "country": "Switzerland"},
    "us-sea": {"city": "Seattle, WA", "country": "USA"}
  },
  "wireguard": {"relays": [
    {"hostname": "se-got-wg-001", "active": true, "owned": true, "location": "se-got", "provider": "31173"},
    {"hostname": "se-got-wg-002", "active": true, "owned": false, "location": "se-got", "provider": "Rented"},
    {"hostname": "ch-zrh-wg-001", "active": true, "owned": true, "location": "ch-zrh", "provider": "M247"},
    {"hostname": "us-sea-wg-406", "active": true, "owned": true, "location": "us-sea", "provider": "100TB"},
    {"hostname": "se-got-wg-003", "active": false, "owned": true, "location": "se-got", "provider": "31173"},
    {"hostname": "se-got-br-001", "active": true, "owned": true, "location": "se-got", "provider": "31173"}
  ]}
}`

func relays(t *testing.T) []Relay {
	t.Helper()
	rs, err := ParseRelays([]byte(relayList))
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

func TestRelaysAreTheActiveWireGuardOnes(t *testing.T) {
	rs := relays(t)
	var hosts []string
	for _, r := range rs {
		hosts = append(hosts, r.Host)
	}
	if want := []string{"se-got-wg-001", "se-got-wg-002", "ch-zrh-wg-001", "us-sea-wg-406"}; !slices.Equal(hosts, want) {
		t.Fatalf("relays = %v, want %v", hosts, want)
	}
	if r := rs[0]; r.CC != "se" || r.Country != "Sweden" || r.City != "Gothenburg" || !r.Owned {
		t.Errorf("relay = %+v", r)
	}
	if got := SocksAddr("se-got-wg-001"); got != "se-got-wg-socks5-001.relays.mullvad.net:1080" {
		t.Errorf("SocksAddr = %q", got)
	}
	if _, err := ParseRelays([]byte(`{"wireguard": {"relays": []}}`)); err == nil {
		t.Error("a list with no relay should be an error")
	}
	if _, err := ReadRelays(filepath.Join(t.TempDir(), "none.json")); !errors.Is(err, ErrNoRelays) {
		t.Errorf("a missing list: %v", err)
	}
}

func TestPickKeepsToCountriesOwnedAndFree(t *testing.T) {
	rs := relays(t)
	for range 50 {
		r, err := Pick(rs, []string{"se"}, true, nil)
		if err != nil || r.Host != "se-got-wg-001" {
			t.Fatalf("owned in se: %v, %v", r, err)
		}
	}
	seen := map[string]bool{}
	for range 200 {
		r, err := Pick(rs, nil, false, map[string]bool{"us-sea-wg-406": true})
		if err != nil {
			t.Fatal(err)
		}
		seen[r.Host] = true
	}
	if len(seen) != 3 || seen["us-sea-wg-406"] {
		t.Errorf("picked %v: want each of the three free relays, never a held one", seen)
	}
	if _, err := Pick(rs, []string{"de"}, false, nil); !errors.Is(err, ErrNoExit) {
		t.Errorf("no relay in de: %v", err)
	}
}

func TestTakeGivesEachCompartmentAnExitOfItsOwn(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "anon", "compartments.json")}
	rs := relays(t)
	o := Options{Route: Mullvad, Owned: true, TorPort: 9150}
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	a, err := Take(s, "acme", "", o, rs, "us-sea-wg-406", now)
	if err != nil || !a.Made || a.Route != Mullvad || a.Relay == "" || a.Relay == "us-sea-wg-406" {
		t.Fatalf("acme = %+v, %v", a, err)
	}
	b, err := Take(s, "leaks", "", o, rs, "us-sea-wg-406", now)
	if err != nil || b.Relay == a.Relay || b.Relay == "us-sea-wg-406" {
		t.Fatalf("leaks = %+v, %v: want a relay of its own", b, err)
	}
	// Owned relays are all held now: a third has none left.
	if _, err := Take(s, "third", "", o, rs, "us-sea-wg-406", now); !errors.Is(err, ErrNoExit) {
		t.Fatalf("third: %v, want no exit", err)
	}
	again, err := Take(s, "acme", "", o, rs, "us-sea-wg-406", now.Add(time.Hour))
	if err != nil || again.Made || again.Moved || again.Relay != a.Relay {
		t.Fatalf("acme again = %+v, %v: want the same exit", again, err)
	}
	// Mullvad took acme's relay out of service: it takes another.
	var left []Relay
	for _, r := range rs {
		if r.Host != a.Relay {
			left = append(left, r)
		}
	}
	if _, err := Forget(s, "leaks"); err != nil {
		t.Fatal(err)
	}
	moved, err := Take(s, "acme", "", o, left, "us-sea-wg-406", now.Add(2*time.Hour))
	if err != nil || !moved.Moved || moved.Relay == a.Relay || moved.Moved == false {
		t.Fatalf("acme moved = %+v, %v", moved, err)
	}
	if _, err := Take(s, "acme", Tor, o, rs, "", now); !errors.Is(err, ErrRouteDiffers) {
		t.Errorf("acme on tor: %v", err)
	}
	t1, err := Take(s, "onions", Tor, o, nil, "", now)
	t2, err2 := Take(s, "more", Tor, o, nil, "", now)
	if err != nil || err2 != nil || t1.TorPort != 9150 || t2.TorPort != 9151 || t1.Proxy() != "socks5h://127.0.0.1:9150" {
		t.Fatalf("tor compartments: %+v %+v %v %v", t1, t2, err, err2)
	}
	for _, bad := range []string{"", "Acme", "-acme", "a b", strings.Repeat("a", 41), "../x"} {
		if _, err := Take(s, bad, "", o, rs, "", now); err == nil {
			t.Errorf("name %q was taken", bad)
		}
	}
	fi, err := os.Stat(s.Path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("the store's mode: %v, %v", fi, err)
	}
	all, err := s.Load()
	if err != nil || len(all) != 3 {
		t.Errorf("load = %v, %v", all, err)
	}
}

func TestWhereNamesNoAddress(t *testing.T) {
	c := Compartment{Name: "acme", Route: Mullvad, Relay: "se-got-wg-001", Country: "Sweden", City: "Gothenburg"}
	if w := c.Where(); w != "a Mullvad exit of its own in Gothenburg, Sweden" {
		t.Errorf("Where = %q", w)
	}
	if p := c.Proxy(); p != "socks5h://se-got-wg-socks5-001.relays.mullvad.net:1080" {
		t.Errorf("Proxy = %q", p)
	}
	ex := Exports(c)
	for _, want := range []string{
		"export ALL_PROXY='socks5h://se-got-wg-socks5-001.relays.mullvad.net:1080'\n",
		"export https_proxy='socks5h://se-got-wg-socks5-001.relays.mullvad.net:1080'\n",
		"export NO_PROXY='localhost,127.0.0.1,::1'\n",
		"export IRIE_ANON='acme'\n",
	} {
		if !strings.Contains(ex, want) {
			t.Errorf("exports lack %q:\n%s", want, ex)
		}
	}
	if q := shellQuote("it's"); q != `'it'\''s'` {
		t.Errorf("shellQuote = %s", q)
	}
}

// A program reads the first of two copies of a variable: the proxy
// variables a shell had go, or an inherited NO_PROXY=* would send a
// program from the Mac's own exit, and another compartment's proxy from
// that one's.
func TestEnvironDropsTheProxiesItHad(t *testing.T) {
	c := Compartment{Name: "acme", Route: Mullvad, Relay: "se-got-wg-001"}
	env := Environ([]string{
		"PATH=/usr/bin", "NO_PROXY=*", "no_proxy=*",
		"ALL_PROXY=socks5h://other.example:1080", "https_proxy=http://proxy.example:3128",
		"FTP_PROXY=http://proxy.example:3128", "IRIE_ANON=other", "HOME=/home/alice",
	}, c)
	seen := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		if _, twice := seen[k]; twice {
			t.Errorf("%s is set twice: %v", k, env)
		}
		seen[k] = v
	}
	want := map[string]string{
		"PATH": "/usr/bin", "HOME": "/home/alice", "IRIE_ANON": "acme",
		"ALL_PROXY": c.Proxy(), "https_proxy": c.Proxy(), "NO_PROXY": "localhost,127.0.0.1,::1", "no_proxy": "localhost,127.0.0.1,::1",
	}
	for k, v := range want {
		if seen[k] != v {
			t.Errorf("%s = %q, want %q", k, seen[k], v)
		}
	}
	if _, ok := seen["FTP_PROXY"]; ok {
		t.Error("a proxy the shell had is kept, ahead of the compartment's ALL_PROXY")
	}
}

func TestTorrcGivesEachTorCompartmentAPort(t *testing.T) {
	d := Dirs{Root: t.TempDir()}
	all := map[string]Compartment{
		"a": {Name: "a", Route: Tor, TorPort: 9151},
		"b": {Name: "b", Route: Tor, TorPort: 9150},
		"c": {Name: "c", Route: Mullvad, Relay: "se-got-wg-001"},
	}
	changed, err := WriteTorrc(d, all)
	if err != nil || !changed {
		t.Fatalf("WriteTorrc: %v %v", changed, err)
	}
	b, _ := os.ReadFile(d.Torrc())
	text := string(b)
	for _, want := range []string{"SocksPort 127.0.0.1:9150\nSocksPort 127.0.0.1:9151\n", "ClientOnly 1\n", "PidFile " + d.TorPid() + "\n", "DataDirectory " + filepath.Join(d.Tor(), "data") + "\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("torrc lacks %q:\n%s", want, text)
		}
	}
	if changed, _ := WriteTorrc(d, all); changed {
		t.Error("the same compartments changed the torrc")
	}
	if ReloadTor(d) {
		t.Error("reloaded a Tor that doesn't run")
	}
}

func TestSearxSettingsAskTheEnginesThroughOneExit(t *testing.T) {
	s := SearxSettings(SearxOptions{Port: 8888, Secret: "abc", Proxy: "socks5h://se-got-wg-socks5-001.relays.mullvad.net:1080"})
	for _, want := range []string{
		"use_default_settings: true\n", "  bind_address: \"127.0.0.1\"\n", "  port: 8888\n", "  secret_key: \"abc\"\n",
		"  formats:\n    - html\n    - json\n", "  enable_metrics: false\n", "  limiter: false\n",
		"  proxies:\n    all://:\n      - \"socks5h://se-got-wg-socks5-001.relays.mullvad.net:1080\"\n",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("settings lack %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "using_tor_proxy") {
		t.Error("a Mullvad exit is not Tor")
	}
	if tor := SearxSettings(SearxOptions{Port: 8888, Secret: "abc", Proxy: "socks5h://127.0.0.1:9150", Tor: true}); !strings.Contains(tor, "  using_tor_proxy: true\n") {
		t.Errorf("tor settings:\n%s", tor)
	}
	d := Dirs{Root: "/data/anon"}
	uv := SearxSteps(d, "git", "uv", "")
	if len(uv) != 4 || uv[1][len(uv[1])-1] != SearxCommit || uv[2][0] != "uv" || uv[3][len(uv[3])-1] != "/data/anon/searxng/src/requirements.txt" {
		t.Errorf("steps with uv: %v", uv)
	}
	if py := SearxSteps(d, "git", "", "python3"); py[2][0] != "python3" || py[3][0] != d.SearxPython() {
		t.Errorf("steps with python3: %v", py)
	}
	if SearxInstalled(d) {
		t.Error("nothing is installed in /data/anon")
	}
}

func TestArgsRoundTrip(t *testing.T) {
	o := ServerOptions{Dirs: Dirs{Root: "/d/anon"}, Compartment: "acme", Search: "http://127.0.0.1:8888", Evidence: "/w/evidence"}
	args := Args(o)
	if args[0] != "anon" || args[1] != "mcp" {
		t.Fatalf("args = %v", args)
	}
	got, err := ParseArgs(args[2:], io.Discard)
	if err != nil || got != o {
		t.Fatalf("ParseArgs = %+v, %v", got, err)
	}
	if _, err := ParseArgs([]string{"--dir", "/d"}, io.Discard); err == nil {
		t.Error("no compartment should be an error")
	}
}

// socks5 is a SOCKS5 proxy for tests: no auth, CONNECT only. It sends every
// connection to to, and records the addresses asked for.
type socks5 struct {
	ln    net.Listener
	to    string
	mu    sync.Mutex
	asked []string
}

func newSocks5(t *testing.T, to string) *socks5 {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &socks5{ln: ln, to: to}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(c)
		}
	}()
	return s
}

func (s *socks5) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

func (s *socks5) serve(c net.Conn) {
	defer c.Close()
	buf := make([]byte, 262)
	if _, err := io.ReadFull(c, buf[:2]); err != nil {
		return
	}
	if _, err := io.ReadFull(c, buf[:buf[1]]); err != nil {
		return
	}
	c.Write([]byte{5, 0})
	if _, err := io.ReadFull(c, buf[:4]); err != nil {
		return
	}
	var host string
	switch buf[3] {
	case 1:
		io.ReadFull(c, buf[:4])
		host = net.IP(buf[:4]).String()
	case 3:
		io.ReadFull(c, buf[:1])
		n := int(buf[0])
		io.ReadFull(c, buf[:n])
		host = string(buf[:n])
	default:
		return
	}
	io.ReadFull(c, buf[:2])
	port := binary.BigEndian.Uint16(buf[:2])
	s.mu.Lock()
	s.asked = append(s.asked, net.JoinHostPort(host, strconv.Itoa(int(port))))
	s.mu.Unlock()
	up, err := net.Dial("tcp", s.to)
	if err != nil {
		c.Write([]byte{5, 1, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer up.Close()
	c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
	go io.Copy(up, c)
	io.Copy(c, up)
}

func (s *socks5) hosts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.asked)
}

// exits fakes Mullvad's check: one view for requests straight out, which
// leave from the Mac's tunnel, another for those through the proxy.
func exits(t *testing.T, mac, proxied string) *socks5 {
	t.Helper()
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, mac) }))
	via := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, proxied) }))
	t.Cleanup(direct.Close)
	t.Cleanup(via.Close)
	old := MullvadCheck
	MullvadCheck = direct.URL + "/json"
	t.Cleanup(func() { MullvadCheck = old })
	return newSocks5(t, strings.TrimPrefix(via.URL, "http://"))
}

func through(t *testing.T, s *socks5) {
	t.Helper()
	old := proxyOf
	proxyOf = func(Compartment) string { return fmt.Sprintf("socks5h://127.0.0.1:%d", s.port()) }
	t.Cleanup(func() { proxyOf = old })
}

func TestVerifyAMullvadExit(t *testing.T) {
	c := Compartment{Name: "acme", Route: Mullvad, Relay: "se-got-wg-001", Country: "Sweden"}
	mac := `{"mullvad_exit_ip": true, "mullvad_exit_ip_hostname": "us-sea-wg-406", "country": "USA", "ip": "203.0.113.9"}`
	for name, tc := range map[string]struct {
		proxied, want string
		ok            bool
	}{
		"its own":     {`{"mullvad_exit_ip": true, "mullvad_exit_ip_hostname": "se-got-wg-socks5-001", "country": "Sweden", "city": "Gothenburg", "ip": "198.51.100.7"}`, "Gothenburg, Sweden, not the Mac's", true},
		"not mullvad": {`{"mullvad_exit_ip": false, "country": "Sweden"}`, "not one of its own", false},
		"another":     {`{"mullvad_exit_ip": true, "mullvad_exit_ip_hostname": "ch-zrh-wg-socks5-001", "country": "Switzerland"}`, "another relay", false},
	} {
		t.Run(name, func(t *testing.T) {
			s := exits(t, mac, tc.proxied)
			through(t, s)
			words, err := Verify(context.Background(), c)
			if tc.ok != (err == nil) || (err == nil && !strings.Contains(words, tc.want)) || (err != nil && !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("Verify = %q, %v; want %q", words, err, tc.want)
			}
			if strings.Contains(words, "198.51.100.7") || (err != nil && strings.Contains(err.Error(), "198.51.100.7")) {
				t.Error("Verify named the exit's address")
			}
			if h := s.hosts(); len(h) != 1 || !strings.HasPrefix(h[0], "127.0.0.1:") {
				t.Errorf("the proxy was asked for %v", h)
			}
		})
	}
	// The Mac's own tunnel leaving from the compartment's relay fails it.
	s := exits(t, `{"mullvad_exit_ip": true, "mullvad_exit_ip_hostname": "se-got-wg-001"}`, `{"mullvad_exit_ip": true, "mullvad_exit_ip_hostname": "se-got-wg-socks5-001"}`)
	through(t, s)
	if _, err := Verify(context.Background(), c); err == nil || !strings.Contains(err.Error(), "the Mac's own") {
		t.Errorf("an exit shared with the Mac: %v", err)
	}
}

func TestVerifyATorExit(t *testing.T) {
	check := func(isTor bool) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, `{"IsTor": %t, "IP": "192.0.2.1"}`, isTor)
		}))
		defer srv.Close()
		old := TorCheck
		TorCheck = srv.URL
		defer func() { TorCheck = old }()
		s := newSocks5(t, strings.TrimPrefix(srv.URL, "http://"))
		c := Compartment{Name: "onions", Route: Tor, TorPort: s.port()}
		words, err := Verify(context.Background(), c)
		if isTor != (err == nil) || strings.Contains(words, "192.0.2.1") {
			t.Errorf("IsTor %t: %q, %v", isTor, words, err)
		}
		if len(s.hosts()) != 1 {
			t.Errorf("the Tor port was not used: %v", s.hosts())
		}
	}
	check(true)
	check(false)
}

func TestWaitPortsWaitsForTorToOpenThem(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	go func() {
		time.Sleep(300 * time.Millisecond)
		l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err == nil {
			defer l.Close()
			time.Sleep(2 * time.Second)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := WaitPorts(ctx, []int{port}); err != nil {
		t.Fatalf("WaitPorts: %v", err)
	}
	short, cancel2 := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel2()
	if err := WaitPorts(short, []int{1}); err == nil {
		t.Error("a port that never opens should time out")
	}
}
