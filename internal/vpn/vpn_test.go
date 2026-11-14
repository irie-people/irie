package vpn

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
)

// TestTheKindsAreTheConfigs: the config offers the kinds this package
// drives, in the same order, and a copy's brand names only those.
func TestTheKindsAreTheConfigs(t *testing.T) {
	if got := Names(); !slices.Equal(got, config.VPNKinds) {
		t.Errorf("kinds %v, config.VPNKinds %v", got, config.VPNKinds)
	}
	for _, name := range config.VPNOffered() {
		if _, ok := KindOf(name); !ok {
			t.Errorf("brand.VPNs names %q, which is no kind of VPN", name)
		}
	}
	for _, k := range Kinds() {
		if k.Title == "" || k.find == nil || k.open == nil {
			t.Errorf("kind %q lacks a title, find or open", k.Name)
		}
		if k.Needs && k.Server == "" {
			t.Errorf("kind %q needs vpn.server and doesn't say what it is", k.Name)
		}
	}
}

func TestWordsForAnyVPN(t *testing.T) {
	for s, want := range map[State]string{
		{State: Connected, Host: "vpn.example.com"}:      "VPN on: vpn.example.com",
		{State: Connected}:                               "VPN on: connected",
		{State: Connecting, Host: "Work VPN"}:            "VPN connecting: Work VPN",
		{State: Disconnected, Cause: "needs login"}:      "VPN off: needs login",
		{State: Error, Cause: "the gateway refused"}:     "VPN in error: the gateway refused",
		{State: Error, Cause: "is_offline", Leaks: true}: "VPN in error: is offline; the network is NOT blocked",
		{State: "weird"}:                                 "VPN state unknown",
	} {
		if got := s.Words(); got != want {
			t.Errorf("%+v: got %q, want %q", s, got, want)
		}
	}
}

// script is an executable shell script in the test's folder, which logs
// its arguments to the file it returns too.
func script(t *testing.T, name, body string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, name)
	log := filepath.Join(dir, "args")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho \"$*\" >> "+log+"\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, log
}

func logged(t *testing.T, log string) string {
	t.Helper()
	b, _ := os.ReadFile(log)
	return string(b)
}

func TestOpen(t *testing.T) {
	if _, err := Open(Settings{Provider: "nothing"}); err == nil || !strings.Contains(err.Error(), "mullvad") {
		t.Errorf("an unknown VPN: %v", err)
	}
	if _, err := Open(Settings{Provider: "system"}); err == nil || !strings.Contains(err.Error(), "vpn.server") {
		t.Errorf("the system VPN without its name: %v", err)
	}
	missing := filepath.Join(t.TempDir(), "none")
	if _, err := Open(Settings{Provider: "mullvad", CLI: missing}); err == nil || !strings.Contains(err.Error(), "vpn.mullvad") {
		t.Errorf("a configured path that isn't there: %v", err)
	}
	if _, err := Open(Settings{Provider: "command"}); err == nil || !strings.Contains(err.Error(), "vpn.status") {
		t.Errorf("the command VPN without its commands: %v", err)
	}
	bin, _ := script(t, "mullvad", "")
	v, err := Open(Settings{Provider: "mullvad", CLI: bin})
	if err != nil || v.Kind().Name != "mullvad" || !slices.Equal(Can(v), []string{CanPlace, CanLockdown, CanTime}) {
		t.Errorf("mullvad: %v, %v", v, err)
	}
}

// TestAMissingCommandSaysWhereItComesFrom: a VPN whose app isn't installed
// names where the app comes from.
func TestAMissingCommandSaysWhereItComesFrom(t *testing.T) {
	k := Kind{Name: "x", Title: "X VPN", Get: "https://example.com/x", find: func(Settings) (string, error) {
		return findCLI("", []string{"no-such-vpn-cli-here"}, []string{filepath.Join(t.TempDir(), "none")})
	}}
	if _, err := k.Find(Settings{}); err == nil || err.Error() != "X VPN isn't installed: its app, from https://example.com/x, brings the command Irie uses." {
		t.Errorf("err = %v", err)
	}
}

func TestTheCommandVPN(t *testing.T) {
	status, _ := script(t, "status", `if [ -f "$(dirname "$0")/up" ]; then echo 'State: Connected to gw-2'; else echo 'State: Disconnected'; exit 3; fi
`)
	up := filepath.Join(filepath.Dir(status), "up")
	connect, clog := script(t, "connect", "touch '"+up+"'\n")
	disconnect, _ := script(t, "disconnect", "rm -f '"+up+"'\n")
	s := Settings{Provider: "command", Server: "vpn.example.com", Status: []string{status, "--now"}, Connect: []string{connect, "--profile", "Work"}, Disconnect: []string{disconnect}, Connected: "(?i)connected to", Timeout: 10 * time.Second}
	v, err := Open(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(Can(v)) != 0 {
		t.Errorf("can %v", Can(v))
	}
	// Off, it exits 3: the output says the state, not the exit code.
	if st, err := v.Status(); err != nil || st != (State{State: Disconnected}) {
		t.Errorf("off: %+v, %v", st, err)
	}
	if err := v.Connect(); err != nil {
		t.Fatal(err)
	}
	if got := logged(t, clog); got != "--profile Work\n" {
		t.Errorf("connect ran with %q", got)
	}
	if st, err := v.Status(); err != nil || st != (State{State: Connected, Host: "vpn.example.com"}) {
		t.Errorf("on: %+v, %v", st, err)
	}
	if err := v.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if st, _ := v.Status(); st.State != Disconnected {
		t.Errorf("after disconnect: %+v", st)
	}
	// Connecting, when its pattern says so, wins over connected.
	s.Connecting = "Connecting|Connected to gw"
	v, _ = Open(s)
	if got := v.(Command).Parse("State: Connected to gw-2"); got.State != Connecting {
		t.Errorf("connecting = %+v", got)
	}
	// What it lacks says what to set.
	s.Connect, s.Connected = nil, "(?i)connected to"
	v, _ = Open(s)
	if err := v.Connect(); err == nil || !strings.Contains(err.Error(), "vpn.connect is empty") {
		t.Errorf("no connect: %v", err)
	}
	for _, bad := range []Settings{
		{Provider: "command", Status: []string{status}},
		{Provider: "command", Status: []string{status}, Connected: "("},
		{Provider: "command", Status: []string{"no-such-vpn-status-here"}, Connected: "x"},
	} {
		if _, err := Open(bad); err == nil {
			t.Errorf("%+v: opened", bad)
		}
	}
	// A failing command's error carries its last lines, not a whole banner.
	failing, _ := script(t, "connect", "echo 'Banner line 1'; echo 'Banner line 2'; echo 'one'; echo 'two'; echo 'error: the gateway refused'; exit 1\n")
	s.Connect = []string{failing}
	v, _ = Open(s)
	if err := v.Connect(); err == nil || strings.Contains(err.Error(), "Banner") || !strings.Contains(err.Error(), "the gateway refused") {
		t.Errorf("err = %v", err)
	}
}

func TestParseScutil(t *testing.T) {
	// As configd's scutil prints them: the state, then an extended status
	// that carries the tunnel's addresses, which are never kept.
	extended := "\nExtended Status <dictionary> {\n  IPv4 : <dictionary> {\n    Addresses : <array> {\n      0 : 192.0.2.44\n    }\n  }\n  Status : 2\n}\n"
	for out, want := range map[string]State{
		"Connected" + extended:       {State: Connected},
		"Connecting\n":               {State: Connecting},
		"Disconnected" + extended:    {State: Disconnected},
		"Disconnecting\n":            {State: Disconnecting},
		"Invalid: No such service\n": {State: Error, Cause: "No such service"},
		"Invalid\n":                  {State: Error, Cause: "the VPN is turned off in System Settings"},
		"\nConnected\n":              {State: Connected},
	} {
		if got, ok := ParseScutil(out); !ok || got != want {
			t.Errorf("%q: got %+v, %v; want %+v", out, got, ok, want)
		}
	}
	if s, ok := ParseScutil("No service\n"); ok {
		t.Errorf("read a state: %+v", s)
	}
}

func TestTheMacsOwnVPN(t *testing.T) {
	bin, log := script(t, "scutil", `case "$1 $2 $3" in
"--nc status Work VPN") printf 'Connected\nExtended Status <dictionary> {\n  IPv4 : <dictionary> {\n    Addresses : <array> {\n      0 : 192.0.2.44\n    }\n  }\n}\n';;
"--nc status Nothing") echo 'No service' >&2; exit 1;;
esac
`)
	m := MacVPN{run: newRunner(bin, "scutil", 10*time.Second), name: "Work VPN"}
	st, err := m.Status()
	if err != nil || st != (State{State: Connected, Host: "Work VPN"}) {
		t.Errorf("status %+v, %v", st, err)
	}
	if strings.Contains(st.Words(), "192.0.2") {
		t.Errorf("kept an address: %q", st.Words())
	}
	if err := m.Connect(); err != nil {
		t.Error(err)
	}
	if err := m.Disconnect(); err != nil {
		t.Error(err)
	}
	if got := logged(t, log); !strings.Contains(got, "--nc start Work VPN\n") || !strings.Contains(got, "--nc stop Work VPN\n") {
		t.Errorf("ran:\n%s", got)
	}
	if _, err := (MacVPN{run: m.run, name: "Nothing"}).Status(); err == nil || !strings.Contains(err.Error(), `no VPN called "Nothing"`) {
		t.Errorf("a VPN that isn't there: %v", err)
	}
}

func TestParseScutilList(t *testing.T) {
	out := `Available network connection services in the current set (*=enabled):
* (Disconnected)   0A1B2C3D-0000-4000-8000-000000000001 VPN (com.wireguard.macos) "home"                           [VPN:com.wireguard.macos]
* (Connected)      0A1B2C3D-0000-4000-8000-000000000002 IPSec              "Work VPN"                       [IPSec]
  (Invalid)        0A1B2C3D-0000-4000-8000-000000000003 L2TP               "Old office"                     [PPP:L2TP]
`
	if got := ParseScutilList(out); !slices.Equal(got, []string{"home", "Work VPN", "Old office"}) {
		t.Errorf("got %q", got)
	}
}

func TestParseNmcli(t *testing.T) {
	out := "Wired connection 1:802-3-ethernet:activated\nWork\\: Corp VPN:vpn:activating\nhome:wireguard:activated\nback\\\\slash:vpn:deactivating\n"
	for name, want := range map[string]string{"Work: Corp VPN": Connecting, "home": Connected, `back\slash`: Disconnecting, "Elsewhere": Disconnected} {
		if got := ParseNmcli(out, name); got.State != want {
			t.Errorf("%q: got %+v, want %s", name, got, want)
		}
	}
	list := "Wired connection 1:802-3-ethernet\nWork\\: Corp VPN:vpn\nhome:wireguard\nlo:loopback\n"
	if got := ParseNmcliList(list); !slices.Equal(got, []string{"Work: Corp VPN", "home"}) {
		t.Errorf("list %q", got)
	}
}

func TestNetworkManager(t *testing.T) {
	bin, log := script(t, "nmcli", `case "$*" in
"-t -f NAME,TYPE,STATE connection show --active") echo 'Work VPN:vpn:activated';;
esac
`)
	n := NetworkManager{run: newRunner(bin, "nmcli", 10*time.Second), name: "Work VPN"}
	if st, err := n.Status(); err != nil || st != (State{State: Connected, Host: "Work VPN"}) {
		t.Errorf("status %+v, %v", st, err)
	}
	if err := n.Connect(); err != nil {
		t.Error(err)
	}
	if err := n.Disconnect(); err != nil {
		t.Error(err)
	}
	if got := logged(t, log); !strings.Contains(got, "--wait 15 connection up id Work VPN\n") || !strings.Contains(got, "connection down id Work VPN\n") {
		t.Errorf("ran:\n%s", got)
	}
}

// tailscaleJSON is `tailscale status --json` in part, with documentation
// and CGNAT addresses, a tailnet named after a company and one exit node.
const tailscaleJSON = `{
  "Version": "1.90.0",
  "BackendState": "Running",
  "TailscaleIPs": ["100.64.0.7", "fd7a:115c:a1e0::7"],
  "Self": {"ID": "n1", "HostName": "alices-mac", "DNSName": "alices-mac.example.ts.net.", "TailscaleIPs": ["100.64.0.7"], "Online": true},
  "CurrentTailnet": {"Name": "example.com", "MagicDNSSuffix": "example.ts.net", "MagicDNSEnabled": true},
  "ExitNodeStatus": {"ID": "n9", "Online": true, "TailscaleIPs": ["100.64.0.9/32"]},
  "Peer": {
    "nodekey:aa": {"ID": "n2", "HostName": "build-box", "DNSName": "build-box.example.ts.net.", "TailscaleIPs": ["100.64.0.2"], "ExitNode": false},
    "nodekey:bb": {"ID": "n9", "HostName": "office-gw", "DNSName": "office-gw.example.ts.net.", "TailscaleIPs": ["100.64.0.9"], "ExitNode": true, "ExitNodeOption": true}
  }
}`

func TestParseTailscale(t *testing.T) {
	s, ok := ParseTailscale([]byte(tailscaleJSON))
	if !ok || s != (State{State: Connected, Host: "exit node office-gw"}) {
		t.Errorf("with an exit node: %+v, %v", s, ok)
	}
	noExit := strings.Replace(tailscaleJSON, `"ExitNodeStatus": {"ID": "n9", "Online": true, "TailscaleIPs": ["100.64.0.9/32"]},`, "", 1)
	noExit = strings.Replace(noExit, `"ExitNode": true`, `"ExitNode": false`, 1)
	if s, _ := ParseTailscale([]byte(noExit)); s != (State{State: Connected, Host: "example.com"}) {
		t.Errorf("without one: %+v", s)
	}
	// A tailnet named after a person's address isn't shown.
	personal := strings.Replace(noExit, `"Name": "example.com"`, `"Name": "alice@example.com"`, 1)
	if s, _ := ParseTailscale([]byte(personal)); s != (State{State: Connected}) {
		t.Errorf("a personal tailnet: %+v", s)
	}
	for backend, want := range map[string]State{
		"Starting":         {State: Connecting, Host: "exit node office-gw"},
		"Stopped":          {State: Disconnected},
		"NeedsLogin":       {State: Disconnected, Cause: "Tailscale needs you to sign in"},
		"NeedsMachineAuth": {State: Disconnected, Cause: "this device waits for the tailnet's admin to approve it"},
		"InUseOtherUser":   {State: Error, Cause: "another user of this computer has Tailscale"},
	} {
		got, ok := ParseTailscale([]byte(strings.Replace(tailscaleJSON, `"Running"`, `"`+backend+`"`, 1)))
		if !ok || got != want {
			t.Errorf("%s: %+v, %v; want %+v", backend, got, ok, want)
		}
	}
	for _, bad := range []string{"", "failed to connect to local tailscaled; it doesn't appear to be running", `{"Version": "1.90.0"}`} {
		if s, ok := ParseTailscale([]byte(bad)); ok {
			t.Errorf("%q: read %+v", bad, s)
		}
	}
	// Nothing of an address survives.
	if s, _ := ParseTailscale([]byte(tailscaleJSON)); strings.Contains(s.Words(), "100.64") || strings.Contains(s.Words(), "alices-mac") {
		t.Errorf("kept the machine's own: %q", s.Words())
	}
}

func TestTailscale(t *testing.T) {
	bin, log := script(t, "tailscale", `case "$1" in
status) cat "$(dirname "$0")/state" 2>/dev/null || { echo 'failed to connect to local tailscaled; it doesn'"'"'t appear to be running (sudo systemctl start tailscaled ?)' >&2; exit 1; };;
esac
`)
	ts := Tailscale{run: newRunner(bin, "tailscale", 10*time.Second)}
	if _, err := ts.Status(); err == nil || !strings.Contains(err.Error(), "doesn't appear to be running") {
		t.Errorf("no tailscaled: %v", err)
	}
	state := filepath.Join(filepath.Dir(bin), "state")
	if err := os.WriteFile(state, []byte(tailscaleJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if s, err := ts.Status(); err != nil || s.State != Connected {
		t.Errorf("status %+v, %v", s, err)
	}
	if !slices.Equal(Can(ts), []string{CanPlace}) {
		t.Errorf("can %v", Can(ts))
	}
	for _, err := range []error{ts.Connect(), ts.Disconnect(), ts.SetPlace([]string{"office-gw"}, true), ts.SetPlace([]string{"none"}, true)} {
		if err != nil {
			t.Error(err)
		}
	}
	if got := logged(t, log); !strings.Contains(got, "\nup\n") || !strings.Contains(got, "\ndown\n") || !strings.Contains(got, "set --exit-node=office-gw\n") || !strings.Contains(got, "set --exit-node=\n") {
		t.Errorf("ran:\n%s", got)
	}
	for _, bad := range [][]string{nil, {"a", "b"}, {"--help"}, {"Office GW"}} {
		if err := ts.CheckPlace(bad); err == nil {
			t.Errorf("%q: took it", bad)
		}
	}
	// Signed out, connecting asks for the sign-in rather than wait on it.
	if err := os.WriteFile(state, []byte(strings.Replace(tailscaleJSON, `"Running"`, `"NeedsLogin"`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ts.Connect(); err == nil || !strings.Contains(err.Error(), "sign in first") {
		t.Errorf("signed out: %v", err)
	}
}

// nordStatus is `nordvpn status` connected, as NordVPN 6.0's own test
// prints it, with a documentation address and a real-looking hostname.
const nordStatus = `Status: Connected
Server: Sweden #123
Hostname: se123.nordvpn.com
IP: 192.0.2.80
Country: Sweden
City: Stockholm
Protocol: NordLynx
Post-quantum VPN: Disabled
Transfer: 69 B received, 69 B sent
Uptime: 13 seconds
`

func TestParseNordVPN(t *testing.T) {
	for out, want := range map[string]State{
		nordStatus:               {State: Connected, City: "Stockholm", Country: "Sweden", Host: "se123"},
		"Status: Connecting\n":   {State: Connecting},
		"Status: Disconnected\n": {State: Disconnected},
		"Status: Paused\nPause time left: 4m 30s\n": {State: Disconnected, Cause: "paused, 4m 30s left"},
		// NordVPN 5's lines, with the technology it no longer prints.
		"\r-\r  \r\r-\r  \rStatus: Connected\nHostname: us4321.nordvpn.com\nCountry: United States\nCity: New York\nCurrent technology: NORDLYNX\n": {State: Connected, City: "New York", Country: "United States", Host: "us4321"},
	} {
		got, ok := ParseNordVPN(out)
		if !ok || got != want {
			t.Errorf("%q: %+v, %v; want %+v", out, got, ok, want)
		}
	}
	if s, ok := ParseNordVPN("You're not logged in.\n"); ok {
		t.Errorf("read %+v", s)
	}
	if s, _ := ParseNordVPN(nordStatus); strings.Contains(s.Words(), "192.0.2") {
		t.Errorf("kept the address: %q", s.Words())
	}
}

func TestNordVPN(t *testing.T) {
	bin, log := script(t, "nordvpn", `case "$1" in
status) printf '`+nordStatus+`';;
esac
`)
	n := NordVPN{run: newRunner(bin, "nordvpn", 10*time.Second)}
	if s, err := n.Status(); err != nil || s.Host != "se123" {
		t.Errorf("status %+v, %v", s, err)
	}
	if !slices.Equal(Can(n), []string{CanPlace}) {
		t.Errorf("can %v", Can(n))
	}
	for _, err := range []error{n.Connect(), n.Disconnect(), n.SetPlace([]string{"united_states", "new_york"}, false)} {
		if err != nil {
			t.Error(err)
		}
	}
	if got := logged(t, log); !strings.Contains(got, "\nconnect\n") || !strings.Contains(got, "\ndisconnect\n") || !strings.Contains(got, "connect united_states new_york\n") {
		t.Errorf("ran:\n%s", got)
	}
	for _, ok := range [][]string{{"sweden"}, {"se"}, {"jp35"}, {"united_states", "new_york"}} {
		if err := n.CheckPlace(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range [][]string{nil, {"a", "b", "c"}, {"--group"}, {"p2p;rm"}} {
		if err := n.CheckPlace(bad); err == nil {
			t.Errorf("%q: took it", bad)
		}
	}
}

// netbirdJSON is `netbird status --json` in part, with documentation and
// CGNAT addresses.
const netbirdJSON = `{"peers":{"total":2,"connected":1,"details":[{"fqdn":"build-box.netbird.cloud","netbirdIp":"100.92.0.2","status":"Connected","iceCandidateEndpoint":{"local":"192.0.2.33:51820","remote":"198.51.100.9:51820"}}]},
"cliVersion":"0.83.1","daemonVersion":"0.83.1","daemonStatus":"Connected",
"management":{"url":"https://api.netbird.io:443","connected":true,"error":""},
"signal":{"url":"https://signal.netbird.io:443","connected":true,"error":""},
"netbirdIp":"100.92.0.7/16","publicKey":"AAAA","usesKernelInterface":false,"fqdn":"alices-mac.netbird.cloud"}`

func TestParseNetBird(t *testing.T) {
	for daemon, want := range map[string]State{
		"Connected":      {State: Connected},
		"Connecting":     {State: Connecting},
		"Idle":           {State: Disconnected},
		"NeedsLogin":     {State: Disconnected, Cause: "NetBird needs you to sign in"},
		"SessionExpired": {State: Disconnected, Cause: "NetBird's sign-in has expired"},
		"LoginFailed":    {State: Error, Cause: "NetBird's sign-in failed"},
		"Rebooting":      {State: Error, Cause: "NetBird says Rebooting"},
	} {
		got, ok := ParseNetBird([]byte(strings.Replace(netbirdJSON, `"daemonStatus":"Connected"`, `"daemonStatus":"`+daemon+`"`, 1)))
		if !ok || got != want {
			t.Errorf("%s: %+v, %v; want %+v", daemon, got, ok, want)
		}
	}
	// A NetBird before 0.67 has no daemonStatus: its management link says.
	old := strings.Replace(netbirdJSON, `"daemonStatus":"Connected",`, "", 1)
	if got, ok := ParseNetBird([]byte(old)); !ok || got.State != Connected {
		t.Errorf("old, linked: %+v, %v", got, ok)
	}
	if got, _ := ParseNetBird([]byte(strings.Replace(old, `"connected":true,"error":""},
"signal"`, `"connected":false,"error":"x"},
"signal"`, 1))); got.State != Disconnected {
		t.Errorf("old, unlinked: %+v", got)
	}
	for _, bad := range []string{"", "Daemon status: NeedsLogin", `{"cliVersion":"0.83.1"}`} {
		if s, ok := ParseNetBird([]byte(bad)); ok {
			t.Errorf("%q: read %+v", bad, s)
		}
	}
	if s, _ := ParseNetBird([]byte(netbirdJSON)); strings.ContainsAny(s.Words(), "0123456789") {
		t.Errorf("kept an address: %q", s.Words())
	}
}

func TestNetBird(t *testing.T) {
	bin, log := script(t, "netbird", `case "$1" in
status) cat "$(dirname "$0")/state";;
esac
`)
	state := filepath.Join(filepath.Dir(bin), "state")
	write := func(daemon string) {
		t.Helper()
		if err := os.WriteFile(state, []byte(strings.Replace(netbirdJSON, `"daemonStatus":"Connected"`, `"daemonStatus":"`+daemon+`"`, 1)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("Idle")
	n := NetBird{run: newRunner(bin, "netbird", 10*time.Second)}
	if s, err := n.Status(); err != nil || s.State != Disconnected {
		t.Errorf("status %+v, %v", s, err)
	}
	if err := n.Connect(); err != nil {
		t.Error(err)
	}
	if err := n.Disconnect(); err != nil {
		t.Error(err)
	}
	if got := logged(t, log); !strings.Contains(got, "\nup\n") || !strings.Contains(got, "\ndown\n") {
		t.Errorf("ran:\n%s", got)
	}
	write("NeedsLogin")
	if err := n.Connect(); err == nil || !strings.Contains(err.Error(), "netbird up") {
		t.Errorf("signed out: %v", err)
	}
}

func TestParseWarp(t *testing.T) {
	for out, want := range map[string]State{
		"Status update: Connected\nNetwork: healthy\n":                                        {State: Connected},
		"Status update: Connecting\n":                                                         {State: Connecting},
		"Status update: Disconnected\nReason: Manual Disconnection\n":                         {State: Disconnected},
		"Status update: Disconnected\nReason: Settings Changed\n":                             {State: Disconnected, Cause: "Settings Changed"},
		"Status update: Unable\nReason: Connectivity check failed due to DNS Lookup Failed\n": {State: Error, Cause: "Connectivity check failed due to DNS Lookup Failed"},
		"Status update: Unable to connect. Reason: Registration Missing\n":                    {State: Error, Cause: "Registration Missing"},
		"Status update: Paused\nReason: Paused By User\n":                                     {State: Error, Cause: "Paused: Paused By User"},
	} {
		if got, ok := ParseWarp(out); !ok || got != want {
			t.Errorf("%q: %+v, %v; want %+v", out, got, ok, want)
		}
	}
	if s, ok := ParseWarp("Error: Failed to connect to the daemon\n"); ok {
		t.Errorf("read %+v", s)
	}
}

func TestWarp(t *testing.T) {
	bin, log := script(t, "warp-cli", `case "$1" in
status) printf 'Status update: Connected\nNetwork: healthy\n';;
disconnect) echo 'Error: the device switch is locked by your organization'; exit 1;;
esac
`)
	w := Warp{run: newRunner(bin, "warp-cli", 10*time.Second)}
	if s, err := w.Status(); err != nil || s.State != Connected || len(Can(w)) != 0 {
		t.Errorf("status %+v, %v, can %v", s, err, Can(w))
	}
	if err := w.Connect(); err != nil {
		t.Error(err)
	}
	if err := w.Disconnect(); err == nil || !strings.Contains(err.Error(), "locked by your organization") {
		t.Errorf("a locked switch: %v", err)
	}
	// Irie never accepts Cloudflare's terms for anyone.
	if got := logged(t, log); strings.Contains(got, "accept-tos") || !strings.Contains(got, "connect\n") {
		t.Errorf("ran:\n%s", got)
	}
	tos, _ := script(t, "warp-cli", "echo 'Accept Terms of Service and Privacy Policy? [y/N]'; exit 1\n")
	if _, err := (Warp{run: newRunner(tos, "warp-cli", 10*time.Second)}).Status(); err == nil || !strings.Contains(err.Error(), "accept its terms first") {
		t.Errorf("terms not accepted: %v", err)
	}
}

// ciscoState is `vpn state` in Cisco's documented form: a banner, the
// state again and again, notices, and the CLI's prompt.
func ciscoState(states ...string) string {
	out := "Cisco Secure Client (version 5.1.0.0) .\n\nCopyright (c) 2004 - 2024 Cisco Systems, Inc.  All Rights Reserved.\n\n\n"
	for _, s := range states {
		out += "  >> state: " + s + "\n"
	}
	return out + "  >> notice: Ready to connect.\n  >> registered with local VPN subsystem.\nVPN>\n"
}

func TestParseCisco(t *testing.T) {
	for out, want := range map[string]State{
		ciscoState("Disconnected", "Disconnected"):     {State: Disconnected},
		ciscoState("Unknown", "Connected"):             {State: Connected},
		ciscoState("Connecting"):                       {State: Connecting},
		ciscoState("Disconnecting"):                    {State: Disconnecting},
		ciscoState("Unknown"):                          {State: Error, Cause: "Cisco Secure Client doesn't know the tunnel's state yet"},
		"  >> State: Connected\n":                      {State: Connected},
		"  >> state: Connected\n  >> state: Unknown\n": {State: Connected},
	} {
		if got, ok := ParseCisco(out); !ok || got != want {
			t.Errorf("%q: %+v, %v; want %+v", out, got, ok, want)
		}
	}
	if s, ok := ParseCisco("Cisco Secure Client (version 5.1.0.0) .\nVPN>\n"); ok {
		t.Errorf("read %+v", s)
	}
}

func TestCisco(t *testing.T) {
	bin, log := script(t, "vpn", `case "$1" in
state) printf '`+ciscoState("Unknown", "Connected")+`';;
connect) printf '  >> contacting host (vpn.example.com) for login information...\n  >> error: Connect capability is unavailable. Another Cisco Secure Client application acquired it.\nVPN>\n';;
esac
`)
	c := Cisco{run: newRunner(bin, "vpn", 10*time.Second), host: "vpn.example.com"}
	if s, err := c.Status(); err != nil || s != (State{State: Connected, Host: "vpn.example.com"}) {
		t.Errorf("status %+v, %v", s, err)
	}
	// The app holds the connection: the person connects in it.
	if err := c.Connect(); err == nil || !strings.Contains(err.Error(), "connects here in its own app") || !strings.Contains(err.Error(), "acquired it") {
		t.Errorf("connect: %v", err)
	}
	if err := c.Disconnect(); err != nil {
		t.Error(err)
	}
	// It connects to the host it was given, and never reads statistics.
	if got := logged(t, log); !strings.Contains(got, "connect vpn.example.com\n") || strings.Contains(got, "stats") {
		t.Errorf("ran:\n%s", got)
	}
	ok, _ := script(t, "vpn", "printf '  >> state: Connecting\\n  >> state: Connected\\n  >> notice: Connected to vpn.example.com.\\nVPN>\\n'\n")
	c = Cisco{run: newRunner(ok, "vpn", 10*time.Second), host: "vpn.example.com"}
	if err := c.Connect(); err != nil {
		t.Errorf("a connect that works: %v", err)
	}
}

func TestParseGlobalProtect(t *testing.T) {
	for out, want := range map[string]State{
		"GlobalProtect status: Connected\n":        {State: Connected},
		"GlobalProtect status: Disconnected\n":     {State: Disconnected},
		"GlobalProtect status: Connecting...\n":    {State: Connecting},
		"Current GlobalProtect status: disabled\n": {State: Disconnected, Cause: "GlobalProtect is disabled"},
		"GlobalProtect status: OnDemand\n":         {State: Error, Cause: "GlobalProtect says ondemand"},
	} {
		if got, ok := ParseGlobalProtect(out); !ok || got != want {
			t.Errorf("%q: %+v, %v; want %+v", out, got, ok, want)
		}
	}
	// show --details' addresses are no state.
	if s, ok := ParseGlobalProtect("Assigned IP address: 192.0.2.132\nGateway IP address: 192.0.2.180\n"); ok {
		t.Errorf("read %+v", s)
	}
}

func TestGlobalProtect(t *testing.T) {
	bin, log := script(t, "globalprotect", `case "$1 $2" in
"show --status") echo 'GlobalProtect status: Connected';;
esac
`)
	g := GlobalProtect{run: newRunner(bin, "globalprotect", 10*time.Second), portal: "vpn.example.com"}
	if s, err := g.Status(); err != nil || s != (State{State: Connected, Host: "vpn.example.com"}) {
		t.Errorf("status %+v, %v", s, err)
	}
	if err := g.Connect(); err != nil {
		t.Error(err)
	}
	if err := g.Disconnect(); err != nil {
		t.Error(err)
	}
	if got := logged(t, log); !strings.Contains(got, "connect --portal vpn.example.com\n") || !strings.Contains(got, "\ndisconnect\n") || strings.Contains(got, "details") || strings.Contains(got, "host-state") {
		t.Errorf("ran:\n%s", got)
	}
}

// fortiConnected is `forticlient vpn status` connected, in FortiClient
// 7.4's form, with a documentation address.
const fortiConnected = "Status: Connected\n  VPN name: corp-vpn\n  Username: alice\n  IP: 192.0.2.77\n  Sent bytes: 2914\n  Recv bytes: 8562\n  Duration: 00:01:52\n"

func TestParseFortiClient(t *testing.T) {
	for out, want := range map[string]State{
		fortiConnected:                  {State: Connected, Host: "corp-vpn"},
		"Status: Connecting...\n":       {State: Connecting},
		"Status: Disconnected\nDONE.\n": {State: Disconnected},
		"Status: Not Running\n":         {State: Disconnected},
		"Status: XAuth\n":               {State: Error, Cause: "FortiClient says xauth"},
	} {
		if got, ok := ParseFortiClient(out); !ok || got != want {
			t.Errorf("%q: %+v, %v; want %+v", out, got, ok, want)
		}
	}
	if s, _ := ParseFortiClient(fortiConnected); strings.Contains(s.Words(), "alice") || strings.Contains(s.Words(), "192.0.2") {
		t.Errorf("kept the user or the address: %q", s.Words())
	}
	if s, ok := ParseFortiClient("Please run this command under non-root user.\n"); ok {
		t.Errorf("read %+v", s)
	}
}

func TestFortiClient(t *testing.T) {
	for _, name := range []string{"forticlient", "fortivpn"} {
		bin, log := script(t, name, `printf 'Status: Not Running\n'
`)
		v, err := KindOf("forticlient")
		if !err {
			t.Fatal("no forticlient kind")
		}
		f, _ := v.open(bin, Settings{Server: "corp-vpn", Timeout: 10 * time.Second})
		if s, err := f.Status(); err != nil || s.State != Disconnected {
			t.Errorf("%s status %+v, %v", name, s, err)
		}
		if err := f.Connect(); err != nil {
			t.Error(err)
		}
		if err := f.Disconnect(); err != nil {
			t.Error(err)
		}
		want := "vpn status\nvpn connect corp-vpn\nvpn disconnect corp-vpn\n"
		if name == "fortivpn" {
			want = "status\nconnect corp-vpn\ndisconnect corp-vpn\n"
		}
		if got := logged(t, log); got != want {
			t.Errorf("%s ran %q, want %q", name, got, want)
		}
	}
}
