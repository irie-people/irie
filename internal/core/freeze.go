package core

// Freeze (DESIGN §4.3, M6) stops a session's whole process tree at once:
// the agent, its subagents and everything any of them started. Thaw
// continues it. The core signals the processes itself and never sends
// SIGUSR1 or SIGUSR2 to a host: a host from before freeze handles only
// SIGHUP, SIGTERM and SIGINT, so either signal would end it and its agent.
//
// A host continues any stop of its agent that it sees, so that the agent
// never wedges, and tmux continues a pane's first process, the host, the
// same way: a stopped host runs again at once and continues its agent. So
// the host is never stopped. A host of this version leaves its agent's
// stops alone while the session's freeze.json exists (host.json says which
// host runs a pane). The core writes that file first, then stops the
// agent's whole process group at once, then every process under the host
// outside that group, parents before children, and walks the process table
// again until no new process turns up. A host from before would continue
// the agent, so there the agent runs on: everything under it stops at
// once, and the hook shim holds the agent at its next tool call while
// freeze.json exists. Thaw continues every stopped process under the host,
// children before parents, and only then removes freeze.json.
//
// Whether a session is frozen comes from the journal's freeze.on and
// freeze.off, so it stays frozen through a core restart, and the core never
// continues it on its own.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"syscall"
	"time"

	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/proc"
)

// freezePasses bounds the walks of the process table that one freeze makes.
const freezePasses = 5

// frozenNote is what an agent that was working is told at its first tool
// call after a thaw (DESIGN §5.5, notes.frozen).
const frozenNote = "[Irie] The user froze this session for %s (%s–%s UTC). Network calls or child processes may have failed meanwhile; re-check before you continue."

// freezeMark is freeze.json.
type freezeMark struct {
	At time.Time `json:"at"`
}

// freezeOp freezes (on) or thaws the sessions a names, and says which it
// froze or thawed.
func (c *Core) freezeOp(a ipc.FreezeArgs, on bool) (ipc.FreezeResult, error) {
	c.mu.Lock()
	ids, err := c.freezeTargets(a, on)
	done := []string{}
	for _, id := range ids {
		x := c.sessions[id]
		var err1 error
		if on {
			err1 = c.freezeLocked(x)
		} else {
			err1 = c.thawLocked(x)
		}
		if err1 != nil {
			c.log.Warn("freeze", "session", id, "on", on, "err", err1)
			if !a.All {
				err = err1
			}
			continue
		}
		done = append(done, id)
	}
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	return ipc.FreezeResult{Sessions: done}, err
}

// freezeTargets are the sessions a names: one, or with All every live
// session that is not yet frozen (on) or is frozen (off), in list order.
// The caller holds c.mu.
func (c *Core) freezeTargets(a ipc.FreezeArgs, on bool) ([]string, error) {
	if err := c.refresh(); err != nil {
		return nil, err
	}
	if !a.All {
		if c.sessions[a.Session] == nil {
			return nil, ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Session)
		}
		return []string{a.Session}, nil
	}
	var ids []string
	for _, s := range c.list() {
		x := c.sessions[s.ID]
		if (x.track.frozen != nil) != on && (!on || s.State != ipc.StateExited && s.State != ipc.StateFailed) {
			ids = append(ids, s.ID)
		}
	}
	return ids, nil
}

// freezeLocked stops session x's process tree. Freezing a frozen session
// again stops what runs in it that started since. The caller holds c.mu.
func (c *Core) freezeLocked(x *session) error {
	if st := x.track.state(x.dead); st == ipc.StateExited || st == ipc.StateFailed {
		return ipc.Errorf(ipc.CodeBusy, "%s has ended, so there is nothing to freeze", x.name)
	}
	p := c.snap.Pane(x.pane)
	if p == nil || p.Dead || p.PID <= 1 {
		return ipc.Errorf(ipc.CodeNotFound, "%s has no running pane", x.name)
	}
	whole := c.hostFreezes(x, p.PID)
	if f := x.track.frozen; f != nil {
		mark, _ := f.Int("mark")
		_, err := stopTree(p.PID, whole, int64(mark))
		return err
	}
	now := time.Now().UTC()
	if err := writeMark(c.paths.FreezeFile(x.id), now); err != nil {
		return err
	}
	t, err := proc.Read()
	if err != nil {
		os.Remove(c.paths.FreezeFile(x.id))
		return err
	}
	stopped, err := stopTree(p.PID, whole, 0)
	if err != nil && len(stopped) == 0 {
		os.Remove(c.paths.FreezeFile(x.id))
		return err
	}
	pids := make([]int, 0, len(stopped))
	for _, s := range stopped {
		pids = append(pids, s.PID)
	}
	c.record(x, event.Event{TS: event.Stamp(now), Source: event.SourceUser, Kind: event.FreezeOn, Data: map[string]any{
		"whole": whole, "pids": pids, "mark": t.Newest(), "working": x.track.live() == ipc.StateWorking,
	}})
	x.note = ""
	// Keys typed meanwhile would reach the agent all at once at the thaw.
	if _, err := c.tmux.Run("select-pane", "-d", "-t", x.pane); err != nil {
		c.log.Warn("freeze: input off", "session", x.id, "err", err)
	}
	c.log.Info("frozen", "session", x.id, "whole", whole, "stopped", len(pids))
	return nil
}

// thawLocked continues session x's process tree. Thawing a session that is
// not frozen continues whatever in it is stopped. The caller holds c.mu.
func (c *Core) thawLocked(x *session) error {
	if p := c.snap.Pane(x.pane); p != nil && !p.Dead && p.PID > 1 {
		if err := continueTree(p.PID); err != nil {
			return err
		}
	}
	if err := os.Remove(c.paths.FreezeFile(x.id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := c.tmux.Run("select-pane", "-e", "-t", x.pane); err != nil {
		c.log.Warn("thaw: input on", "session", x.id, "err", err)
	}
	f := x.track.frozen
	if f == nil {
		return nil
	}
	now := time.Now().UTC()
	from := f.Time()
	c.record(x, event.Event{TS: event.Stamp(now), Source: event.SourceUser, Kind: event.FreezeOff, Data: map[string]any{
		"frozen_ms": now.Sub(from).Milliseconds(),
	}})
	if working, _ := f.Data["working"].(bool); working {
		x.note = fmt.Sprintf(frozenNote, now.Sub(from).Round(time.Second), from.Format("15:04:05"), now.Format("15:04:05"))
	}
	c.log.Info("thawed", "session", x.id, "frozen", now.Sub(from).Round(time.Millisecond))
	return nil
}

// hostFreezes reports whether the host with pid hostPID that runs x leaves
// its agent stopped while x is frozen, which a host from before freeze does
// not. The caller holds c.mu.
func (c *Core) hostFreezes(x *session, hostPID int) bool {
	i, err := host.ReadInfo(c.paths.HostFile(x.id))
	return err == nil && i.PID == hostPID && i.Freeze >= 1
}

// keepFrozen stops what started in a frozen session since it was frozen,
// such as a hook a running agent runs when its host is from before freeze,
// and makes freeze.json agree with the journal, as after a restart. It
// never continues anything. The caller holds c.mu.
func (c *Core) keepFrozen() {
	for _, x := range c.sessions {
		f := x.track.frozen
		if f == nil {
			continue
		}
		p := c.snap.Pane(x.pane)
		if p == nil || p.Dead || p.PID <= 1 {
			continue
		}
		file := c.paths.FreezeFile(x.id)
		if _, err := os.Stat(file); errors.Is(err, os.ErrNotExist) {
			if err := writeMark(file, f.Time()); err != nil {
				c.log.Warn("freeze", "session", x.id, "err", err)
			}
		}
		mark, _ := f.Int("mark")
		if _, err := stopTree(p.PID, c.hostFreezes(x, p.PID), int64(mark)); err != nil {
			c.log.Warn("freeze", "session", x.id, "err", err)
		}
	}
}

// takeNote returns the note an agent is due at the hook evs came from, once:
// at its first tool call or prompt after a thaw. The caller holds c.mu.
func (c *Core) takeNote(x *session, evs []event.Event) string {
	if x.note == "" || x.track.frozen != nil {
		return ""
	}
	for _, e := range evs {
		switch e.Kind {
		case event.ToolStarted, event.ToolFinished, event.ToolFailed, event.PromptSubmitted:
			n := x.note
			x.note = ""
			return n
		}
	}
	return ""
}

// agentOf is the agent a host runs: its child that leads a process group of
// its own.
func agentOf(t proc.Table, hostPID int) (proc.Proc, bool) {
	for _, p := range t.Children(hostPID) {
		if p.PGID == p.PID && !p.Zombie {
			return p, true
		}
	}
	return proc.Proc{}, false
}

// stopTree stops what the host hostPID runs, parents before children, and
// returns what it stopped. With whole, the agent's process group stops at
// once; without, the agent itself is left running, since its host would
// continue it. Only processes that started after since stop. Each pass
// reads the process table again, until a pass finds nothing new to stop.
func stopTree(hostPID int, whole bool, since int64) ([]proc.Proc, error) {
	var stopped []proc.Proc
	sent := map[int]int{}
	group := false
	for range freezePasses {
		t, err := proc.Read()
		if err != nil {
			return stopped, err
		}
		agent, ok := agentOf(t, hostPID)
		n := 0
		groupNow := false
		if whole && ok && !agent.Stopped && !group && agent.Start > since {
			if err := syscall.Kill(-agent.PID, syscall.SIGSTOP); err == nil {
				group, groupNow = true, true
				n++
			}
		}
		for _, p := range t.Tree(hostPID) {
			switch {
			case p.Zombie || p.Stopped || p.Start <= since || sent[p.PID] >= 2:
				continue
			case ok && p.PID == agent.PID && !whole:
				continue
			case groupNow && ok && p.PGID == agent.PID:
				// The group's stop reached it.
				stopped = append(stopped, p)
				continue
			}
			if err := syscall.Kill(p.PID, syscall.SIGSTOP); err == nil {
				sent[p.PID]++
				stopped = append(stopped, p)
				n++
			}
		}
		if n == 0 {
			break
		}
	}
	return stopped, nil
}

// continueTree continues every stopped process the host hostPID runs,
// children before parents, so that no parent sees a child of its stopped.
func continueTree(hostPID int) error {
	t, err := proc.Read()
	if err != nil {
		return err
	}
	tree := t.Tree(hostPID)
	slices.Reverse(tree)
	for _, p := range tree {
		if p.Stopped {
			syscall.Kill(p.PID, syscall.SIGCONT)
		}
	}
	return nil
}

// writeMark writes freeze.json.
func writeMark(path string, at time.Time) error {
	b, err := json.Marshal(freezeMark{At: at})
	if err != nil {
		return err
	}
	return filetx.WriteFile(path, append(b, '\n'), 0o600)
}
