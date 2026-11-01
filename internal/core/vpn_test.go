package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/tmux"
	"github.com/irie-people/irie/internal/vpn"
)

// The daemon's states as mullvad-cli 2026.3 prints them, with documentation
// addresses.
const (
	testConnected    = `{"state":"connected","details":{"location":{"latitude":57.7,"longitude":11.9,"ipv4":"192.0.2.10","city":"Gothenburg","country":"Sweden","location":{"hostname":["se","got","se-got-wg-001"]}},"feature_indicators":[]}}`
	testDisconnected = `{"state":"disconnected","details":{"location":{"ipv4":"198.51.100.7","city":"Home","country":"Here"},"locked_down":false}}`
)

// vpnCore is a core with vpn.on and a fake mullvad, which logs what it is
// asked and says it is connected.
func vpnCore(t *testing.T) (*Core, string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "mullvad")
	log := filepath.Join(dir, "args")
	script := `#!/bin/sh
echo "$*" >> ` + log + `
case "$1 $2" in
"status --json") echo '` + testConnected + `';;
"account get") printf 'Mullvad account:    1234123412341234\nExpires at:         2099-01-01 00:00:00 +00:00\n';;
"lockdown-mode get") echo 'Block traffic when the VPN is disconnected: on';;
"status listen") echo '` + testConnected + `'; sleep 30;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.VPN = config.VPN{On: true, Mullvad: bin}
	c := bareCore(t, cfg)
	c.vpn = vpnState{cancel: func() {}, bin: bin, read: true, lockdown: true, expires: time.Now().Add(41*24*time.Hour + time.Hour)}
	c.vpn.state, _ = vpn.Parse([]byte(testConnected))
	return c, log
}

func TestTheVPNsState(t *testing.T) {
	c, _ := vpnCore(t)
	r, err := c.vpnOp(0, ipc.VPNArgs{})
	if err != nil {
		t.Fatal(err)
	}
	if want := "VPN on: Gothenburg, Sweden (se-got-wg-001), lockdown on, 41 days left"; r.Text != want || r.VPN == nil || r.VPN.Host != "se-got-wg-001" {
		t.Errorf("got %q, %+v; want %q", r.Text, r.VPN, want)
	}
	if r, err := c.vpnOp(0, ipc.VPNArgs{Do: ipc.VPNTime}); err != nil || !strings.HasPrefix(r.Text, "Mullvad: 41 days left, until ") {
		t.Errorf("time: %q, %v", r.Text, err)
	}
	// The header carries the same, and nothing of the account or the address.
	c.mu.Lock()
	h := c.headerNow()
	c.mu.Unlock()
	if h.VPN == nil || h.VPN.Text != r.Text && !strings.HasPrefix(h.VPN.Text, "VPN on: Gothenburg") {
		t.Errorf("header %+v", h.VPN)
	}
}

func TestTheVPNsChanges(t *testing.T) {
	c, log := vpnCore(t)
	for _, a := range []ipc.VPNArgs{{Do: ipc.VPNOff}, {Do: ipc.VPNOn}, {Do: ipc.VPNLockdown, Args: []string{"off"}}, {Do: ipc.VPNPlace, Args: []string{"se", "got"}}} {
		if _, err := c.vpnOp(0, a); err != nil {
			t.Fatalf("%+v: %v", a, err)
		}
	}
	b, _ := os.ReadFile(log)
	// A place while connected reconnects through it.
	for _, want := range []string{"disconnect --wait\n", "connect --wait\n", "lockdown-mode set off\n", "relay set location se got\nreconnect --wait\n"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("mullvad was not asked %q:\n%s", want, b)
		}
	}
	for _, a := range []ipc.VPNArgs{{Do: ipc.VPNPlace, Args: []string{"--help"}}, {Do: ipc.VPNLockdown, Args: []string{"maybe"}}, {Do: "explode"}} {
		if _, err := c.vpnOp(0, a); err == nil {
			t.Errorf("%+v: took it", a)
		}
	}
}

// TestAnAgentCannotChangeTheVPN: a change from a process inside an agent's
// pane is refused, through the op and through typed words; the state is
// anyone's to read.
func TestAnAgentCannotChangeTheVPN(t *testing.T) {
	c, log := vpnCore(t)
	// This test's own parent stands for an agent's host.
	c.snap = tmux.Snapshot{Panes: []tmux.Pane{{ID: "%1", Role: tmux.RoleAgent, PID: os.Getppid()}}}
	if !c.fromAgent(os.Getpid()) {
		t.Fatal("a child of an agent's host is not the agent's")
	}
	if c.fromAgent(0) || c.fromAgent(os.Getppid()+1_000_000) {
		t.Error("an unknown process is an agent's")
	}
	if _, err := c.vpnOp(os.Getpid(), ipc.VPNArgs{Do: ipc.VPNOff}); err == nil || !strings.Contains(err.Error(), "An agent can't change the VPN") {
		t.Errorf("err = %v", err)
	}
	if _, err := c.input(ipc.InputArgs{Text: ":vpn off"}, os.Getpid()); err == nil {
		t.Error(":vpn off from an agent went through")
	}
	if r, err := c.input(ipc.InputArgs{Text: ":vpn"}, os.Getpid()); err != nil || r.Did != ipc.InputAnswered || !strings.HasPrefix(r.Text, "VPN on") {
		t.Errorf(":vpn from an agent: %+v, %v", r, err)
	}
	if b, _ := os.ReadFile(log); strings.Contains(string(b), "disconnect") {
		t.Errorf("mullvad was asked anyway:\n%s", b)
	}
}

func TestTheVPNWhenOff(t *testing.T) {
	c := bareCore(t, config.Default())
	if _, err := c.vpnOp(0, ipc.VPNArgs{}); err == nil || !strings.Contains(err.Error(), "[vpn] on = true") {
		t.Errorf("err = %v", err)
	}
	c.mu.Lock()
	h := c.headerNow()
	c.mu.Unlock()
	if h.VPN != nil {
		t.Errorf("header has a VPN while it is off: %+v", h.VPN)
	}
}

// TestTheCoreFollowsTheVPN: with vpn.on the core starts the listener, and
// its state reaches the header; with it off again, it stops.
func TestTheCoreFollowsTheVPN(t *testing.T) {
	c, _ := vpnCore(t)
	c.vpn = vpnState{}
	c.mu.Lock()
	c.keepVPN()
	c.mu.Unlock()
	deadline := time.Now().Add(10 * time.Second)
	for {
		c.mu.Lock()
		h := c.vpnHeader(time.Now())
		c.mu.Unlock()
		if h != nil && h.State == vpn.Connected && h.Lockdown && h.Expires != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("header %+v", h)
		}
		time.Sleep(50 * time.Millisecond)
	}
	cfg := *c.cfg()
	cfg.VPN.On = false
	c.useConfig(cfg)
	c.mu.Lock()
	c.keepVPN()
	stopped := c.vpn.cancel == nil
	c.mu.Unlock()
	if !stopped {
		t.Error("still following the VPN with vpn.on off")
	}
}

func TestVPNWordsWithoutState(t *testing.T) {
	if got := vpnWords(vpnState{problem: "no mullvad here"}, time.Now()); got != "VPN unknown: no mullvad here" {
		t.Errorf("got %q", got)
	}
	s, _ := vpn.Parse([]byte(testDisconnected))
	if got := vpnWords(vpnState{read: true, state: s, lockdown: true}, time.Now()); got != "VPN off" {
		t.Errorf("got %q", got)
	}
}
