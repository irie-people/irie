package core

// The list's order (DESIGN §4.1). The core keeps one order, which both
// sidebars draw: a new session goes last, the user drags one elsewhere
// (reorder) or promotes it to the top, and a state change never moves one.
// The order lives in a user option of the agents' tmux session, as the
// registry lives in pane options, so a restarted core keeps it. A closed
// session keeps its place for its reopen.

import (
	"cmp"
	"slices"
	"strings"

	"github.com/irie-hq/irie/internal/event"
	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/tmux"
)

// optOrder holds the order: session ids, space-separated, from the top.
const optOrder = "@irie_order"

// agentsTarget is exactly the agents' session, for its options.
const agentsTarget = "=" + tmux.AgentsSession + ":"

// loadOrder reads the order a previous core kept.
func (c *Core) loadOrder() {
	out, err := c.tmux.Run("show-options", "-qv", "-t", agentsTarget, optOrder)
	if err != nil {
		c.log.Warn("order", "err", err)
		return
	}
	for _, id := range strings.Fields(out) {
		if sessionID.MatchString(id) && !slices.Contains(c.order, id) {
			c.order = append(c.order, id)
		}
	}
}

// sortRows puts rows in the list's order and numbers their places: the
// sessions the order names first, as it names them, then the rest in the
// order they started. The caller holds c.mu.
func (c *Core) sortRows(rows []ipc.Session) {
	pos := make(map[string]int, len(c.order))
	for i, id := range c.order {
		pos[id] = i
	}
	slices.SortFunc(rows, func(a, b ipc.Session) int {
		pa, oka := pos[a.ID]
		pb, okb := pos[b.ID]
		switch {
		case oka && okb:
			return cmp.Compare(pa, pb)
		case oka:
			return -1
		case okb:
			return 1
		}
		if n := a.Created.Compare(b.Created); n != 0 {
			return n
		}
		return strings.Compare(a.ID, b.ID)
	})
	for i := range rows {
		rows[i].Order = i
	}
}

// fullOrder is the order as it stands, with every live session in it and
// the closed ones it holds kept in their places. The caller holds c.mu.
func (c *Core) fullOrder() []string {
	closed := map[string]bool{}
	for _, r := range c.closes.list {
		closed[r.ID] = true
	}
	var ids []string
	for _, id := range c.order {
		if c.sessions[id] != nil || closed[id] {
			ids = append(ids, id)
		}
	}
	for _, s := range c.list() {
		if !slices.Contains(ids, s.ID) {
			ids = append(ids, s.ID)
		}
	}
	return ids
}

// setOrder makes ids the order and keeps it in tmux. The caller holds c.mu.
func (c *Core) setOrder(ids []string) {
	c.order = ids
	if _, err := c.tmux.Run("set-option", "-t", agentsTarget, optOrder, strings.Join(ids, " ")); err != nil {
		c.log.Warn("order", "err", err)
	}
}

// reorder puts a session just before another in the list, or last.
func (c *Core) reorder(a ipc.ReorderArgs) error {
	c.mu.Lock()
	err := c.putBefore(a.Session, a.Before)
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	return err
}

// putBefore puts session id just before session before, or last when
// before is empty. The caller holds c.mu.
func (c *Core) putBefore(id, before string) error {
	x := c.sessions[id]
	if x == nil {
		return ipc.Errorf(ipc.CodeNotFound, "no session %q", id)
	}
	if before != "" && c.sessions[before] == nil {
		return ipc.Errorf(ipc.CodeNotFound, "no session %q", before)
	}
	if before == id {
		return nil
	}
	ids := slices.DeleteFunc(c.fullOrder(), func(s string) bool { return s == id })
	at := len(ids)
	if before != "" {
		at = slices.Index(ids, before)
	}
	c.setOrder(slices.Insert(ids, at, id))
	c.record(x, event.Event{Source: event.SourceUser, Kind: event.UIReordered, Data: map[string]any{"before": before}})
	return nil
}

// toTop puts session id first in the list. The caller holds c.mu.
func (c *Core) toTop(id string) {
	ids := slices.DeleteFunc(c.fullOrder(), func(s string) bool { return s == id })
	c.setOrder(slices.Insert(ids, 0, id))
}
