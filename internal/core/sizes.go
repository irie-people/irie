package core

import (
	"maps"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/tmux"
)

// Each session's terminal size. The Mac app shows a session's panes beside
// its terminal, or not, as that session last had them (Rolodex.swift), so
// one session's terminal is narrower than another's. Every agent window
// keeps the size the app shows its own session at, rather than the slot's,
// so showing a session resizes no other agent; and a swap gives the slot
// the size of the session coming in before it comes, so it comes in at its
// own size too (tmux.SwapIn), and the app's terminal takes that size after.
// Until it has, irie:0 holds that size rather than follow its client
// (tmux.Want.Hold).

// sizeHold is how long irie:0 holds the size a swap gave it: long enough
// for the app's terminal to take it, even on a busy machine.
const sizeHold = 1500 * time.Millisecond

// slotSizes are the sizes the Mac app last said it shows each session's
// terminal at (ipc.OpSizes): all, every session's, and by, a session's own
// where it differs. A zero all keeps every agent window at the slot's size.
type slotSizes struct {
	all     tmux.Size
	by      map[string]tmux.Size
	swapped time.Time // when a swap last ran while the app was open
}

// sizesOp keeps the sizes the app shows each session's terminal at, and
// resizes the windows whose size changed. The caller does not hold c.mu.
func (c *Core) sizesOp(a ipc.SizesArgs) error {
	ok := func(z ipc.Size) bool { return z.W > 0 && z.H > 0 && z.W <= ipc.SizeMax && z.H <= ipc.SizeMax }
	var all tmux.Size
	var by map[string]tmux.Size
	if a.Size != (ipc.Size{}) {
		if !ok(a.Size) {
			return ipc.Errorf(ipc.CodeBadArgs, "a size is 1 to %d columns by 1 to %d rows, not %dx%d", ipc.SizeMax, ipc.SizeMax, a.Size.W, a.Size.H)
		}
		all = tmux.Size{W: a.Size.W, H: a.Size.H}
		for id, z := range a.Sessions {
			// A peer's sessions are sized on their own machine.
			if !sessionID.MatchString(id) {
				continue
			}
			if !ok(z) {
				return ipc.Errorf(ipc.CodeBadArgs, "session %s's size is 1 to %d columns by 1 to %d rows, not %dx%d", id, ipc.SizeMax, ipc.SizeMax, z.W, z.H)
			}
			if s := (tmux.Size{W: z.W, H: z.H}); s != all {
				if by == nil {
					by = map[string]tmux.Size{}
				}
				by[id] = s
			}
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sizes.all == all && maps.Equal(c.sizes.by, by) {
		return nil
	}
	c.sizes.all, c.sizes.by = all, by
	if c.appViews == 0 {
		return nil
	}
	return c.refresh()
}

// noteSwap notes that a swap runs now, which may give irie:0 the size of
// the session it shows (tmux.SwapIn): irie:0 holds it a moment. The caller
// holds c.mu.
func (c *Core) noteSwap() {
	if c.appViews > 0 {
		c.sizes.swapped = time.Now()
	}
}
