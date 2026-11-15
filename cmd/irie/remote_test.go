package main

import (
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// TestRemotePanesSaysWhereThePhoneOpensEach: `remote panes` gives each
// pane of remote.panes at every address of the machine, and how it stands
// when the core says.
func TestRemotePanesSaysWhereThePhoneOpensEach(t *testing.T) {
	file := "/home/alice/.config/irie/config.toml"
	if got := panesText(config.Remote{On: true}, "Studio", nil, nil, file); !strings.Contains(got, `No pane reaches the phone yet. Name one under [remote.panes] in `+file) {
		t.Errorf("no panes:\n%s", got)
	}
	rm := config.Remote{On: true, Panes: map[string]int{"shop.till": 7390, "shop.stock": 7391, "gone.gone": 7392}}
	part := func(name, title, state string) ipc.PluginPart {
		return ipc.PluginPart{Name: name, State: state, Pane: &ipc.PluginPane{ID: "shop." + name, Title: title, Kind: ipc.PaneWeb, Origin: "http://127.0.0.1:8080"}}
	}
	res := &ipc.PluginsResult{Plugins: []ipc.Plugin{{Name: "shop", On: true, Parts: []ipc.PluginPart{part("till", "Till", ipc.PartReady), part("stock", "Stock", ipc.PartOff)}}}}
	got := panesText(rm, "Studio", []string{"studio.local", "192.168.1.20"}, res, file)
	for _, want := range []string{
		"The panes phones reach on Studio (remote.panes in " + file + "):",
		"  gone.gone: no manifest in the plugins folder has it\n    https://studio.local:7392/\n",
		"  Stock (shop.stock): off; turn its plugin on in Settings › Plugins\n    https://studio.local:7391/\n    or https://192.168.1.20:7391/\n",
		"  Till (shop.till)\n    https://studio.local:7390/\n    or https://192.168.1.20:7390/\n",
		"then Share › Add to Home Screen.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "phone app is off") {
		t.Errorf("remote.on is set:\n%s", got)
	}
	// With the core down it still says where, and with the app off, that too.
	got = panesText(config.Remote{Panes: map[string]int{"shop.till": 7390}}, "Studio", []string{"studio.local"}, nil, file)
	if !strings.HasPrefix(got, "The phone app is off on this machine") || !strings.Contains(got, "  shop.till\n    https://studio.local:7390/\n") {
		t.Errorf("the core down and the app off:\n%s", got)
	}
}
