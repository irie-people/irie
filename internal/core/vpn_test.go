//go:build !novpn

package core

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/plugins"
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
case "$1 $2 $3" in
"status --json listen") echo '` + testConnected + `'; sleep 30;;
"status --json "*) echo '` + testConnected + `';;
"account get "*) printf 'Mullvad account:    1234123412341234\nExpires at:         2099-01-01 00:00:00 +00:00\n';;
"lockdown-mode get "*) echo 'Block traffic when the VPN is disconnected: on';;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.VPN = config.VPN{On: true, Provider: "mullvad", Mullvad: bin}
	c := bareCore(t, cfg)
	c.vpn = vpnState{cancel: func() {}, gen: 1, settings: plugins.VPNSettings(cfg), vpn: vpn.Mullvad{Bin: bin}, read: true, lockdown: true, expires: time.Now().Add(41*24*time.Hour + time.Hour)}
	c.vpn.state, _ = vpn.ParseMullvad([]byte(testConnected))
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
	// Off, as in Irie; a copy whose brand names its VPNs has it on.
	off := config.Default()
	off.VPN.On = false
	c := bareCore(t, off)
	if _, err := c.vpnOp(0, ipc.VPNArgs{}); err == nil || !strings.Contains(err.Error(), brand.Slug+" setup vpn") || !strings.Contains(err.Error(), brand.Slug+" plugins on vpn") {
		t.Errorf("err = %v", err)
	}
	c.mu.Lock()
	h := c.headerNow()
	c.mu.Unlock()
	if h.VPN != nil {
		t.Errorf("header has a VPN while it is off: %+v", h.VPN)
	}
	// vpn.on with the VPN plugin off is off too, and says why (DESIGN
	// §4.17); the privacy plugin off leaves it on.
	cfg := config.Default()
	cfg.VPN.On, cfg.Plugins.VPN = true, false
	c = bareCore(t, cfg)
	if _, err := c.vpnOp(0, ipc.VPNArgs{}); err == nil || !strings.Contains(err.Error(), "as the vpn plugin is") {
		t.Errorf("err with the plugin off = %v", err)
	}
	if cfg.Plugins.VPN, cfg.Plugins.Privacy = true, false; !cfg.VPNOn() {
		t.Error("the privacy plugin off turns the VPN off")
	}
	c.mu.Lock()
	h = c.headerNow()
	c.mu.Unlock()
	if h.VPN != nil {
		t.Errorf("header has a VPN while its plugin is off: %+v", h.VPN)
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
	s, _ := vpn.ParseMullvad([]byte(testDisconnected))
	if got := vpnWords(vpnState{read: true, state: s, lockdown: true}, time.Now()); got != "VPN off" {
		t.Errorf("got %q", got)
	}
}

// commandCore is a core with vpn.on that follows a VPN of the person's own
// commands: a status that says whether a file is there, and a connect and
// a disconnect that make and remove it.
func commandCore(t *testing.T) (*Core, string) {
	t.Helper()
	dir := t.TempDir()
	up := filepath.Join(dir, "up")
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cfg := config.Default()
	cfg.VPN = config.VPN{
		On: true, Provider: "command", Server: "vpn.example.com",
		Status:     []string{write("status", "if [ -f '"+up+"' ]; then echo 'Connected'; else echo 'Disconnected'; exit 1; fi")},
		Connect:    []string{write("connect", "touch '"+up+"'")},
		Disconnect: []string{write("disconnect", "rm -f '"+up+"'")},
		Connected:  "^Connected",
	}
	c := bareCore(t, cfg)
	// Its following stops with the test, not with the package.
	t.Cleanup(func() {
		c.mu.Lock()
		if c.vpn.cancel != nil {
			c.vpn.cancel()
		}
		c.mu.Unlock()
	})
	return c, up
}

// waitVPN waits until the header's VPN is as ok says.
func waitVPN(t *testing.T, c *Core, ok func(*ipc.VPN) bool) *ipc.VPN {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		c.mu.Lock()
		h := c.vpnHeader(time.Now())
		c.mu.Unlock()
		if h != nil && ok(h) {
			return h
		}
		if time.Now().After(deadline) {
			t.Fatalf("header %+v", h)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestTheCoreFollowsAnyVPN: a VPN that doesn't say its own changes is
// asked every few seconds, its words drive it, and the words it lacks are
// refused with the reason (DESIGN §4.18).
func TestTheCoreFollowsAnyVPN(t *testing.T) {
	defer vpnPollEvery.Store(vpnPollEvery.Load())
	vpnPollEvery.Store(int64(20 * time.Millisecond))
	c, up := commandCore(t)
	c.mu.Lock()
	c.keepVPN()
	c.mu.Unlock()
	h := waitVPN(t, c, func(h *ipc.VPN) bool { return h.State == vpn.Disconnected })
	if h.Kind != "command" || h.Title != "Custom VPN" || len(h.Can) != 0 || h.Text != "VPN off" {
		t.Errorf("header %+v", h)
	}
	r, err := c.vpnOp(0, ipc.VPNArgs{Do: ipc.VPNOn})
	if err != nil || r.Text != "Connected. VPN on: vpn.example.com." {
		t.Errorf("on: %q, %v", r.Text, err)
	}
	if _, err := os.Stat(up); err != nil {
		t.Errorf("connect didn't run: %v", err)
	}
	waitVPN(t, c, func(h *ipc.VPN) bool { return h.State == vpn.Connected && h.Host == "vpn.example.com" })
	for a, want := range map[string]ipc.VPNArgs{
		"has no lockdown mode":      {Do: ipc.VPNLockdown, Args: []string{"on"}},
		"has no places":             {Do: ipc.VPNPlace, Args: []string{"se"}},
		"has no account time":       {Do: ipc.VPNTime},
		`vpn does not "explode"`:    {Do: "explode"},
		"takes one VPN":             {Do: ipc.VPNUse},
		`doesn't drive "openvpn99"`: {Do: ipc.VPNUse, Args: []string{"openvpn99"}},
	} {
		if _, err := c.vpnOp(0, want); err == nil || !strings.Contains(err.Error(), a) {
			t.Errorf("%+v: %v, want %q", want, err, a)
		}
	}
	if r, err := c.vpnOp(0, ipc.VPNArgs{Do: ipc.VPNOff}); err != nil || r.Text != "Disconnected. VPN off." {
		t.Errorf("off: %q, %v", r.Text, err)
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

// TestUseAnotherVPN: :vpn use NAME writes vpn.provider and turns VPN
// control on, and the core follows the new VPN at once; an agent can't.
func TestUseAnotherVPN(t *testing.T) {
	if !slices.Contains(config.VPNOffered(), "command") {
		t.Skip("this copy doesn't offer the custom VPN")
	}
	defer vpnPollEvery.Store(vpnPollEvery.Load())
	vpnPollEvery.Store(int64(20 * time.Millisecond))
	c, _ := commandCore(t)
	cfg := *c.cfg()
	cfg.VPN.On, cfg.VPN.Provider = false, "mullvad"
	c.useConfig(cfg)
	// The file holds the commands, as the person's would: the change is
	// made to it, and read back.
	v := cfg.VPN
	file := fmt.Sprintf("[vpn]\nprovider = \"mullvad\"\nserver = %q\nstatus = [%q]\nconnect = [%q]\ndisconnect = [%q]\nconnected = %q\n", v.Server, v.Status[0], v.Connect[0], v.Disconnect[0], v.Connected)
	if err := os.MkdirAll(filepath.Dir(c.paths.ConfigFile()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.paths.ConfigFile(), []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	c.snap = tmux.Snapshot{Panes: []tmux.Pane{{ID: "%1", Role: tmux.RoleAgent, PID: os.Getppid()}}}
	if _, err := c.vpnOp(os.Getpid(), ipc.VPNArgs{Do: ipc.VPNUse, Args: []string{"command"}}); err == nil || !strings.Contains(err.Error(), "An agent can't change the VPN") {
		t.Errorf("from an agent: %v", err)
	}
	r, err := c.input(ipc.InputArgs{Text: ":vpn use command"}, 0)
	if err != nil || r.Text != brand.Name+" follows Custom VPN now." {
		t.Fatalf("use: %+v, %v", r, err)
	}
	if v := c.cfg().VPN; v.Provider != "command" || !v.On || !c.cfg().Plugins.VPN {
		t.Errorf("vpn = %+v", v)
	}
	// The last VPN's server stays behind.
	if b, _ := os.ReadFile(c.paths.ConfigFile()); !strings.Contains(string(b), `provider = "command"`) || strings.Contains(string(b), "server =") {
		t.Errorf("config.toml:\n%s", b)
	}
	h := waitVPN(t, c, func(h *ipc.VPN) bool { return h.Kind == "command" && h.State == vpn.Disconnected })
	if h.Title != "Custom VPN" {
		t.Errorf("header %+v", h)
	}
	// Another VPN's settings follow it afresh, as another following.
	c.mu.Lock()
	gen := c.vpn.gen
	c.mu.Unlock()
	cfg = *c.cfg()
	cfg.VPN.Server = "gw2.example.com"
	c.useConfig(cfg)
	c.mu.Lock()
	c.keepVPN()
	again := c.vpn.gen
	c.mu.Unlock()
	if again != gen+1 {
		t.Errorf("following %d after a new server, want %d", again, gen+1)
	}
}

// TestAnOlderReadNeverReplacesANewer: a poll that began before :vpn on and
// ends after it keeps its state to itself.
func TestAnOlderReadNeverReplacesANewer(t *testing.T) {
	var v vpnState
	v.setState(5, vpn.State{State: vpn.Connected})
	v.setState(4, vpn.State{State: vpn.Disconnected})
	if v.state.State != vpn.Connected || v.readN != 5 {
		t.Errorf("state %+v, read %d", v.state, v.readN)
	}
	v.setState(6, vpn.State{State: vpn.Disconnected})
	if v.state.State != vpn.Disconnected {
		t.Errorf("a newer read lost: %+v", v.state)
	}
}
