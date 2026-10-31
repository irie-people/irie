package core

// Promote (DESIGN §4.3, M7) puts a session at the top of the list with a
// badge, and gives its whole process tree the machine first, at once.
// Without root no process can be raised above normal priority, so the
// others step back instead: while any session is promoted, every other
// session's tree runs in macOS's background state (proc.SetBackground), with
// the least CPU priority and throttled disk and network I/O, and what those
// trees start from then on inherits it. The promoted trees stay as they
// are. A raised nice value would serve CPU alone, and only root can lower
// it again. Putting a session back, or closing it, takes the others out of
// the background. What Irie hands out in turn, spawns with their worktrees
// and messages typed to agents, serves a promoted session first (turns.go).
// Memory cannot be set aside: macOS gives a process no way to reserve it
// for another, and the background processes are the ones it slows first.
// The same background state serves focus (pressure.go): while the machine
// is busy, every session but the one on show steps back.

import (
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/proc"
)

// bgProc is a process Irie put in the background.
type bgProc struct {
	start   int64  // tells it from a later process with its pid
	session string // whose tree it was in
}

// setPriority promotes a session (level 1), which also puts it at the top
// of the list and unparks it, or puts it back (level 0). It takes effect
// before it returns. The unpark goes with the promotion, so it is no step
// of the history.
func (c *Core) setPriority(a ipc.PriorityArgs) error {
	if a.Level < 0 || a.Level > 1 {
		return ipc.Errorf(ipc.CodeBadArgs, "priority level %d: it is 0 or 1", a.Level)
	}
	c.mu.Lock()
	x := c.sessions[a.Session]
	if x == nil {
		c.mu.Unlock()
		return ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Session)
	}
	if a.Level > 0 {
		c.toTop(x.id)
		c.setParked(x.id, false)
	}
	if x.track.priority != a.Level {
		data := map[string]any{"level": a.Level}
		if a.Reason != "" {
			data["reason"] = a.Reason
		}
		c.record(x, event.Event{Source: event.SourceUser, Kind: event.PrioritySet, Data: data})
	}
	c.background()
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	return nil
}

// background puts each session's process tree in the background, or takes
// it out, as the promotions and the machine want: while a live session is
// promoted, the others' trees are in the background, and while the machine
// is busy, every tree but those of the sessions on show and the promoted
// ones (pressure.go). A process Irie put there that left every tree, such
// as a daemon, comes out. The caller holds c.mu.
func (c *Core) background() {
	if !proc.CanBackground {
		return
	}
	yielding := c.pressure.why != ""
	promoted, demoted := false, len(c.bg) > 0
	for _, x := range c.sessions {
		promoted = promoted || x.track.priority > 0 && !x.dead
		demoted = demoted || x.demoted || x.leftover
	}
	if !promoted && !demoted && !yielding {
		return
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
	for _, x := range c.sessions {
		p := c.snap.Pane(x.pane)
		if p == nil || p.Dead || p.PID <= 1 {
			// No process to move: what Irie put in the background from its
			// tree is let go below once it ends or leaves.
			x.demoted, x.leftover = false, false
			continue
		}
		want := x.track.priority == 0 && (promoted || yielding && !c.onShow(x))
		for _, q := range t.Tree(p.PID) {
			inTree[q.PID] = true
			b, ok := c.bg[q.PID]
			ok = ok && b.start == q.Start
			switch {
			case want && !ok:
				if err := proc.SetBackground(q.PID, true); err == nil {
					c.bg[q.PID] = bgProc{start: q.Start, session: x.id}
				}
			case !want && (ok || x.demoted || x.leftover):
				// What it started while in the background inherited that,
				// so every process of a tree that was there comes out.
				proc.SetBackground(q.PID, false)
				delete(c.bg, q.PID)
			}
		}
		x.demoted, x.leftover = want, false
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
