package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// TestThePluginsOp: the op lists the VPN, privacy, writing, markets, mail,
// Bitwarden, Terms in short, browser extension, Yoink and founding documents
// plugins and their parts, and :plugins answers with them in words (DESIGN §4.17).
func TestThePluginsOp(t *testing.T) {
	c := bareCore(t, config.Default())
	r, err := c.pluginsOp(ipc.PluginsArgs{})
	if err != nil || len(r.Plugins) != 10 || r.Plugins[0].Name != "vpn" || !r.Plugins[0].On || len(r.Plugins[0].Parts) != 1 || r.Plugins[1].Name != "privacy" || !r.Plugins[1].On || len(r.Plugins[1].Parts) != 3 ||
		r.Plugins[2].Name != "writing" || !r.Plugins[2].On || len(r.Plugins[2].Parts) != 2 || r.Plugins[3].Name != "markets" || r.Plugins[3].On || len(r.Plugins[3].Parts) != 1 ||
		r.Plugins[4].Name != "mail" || !r.Plugins[4].On || len(r.Plugins[4].Parts) != 2 || r.Plugins[5].Name != "bitwarden" || !r.Plugins[5].On || len(r.Plugins[5].Parts) != 2 ||
		r.Plugins[6].Name != "terms" || !r.Plugins[6].On || len(r.Plugins[6].Parts) != 1 || r.Plugins[6].Parts[0].Pane == nil ||
		r.Plugins[7].Name != "extension" || !r.Plugins[7].On || len(r.Plugins[7].Parts) != 1 ||
		r.Plugins[8].Name != "yoink" || r.Plugins[8].On || len(r.Plugins[8].Parts) != 1 || r.Plugins[8].Parts[0].Pane == nil ||
		r.Plugins[9].Name != "founding" || r.Plugins[9].On || len(r.Plugins[9].Parts) != 1 || r.Plugins[9].Parts[0].Pane == nil {
		t.Fatalf("plugins = %+v", r.Plugins)
	}
	in, err := c.input(ipc.InputArgs{Text: ":plugins"}, 0)
	if err != nil || in.Did != ipc.InputAnswered || !strings.HasPrefix(in.Text, "VPN plugin: on.") || !strings.Contains(in.Text, "Privacy plugin: on.") || !strings.Contains(in.Text, "Dead man's switch") || !strings.Contains(in.Text, "Writing plugin: on.") || !strings.Contains(in.Text, "Mail plugin: on.") || !strings.Contains(in.Text, "Markets plugin: off.") || !strings.Contains(in.Text, "Bitwarden plugin: on.") || !strings.Contains(in.Text, "Terms in short plugin: on.") || !strings.Contains(in.Text, "Browser extension plugin: on.") || !strings.Contains(in.Text, "Yoink plugin: off.") || !strings.Contains(in.Text, "Founding documents plugin: off.") {
		t.Errorf(":plugins = %+v, %v", in, err)
	}
	// A part turned on through the op: the core writes its key.
	on, off := true, false
	r, err = c.pluginsOp(ipc.PluginsArgs{Plugin: "privacy", Part: "browser", On: &on})
	if err != nil || !c.cfg().BrowserOn() {
		t.Fatalf("browser on: %v, connector %q", err, c.cfg().Browser.Connector)
	}
	if b, _ := os.ReadFile(c.paths.ConfigFile()); !strings.Contains(string(b), `connector = "on"`) {
		t.Errorf("config.toml:\n%s", b)
	}
	// The plugin off turns its parts off, and each keeps its own switch.
	r, err = c.pluginsOp(ipc.PluginsArgs{Plugin: "privacy", On: &off})
	if err != nil || c.cfg().BrowserOn() || c.cfg().Browser.Connector != "on" {
		t.Fatalf("privacy off: %v, browser on %v", err, c.cfg().BrowserOn())
	}
	for _, x := range r.Plugins[1].Parts {
		if x.State != ipc.PartPluginOff && x.State != ipc.PartHeld {
			t.Errorf("%s = %q with the plugin off", x.Name, x.State)
		}
	}
	if _, err := c.pluginsOp(ipc.PluginsArgs{Plugin: "bitwarden", Part: "vault", On: &on}); err == nil || !strings.Contains(err.Error(), "isn't in Irie yet") {
		t.Errorf("vault on: %v", err)
	}
}

// TestAnonymousResearchWaitsForItsPlugin: +anon says the plugin is off
// before anything else.
func TestAnonymousResearchWaitsForItsPlugin(t *testing.T) {
	cfg := config.Default()
	cfg.Anon.On, cfg.Plugins.Privacy = true, false
	c := bareCore(t, cfg)
	if _, _, err := c.prepareAnon("leads", "mullvad"); err == nil || !strings.Contains(err.Error(), "irie plugins on privacy") {
		t.Errorf("err = %v", err)
	}
}

// TestThePluginsOpTakesYourOwn: a manifest in the plugins folder is a plugin
// after Irie's own, with its pane, a bundle switches it through the op, and
// :plugins says the bundle first (docs/proposals/plugin-panes.md).
func TestThePluginsOpTakesYourOwn(t *testing.T) {
	c := bareCore(t, config.Default())
	dir := c.paths.Plugins()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		"invoices.toml": "title = \"Invoices\"\n[panes.invoices]\naddress = \"http://127.0.0.1:1/\"\nstart = \"make serve\"\n",
		"shop.toml":     "title = \"Shop\"\nplugins = [\"invoices\"]\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r, err := c.pluginsOp(ipc.PluginsArgs{})
	if err != nil || len(r.Bundles) != 1 || r.Bundles[0].State != ipc.BundleOn {
		t.Fatalf("plugins = %+v, %v", r, err)
	}
	p := r.Plugins[len(r.Plugins)-1]
	if p.Name != "invoices" || !p.On || len(p.Parts) != 1 || p.Parts[0].Pane == nil || p.Parts[0].Pane.Address != "http://127.0.0.1:1/" || p.Parts[0].State != ipc.PartNeeds {
		t.Fatalf("invoices = %+v", p)
	}
	if !strings.HasPrefix(r.Text, "Shop bundle: on.") {
		t.Errorf("text = %q", r.Text)
	}
	off := false
	r, err = c.pluginsOp(ipc.PluginsArgs{Bundle: "shop", On: &off})
	if err != nil || r.Bundles[0].State != ipc.BundleOff || c.cfg().Plugins.ManifestOn("invoices") {
		t.Fatalf("shop off: %+v, %v", r.Bundles, err)
	}
	if b, _ := os.ReadFile(c.paths.ConfigFile()); !strings.Contains(string(b), "invoices = false") {
		t.Errorf("config.toml:\n%s", b)
	}
	on := true
	if _, err := c.pluginsOp(ipc.PluginsArgs{Plugin: "invoices", On: &on}); err != nil || !c.cfg().Plugins.ManifestOn("invoices") {
		t.Errorf("invoices on: %v", err)
	}
	if _, err := c.pluginsOp(ipc.PluginsArgs{Bundle: "nothing", On: &on}); err == nil || !strings.Contains(err.Error(), `no bundle "nothing"`) {
		t.Errorf("no bundle: %v", err)
	}
	in, err := c.input(ipc.InputArgs{Text: ":plugins"}, 0)
	if err != nil || !strings.HasPrefix(in.Text, "Shop bundle: on.") || !strings.Contains(in.Text, "Invoices plugin: on.") {
		t.Errorf(":plugins = %+v, %v", in, err)
	}
}
