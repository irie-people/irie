package core

// Hook events the core missed (DESIGN §3.3). While no core listens, as for
// the seconds an install takes to restart it, an agent's hook shim keeps
// each event it could not give in the session's missed.ndjson
// (hook.Missed). Each tick, the core takes the file of each live session
// once nothing has been added to it for missedSettle, and records its
// events as hooks that came late, at the time they came (takeHook): so a
// turn that ended or failed meanwhile is not lost, and the row does not
// stay "working" with its agent back at its prompt. An event no later than
// the last one the core heard from the session's agent is left out: the
// agent went on since, and each session's events stay in the order they
// came.

import (
	"os"
	"time"

	"github.com/irie-people/irie/internal/hook"
	"github.com/irie-people/irie/internal/ipc"
)

// missedSettle is how long a missed.ndjson must stand unchanged before the
// core takes it, so that a shim still writing its line finishes first.
const missedSettle = time.Second

// takeMissed records the events of each live session's missed.ndjson that
// has settled, and removes the file. It holds no lock when it is called.
func (c *Core) takeMissed(now time.Time) {
	c.mu.Lock()
	var ids []string
	for id, x := range c.sessions {
		if !x.dead {
			ids = append(ids, id)
		}
	}
	c.mu.Unlock()
	for _, id := range ids {
		file := c.paths.MissedFile(id)
		fi, err := os.Stat(file)
		if err != nil || now.Sub(fi.ModTime()) < missedSettle {
			continue
		}
		// A shim that writes after this starts a file of its own.
		taken := file + ".taken"
		if err := os.Rename(file, taken); err != nil {
			continue
		}
		f, err := os.Open(taken)
		if err != nil {
			continue
		}
		missed := hook.ReadMissed(f)
		f.Close()
		os.Remove(taken)
		n := 0
		for _, m := range missed {
			c.mu.Lock()
			x := c.sessions[id]
			stale := x == nil || !m.At.After(x.track.heard)
			c.mu.Unlock()
			if stale {
				continue
			}
			if _, err := c.takeHook(ipc.HookArgs{Harness: m.Harness, Session: id, Payload: m.Payload}, m.At); err != nil {
				c.log.Warn("missed hook", "session", id, "err", err)
				continue
			}
			n++
		}
		c.log.Info("missed hooks", "session", id, "kept", len(missed), "taken", n)
	}
}
