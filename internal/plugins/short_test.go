package plugins

import (
	"testing"

	"github.com/irie-people/irie/internal/ipc"
)

// TestShortSaysAPluginsStateInALine: Short says whether a plugin is on, and
// which of its parts are ready and which need setting up, passing over the
// parts a build holds back. It holds for any plugin, so a copy that leaves
// Irie's own plugins out, with their tests, still tests it.
func TestShortSaysAPluginsStateInALine(t *testing.T) {
	parts := []ipc.PluginPart{
		{Title: "Rows", State: ipc.PartReady},
		{Title: "Hose", State: ipc.PartNeeds},
		{Title: "Shed", State: ipc.PartHeld},
		{Title: "Gate", State: ipc.PartReady},
		{Title: "Pond", State: ipc.PartOff},
	}
	for _, c := range []struct {
		p    ipc.Plugin
		want string
	}{
		{ipc.Plugin{Title: "Garden", On: true, Parts: parts}, "Garden plugin: on; ready: Rows, Gate; needs setting up: Hose"},
		{ipc.Plugin{Title: "Garden", On: true, Parts: parts[:1]}, "Garden plugin: on; ready: Rows"},
		{ipc.Plugin{Title: "Garden", On: true, Parts: parts[1:2]}, "Garden plugin: on; needs setting up: Hose"},
		{ipc.Plugin{Title: "Garden", On: true, Parts: parts[2:3]}, "Garden plugin: on; no part of it is on yet"},
		{ipc.Plugin{Title: "Garden", On: true, Parts: parts[4:]}, "Garden plugin: on; no part of it is on yet"},
		{ipc.Plugin{Title: "Garden", On: false, Parts: parts}, "Garden plugin: off"},
	} {
		if got := Short(c.p); got != c.want {
			t.Errorf("Short(%+v) = %q, want %q", c.p, got, c.want)
		}
	}
}
