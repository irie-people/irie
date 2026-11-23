package core

// The layout's copy (DESIGN §4.1, §4.5). The list's order, its spaces, its
// groups and the rows' colours live in user options of the agents' tmux
// session, so a core that restarts keeps them. When the tmux server itself
// ends, as when the Mac restarts or loses power, they end with it, and the
// sessions lost with it would come back in the first space, in no group,
// in the order they started. So the core keeps a copy of each of those
// options in the state folder, written whenever it reads one from tmux or
// sets one. A core that finds one unset, as on a new server, takes the
// copy's value and puts it back in tmux. A lost session then comes back to
// its place, its space and its group: reopen keeps its id, which the order
// and the colours name, and its closed record keeps its space and group
// (lost.go). tmux stays the one the core goes by: the copy is read only
// when tmux has nothing.

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/irie-people/irie/internal/filetx"
)

// layoutFile is the copy's name in the state folder: a JSON object from
// each option's name to its value, as tmux holds it.
const layoutFile = "layout.json"

// layoutPath is where the copy is.
func (c *Core) layoutPath() string { return filepath.Join(c.paths.State, layoutFile) }

// readLayout reads the copy, or nothing when there is none or it is bad,
// or the core has no state folder to keep one in, as some tests make it.
func (c *Core) readLayout() map[string]string {
	if c.paths.State == "" {
		return nil
	}
	b, err := os.ReadFile(c.layoutPath())
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			c.log.Warn("layout", "err", err)
		}
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		c.log.Warn("layout", "err", err)
		return nil
	}
	return m
}

// layoutOption reads layout option opt of the agents' session and keeps it
// in the copy. When tmux has none, as a new server has none, it is the
// copy's value, which goes back into tmux too. It runs as the core starts.
func (c *Core) layoutOption(opt string) (string, error) {
	out, err := c.tmux.Run("show-options", "-qv", "-t", agentsTarget, opt)
	if err != nil {
		return "", err
	}
	if v := strings.TrimSpace(out); v != "" {
		c.keepLayout(opt, v)
		return v, nil
	}
	v := strings.TrimSpace(c.readLayout()[opt])
	if v == "" {
		return "", nil
	}
	if _, err := c.tmux.Run("set-option", "-t", agentsTarget, opt, v); err != nil {
		c.log.Warn("layout", "option", opt, "err", err)
	}
	c.log.Info("layout: from its copy", "option", opt)
	return v, nil
}

// setLayoutOption sets layout option opt of the agents' session to value,
// and keeps it in the copy. The caller holds c.mu.
func (c *Core) setLayoutOption(opt, value string) error {
	_, err := c.tmux.Run("set-option", "-t", agentsTarget, opt, value)
	c.keepLayout(opt, strings.TrimSpace(value))
	return err
}

// keepLayout writes value as option opt's in the copy, unless it is there
// already or the core has no state folder. The caller holds c.mu, or the
// core is starting.
func (c *Core) keepLayout(opt, value string) {
	if c.paths.State == "" {
		return
	}
	if c.layout == nil {
		if c.layout = c.readLayout(); c.layout == nil {
			c.layout = map[string]string{}
		}
	}
	if v, ok := c.layout[opt]; ok && v == value {
		return
	}
	c.layout[opt] = value
	b, err := json.Marshal(c.layout)
	if err == nil {
		err = filetx.WriteFile(c.layoutPath(), b, 0o600)
	}
	if err != nil {
		c.log.Warn("layout", "err", err)
	}
}
