package core

import (
	"os"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// TestThePluginsOp: the op lists the privacy and writing plugins and their
// parts, and :plugins answers with them in words (DESIGN §4.17).
func TestThePluginsOp(t *testing.T) {
	c := bareCore(t, config.Default())
	r, err := c.pluginsOp(ipc.PluginsArgs{})
	if err != nil || len(r.Plugins) != 2 || r.Plugins[0].Name != "privacy" || !r.Plugins[0].On || len(r.Plugins[0].Parts) != 5 || r.Plugins[1].Name != "writing" || !r.Plugins[1].On || len(r.Plugins[1].Parts) != 1 {
		t.Fatalf("plugins = %+v", r.Plugins)
	}
	in, err := c.input(ipc.InputArgs{Text: ":plugins"}, 0)
	if err != nil || in.Did != ipc.InputAnswered || !strings.HasPrefix(in.Text, "Privacy plugin: on.") || !strings.Contains(in.Text, "Dead man's switch") || !strings.Contains(in.Text, "Writing plugin: on.") {
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
	for _, x := range r.Plugins[0].Parts {
		if x.State != ipc.PartPluginOff && x.State != ipc.PartHeld {
			t.Errorf("%s = %q with the plugin off", x.Name, x.State)
		}
	}
	if _, err := c.pluginsOp(ipc.PluginsArgs{Plugin: "privacy", Part: "vault", On: &on}); err == nil || !strings.Contains(err.Error(), "isn't in Irie yet") {
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
