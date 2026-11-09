package core

import (
	"encoding/json"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/plugins"
)

// pluginsOp answers the plugins op (DESIGN §4.17): Irie's plugins and how
// far each part is set up on this computer, with what the core knows now
// of the VPN, after turning a plugin or a part on or off when asked. It
// reads files only, never the network or an account.
func (c *Core) pluginsOp(a ipc.PluginsArgs) (ipc.PluginsResult, error) {
	if a.On != nil {
		edits, err := plugins.Edits(a.Plugin, a.Part, *a.On)
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
	all := plugins.Status(plugins.Env{Config: *c.cfg(), Paths: c.paths, Live: live})
	return ipc.PluginsResult{Plugins: all, Text: plugins.Text(all)}, nil
}
