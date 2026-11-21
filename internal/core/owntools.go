package core

import (
	"github.com/irie-people/irie/internal/harness"
	"github.com/irie-people/irie/internal/plugins"
)

// ownToolsMCP adds your plugins' tools to a session's servers, mcp, and
// returns them (DESIGN §4.17): the tools of each plugin of yours that is on,
// whose table reads and whose program is here. Irie's own servers are in mcp
// first and keep their names, so tools of yours with one of them are left
// out, as tools whose program is missing are; the plugin's part says why,
// and the core's log names the session. An anonymous session never comes
// here. The caller holds c.mu.
func (c *Core) ownToolsMCP(x *session, mcp map[string]harness.MCPServer) map[string]harness.MCPServer {
	for _, t := range plugins.Tools(plugins.Env{Config: *c.cfg(), Paths: c.paths}) {
		if !t.On || t.Problem != "" {
			continue
		}
		id := t.Plugin + "." + t.Name
		if _, taken := mcp[t.Name]; taken {
			c.log.Warn("tools of yours left out: a server of the session's own has their name", "session", x.id, "tools", id)
			continue
		}
		prog, err := plugins.ToolsProgram(t)
		if err != nil {
			c.log.Warn("tools of yours left out", "session", x.id, "tools", id, "err", err)
			continue
		}
		if mcp == nil {
			mcp = map[string]harness.MCPServer{}
		}
		mcp[t.Name] = harness.MCPServer{Command: prog, Args: append([]string{}, t.Run[1:]...)}
	}
	return mcp
}
