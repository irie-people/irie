//go:build !novpn && !noprivacy && !nowriting && !nomarkets && !nomail && !nobitwarden && !noterms && !noextension && !noyoink && !nochat && !noobsidian && !nofounding && !nojammin

package core

import (
	"os"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// TestThePluginsOp names every one of Irie's plugins, so it runs only in a
// build with them all: a copy that leaves some out lists fewer.

// TestThePluginsOp: the op lists the VPN, privacy, writing, markets, mail,
// Bitwarden, Terms in short, browser extension, Yoink, chat, Obsidian,
// Charters and Jammin plugins and their parts, and :plugins
// answers with them in words (DESIGN §4.17).
func TestThePluginsOp(t *testing.T) {
	c := bareCore(t, config.Default())
	r, err := c.pluginsOp(ipc.PluginsArgs{})
	if err != nil || len(r.Plugins) != 13 || r.Plugins[0].Name != "vpn" || !r.Plugins[0].On || len(r.Plugins[0].Parts) != 1 || r.Plugins[1].Name != "privacy" || !r.Plugins[1].On || len(r.Plugins[1].Parts) != 3 ||
		r.Plugins[2].Name != "writing" || !r.Plugins[2].On || len(r.Plugins[2].Parts) != 2 || r.Plugins[3].Name != "markets" || r.Plugins[3].On || len(r.Plugins[3].Parts) != 1 ||
		r.Plugins[4].Name != "mail" || !r.Plugins[4].On || len(r.Plugins[4].Parts) != 2 || r.Plugins[5].Name != "bitwarden" || !r.Plugins[5].On || len(r.Plugins[5].Parts) != 2 ||
		r.Plugins[6].Name != "terms" || !r.Plugins[6].On || len(r.Plugins[6].Parts) != 1 || r.Plugins[6].Parts[0].Pane == nil ||
		r.Plugins[7].Name != "extension" || !r.Plugins[7].On || len(r.Plugins[7].Parts) != 1 ||
		r.Plugins[8].Name != "yoink" || r.Plugins[8].On || len(r.Plugins[8].Parts) != 1 || r.Plugins[8].Parts[0].Pane == nil ||
		r.Plugins[9].Name != "chat" || !r.Plugins[9].On || len(r.Plugins[9].Parts) != 3 || r.Plugins[9].Parts[0].Pane == nil ||
		r.Plugins[10].Name != "obsidian" || !r.Plugins[10].On || len(r.Plugins[10].Parts) != 2 ||
		r.Plugins[11].Name != "founding" || r.Plugins[11].On || len(r.Plugins[11].Parts) != 2 || r.Plugins[11].Parts[0].Pane == nil ||
		r.Plugins[12].Name != "jammin" || !r.Plugins[12].On || len(r.Plugins[12].Parts) != 3 || r.Plugins[12].Parts[0].Pane == nil {
		t.Fatalf("plugins = %+v", r.Plugins)
	}
	in, err := c.input(ipc.InputArgs{Text: ":plugins"}, 0)
	if err != nil || in.Did != ipc.InputAnswered || !strings.HasPrefix(in.Text, "VPN plugin: on.") || !strings.Contains(in.Text, "Privacy plugin: on.") || !strings.Contains(in.Text, "Dead man's switch") || !strings.Contains(in.Text, "Writing plugin: on.") || !strings.Contains(in.Text, "Mail plugin: on.") || !strings.Contains(in.Text, "Markets plugin: off.") || !strings.Contains(in.Text, "Bitwarden plugin: on.") || !strings.Contains(in.Text, "Terms in short plugin: on.") || !strings.Contains(in.Text, "Browser extension plugin: on.") || !strings.Contains(in.Text, "Yoink plugin: off.") || !strings.Contains(in.Text, "Chat plugin: on.") || !strings.Contains(in.Text, "Obsidian plugin: on.") || !strings.Contains(in.Text, "Charters plugin: off.") || !strings.Contains(in.Text, "Jammin plugin: on.") {
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
	if _, err := c.pluginsOp(ipc.PluginsArgs{Plugin: "bitwarden", Part: "vault", On: &on}); err == nil || !strings.Contains(err.Error(), "isn't in "+brand.Name+" yet") {
		t.Errorf("vault on: %v", err)
	}
}
