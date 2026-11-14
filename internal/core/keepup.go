package core

// The agent keeps its network (DESIGN §4.3). macOS's background state
// throttles a process's network as well as its CPU and disk: an agent in it
// gets its API reply in a trickle, and a long reply fails with "The
// response stopped arriving". So while a session's tree steps back, for a
// promotion or for Gabriel, its agent stays at normal priority, and so do
// the host that runs it and Irie's own helpers that the agent runs, with
// what they start: its hook calls, which Claude Code gives up on after
// their timeout, its status line, and Irie's browser connector with the
// browser it drives, which waits on the network for the agent as the
// agent's own web fetches do. Everything else in the tree steps back: the
// agent's builds, tests and other tools. What the agent starts comes in at
// normal priority, as the agent runs, and steps back at the core's next
// tick, within a second; what that starts in turn inherits the background.

import (
	"path/filepath"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/proc"
)

// maxComm is how much of a program's name the process table keeps
// (MAXCOMLEN in the macOS SDK's sys/param.h).
const maxComm = 16

// keepsUp reports whether process q of the tree under a session's pane,
// root, keeps normal priority while the rest of the tree steps back: the
// agent, which the pane's host (root, never in the tree) runs, and each of
// the agent's children that runs Irie's own program, own, with all it
// starts.
func keepsUp(t proc.Table, root int, own string, q proc.Proc) bool {
	if q.PPID == root {
		return true
	}
	for range 64 {
		parent, ok := t.Get(q.PPID)
		if !ok {
			return false
		}
		if parent.PPID == root {
			// q is a child of the agent.
			return q.Name == own
		}
		q = parent
	}
	return false
}

// ownName is the name the process table gives Irie's own program, which
// the agents run for their hooks, status line and browser.
func (c *Core) ownName() string {
	name := brand.Slug
	if c.bin != "" {
		name = filepath.Base(c.bin)
	}
	if len(name) > maxComm {
		name = name[:maxComm]
	}
	return name
}
