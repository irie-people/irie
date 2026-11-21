package core

// Pinning (DESIGN §4.1). A pinned session waits in the Pinned group at the
// top of the list, Parked's opposite, and no close reaches it but your own
// of it alone: not Close Others, Close Below or Close All Finished, nor
// spawn.close_when_done. Pinning puts it last in Pinned, and unpinning
// puts it first among the rest, just under Pinned, where the eye already
// is. A session is pinned or parked, never both: pinning a parked one
// unparks it, and parking a pinned one unpins it. Pins are in the journal
// (ui.pinned, ui.unpinned), as parks are, so a restarted core keeps them,
// and each pin or unpin you ask for is a step of the history (§4.5).

import (
	"fmt"
	"slices"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
)

// pin pins a session (on) or unpins it, and puts it last in Pinned, or
// first among the rest; or, for a drag across Pinned's edge, just before
// a.Before. Pinning one that is pinned already, or unpinning one that is
// not, changes nothing. When it changed something, step is what undoing
// and redoing it need: the sessions that came after it before and now,
// and whether the pin unparked it. It records no step of its own: the op
// does (control.go).
func (c *Core) pin(a ipc.ParkArgs, on bool) (step map[string]any, err error) {
	c.mu.Lock()
	defer func() {
		frames := c.changes()
		c.mu.Unlock()
		c.publish(frames)
	}()
	x := c.sessions[a.Session]
	if x == nil {
		return nil, ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Session)
	}
	if a.Before != "" && c.sessions[a.Before] == nil {
		return nil, ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Before)
	}
	if x.track.pinned == on {
		return nil, nil
	}
	from, unparks := c.nextOf(x.id), on && x.track.parked
	if unparks {
		c.setParked(x.id, false)
	}
	c.setPinned(x.id, on)
	switch {
	case a.Before != "" && a.Before != x.id:
		c.putBefore(x.id, a.Before)
	case on:
		c.toEnd(x.id)
	default:
		c.toFirstUnpinned(x.id)
	}
	step = map[string]any{"from": from, "to": c.nextOf(x.id)}
	if unparks {
		step["parked"] = true
	}
	return step, nil
}

// setPinned pins or unpins session id, and reports whether that changed
// anything. The caller holds c.mu.
func (c *Core) setPinned(id string, on bool) bool {
	x := c.sessions[id]
	if x == nil || x.track.pinned == on {
		return false
	}
	kind := event.UIPinned
	if !on {
		kind = event.UIUnpinned
	}
	c.record(x, event.Event{Source: event.SourceUser, Kind: kind})
	return true
}

// toTop sends session id, as a spawn with SpawnArgs.Top asks, to the top
// (DESIGN §4.1): pinned, and first in Pinned, so first in the list of
// whichever space it is in, over every card; and boosted (§4.3). Each is
// recorded as the user's, as their own pin and boost are, so a restarted
// core keeps both, and the boost lasts until they stop it. A session is
// pinned or parked, never both, so a parked one is unparked. It records no
// step of the history: the spawn's is the step, whose undo closes the
// session, and the card's own Unpin and Stop Boosting undo the rest. The
// caller holds c.mu.
func (c *Core) toTop(id string) {
	x := c.sessions[id]
	if x == nil {
		return
	}
	if x.track.parked {
		c.setParked(id, false)
	}
	c.setPinned(id, true)
	ids := slices.DeleteFunc(c.fullOrder(), func(s string) bool { return s == id })
	c.setOrder(slices.Insert(ids, 0, id))
	c.setLevel(x, 1, "sent to the top")
}

// toFirstUnpinned puts session id first among this machine's sessions that
// are neither pinned nor parked: just under Pinned; one of a group of
// yours, last in the innermost group around it that holds another card,
// where the group is (groups.go). The caller holds c.mu.
func (c *Core) toFirstUnpinned(id string) {
	if x := c.sessions[id]; x != nil {
		if last := c.edgeIn(x, true); last != "" {
			c.gather([]string{id}, last, "")
			return
		}
	}
	ids := slices.DeleteFunc(c.fullOrder(), func(s string) bool { return s == id })
	at := len(ids)
	for i, s := range ids {
		if y := c.sessions[s]; y != nil && !y.track.pinned && !y.track.parked {
			at = i
			break
		}
	}
	c.setOrder(slices.Insert(ids, at, id))
}

// Action kinds of pin (undo.go).
const (
	actPin   = "pin"
	actUnpin = "unpin"
)

// Undoing a pin unpins the session in the place it left, parked again if
// the pin unparked it, and redoing it pins it again where the pin put it;
// an unpin the same way round.
func init() {
	registerAction(actPin, actionKind{verb: "pin",
		undo: effect{check: checkPin(false), do: doPin(false, "from")},
		redo: effect{check: checkPin(true), do: doPin(true, "to")}})
	registerAction(actUnpin, actionKind{verb: "unpin",
		undo: effect{check: checkPin(true), do: doPin(true, "from")},
		redo: effect{check: checkPin(false), do: doPin(false, "to")}})
}

// checkPin is what makes pinning a's session (on), or unpinning it,
// uncertain: that it is closed or gone, or already as the step would leave
// it. A new name is no matter; the step takes it (doPin).
func checkPin(on bool) func(c *Core, a action) []problem {
	verb, already := "unpin", "is not pinned any more"
	if on {
		verb, already = "pin", "is pinned already"
	}
	return func(c *Core, a action) []problem {
		x := c.sessions[a.Session]
		if x == nil {
			if _, ok := c.closedRecord(a.Session); ok {
				return []problem{{code: "closed", question: fmt.Sprintf("%q is closed, so there is nothing to %s.", c.nameOf(a.Session), verb), recommend: ipc.ChoiceSkip}}
			}
			return []problem{gone(a)}
		}
		if x.track.pinned == on {
			return []problem{{code: "already", question: fmt.Sprintf("%q %s.", x.name, already),
				options: []ipc.ChoiceOption{{Key: ipc.ChoiceSettle, Label: "Leave it as it is and move on",
					Detail: fmt.Sprintf("Nothing needs %sning, so the history moves on as if "+brand.Name+" had %sned it.", verb, verb)}},
				recommend: ipc.ChoiceSettle}}
		}
		return nil
	}
}

// doPin pins a's session (on) or unpins it, parks or unparks it again when
// the step's pin unparked it, and puts it in the place its data keeps as k.
// It takes the name the session has now.
func doPin(on bool, k string) func(c *Core, a action) (action, error) {
	return func(c *Core, a action) (action, error) {
		c.mu.Lock()
		x := c.sessions[a.Session]
		if x == nil {
			c.mu.Unlock()
			return a, ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Session)
		}
		if on && a.flag("parked") {
			c.setParked(x.id, false)
		}
		c.setPinned(x.id, on)
		if !on && a.flag("parked") {
			c.setParked(x.id, true)
		}
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
