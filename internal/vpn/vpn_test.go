package vpn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The daemon's TunnelState as mullvad-cli 2026.3 prints it with --json, with
// documentation addresses (RFC 5737, RFC 3849).
const (
	connectedJSON    = `{"state":"connected","details":{"endpoint":{"endpoint":{"address":"192.0.2.10:51820","protocol":"udp"},"tunnel_type":"wireguard","quantum_resistant":true,"proxy":null,"obfuscation":null,"entry_endpoint":null,"tunnel_interface":"utun4","daita":false},"location":{"latitude":57.70887,"longitude":11.97456,"ipv4":"192.0.2.10","ipv6":"2001:db8::10","mullvad_exit_ip":true,"city":"Gothenburg","country":"Sweden","location":{"hostname":["se","got","se-got-wg-001"]},"entry_city":null,"entry_country":null,"entry_location":null},"feature_indicators":["LockdownMode","QuantumResistance"]}}`
	connectingJSON   = `{"state":"connecting","details":{"endpoint":{},"location":{"latitude":59.3,"longitude":18.0,"ipv4":null,"ipv6":null,"mullvad_exit_ip":false,"city":"Stockholm","country":"Sweden","location":{"city":["se","sto"]}},"feature_indicators":[]}}`
	disconnectedJSON = `{"state":"disconnected","details":{"location":{"latitude":40.7,"longitude":-74.0,"ipv4":"198.51.100.7","ipv6":"2001:db8::7","mullvad_exit_ip":false,"city":"Somewhere","country":"Elsewhere","location":null},"locked_down":true}}`
	errorJSON        = `{"state":"error","details":{"cause":{"reason":"is_offline"},"block_failure":null}}`
	leakyErrorJSON   = `{"state":"error","details":{"cause":{"reason":"set_firewall_policy_error","details":{"reason":"generic"}},"block_failure":{"reason":"generic"}}}`
)

func TestParse(t *testing.T) {
	for line, want := range map[string]State{
		connectedJSON:    {State: Connected, City: "Gothenburg", Country: "Sweden", Host: "se-got-wg-001"},
		connectingJSON:   {State: Connecting, City: "Stockholm", Country: "Sweden"},
		disconnectedJSON: {State: Disconnected, Blocked: true},
		errorJSON:        {State: Error, Cause: "is_offline", Blocking: true},
		leakyErrorJSON:   {State: Error, Cause: "set_firewall_policy_error"},
		`{"state":"disconnecting","details":"nothing"}`: {State: Disconnecting},
	} {
		got, ok := Parse([]byte(line))
		if !ok || got != want {
			t.Errorf("%.40s…: got %+v, %v; want %+v", line, got, ok, want)
		}
	}
	for _, line := range []string{"", "Warning: You are not logged in to an account.", `{"relays":[]}`, `{"state":"weird"}`, `{"auto_connect":true}`} {
		if s, ok := Parse([]byte(line)); ok {
			t.Errorf("%q: read as a state, %+v", line, s)
		}
	}
}

// TestNoAddressIsKept: nothing of the machine's own address or place, nor
// the relay's address, survives the parse.
func TestNoAddressIsKept(t *testing.T) {
	for _, line := range []string{connectedJSON, disconnectedJSON} {
		s, _ := Parse([]byte(line))
		b, _ := json.Marshal(s)
		for _, leak := range []string{"192.0.2", "198.51.100", "2001:db8", "57.7", "40.7", "Somewhere", "Elsewhere"} {
			if strings.Contains(string(b)+s.Words(), leak) {
				t.Errorf("%s kept %q", b, leak)
			}
		}
	}
}

func TestWords(t *testing.T) {
	for line, want := range map[string]string{
		connectedJSON:    "VPN on: Gothenburg, Sweden (se-got-wg-001)",
		connectingJSON:   "VPN connecting: Stockholm, Sweden",
		disconnectedJSON: "VPN off, and lockdown mode blocks the network",
		errorJSON:        "VPN in error: is offline; the network is blocked meanwhile",
		leakyErrorJSON:   "VPN in error: set firewall policy error; the network is NOT blocked",
	} {
		s, _ := Parse([]byte(line))
		if got := s.Words(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestParseExpiry(t *testing.T) {
	out := "Mullvad account:    1234123412341234\nExpires at:         2026-12-31 13:45:10.123456 +01:00\nDevice name:        Happy Otter\n"
	got, ok := ParseExpiry(out)
	if !ok || !got.Equal(time.Date(2026, 12, 31, 12, 45, 10, 123456000, time.UTC)) {
		t.Errorf("got %v, %v", got, ok)
	}
	if got, ok := ParseExpiry("Expires at: 2027-01-02 03:04:05 -05:00"); !ok || got.UTC().Hour() != 8 {
		t.Errorf("without fractions: %v, %v", got, ok)
	}
	for _, bad := range []string{"Not logged in on any account", "Mullvad account: 1234123412341234", "Expires at: soon"} {
		if _, ok := ParseExpiry(bad); ok {
			t.Errorf("%q: read an expiry", bad)
		}
	}
}

func TestParseLockdown(t *testing.T) {
	for out, want := range map[string]bool{"Block traffic when the VPN is disconnected: on\n": true, "Block traffic when the VPN is disconnected: off": false} {
		if got, ok := ParseLockdown(out); !ok || got != want {
			t.Errorf("%q: %v, %v", out, got, ok)
		}
	}
	if _, ok := ParseLockdown("garbage"); ok {
		t.Error("read garbage")
	}
}

func TestCheckLocation(t *testing.T) {
	for _, ok := range [][]string{{"se"}, {"any"}, {"se", "got"}, {"se", "got", "se-got-wg-001"}, {"se-got-wg-001"}} {
		if err := CheckLocation(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range [][]string{nil, {"Sweden"}, {"se", "gothenburg"}, {"se", "got", "x", "y"}, {"--help"}, {"se;rm"}} {
		if err := CheckLocation(bad); err == nil {
			t.Errorf("%q: took it", bad)
		}
	}
}

// fakeMullvad is a mullvad that answers from a script, as the real one does.
func fakeMullvad(t *testing.T, body string) CLI {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "mullvad")
	log := filepath.Join(dir, "args")
	script := "#!/bin/sh\necho \"$*\" >> " + log + "\n" + body
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return CLI{Bin: bin, Timeout: 10 * time.Second}
}

func TestCLI(t *testing.T) {
	c := fakeMullvad(t, `case "$1 $2" in
"status --json") echo 'Warning: You are not logged in to an account.'; echo '`+connectedJSON+`';;
"account get") printf 'Mullvad account:    1234123412341234\nExpires at:         2026-12-31 13:45:10 +01:00\n';;
"lockdown-mode get") echo 'Block traffic when the VPN is disconnected: on';;
"relay set") echo 'Relay constraints updated';;
"connect --wait"|"disconnect --wait"|"reconnect --wait"|"lockdown-mode set") ;;
*) echo "error: unrecognized subcommand '$1'" >&2; exit 2;;
esac
`)
	s, err := c.Status()
	if err != nil || s.Host != "se-got-wg-001" {
		t.Errorf("status %+v, %v", s, err)
	}
	if e, err := c.Expiry(); err != nil || e.UTC().Hour() != 12 {
		t.Errorf("expiry %v, %v", e, err)
	}
	if on, err := c.Lockdown(); err != nil || !on {
		t.Errorf("lockdown %v, %v", on, err)
	}
	for _, f := range []func() error{c.Connect, c.Disconnect, c.Reconnect, func() error { return c.SetLockdown(true) }, func() error { return c.SetLocation([]string{"se", "got"}) }} {
		if err := f(); err != nil {
			t.Error(err)
		}
	}
	if err := c.SetLocation([]string{"--help"}); err == nil {
		t.Error("ran a flag as a place")
	}
	b, _ := os.ReadFile(filepath.Join(filepath.Dir(c.Bin), "args"))
	if !strings.Contains(string(b), "relay set location se got\n") || !strings.Contains(string(b), "lockdown-mode set on\n") {
		t.Errorf("ran:\n%s", b)
	}
	bad := fakeMullvad(t, "echo 'Error: the daemon is not running' >&2; exit 1\n")
	if _, err := bad.Status(); err == nil || !strings.Contains(err.Error(), "daemon is not running") {
		t.Errorf("err = %v", err)
	}
	// The account's number is in no error, whatever happens.
	acct := fakeMullvad(t, "echo 'Mullvad account: 1234123412341234'; exit 1\n")
	if _, err := acct.Expiry(); err == nil || strings.Contains(err.Error(), "1234") {
		t.Errorf("err = %v", err)
	}
}

func TestListen(t *testing.T) {
	relays := `{"countries":[` + strings.Repeat(`{"name":"X","code":"xx","cities":[]},`, 40000) + `{"name":"Y"}]}`
	c := fakeMullvad(t, "echo '"+disconnectedJSON+"'\necho 'Warning: You are not logged in to an account.'\necho '"+relays+"'\necho '{\"auto_connect\":true,\"lockdown_mode\":true}'\necho '"+connectingJSON+"'\necho '"+connectedJSON+"'\n")
	var got []string
	var lockdown []bool
	err := c.Listen(context.Background(), func(s State) { got = append(got, s.State) }, func(on bool) { lockdown = append(lockdown, on) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "disconnected,connecting,connected" || len(lockdown) != 1 || !lockdown[0] {
		t.Errorf("states %v, lockdown %v", got, lockdown)
	}
}
