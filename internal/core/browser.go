//go:build !noprivacy

package core

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/browser"
	"github.com/irie-people/irie/internal/harness"
	"github.com/irie-people/irie/internal/repo"
	"github.com/irie-people/irie/internal/tmux"
)

// The shared browser server's window in irie-agents, and its pane's role.
const (
	browserWindow = brand.Slug + "-browser"
	roleBrowser   = "browser"
)

// browserServer is the browser connector a session gets (DESIGN §4.15):
// Irie's own MCP server, with the session's browser's profile in the
// session's folder, made fresh at each start and removed after. That is
// the shared server, `irie browser serve`, one for every session, while it
// runs; else, as for the session that starts it, an `irie browser` of the
// session's own. A session gets it while browser.connector is on and the
// browser is found; on with no browser found is a warning on the session's
// row instead, which says how to install it. env is the agent's
// environment.
func (c *Core) browserServer(id string, env []string) (*harness.MCPServer, string) {
	if !c.cfg().BrowserOn() {
		return nil, ""
	}
	o := c.browserOptions()
	if _, err := browser.Find(o.Path); err != nil {
		if errors.Is(err, browser.ErrNotFound) {
			return nil, "browser off: Mullvad Browser is not installed (" + brand.Slug + " setup browser)"
		}
		return nil, "browser off: browser.path: " + err.Error()
	}
	if srv := c.sharedBrowser(id, env); srv != nil {
		return srv, ""
	}
	o.Profile = filepath.Join(c.paths.Session(id), "browser")
	return &harness.MCPServer{Command: c.bin, Args: browser.Args(o)}, ""
}

// browserOptions are every session's browser's options, from [browser].
func (c *Core) browserOptions() browser.Options {
	b := c.cfg().Browser
	path := b.Path
	if path != "" {
		path = repo.Expand(path, home())
	}
	return browser.Options{
		Path:     path,
		Template: c.paths.BrowserTemplate(),
		Headless: b.Headless,
		Idle:     b.Idle,
		Proxy:    b.Proxy,
		Search:   b.Search,
	}
}

// sharedBrowser is session id's entry for the shared browser server: its
// address, and the session's token, which reaches its own browser and tabs
// alone. It is nil while the server doesn't answer yet; the first session
// that wants it starts it, and keepBrowser keeps it from then on. An agent
// that would send a request to 127.0.0.1 through a proxy gets none either.
// The caller holds c.mu.
func (c *Core) sharedBrowser(id string, env []string) *harness.MCPServer {
	if proxied(env) {
		return nil
	}
	panes := c.snap.ByRole(roleBrowser)
	if len(panes) == 0 {
		c.startBrowser()
		return nil
	}
	if panes[0].Dead {
		return nil
	}
	dir := c.paths.BrowserServer()
	st, err := browser.ReadStatus(dir)
	if err != nil || st.Addr == "" || st.PID <= 1 || syscall.Kill(st.PID, 0) != nil {
		return nil
	}
	key, err := browser.LoadKey(dir)
	if err != nil {
		c.log.Warn("browser: the shared server's key", "err", err)
		return nil
	}
	return &harness.MCPServer{
		Type:    harness.MCPHTTP,
		URL:     "http://" + st.Addr + browser.Endpoint,
		Headers: map[string]string{"Authorization": "Bearer " + browser.Token(key, id)},
		Timeout: browser.CallTimeout,
	}
}

// proxied reports whether a program with env, such as Claude Code, would
// send a request for 127.0.0.1 through a proxy: one is set, and NO_PROXY
// doesn't leave the address out.
func proxied(env []string) bool {
	set, skip := false, false
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(k) {
		case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY":
			set = set || v != ""
		case "NO_PROXY":
			for _, h := range strings.Split(v, ",") {
				if h = strings.TrimSpace(h); h == "*" || h == "127.0.0.1" {
					skip = true
				}
			}
		}
	}
	return set && !skip
}

// browserServe is the shared server's command line.
func (c *Core) browserServe() []string {
	so := browser.SharedOptions{Options: c.browserOptions(), Sessions: c.paths.Sessions(), Dir: c.paths.BrowserServer()}
	return append([]string{c.bin}, browser.ServeArgs(so)...)
}

// browserSpec is what the shared server runs with, which its pane keeps in
// @irie_version: a server of another version, or of other settings, is
// started anew once it has no browser open.
func (c *Core) browserSpec() string {
	return c.version + " " + strings.Join(c.browserServe()[1:], " ")
}

// startBrowser starts the shared browser server in a window of its own,
// unless one started since the last snapshot. The caller holds c.mu.
func (c *Core) startBrowser() {
	if roles, err := c.tmux.Run("list-panes", "-a", "-F", "#{"+tmux.OptRole+"}"); err != nil || slices.Contains(strings.Fields(roles), roleBrowser) {
		return
	}
	out, err := c.tmux.Run(append([]string{"new-window", "-d", "-t", tmux.AgentsSession + ":", "-n", browserWindow, "-P", "-F", "#{pane_id}", "--"}, c.browserServe()...)...)
	if err != nil {
		c.log.Warn("browser: start the shared server", "err", err)
		return
	}
	pane := strings.TrimSpace(out)
	if _, err := c.tmux.RunList([][]string{
		{"set-option", "-p", "-t", pane, tmux.OptRole, roleBrowser},
		{"set-option", "-p", "-t", pane, tmux.OptVersion, c.browserSpec()},
	}); err != nil {
		c.log.Warn("browser: the shared server's pane", "err", err)
	}
	c.log.Info("browser: the shared server starts", "pane", pane)
}

// keepBrowser keeps the shared browser server once a session started it.
// While browser.connector is on, it starts it again when it exited. A
// server of another version, as after an install, or of other settings is
// asked to retire (SIGUSR1): it exits once no browser of its is open and
// no call is under way, so no agent loses its tabs, and is started anew.
// With the connector off it retires too, and its pane goes. Like every
// pane of Irie's own, it is never an agent's. The caller holds c.mu.
func (c *Core) keepBrowser() {
	panes := c.snap.ByRole(roleBrowser)
	if len(panes) == 0 {
		return
	}
	var cmds [][]string
	for _, p := range panes[1:] {
		cmds = append(cmds, []string{"kill-pane", "-t", p.ID})
	}
	p, on, spec := panes[0], c.cfg().BrowserOn(), c.browserSpec()
	switch {
	case p.Dead && on:
		cmds = append(cmds, append([]string{"respawn-pane", "-k", "-t", p.ID, "--"}, c.browserServe()...),
			[]string{"set-option", "-p", "-t", p.ID, tmux.OptVersion, spec})
		c.log.Info("browser: the shared server starts again", "pane", p.ID, "status", p.DeadStatus)
	case p.Dead:
		cmds = append(cmds, []string{"kill-pane", "-t", p.ID})
	case !on || p.Version != spec:
		if p.PID > 1 {
			syscall.Kill(p.PID, syscall.SIGUSR1)
		}
	}
	if len(cmds) > 0 {
		if _, err := c.tmux.RunList(cmds); err != nil {
			c.log.Warn("browser: keep the shared server", "err", err)
		}
	}
}

// privacyBuilt says whether this build has the privacy plugin (DESIGN
// §4.17); privacy_off.go says not, for go build -tags noprivacy.
const privacyBuilt = true

// browserMCP is the session's MCP servers for Irie's browser, by name, or
// nil and why not. env is the agent's environment.
func (c *Core) browserMCP(id string, env []string) (map[string]harness.MCPServer, string) {
	srv, warn := c.browserServer(id, env)
	if warn != "" || srv == nil {
		return nil, warn
	}
	return map[string]harness.MCPServer{browser.ServerName: *srv}, ""
}
