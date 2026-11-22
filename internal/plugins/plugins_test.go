//go:build !noprivacy

package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/dms"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/skills"
)

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

func TestPrivacyIsOnByDefaultWithItsPartsOff(t *testing.T) {
	e := env(t)
	p := privacyStatus(t, e)
	if !p.On || p.Title != "Privacy" || p.Setup != ":setup privacy" {
		t.Fatalf("privacy = %+v", p)
	}
	want := map[string]string{
		Anon:    ipc.PartOff,
		Browser: ipc.PartOff,
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
	if !e.Config.Plugins.Privacy || e.Config.AnonOn() || e.Config.BrowserOn() {
		t.Errorf("defaults: privacy %v, anon %v, browser %v", e.Config.Plugins.Privacy, e.Config.AnonOn(), e.Config.BrowserOn())
	}
}

func TestAPartOnSaysWhatItLacks(t *testing.T) {
	e := env(t)
	e.Config.Anon.On, e.Config.Browser.Connector = true, "on"
	p := privacyStatus(t, e)
	for _, name := range []string{Anon, Browser} {
		if got := part(t, p, name); got.State != ipc.PartNeeds {
			t.Errorf("%s with nothing installed = %q (%q), want needs", name, got.State, got.Detail)
		}
	}
	if d := part(t, p, Anon).Detail; !strings.Contains(d, "mullvad.net") {
		t.Errorf("anonymous research doesn't say where to get Mullvad: %q", d)
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
	for _, name := range []string{Anon, Browser} {
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
	e.Config.Anon.On, e.Config.Browser.Connector = true, "on"
	e.Config.Plugins.Privacy = false
	if e.Config.AnonOn() || e.Config.BrowserOn() {
		t.Error("a part runs while its plugin is off")
	}
	p := privacyStatus(t, e)
	if p.On {
		t.Error("privacy is on")
	}
	for _, x := range p.Parts {
		if x.State != ipc.PartPluginOff {
			t.Errorf("%s = %q, want %q", x.Name, x.State, ipc.PartPluginOff)
		}
	}
	// Each part's own switch still shows as the file has it.
	if !part(t, p, Anon).On || !part(t, p, Browser).On {
		t.Error("a part's own switch reads off while its plugin is")
	}
	for _, name := range []string{Anon, Browser} {
		if why := Why(e.Config, name); !strings.Contains(why, "the privacy plugin is") || !strings.Contains(why, brand.Slug+" plugins on privacy") {
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
		// Another plugin's skill may be out for its own reason: the chat
		// plugin's waits for an account to write for, and the founding
		// documents' plugin is off by default.
		if c.Name != "dead-mans-switch" && !c.On && c.Why != "no chat account yet" && !strings.Contains(c.Why, "founding plugin is off") {
			t.Errorf("%s left out: %s", c.Name, c.Why)
		}
	}
}

func TestWhySaysWhatTurnsAPartOn(t *testing.T) {
	cfg := config.Default()
	if why := Why(cfg, Anon); !strings.Contains(why, brand.Slug+" setup anon") {
		t.Errorf("Why(anon) = %q", why)
	}
	cfg.Anon.On = true
	if why := Why(cfg, Anon); why != "" {
		t.Errorf("Why for parts that are on = %q", why)
	}
	// A part no plugin of this build has, as in a build that leaves its
	// plugin out, refuses rather than reads as on.
	if why := Why(cfg, "nothing"); !strings.Contains(why, "isn't in this build") {
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
		{"nothing", "", brand.Name + "'s plugins are "},
		{"privacy", "nothing", "its parts are anon, browser, switch"},
		{"privacy", "switch", "no switch of its own"},
	} {
		if _, err := Edits(c.plugin, c.part, true); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("Edits(%q, %q) = %v, want %q", c.plugin, c.part, err, c.want)
		}
	}
}

func TestTextAndShort(t *testing.T) {
	e := env(t)
	e.Config.Anon.On = true
	text := Text(Status(e))
	for _, want := range []string{"Privacy plugin: on.", "  ! Anonymous research: ", "  ● Dead man's switch: "} {
		if !strings.Contains(text, want) {
			t.Errorf("Text lacks %q:\n%s", want, text)
		}
	}
	p := privacyStatus(t, e)
	if s := Short(p); s != "Privacy plugin: on; ready: Dead man's switch; needs setting up: Anonymous research" {
		t.Errorf("Short = %q", s)
	}
	p.On = false
	if s := Short(p); s != "Privacy plugin: off" {
		t.Errorf("Short off = %q", s)
	}
}

func TestConfigReadsThePluginsSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[anon]\non = true\n\n[plugins]\nprivacy = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Plugins.Privacy || !c.Anon.On || c.AnonOn() {
		t.Errorf("plugins.privacy = false read as %+v, anon.on %v, AnonOn %v", c.Plugins, c.Anon.On, c.AnonOn())
	}
	if tree := config.Tree(c); tree["plugins"].(map[string]any)["privacy"] != false {
		t.Errorf("the tree's plugins = %v", tree["plugins"])
	}
}

// TestABundlesPartGoesOnWithItsPlugin: a part a bundle names goes on with
// its plugin, and off by its own switch alone, so the plugin's other parts
// stay (docs/proposals/plugin-panes.md).
func TestABundlesPartGoesOnWithItsPlugin(t *testing.T) {
	e := env(t)
	e.Config.Plugins.Privacy = false
	manifest(t, e, "travel.toml", "plugins = [\"privacy.anon\"]\n")
	b := bundle(t, e, "travel")
	if b.State != ipc.BundleOff || len(b.Plugins) != 1 || b.Plugins[0].Title != "Anonymous research" || b.Plugins[0].On || b.Plugins[0].State != ipc.PartPluginOff {
		t.Fatalf("bundle = %+v", b)
	}
	dir := e.Paths.Plugins()
	on, err := BundleEdits(dir, "travel", true)
	if err != nil || len(on) != 2 || on[0] != (config.Edit{Key: "plugins.privacy", Value: true}) || on[1] != (config.Edit{Key: "anon.on", Value: true}) {
		t.Fatalf("on = %v, %v", on, err)
	}
	off, err := BundleEdits(dir, "travel", false)
	if err != nil || len(off) != 1 || off[0] != (config.Edit{Key: "anon.on", Value: false}) {
		t.Fatalf("off = %v, %v", off, err)
	}
	e.Config.Plugins.Privacy, e.Config.Anon.On = true, true
	if b := bundle(t, e, "travel"); b.State != ipc.BundleOn || !b.Plugins[0].On {
		t.Errorf("on = %+v", b)
	}
}
