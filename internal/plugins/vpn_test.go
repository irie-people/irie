//go:build !novpn

package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/vpn"
)

// vpnEnv is a computer with no VPN's command on it: every one is configured
// to a path that isn't there. It follows Mullvad VPN, off, whatever VPN
// and server this copy's brand names.
func vpnEnv(t *testing.T) Env {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.VPN.On, cfg.VPN.Provider, cfg.VPN.Server = false, "mullvad", ""
	cfg.VPN.Mullvad = filepath.Join(dir, "none", "mullvad")
	cfg.VPN.CLI = filepath.Join(dir, "none", "vpn")
	return Env{Config: cfg, Paths: paths.Paths{Config: filepath.Join(dir, "config"), State: filepath.Join(dir, "state"), Data: filepath.Join(dir, "data"), Runtime: filepath.Join(dir, "run")}}
}

func vpnStatus(t *testing.T, e Env) ipc.PluginPart {
	t.Helper()
	for _, p := range Status(e) {
		if p.Name == "vpn" {
			if len(p.Parts) != 1 {
				t.Fatalf("vpn parts = %+v", p.Parts)
			}
			return p.Parts[0]
		}
	}
	t.Fatal("no vpn plugin")
	return ipc.PluginPart{}
}

// TestTheVPNIsAPluginOfItsOwn: on by default with its part off, apart from
// the privacy plugin, titled by the VPN that vpn.provider names.
func TestTheVPNIsAPluginOfItsOwn(t *testing.T) {
	e := vpnEnv(t)
	p, ok := Find("vpn")
	if !ok || p.Title != "VPN" || !p.IsOn(e.Config) || p.Setup != "" || p.Key() != "plugins.vpn" {
		t.Fatalf("vpn plugin = %+v, %v", p, ok)
	}
	if All()[0].Name != "vpn" {
		t.Errorf("the VPN plugin isn't first: %v", Names())
	}
	// It names the VPNs this copy offers.
	for _, name := range config.VPNOffered() {
		k, _ := vpn.KindOf(name)
		if !strings.Contains(p.Summary, k.Title) && !strings.Contains(p.Summary, kindWords(k)) {
			t.Errorf("summary %q lacks %s", p.Summary, k.Title)
		}
	}
	x := vpnStatus(t, e)
	if x.Name != VPN || x.Title != "Mullvad VPN" || x.State != ipc.PartOff || x.Switch != "vpn.on" || x.Setup != ":setup vpn" {
		t.Errorf("part = %+v", x)
	}
	if d := config.Default(); d.VPNOn() != (brand.VPNs != "") {
		t.Errorf("VPNOn by default = %v, with brand.VPNs %q", d.VPNOn(), brand.VPNs)
	}
	e.Config.VPN.Provider = "system"
	if x := vpnStatus(t, e); x.Title != "The system's VPN" {
		t.Errorf("title for system = %q", x.Title)
	}
}

func TestTheVPNSaysWhatItLacks(t *testing.T) {
	e := vpnEnv(t)
	e.Config.VPN.On = true
	if x := vpnStatus(t, e); x.State != ipc.PartNeeds || !strings.Contains(x.Detail, "vpn.mullvad") {
		t.Errorf("mullvad at a path that isn't there = %+v", x)
	}
	e.Config.VPN.Mullvad = ""
	t.Setenv("PATH", t.TempDir())
	if _, err := os.Stat("/usr/local/bin/mullvad"); err != nil {
		if x := vpnStatus(t, e); x.State != ipc.PartNeeds || !strings.Contains(x.Detail, "mullvad.net") {
			t.Errorf("mullvad not installed = %+v", x)
		}
	}
	e.Config.VPN.Provider = "system"
	if x := vpnStatus(t, e); x.State != ipc.PartNeeds || !strings.Contains(x.Detail, "needs vpn.server") || !strings.Contains(x.Detail, "setup vpn") {
		t.Errorf("system without a name = %+v", x)
	}
	e.Config.VPN.Provider = "command"
	if x := vpnStatus(t, e); x.State != ipc.PartNeeds || !strings.Contains(x.Detail, "vpn.status") {
		t.Errorf("command without commands = %+v", x)
	}
	e.Config.VPN.Status, e.Config.VPN.Connected = []string{program(t, "status")}, "Connected"
	if x := vpnStatus(t, e); x.State != ipc.PartReady || !strings.Contains(x.Detail, "Custom VPN") {
		t.Errorf("command with its status = %+v", x)
	}
	e.Config.VPN.Provider, e.Config.VPN.Mullvad = "mullvad", program(t, "mullvad")
	if x := vpnStatus(t, e); x.State != ipc.PartReady || !strings.Contains(x.Detail, "Irie shows Mullvad VPN in every client") {
		t.Errorf("mullvad installed = %+v", x)
	}
	e.Config.VPN.On = false
	if x := vpnStatus(t, e); x.State != ipc.PartOff || !strings.Contains(x.Detail, "Mullvad VPN is installed") {
		t.Errorf("mullvad installed, off = %+v", x)
	}
}

func TestTheCoreSaysHowTheVPNIsNow(t *testing.T) {
	e := vpnEnv(t)
	e.Config.VPN.On, e.Config.VPN.Mullvad = true, program(t, "mullvad")
	e.Live.VPN = &ipc.VPN{State: "connected", Text: "Connected in Malmö, Sweden."}
	if got := vpnStatus(t, e); got.State != ipc.PartReady || got.Detail != "Connected in Malmö, Sweden." {
		t.Errorf("vpn = %+v", got)
	}
	e.Live.VPN = &ipc.VPN{Problem: "mullvad status: the daemon isn't running"}
	if got := vpnStatus(t, e); got.State != ipc.PartNeeds || !strings.Contains(got.Detail, "daemon") {
		t.Errorf("vpn with a problem = %+v", got)
	}
}

// TestTheVPNPluginsSwitch: its plugin off turns it off whatever vpn.on
// says, the privacy plugin's switch doesn't, and the words say which turns
// it on.
func TestTheVPNPluginsSwitch(t *testing.T) {
	cfg := config.Default()
	cfg.VPN.On, cfg.VPN.Provider = false, "mullvad"
	if why := Why(cfg, VPN); !strings.Contains(why, "irie setup vpn") || !strings.Contains(why, "irie plugins on vpn") {
		t.Errorf("Why(vpn) = %q", why)
	}
	cfg.VPN.On = true
	if why := Why(cfg, VPN); why != "" || !cfg.VPNOn() {
		t.Errorf("on: Why %q, VPNOn %v", why, cfg.VPNOn())
	}
	cfg.Plugins.Privacy = false
	if !cfg.VPNOn() {
		t.Error("the privacy plugin off turned the VPN off")
	}
	cfg.Plugins.VPN = false
	if why := Why(cfg, VPN); cfg.VPNOn() || !strings.Contains(why, "as the vpn plugin is") || !strings.Contains(why, "Mullvad VPN is off") {
		t.Errorf("plugin off: Why %q, VPNOn %v", why, cfg.VPNOn())
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[vpn]\non = true\n\n[plugins]\nvpn = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if c, err := config.Load(path); err != nil || c.Plugins.VPN || !c.VPN.On || c.VPNOn() {
		t.Errorf("plugins.vpn = false read as %+v, vpn.on %v, VPNOn %v, %v", c.Plugins, c.VPN.On, c.VPNOn(), err)
	}
}

// TestTheVPNPluginComesOnWhole: turning the plugin on turns its one part
// on too; off leaves the part's switch as it was.
func TestTheVPNPluginComesOnWhole(t *testing.T) {
	for _, c := range []struct {
		part string
		on   bool
		want []config.Edit
	}{
		{"", true, []config.Edit{{Key: "plugins.vpn", Value: true}, {Key: "vpn.on", Value: true}}},
		{"", false, []config.Edit{{Key: "plugins.vpn", Value: false}}},
		{"vpn", true, []config.Edit{{Key: "vpn.on", Value: true}}},
		{"Mullvad VPN", false, nil},
	} {
		got, err := Edits("vpn", c.part, c.on)
		if c.want == nil {
			if err == nil || !strings.Contains(err.Error(), "no part") {
				t.Errorf("Edits(vpn, %q) = %+v, %v", c.part, got, err)
			}
			continue
		}
		if err != nil || len(got) != len(c.want) {
			t.Errorf("Edits(vpn, %q, %v) = %+v, %v", c.part, c.on, got, err)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("Edits(vpn, %q, %v)[%d] = %+v, want %+v", c.part, c.on, i, got[i], c.want[i])
			}
		}
		if _, err := config.Apply(nil, got); err != nil {
			t.Errorf("config.Apply(%+v): %v", got, err)
		}
	}
}

func TestVPNSettings(t *testing.T) {
	home, _ := os.UserHomeDir()
	cfg := config.Default()
	cfg.VPN.Provider, cfg.VPN.Mullvad, cfg.VPN.CLI = "mullvad", "~/bin/mullvad", "~/bin/other"
	if s := VPNSettings(cfg); s.CLI != filepath.Join(home, "bin", "mullvad") {
		t.Errorf("mullvad's CLI = %q", s.CLI)
	}
	cfg.VPN.Provider, cfg.VPN.Server = "command", "vpn.example.com"
	cfg.VPN.Status, cfg.VPN.Connect = []string{"~/bin/myvpn", "status", "~"}, []string{"myvpn", "up"}
	s := VPNSettings(cfg)
	if s.CLI != filepath.Join(home, "bin", "other") || s.Server != "vpn.example.com" || s.Status[0] != filepath.Join(home, "bin", "myvpn") || s.Status[2] != "~" || s.Connect[0] != "myvpn" || s.Disconnect != nil {
		t.Errorf("settings = %+v", s)
	}
}
