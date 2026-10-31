package core

// Irie's own processes keep running, and out of the background (DESIGN
// §2.1, principle 7). Only the user's own freeze or promote slows or stops
// an agent, and nothing Irie does ever slows or stops the core, the
// sidebar, the supervisors that restart them or the tmux server that holds
// every pane. Something else may: a stray SIGSTOP from a script or a chaos
// monkey, a debugger left attached, or a shell in the background state
// that started the server, whose every pane would inherit that state. So a
// watchdog of the core's own looks twice a second, continues any of them
// that is stopped and takes any that is in the background out. It takes no
// lock, starts no process and asks tmux nothing, so it also frees a stopped
// server whose clients hold the core's lock until their deadline. A
// supervisor does the same for its own child, the core included
// (supervise.go).

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/irie-people/irie/internal/proc"
	"github.com/irie-people/irie/internal/tmux"
)

// steadyEvery is how often the watchdog looks.
const steadyEvery = 500 * time.Millisecond

// keepSteady runs the watchdog until stop closes.
func (c *Core) keepSteady(stop <-chan struct{}) {
	t := time.NewTicker(steadyEvery)
	defer t.Stop()
	for {
		c.steady()
		select {
		case <-t.C:
		case <-stop:
			return
		}
	}
}

// steady continues each of Irie's own processes that is stopped, and takes
// each out of the background. They are the tmux server, the core's own
// process group (its supervisor, the core and what the core runs) and the
// sidebar's (its supervisor and the sidebar): a pane's first process leads
// a group of its own, and what it starts stays in it.
func (c *Core) steady() {
	if c.server == 0 {
		c.server = c.findServer()
	}
	var own []proc.Proc
	if c.server > 0 {
		if p, err := proc.One(c.server); err == nil {
			own = append(own, p)
		}
	}
	groups := []int{syscall.Getpgrp()}
	if g := int(c.sidebarGroup.Load()); g > 1 && g != groups[0] {
		groups = append(groups, g)
	}
	for _, g := range groups {
		if ps, err := proc.Group(g); err == nil {
			own = append(own, ps...)
		}
	}
	self := os.Getpid()
	for _, p := range own {
		if p.PID <= 1 || p.Zombie {
			continue
		}
		if p.Stopped && p.PID != self {
			c.log.Warn("steady: continued", "pid", p.PID, "name", p.Name)
			syscall.Kill(p.PID, syscall.SIGCONT)
		}
		if proc.CanBackground {
			// A no-op for a process that is not there: macOS tells no other
			// process whether one is.
			proc.SetBackground(p.PID, false)
		}
	}
}

// findServer returns the pid of the tmux server whose pane runs the core:
// its first forebear that runs the tmux binary, its supervisor's parent.
// It asks tmux nothing, so it works while the server is stopped. -1 means
// there is none, as for a core started outside tmux.
func (c *Core) findServer() int {
	name := filepath.Base(c.tmux.Bin)
	pid := os.Getppid()
	for range 4 {
		if pid <= 1 {
			break
		}
		p, err := proc.One(pid)
		if err != nil {
			return 0 // try again next time
		}
		// The system keeps the first 15 or 16 bytes of a command's name.
		if p.Name != "" && strings.HasPrefix(name, p.Name) {
			return pid
		}
		pid = p.PPID
	}
	return -1
}

// noteSidebar tells the watchdog where the sidebar runs, from the panes
// the tick just read. The caller holds c.mu.
func (c *Core) noteSidebar() {
	for _, p := range c.snap.Panes {
		if p.Role == tmux.RoleUI && !p.Dead && p.PID > 1 {
			c.sidebarGroup.Store(int64(p.PID))
			return
		}
	}
}
