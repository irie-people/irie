package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/dms"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/skills"
)

// env is a computer with nothing of the privacy plugin's on it: every
// program it looks for is configured to a path that isn't there, so the
// machine the tests run on doesn't matter.
func env(t *testing.T) Env {
	t.Helper()
	dir := t.TempDir()
	p := paths.Paths{Config: filepath.Join(dir, "config"), State: filepath.Join(dir, "state"), Data: filepath.Join(dir, "data"), Runtime: filepath.Join(dir, "run")}
	cfg := config.Default()
	cfg.VPN.Mullvad = filepath.Join(dir, "none", "mullvad")
	cfg.Anon.Tor = filepath.Join(dir, "none", "tor")
	cfg.Browser.Path = filepath.Join(dir, "none", "mullvad-browser")
	return Env{Config: cfg, Paths: p}
}

// program makes an executable file in the test's folder and returns it.
func program(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func privacyStatus(t *testing.T, e Env) ipc.Plugin {
	t.Helper()
	for _, p := range Status(e) {
		if p.Name == "privacy" {
			return p
		}
	}
	t.Fatal("no privacy plugin")
	return ipc.Plugin{}
}

func part(t *testing.T, p ipc.Plugin, name string) ipc.PluginPart {
	t.Helper()
	for _, x := range p.Parts {
		if x.Name == name {
			return x
		}
	}
	t.Fatalf("no part %q", name)
	return ipc.PluginPart{}
}

func TestPrivacyIsOnByDefaultWithItsPartsOff(t *testing.T) {
	e := env(t)
	p := privacyStatus(t, e)
	if !p.On || p.Title != "Privacy" || p.Setup != ":setup privacy" {
		t.Fatalf("privacy = %+v", p)
	}
	want := map[string]string{
		VPN:     ipc.PartOff,
		Anon:    ipc.PartOff,
		Browser: ipc.PartOff,
		Vault:   ipc.PartHeld,
		Switch:  ipc.PartReady, // the guide's skill is on, and does nothing until asked
	}
	if len(p.Parts) != len(want) {
		t.Fatalf("parts = %+v", p.Parts)
	}
	for name, state := range want {
		if got := part(t, p, name); got.State != state || got.Detail == "" {
			t.Errorf("%s = %q (%q), want %q", name, got.State, got.Detail, state)
		}
	}
	if !e.Config.Plugins.Privacy || e.Config.VPNOn() || e.Config.AnonOn() || e.Config.BrowserOn() {
		t.Errorf("defaults: privacy %v, vpn %v, anon %v, browser %v", e.Config.Plugins.Privacy, e.Config.VPNOn(), e.Config.AnonOn(), e.Config.BrowserOn())
	}
}

func TestAPartOnSaysWhatItLacks(t *testing.T) {
	e := env(t)
	e.Config.VPN.On, e.Config.Anon.On, e.Config.Browser.Connector = true, true, "on"
	p := privacyStatus(t, e)
	for _, name := range []string{VPN, Anon, Browser} {
		if got := part(t, p, name); got.State != ipc.PartNeeds {
			t.Errorf("%s with nothing installed = %q (%q), want needs", name, got.State, got.Detail)
		}
	}
	if d := part(t, p, VPN).Detail; !strings.Contains(d, "mullvad.net") {
		t.Errorf("the VPN's detail doesn't say where to get it: %q", d)
	}
	// A browser.path that isn't there says so: the browser Irie would find
	// by itself depends on the machine.
	if d := part(t, p, Browser).Detail; !strings.Contains(d, "browser.path") {
		t.Errorf("the browser's detail doesn't name browser.path: %q", d)
	}

	// With the programs there, each is ready, and says what it lacks
	// besides.
	e.Config.VPN.Mullvad = program(t, "mullvad")
	e.Config.Browser.Path = program(t, "mullvad-browser")
	p = privacyStatus(t, e)
	for _, name := range []string{VPN, Anon, Browser} {
		if got := part(t, p, name); got.State != ipc.PartReady {
			t.Errorf("%s installed = %q (%q), want ready", name, got.State, got.Detail)
		}
	}
	if d := part(t, p, Anon).Detail; !strings.Contains(d, "Tor and SearXNG aren't installed") {
		t.Errorf("anonymous research doesn't say Tor and SearXNG are missing: %q", d)
	}
	if d := part(t, p, Browser).Detail; !strings.Contains(d, "starts slowly") {
		t.Errorf("the browser doesn't say it has no clean profile yet: %q", d)
	}
}

func TestTheCoreSaysHowTheVPNIsNow(t *testing.T) {
	e := env(t)
	e.Config.VPN.On, e.Config.VPN.Mullvad = true, program(t, "mullvad")
	e.Live.VPN = &ipc.VPN{State: "connected", Text: "Connected in Malmö, Sweden."}
	if got := part(t, privacyStatus(t, e), VPN); got.State != ipc.PartReady || got.Detail != "Connected in Malmö, Sweden." {
		t.Errorf("vpn = %+v", got)
	}
	e.Live.VPN = &ipc.VPN{Problem: "mullvad status: the daemon isn't running"}
	if got := part(t, privacyStatus(t, e), VPN); got.State != ipc.PartNeeds || !strings.Contains(got.Detail, "daemon") {
		t.Errorf("vpn with a problem = %+v", got)
	}
}

func TestTheSwitchSaysWhenItWasMade(t *testing.T) {
	e := env(t)
	made := time.Date(2026, 11, 1, 12, 0, 0, 0, time.Local)
	if err := (dms.Store{Dir: filepath.Join(e.Paths.State, "dms")}).Save(dms.State{Made: made}); err != nil {
		t.Fatal(err)
	}
	if got := part(t, privacyStatus(t, e), Switch); got.State != ipc.PartReady || !strings.Contains(got.Detail, "1 November 2026") {
		t.Errorf("switch = %+v", got)
	}
	e.Config.Skills.Disabled = []string{"dead-mans-switch"}
	if got := part(t, privacyStatus(t, e), Switch); got.State != ipc.PartOff || got.On {
		t.Errorf("switch left out = %+v", got)
	}
}

func TestPrivacyOffTurnsEveryPartOff(t *testing.T) {
	e := env(t)
	e.Config.VPN.On, e.Config.Anon.On, e.Config.Browser.Connector = true, true, "on"
	e.Config.Plugins.Privacy = false
	if e.Config.VPNOn() || e.Config.AnonOn() || e.Config.BrowserOn() {
		t.Error("a part runs while its plugin is off")
	}
	p := privacyStatus(t, e)
	if p.On {
		t.Error("privacy is on")
	}
	for _, x := range p.Parts {
		want := ipc.PartPluginOff
		if x.Name == Vault {
			want = ipc.PartHeld
		}
		if x.State != want {
			t.Errorf("%s = %q, want %q", x.Name, x.State, want)
		}
	}
	// Each part's own switch still shows as the file has it.
	if !part(t, p, VPN).On || !part(t, p, Anon).On {
		t.Error("a part's own switch reads off while its plugin is")
	}
	for _, name := range []string{VPN, Anon, Browser} {
		if why := Why(e.Config, name); !strings.Contains(why, "the privacy plugin is") || !strings.Contains(why, "irie plugins on privacy") {
			t.Errorf("Why(%s) = %q", name, why)
		}
	}
	// Its skills stay out of sessions, and say why.
	left := SkillsLeft(e.Config)
	if !strings.Contains(left["dead-mans-switch"], "plugins.privacy") {
		t.Errorf("skills left = %v", left)
	}
	for _, c := range skills.Choose(skills.Bundled(), SkillSettings(e.Config, true)) {
		if c.Name == "dead-mans-switch" && (c.On || !strings.Contains(c.Why, "privacy plugin is off")) {
			t.Errorf("the guide's skill = %+v", c)
		}
		if c.Name != "dead-mans-switch" && !c.On {
			t.Errorf("%s left out: %s", c.Name, c.Why)
		}
	}
}

func TestWhySaysWhatTurnsAPartOn(t *testing.T) {
	cfg := config.Default()
	if why := Why(cfg, VPN); !strings.Contains(why, "irie plugins on privacy vpn") {
		t.Errorf("Why(vpn) = %q", why)
	}
	if why := Why(cfg, Anon); !strings.Contains(why, "irie setup anon") {
		t.Errorf("Why(anon) = %q", why)
	}
	if why := Why(cfg, Vault); !strings.Contains(why, "isn't in Irie yet") {
		t.Errorf("Why(vault) = %q", why)
	}
	cfg.VPN.On, cfg.Anon.On = true, true
	if why := Why(cfg, VPN) + Why(cfg, Anon); why != "" {
		t.Errorf("Why for parts that are on = %q", why)
	}
	if why := Why(cfg, "nothing"); why != "" {
		t.Errorf("Why(nothing) = %q", why)
	}
}

func TestEdits(t *testing.T) {
	for _, c := range []struct {
		plugin, part string
		on           bool
		key          string
		value        any
	}{
		{"privacy", "", false, "plugins.privacy", false},
		{"privacy", "", true, "plugins.privacy", true},
		{"privacy", "vpn", true, "vpn.on", true},
		{"privacy", "anon", false, "anon.on", false},
		{"privacy", "browser", true, "browser.connector", "on"},
		{"privacy", "Mullvad Browser for agents", false, "browser.connector", "off"},
	} {
		got, err := Edits(c.plugin, c.part, c.on)
		if err != nil || len(got) != 1 || got[0].Key != c.key || got[0].Value != c.value {
			t.Errorf("Edits(%q, %q, %v) = %+v, %v", c.plugin, c.part, c.on, got, err)
		}
		// Each edit is one config.toml takes.
		if _, err := config.Apply(nil, got); err != nil {
			t.Errorf("config.Apply(%+v): %v", got, err)
		}
	}
	for _, c := range []struct{ plugin, part, want string }{
		{"nothing", "", "Irie's plugins are privacy"},
		{"privacy", "nothing", "its parts are vpn, anon, browser, vault, switch"},
		{"privacy", "switch", "no switch of its own"},
		{"privacy", "vault", "isn't in Irie yet"},
	} {
		if _, err := Edits(c.plugin, c.part, true); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("Edits(%q, %q) = %v, want %q", c.plugin, c.part, err, c.want)
		}
	}
}

func TestTextAndShort(t *testing.T) {
	e := env(t)
	e.Config.Anon.On = true
	all := Status(e)
	text := Text(all)
	for _, want := range []string{"Privacy plugin: on.", "  · Mullvad VPN: ", "  ! Anonymous research: ", "  ○ Bitwarden for agents: ", "  ● Dead man's switch: "} {
		if !strings.Contains(text, want) {
			t.Errorf("Text lacks %q:\n%s", want, text)
		}
	}
	if s := Short(all[0]); s != "Privacy plugin: on; ready: Dead man's switch; needs setting up: Anonymous research" {
		t.Errorf("Short = %q", s)
	}
	all[0].On = false
	if s := Short(all[0]); s != "Privacy plugin: off" {
		t.Errorf("Short off = %q", s)
	}
}

func TestConfigReadsThePluginsSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[vpn]\non = true\n\n[plugins]\nprivacy = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Plugins.Privacy || !c.VPN.On || c.VPNOn() {
		t.Errorf("plugins.privacy = false read as %+v, vpn.on %v, VPNOn %v", c.Plugins, c.VPN.On, c.VPNOn())
	}
	if tree := config.Tree(c); tree["plugins"].(map[string]any)["privacy"] != false {
		t.Errorf("the tree's plugins = %v", tree["plugins"])
	}
}
