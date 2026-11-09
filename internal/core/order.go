package core

// The list's order (DESIGN §4.1). The core keeps one order, which both
// sidebars draw: a new session goes last, the user drags one elsewhere
// (reorder) or promotes it to the top, and a state change never moves one.
// The order lives in a user option of the agents' tmux session, as the
// registry lives in pane options, so a restarted core keeps it. A closed
// session keeps its place for its reopen.
//
// Parking a session puts it last in the Parked group at the end of the
// list, and unparking it puts it last among the rest, just above Parked:
// either way it goes to the end of the one order, which the sidebars draw
// with the parked sessions apart. Park is in the journal (ui.parked,
// ui.unparked), which a restarted core replays. A park or unpark the user
// asks for is a step of the history (DESIGN §4.5), so undo takes it back,
// to the place it left.

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/tmux"
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

// reorder puts a session just before another in the list, or last. When
// that moved it, step is what undoing it needs (§4.5): the sessions that
// came after it before, and those that do now.
func (c *Core) reorder(a ipc.ReorderArgs) (step map[string]any, err error) {
	c.mu.Lock()
	from, was := c.nextOf(a.Session), c.fullOrder()
	err = c.putBefore(a.Session, a.Before)
	if err == nil && !slices.Equal(was, c.fullOrder()) {
		step = map[string]any{"from": from, "to": c.nextOf(a.Session)}
	}
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	return step, err
}

// placeAnchors is how many of the sessions after one a step keeps to find
// its place again by: should some of them go, the first still there will do.
const placeAnchors = 3

// nextOf are the sessions that follow session id in the order, the nearest
// first, placeAnchors of them at most. The caller holds c.mu.
func (c *Core) nextOf(id string) []string {
	ids := c.fullOrder()
	i := slices.Index(ids, id)
	if i < 0 {
		return []string{}
	}
	return slices.Clone(ids[i+1 : min(len(ids), i+1+placeAnchors)])
}

// placeFor is where putting id back by next puts it: in the order without
// it, rest, just before the first of next that rest has, or last.
func placeFor(ids []string, id string, next []string) (rest []string, at int) {
	rest = slices.DeleteFunc(slices.Clone(ids), func(s string) bool { return s == id })
	for _, n := range next {
		if i := slices.Index(rest, n); i >= 0 {
			return rest, i
		}
	}
	return rest, len(rest)
}

// placed reports whether session id is where putting it back by next would
// put it. The caller holds c.mu.
func (c *Core) placed(id string, next []string) bool {
	ids := c.fullOrder()
	_, at := placeFor(ids, id, next)
	return slices.Index(ids, id) == at
}

// putBack puts session id back in the place next names (placeFor), and
// records it as reordered by the user, as undo and redo are the user's.
// The caller holds c.mu.
func (c *Core) putBack(id string, next []string) {
	rest, at := placeFor(c.fullOrder(), id, next)
	c.setOrder(slices.Insert(rest, at, id))
	if x := c.sessions[id]; x != nil {
		before := ""
		if at < len(rest) {
			before = rest[at]
		}
		c.record(x, event.Event{Source: event.SourceUser, Kind: event.UIReordered, Data: map[string]any{"before": before}})
	}
}

// Action kind of reorder (undo.go): undo puts the session back where it
// was, and redo where the user put it.
const actReorder = "reorder"

func init() {
	registerAction(actReorder, actionKind{verb: "reorder",
		undo: effect{check: checkPlace("from"), do: doPlace("from")},
		redo: effect{check: checkPlace("to"), do: doPlace("to")}})
}

// checkPlace is what makes putting a's session in the place its data keeps
// as k uncertain: that it is closed or gone, or is in that place already.
func checkPlace(k string) func(c *Core, a action) []problem {
	return func(c *Core, a action) []problem {
		x := c.sessions[a.Session]
		if x == nil {
			return notOpen(c, a, "reorder")
		}
		if c.placed(x.id, a.strs(k)) {
			return []problem{alreadyDone(fmt.Sprintf("%q is in that place already.", x.name))}
		}
		return nil
	}
}

// doPlace puts a's session in the place its data keeps as k.
func doPlace(k string) func(c *Core, a action) (action, error) {
	return func(c *Core, a action) (action, error) {
		c.mu.Lock()
		x := c.sessions[a.Session]
		if x == nil {
			c.mu.Unlock()
			return a, ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Session)
		}
		c.putBack(x.id, a.strs(k))
		a.Name = x.name
		frames := c.changes()
		c.mu.Unlock()
		c.publish(frames)
		return a, nil
	}
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

// toEnd puts session id last in the list. The caller holds c.mu.
func (c *Core) toEnd(id string) {
	ids := slices.DeleteFunc(c.fullOrder(), func(s string) bool { return s == id })
	c.setOrder(append(ids, id))
}

// park parks a session (on) or unparks it, and puts it last in the order,
// so it goes to the end of the group it joins, Parked or the rest; or,
// for a drag across Parked's edge, just before a.Before. Parking one that
// is parked already, or unparking one that is not, changes nothing and
// moves nothing. When it changed something, step is what undoing and
// redoing it need (§4.5): the sessions that came after it before, and
// those that do now. It records no step of its own: the op does
// (control.go).
func (c *Core) park(a ipc.ParkArgs, on bool) (step map[string]any, err error) {
	c.mu.Lock()
	defer func() {
		frames := c.changes()
		c.mu.Unlock()
		c.publish(frames)
	}()
	if a.Before != "" && c.sessions[a.Before] == nil {
		return nil, ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Before)
	}
	from := c.nextOf(a.Session)
	changed, err := c.setParked(a.Session, on)
	if !changed {
		return nil, err
	}
	if a.Before != "" && a.Before != a.Session {
		c.putBefore(a.Session, a.Before)
	} else {
		c.toEnd(a.Session)
	}
	return map[string]any{"from": from, "to": c.nextOf(a.Session)}, nil
}

// setParked parks or unparks session id, and reports whether that changed
// anything. The caller holds c.mu.
func (c *Core) setParked(id string, on bool) (bool, error) {
	x := c.sessions[id]
	if x == nil {
		return false, ipc.Errorf(ipc.CodeNotFound, "no session %q", id)
	}
	if x.track.parked == on {
		return false, nil
	}
	kind := event.UIParked
	if !on {
		kind = event.UIUnparked
	}
	c.record(x, event.Event{Source: event.SourceUser, Kind: kind})
	return true, nil
}

// Action kinds of park (undo.go).
const (
	actPark   = "park"
	actUnpark = "unpark"
)

// Undoing a park unparks the session in the place it left, and redoing it
// parks it again where the park put it; an unpark the same way round.
func init() {
	registerAction(actPark, actionKind{verb: "park",
		undo: effect{check: checkPark(false), do: doPark(false, "from")},
		redo: effect{check: checkPark(true), do: doPark(true, "to")}})
	registerAction(actUnpark, actionKind{verb: "unpark",
		undo: effect{check: checkPark(true), do: doPark(true, "from")},
		redo: effect{check: checkPark(false), do: doPark(false, "to")}})
}

// checkPark is what makes parking a's session (on), or unparking it,
// uncertain: that it is closed or gone, or already as the step would leave
// it. A new name is no matter; the step takes it (doPark).
func checkPark(on bool) func(c *Core, a action) []problem {
	verb, already := "unpark", "is not parked any more"
	if on {
		verb, already = "park", "is parked already"
	}
	return func(c *Core, a action) []problem {
		x := c.sessions[a.Session]
		if x == nil {
			if _, ok := c.closedRecord(a.Session); ok {
				return []problem{{code: "closed", question: fmt.Sprintf("%q is closed, so there is nothing to %s.", c.nameOf(a.Session), verb), recommend: ipc.ChoiceSkip}}
			}
			return []problem{gone(a)}
		}
		if x.track.parked == on {
			return []problem{{code: "already", question: fmt.Sprintf("%q %s.", x.name, already),
				options: []ipc.ChoiceOption{{Key: ipc.ChoiceSettle, Label: "Leave it as it is and move on",
					Detail: fmt.Sprintf("Nothing needs %sing, so the history moves on as if Irie had %sed it.", verb, verb)}},
				recommend: ipc.ChoiceSettle}}
		}
		return nil
	}
}

// doPark parks a's session (on) or unparks it, and puts it in the place its
// data keeps as k: where the session was before the step (from), or where
// the step put it (to). A step kept from before park and unpark moved a
// session has no place, and the session stays where it is, as it did then.
// It takes the name the session has now.
func doPark(on bool, k string) func(c *Core, a action) (action, error) {
	return func(c *Core, a action) (action, error) {
		c.mu.Lock()
		x := c.sessions[a.Session]
		if x == nil {
			c.mu.Unlock()
			return a, ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Session)
		}
		c.setParked(x.id, on)
		if _, ok := a.Data[k]; ok {
			c.putBack(x.id, a.strs(k))
		}
		a.Name = x.name
		frames := c.changes()
		c.mu.Unlock()
		c.publish(frames)
		return a, nil
	}
}
