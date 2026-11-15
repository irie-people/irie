package core

import (
	"encoding/json"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/plugins"
)

// pluginsOp answers the plugins op (DESIGN §4.17): Irie's plugins, then
// yours from the manifests in the plugins folder, and how far each part is
// set up on this computer, with what the core knows now of the VPN, and
// your bundles (docs/proposals/plugin-panes.md); after turning a plugin, a
// part or a bundle on or off when asked. It reads files, and knocks once on
// each web pane's address on this computer, never the network or an
// account.
func (c *Core) pluginsOp(a ipc.PluginsArgs) (ipc.PluginsResult, error) {
	dir := c.paths.Plugins()
	if a.On != nil {
		var edits []config.Edit
		var err error
		if a.Bundle != "" {
			edits, err = plugins.BundleEdits(dir, a.Bundle, *a.On)
		} else {
			edits, err = plugins.EditsHere(dir, a.Plugin, a.Part, *a.On)
		}
		if err != nil {
			return ipc.PluginsResult{}, ipc.Errorf(ipc.CodeBadArgs, "%v", err)
		}
		sets := make([]ipc.ConfigSet, len(edits))
		for i, e := range edits {
			v, err := json.Marshal(e.Value)
			if err != nil {
				return ipc.PluginsResult{}, err
			}
			sets[i] = ipc.ConfigSet{Key: e.Key, Value: v}
		}
		if err := c.configure(c.paths.ConfigFile(), sets); err != nil {
			return ipc.PluginsResult{}, err
		}
	}
	c.mu.Lock()
	live := plugins.Live{VPN: c.vpnHeader(time.Now())}
	c.mu.Unlock()
	if c.cfg().MailOn() {
		live.Mail = c.mailLive()
	}
	all, bundles := plugins.Here(plugins.Env{Config: *c.cfg(), Paths: c.paths, Live: live})
	return ipc.PluginsResult{Plugins: all, Bundles: bundles, Text: plugins.AllText(all, bundles)}, nil
}
