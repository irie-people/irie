package core

// A boost (DESIGN §4.3, M7), which the op and `ctl promote` still call
// promoting, gives a session's whole process tree the machine first, at
// once, with a badge on its card. It moves no card: the list's order is the
// user's alone (order.go), and undoing a boost gives back only the level.
// Without root no process can be raised above normal priority, so the
// others step back instead: while any session is promoted, every other
// session's tree runs in macOS's background state (proc.SetBackground), with
// the least CPU priority and throttled disk and network I/O, and what those
// trees start from then on inherits it. The promoted trees stay as they
// are, and so does the tab the user is on while they are at the computer
// (Gabriel's top, gabriel.go), which comes first of all. A raised nice
// value would serve CPU alone, and only root can lower it again. Putting a
// session back, or closing it, takes the others out of the background.
// What Irie hands out in turn, spawns with their worktrees and messages
// typed to agents, serves a promoted session first (turns.go). Memory
// cannot be set aside: macOS gives a process no way to reserve it for
// another, and the background processes are the ones it slows first.

import (
	"fmt"

	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/proc"
)

// bgProc is a process Irie put in the background.
type bgProc struct {
	start   int64  // tells it from a later process with its pid
	session string // whose tree it was in
}

// setPriority boosts a session (level 1) or puts it back (level 0), and
// moves no card. It takes effect before it returns. When the level
// changed, step is not nil, for the history (§4.5): undoing it gives back
// the level, and only the level.
func (c *Core) setPriority(a ipc.PriorityArgs) (step map[string]any, err error) {
	if a.Level < 0 || a.Level > 1 {
		return nil, ipc.Errorf(ipc.CodeBadArgs, "priority level %d: it is 0 or 1", a.Level)
	}
	c.mu.Lock()
	x := c.sessions[a.Session]
	if x == nil {
		c.mu.Unlock()
		return nil, ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Session)
	}
	if x.track.priority != a.Level {
		step = map[string]any{}
	}
	c.setLevel(x, a.Level, a.Reason)
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	return step, nil
}

// setLevel gives session x the priority level where it is in the list, and
// the machine to the promoted sessions. The caller holds c.mu.
func (c *Core) setLevel(x *session, level int, reason string) {
	if x.track.priority != level {
		data := map[string]any{"level": level}
		if reason != "" {
			data["reason"] = reason
		}
		c.record(x, event.Event{Source: event.SourceUser, Kind: event.PrioritySet, Data: data})
	}
	// The tab the user is on, Gabriel's top, stays out: he keeps it known
	// whenever they are at the computer.
	c.background()
}

// Action kinds of priority (undo.go): boosting a session, and putting it
// back. Each undoes the other's level, and only the level, as a boost moves
// no card. A step from before kept where a promotion moved the session from
// and whether it was parked, which undo now leaves be.
const (
	actPromote   = "promote"
	actUnpromote = "unpromote"
)

func init() {
	registerAction(actPromote, actionKind{verb: "boost",
		undo: effect{check: checkLevel(0), do: doLevel(0)},
		redo: effect{check: checkLevel(1), do: doLevel(1)}})
	registerAction(actUnpromote, actionKind{verb: "stop boosting",
		undo: effect{check: checkLevel(1), do: doLevel(1)},
		redo: effect{check: checkLevel(0), do: doLevel(0)}})
}

// checkLevel is what makes giving a's session the priority level uncertain:
// that it is closed or gone, or has that level already.
func checkLevel(level int) func(c *Core, a action) []problem {
	verb, already := "stop boosting", "is not boosted any more"
	if level > 0 {
		verb, already = "boost", "is boosted already"
	}
	return func(c *Core, a action) []problem {
		x := c.sessions[a.Session]
		if x == nil {
			return notOpen(c, a, verb)
		}
		if x.track.priority == level {
			return []problem{alreadyDone(fmt.Sprintf("%q %s.", x.name, already))}
		}
		return nil
	}
}

// doLevel gives a's session the priority level where it is, and changes
// nothing else.
func doLevel(level int) func(c *Core, a action) (action, error) {
	return func(c *Core, a action) (action, error) {
		c.mu.Lock()
		x := c.sessions[a.Session]
		if x == nil {
			c.mu.Unlock()
			return a, ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Session)
		}
		c.setLevel(x, level, "")
		a.Name = x.name
		frames := c.changes()
		c.mu.Unlock()
		c.publish(frames)
		return a, nil
	}
}

// background puts each session's process tree in the background, or takes
// it out, as the promotions and Gabriel want (stepsBack): while a live
// session is promoted, the others' trees are in the background; while
// Gabriel holds back (gabriel.go), so are those of the sessions out of the
// user's focus, and while the tab the user is on works the machine, every
// other but an install's. The tab they are on never is, nor a promoted
// tree but for that tab. A process Irie put there that left every tree,
// such as a daemon, comes out. The caller holds c.mu.
func (c *Core) background() {
	if !proc.CanBackground {
		return
	}
	raised, demoted := map[string]bool{}, len(c.bg) > 0
	for id, x := range c.sessions {
		if x.track.priority > 0 && !x.dead {
			raised[id] = true
		}
		demoted = demoted || x.demoted
	}
	if len(raised) == 0 && !demoted && !c.gab.holding {
		return
	}
	if len(raised) > 0 {
		// A promoted session waits on the subtasks it delegated as on its
		// own work.
		raised = c.withDelegates(raised)
	}
	t, err := proc.Read()
	if err != nil {
		c.log.Warn("background", "err", err)
		return
	}
	if c.bg == nil {
		c.bg = map[int]bgProc{}
	}
	inTree := map[int]bool{}
	own := c.ownName()
	for _, x := range c.sessions {
		p := c.snap.Pane(x.pane)
		if p == nil || p.Dead || p.PID <= 1 {
			// No process to move: what Irie put in the background from its
			// tree is let go below once it ends or leaves. A dead session
			// that kept the flag, as every session has from the core's
			// start, would have each pass read the whole process table.
			x.demoted = false
			continue
		}
		want := c.stepsBack(x.id, raised)
		for _, q := range t.Tree(p.PID) {
			inTree[q.PID] = true
			b, ok := c.bg[q.PID]
			ok = ok && b.start == q.Start
			// The agent keeps its network, and its own helpers theirs:
			// only the rest of the tree steps back (keepup.go). They come
			// out once, as this core finds the tree, from where a core
			// before it may have left them, and never go in.
			kept := keepsUp(t, p.PID, own, q)
			back := want && !kept
			switch {
			case back && !ok:
				if err := proc.SetBackground(q.PID, true); err == nil {
					c.bg[q.PID] = bgProc{start: q.Start, session: x.id}
				}
			case !back && (ok || x.demoted && (!kept || !x.keptOut)):
				// What it started while in the background inherited that,
				// so every process of a tree that was there comes out.
				proc.SetBackground(q.PID, false)
				delete(c.bg, q.PID)
			}
		}
		x.demoted, x.keptOut = want, true
	}
	for pid, b := range c.bg {
		switch {
		case !t.Alive(proc.Proc{PID: pid, Start: b.start}):
			delete(c.bg, pid)
		case !inTree[pid]:
			proc.SetBackground(pid, false)
			delete(c.bg, pid)
		}
	}
}

// stepsBack says whether session id's tree belongs in the background, by
// the order of the user's focus (gabriel.go): never the tab they are on,
// Gabriel's top; while the top works the machine, every other session but
// an install, the rest of the focus and the promoted ones too; while a
// session is promoted, every one not raised (the promoted, with the
// subtasks they delegated); and while Gabriel holds back, every one out of
// the user's focus. The caller holds c.mu.
func (c *Core) stepsBack(id string, raised map[string]bool) bool {
	g := &c.gab
	switch {
	case g.top[id]:
		return false
	case g.topBusy:
		// The tab on top decides while it works, a promotion's claim
		// included: everything but an install steps back for it.
		return !g.installs[id]
	case c.boosted(id):
		// Boosted after its API errors (boost.go): with the rest of the
		// focus, whatever is promoted or held back, and pushing back none.
		return false
	case len(raised) > 0:
		return !raised[id]
	}
	return g.holding && !g.focus[id]
}

// letGo takes every tree Irie put in the background out of it, and what
// Gabriel slowed outside Irie, as the core stops.
func (c *Core) letGo() {
	c.mu.Lock()
	if proc.CanBackground {
		if t, err := proc.Read(); err == nil {
			for _, x := range c.sessions {
				if p := c.snap.Pane(x.pane); x.demoted && p != nil && !p.Dead && p.PID > 1 {
					for _, q := range t.Tree(p.PID) {
						proc.SetBackground(q.PID, false)
					}
				}
			}
			for pid := range c.bg {
				proc.SetBackground(pid, false)
			}
		}
	}
	c.bg = nil
	c.mu.Unlock()
	c.releaseOutside()
}
