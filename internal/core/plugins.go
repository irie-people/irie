package core

import (
	"encoding/json"
	"strings"
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
		if *a.On {
			// Your word: what each plugin of yours turned on runs now is
			// what it may run, until it changes (DESIGN §4.17). An agent
			// can't get here: CallFrom refuses its "on".
			names := []string{a.Plugin}
			for _, e := range edits {
				if name, ok := strings.CutPrefix(e.Key, "plugins.manifests."); ok && e.Value == true {
					names = append(names, name)
				}
			}
			c.mu.Lock()
			c.pinPlugins(names)
			c.mu.Unlock()
		}
	}
	c.mu.Lock()
	live := plugins.Live{VPN: c.vpnHeader(time.Now())}
	c.mu.Unlock()
	if c.cfg().MailOn() {
		live.Mail = c.mailLive()
	}
	if c.cfg().ChatOn() {
		live.Chat = c.chatLive()
	}
	if c.cfg().JamminOn() {
		live.Jammin = c.jamminLive()
	}
	all, bundles := plugins.Here(plugins.Env{Config: *c.cfg(), Paths: c.paths, Live: live})
	return ipc.PluginsResult{Plugins: all, Bundles: bundles, Text: plugins.AllText(all, bundles)}, nil
}
