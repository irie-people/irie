package core

import (
	"strings"

	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/tmux"
)

// The slot's two views of the session you picked (DESIGN §4.7): its
// conversation, which the placeholder pane draws, or its raw terminal,
// its own pane swapped in. ui.flip_key flips between them. While the Mac
// app is open the slot shows the raw terminal, since the app draws the
// conversation itself.

// Server options that keep the view for a core that restarts.
const (
	optSelected = "@irie_selected"
	optRaw      = "@irie_raw"
)

// rawView reports whether the slot shows the picked session's raw
// terminal. The caller holds c.mu.
func (c *Core) rawView() bool { return c.raw || c.appViews > 0 }

// loadView takes up the view a previous core kept, or else starts with the
// configured one. The caller holds c.mu.
func (c *Core) loadView() {
	c.raw = c.cfg().UI.View == "terminal"
	out, err := c.tmux.Run("display-message", "-p", "#{"+optSelected+"} #{"+optRaw+"}")
	if err != nil {
		return
	}
	selected, raw, _ := strings.Cut(strings.TrimSpace(out), " ")
	if sessionID.MatchString(selected) {
		c.selected = selected
	} else if snap, err := c.tmux.Snapshot(); err == nil {
		// No view kept yet: the session whose terminal the slot shows is
		// the one picked.
		if slot := snap.SlotPane(); slot != nil && slot.Role == tmux.RoleAgent && sessionID.MatchString(slot.IrieSession) {
			c.selected = slot.IrieSession
		}
	}
	switch raw {
	case "1":
		c.raw = true
	case "0":
		c.raw = false
	}
}

// viewCmds are the commands that show session x as the view says: its raw
// terminal swapped into the slot, or its conversation, which the
// placeholder draws, back in the slot. They also keep the view for a core
// that restarts. The caller holds c.mu.
func (c *Core) viewCmds(x *session, focus bool) ([][]string, error) {
	var cmds [][]string
	if c.rawView() {
		swap, err := tmux.SwapIn(c.snap, x.pane, focus)
		if err != nil {
			return nil, err
		}
		cmds = swap
	} else {
		if slot := c.snap.SlotPane(); slot != nil && slot.Role != tmux.RolePlaceholder {
			out, err := tmux.SwapOut(c.snap, slot.ID)
			if err != nil {
				return nil, err
			}
			cmds = append(cmds, out...)
		}
		if focus {
			cmds = append(cmds, []string{"select-pane", "-t", tmux.Slot})
		}
	}
	return append(cmds, c.keepCmds()...), nil
}

// keepCmds record the view in the server's options. The caller holds c.mu.
func (c *Core) keepCmds() [][]string {
	raw := "0"
	if c.raw {
		raw = "1"
	}
	return [][]string{{"set-option", "-g", optSelected, c.selected}, {"set-option", "-g", optRaw, raw}}
}

// keepView keeps the slot on the view: an agent shown while the view is
// the conversation goes home, as when the Mac app closes, and the picked
// session's terminal comes in while the view is raw, as when the app
// opens. A picked session that is gone is forgotten. The caller holds c.mu.
func (c *Core) keepView() {
	slot := c.snap.SlotPane()
	if slot == nil {
		return
	}
	x := c.sessions[c.selected]
	if c.selected != "" && x == nil {
		c.selected = ""
	}
	var cmds [][]string
	switch {
	case c.rawView() && x != nil && slot.Role == tmux.RolePlaceholder:
		cmds, _ = tmux.SwapIn(c.snap, x.pane, false)
	case !c.rawView() && slot.Role != tmux.RolePlaceholder:
		cmds, _ = tmux.SwapOut(c.snap, slot.ID)
	}
	if len(cmds) == 0 {
		return
	}
	if _, err := c.tmux.RunList(cmds); err != nil {
		c.log.Warn("keep the view", "err", err)
		return
	}
	if err := c.refresh(); err != nil {
		c.log.Warn("poll", "err", err)
	}
}

// flip turns the slot's view of the picked session between its
// conversation and its raw terminal, and gives the slot the keyboard.
func (c *Core) flip() (ipc.FlipResult, error) {
	c.mu.Lock()
	c.raw = !c.raw
	var err error
	if x := c.sessions[c.selected]; x != nil {
		err = c.swapIn(x.id, true)
	} else if _, e := c.tmux.RunList(c.keepCmds()); e != nil {
		err = e
	}
	res := ipc.FlipResult{Terminal: c.rawView()}
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	return res, err
}
