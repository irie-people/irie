package core

import (
	"errors"
	"path/filepath"

	"github.com/irie-people/irie/internal/browser"
	"github.com/irie-people/irie/internal/harness"
	"github.com/irie-people/irie/internal/repo"
)

// browserServer is the browser connector a session gets (DESIGN §4.15):
// Irie's own MCP server, `irie browser`, with its browser's profile in the
// session's folder, made fresh at each start and removed after. A session
// gets it while browser.connector is on and the browser is found; on with
// no browser found is a warning on the session's row instead, which says
// how to install it.
func (c *Core) browserServer(id string) (*harness.MCPServer, string) {
	if !c.cfg().BrowserOn() {
		return nil, ""
	}
	b := c.cfg().Browser
	path := b.Path
	if path != "" {
		path = repo.Expand(path, home())
	}
	if _, err := browser.Find(path); err != nil {
		if errors.Is(err, browser.ErrNotFound) {
			return nil, "browser off: Mullvad Browser is not installed (irie setup browser)"
		}
		return nil, "browser off: browser.path: " + err.Error()
	}
	o := browser.Options{
		Path:     path,
		Profile:  filepath.Join(c.paths.Session(id), "browser"),
		Template: c.paths.BrowserTemplate(),
		Headless: b.Headless,
		Idle:     b.Idle,
		Proxy:    b.Proxy,
		Search:   b.Search,
	}
	return &harness.MCPServer{Command: c.bin, Args: browser.Args(o)}, ""
}
