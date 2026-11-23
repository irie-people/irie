package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

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
