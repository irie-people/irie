package core

// Focus (DESIGN §4.3): while the machine runs short of memory or CPU, the
// session on show gets it first. macOS lets a process slow another of its
// user's down, and undo that, but never raise one above the rest, so the
// others step back: the core puts every other session's process tree in
// the background state, as promote does (priority.go). The session on
// show keeps the CPU and the disk first, and so do the core and the
// windows that draw the sessions, Irie.app and the sidebar, which no
// session's tree holds; the trees in the background run on, slower.
//
// The machine is busy once the work waiting to run leaves fewer than
// priority.headroom_cpus CPUs free, the kernel warns of memory pressure,
// or less memory is free than a model load's reserve (DESIGN §6.3) and
// priority.headroom_gib more. It is calm again once it has had room for
// calmFor: a quarter of the CPUs it may use free, and the headroom twice
// over in memory. The run queue's average is over a minute, which smooths
// it too, so the trees do not flap in and out.

import (
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/localmodel"
	"github.com/irie-people/irie/internal/proc"
)

// calmFor is how long the machine must have had room before the other
// sessions come out of the background.
const calmFor = 30 * time.Second

// Why the machine is busy (ipc.Header.Busy).
const (
	shortMemory = "memory"
	shortCPU    = "cpu"
)

// pressure is whether the machine is busy, so that every session but the
// one on show yields to it.
type pressure struct {
	why  string    // shortMemory or shortCPU while busy; "" while not
	calm time.Time // while busy, since when the machine has had room again
}

// next is the state after a reading at now: l is how busy the machine is,
// m its memory, p how much room the session on show keeps.
func (b pressure) next(now time.Time, l proc.Load, m localmodel.Machine, p config.Priority) pressure {
	short, roomy := "", true
	if l.CPUs > 0 {
		free := float64(l.CPUs - p.HeadroomCPUs)
		if l.Avg > free {
			short = shortCPU
		}
		roomy = l.Avg <= free*3/4
	}
	if m.Total > 0 {
		reserve, headroom := localmodel.Reserve(m), int64(p.HeadroomGiB)*localmodel.GiB
		if m.Available < reserve+headroom {
			short = shortMemory
		}
		roomy = roomy && m.Available >= reserve+2*headroom
	}
	if l.Memory >= 2 {
		short, roomy = shortMemory, false
	}
	switch {
	case short != "":
		return pressure{why: short}
	case b.why == "" || !roomy:
		return pressure{why: b.why}
	case b.calm.IsZero():
		return pressure{why: b.why, calm: now}
	case now.Sub(b.calm) >= calmFor:
		return pressure{}
	}
	return b
}

// watchPressure reads how busy the machine is, and says whether that
// changed. With priority.focus off, or where no process can put another
// in the background, the machine is never busy. The caller holds c.mu.
func (c *Core) watchPressure(now time.Time) bool {
	was := c.pressure.why
	p := c.cfg().Priority
	if !p.Focus || !proc.CanBackground {
		c.pressure = pressure{}
		return was != ""
	}
	l, err := proc.ReadLoad()
	if err != nil {
		if !c.loadFailed {
			c.log.Warn("busy", "err", err)
		}
		c.loadFailed = true
		return false
	}
	c.loadFailed = false
	c.pressure = c.pressure.next(now, l, c.machine, p)
	if c.pressure.why == was {
		return false
	}
	c.log.Info("busy", "why", c.pressure.why, "load", l.Avg, "cpus", l.CPUs, "memory_pressure", l.Memory,
		"available", c.machine.Available, "focus", c.selected)
	return true
}

// onShow reports whether x is the session the user is looking at: the one
// in the slot, the one picked, or the one beside its teammates. The caller
// holds c.mu.
func (c *Core) onShow(x *session) bool {
	return x.shown || x.id == c.selected
}
