package core

// Rest (DESIGN §4.3). A frozen agent is stopped, and keeps its memory. On
// 2026-11-23, 57 Claude Code agents held 22 GB of an 18 GB Mac, most of
// them idle for hours or days; the kernel compressed what it could, the
// swap ran out of disk, and the Mac panicked. A rest ends an idle
// session's agent and keeps its card. Its pane stays in its window, dead,
// so its name, its place, its space, its group and its worktree stay with
// it, and its conversation stays in its transcript. Waking it starts its
// agent again in the same pane, carrying the conversation on, as a reopen
// does (claude --resume): when you show it, when a message or a jot is
// sent to it, at irie ctl wake, or, for one that rests frozen, at its
// thaw.
//
// Raffi rests cards by himself while memory is short (restUnderPressure),
// the longest idle first, one at a time, never your tab or your focus, a
// card at work, one whose helpers or teammates work, or one that waits on
// you. A restore after a restart brings back the cards that were not at
// work resting, so that it never starts every agent at once (lost.go).

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/localmodel"
	"github.com/irie-people/irie/internal/sysload"
	"github.com/irie-people/irie/internal/tmux"
)

// restIdle is how long a card must have been idle before Raffi rests it
// by himself: one you left a moment ago may be taken up again.
const restIdle = 15 * time.Minute

// restBelow and restUntil are the shares of the memory free, in percent,
// at which Raffi starts and stops resting cards: he starts under
// restBelow, at the kernel's warning of memory pressure, or once the swap
// is in use with the disk under its floor, and goes on until restUntil is
// free and the warning has passed, so that it does not flap.
const (
	restBelow = 15
	restUntil = 25
)

// restWarnFor is how long the kernel's warning of memory pressure must
// last before Raffi rests a card for it: a build or a test run on a busy
// machine raises it for a few seconds, and passes. Under restBelow, or at
// critical, he rests at once.
const restWarnFor = 30 * time.Second

// restAtOnce is how many agents may be ending at once for Raffi's rests:
// one while memory is short, a few while the kernel says it is critical
// or the swap may not grow.
const (
	restAtOnce     = 1
	restAtOnceDire = 3
)

// restPromptWait is how long a message to a resting card waits for its
// woken agent to start before it is typed all the same; then it waits up
// to limitPromptWait for the agent's prompt.
const restPromptWait = 90 * time.Second

// restOff says the automatic rests are off for this core, as a test
// core's are (internal/testutil): it would rest the tests' sessions when
// the machine they run on is short of memory.
func restOff() bool { return os.Getenv(brand.Env("REST")) == "off" }

// restIdleEnv sets restIdle for a test core, as a duration such as "0s".
const restIdleEnv = "IRIE_REST_IDLE"

// idleEnough is how long a card must have been idle before Raffi rests it:
// restIdle, or restIdleEnv's for a test core.
func idleEnough() time.Duration {
	if d, err := time.ParseDuration(os.Getenv(restIdleEnv)); err == nil && d >= 0 {
		return d
	}
	return restIdle
}

// restState is what the core knows of rests under way. c.mu guards it.
type restState struct {
	// ending are the resting sessions whose agents are still ending, by
	// id: a wake waits for its own.
	ending map[string]chan struct{}
	// waking are the sessions a wake is starting, by id.
	waking map[string]bool
	// short says memory is short now, by restShort, and since when.
	short      bool
	shortSince time.Time
	// warnSince is since when the kernel has warned of memory pressure,
	// while it does.
	warnSince time.Time
	// stuck is when Raffi last said that memory is short and nothing may
	// rest, so that core.log says it once while it lasts.
	stuck time.Time
}

// restLine is a resting card's detail, in plain words.
func restLine(r *ipc.Rest, frozen bool) string {
	if frozen {
		return "Frozen and resting: its agent ended, and thawing it starts it again"
	}
	why := "Resting"
	switch r.Why {
	case ipc.RestMemory:
		why = "Resting to free memory"
		if r.Freed > 0 {
			why = "Resting to free " + localmodel.Bytes(r.Freed)
		}
	case ipc.RestRestore:
		why = "Resting since the restart"
	}
	return why + ": opening it, or a message, starts its agent again"
}

// restable says why session x cannot rest now, or "" when it can. auto is
// Raffi's own rest, which leaves alone what you may take up any moment:
// your tab and your focus, a card idle for less than restIdle, and one
// whose jots wait to be typed. The caller holds c.mu.
func (c *Core) restable(x *session, auto bool, now time.Time) string {
	t := &x.track
	switch {
	case t.rest != nil:
		return "it rests already"
	case x.dead:
		return "its agent has ended"
	case t.frozen != nil && (t.held || t.atWork()):
		return "it was frozen at work, which a rest would cut short"
	case t.boost != nil:
		return "it is boosted after API errors"
	case x.track.away != "":
		return "its conversation went on in Claude Code's background"
	case len(x.team) > 0:
		return "its teammates work in terminals of their own"
	case x.anon != "":
		return "it researches anonymously, in a compartment of its own"
	case len(x.followUps) > 0 || x.sending:
		return "jots wait to be typed to it"
	case t.waiting > 0:
		// Its last turn left work of its own in the background, or a
		// wakeup, which its agent's end would end too.
		return "its own work goes on in the background"
	}
	if _, waits := x.subsAtWork(); waits {
		return "its helpers or teammates still work"
	}
	switch st := t.live(); st {
	case ipc.StateIdle:
	case ipc.StateDone:
		// A turn you have not seen waits on you; you may still rest it.
		if auto {
			return "its finished turn waits for you to look"
		}
	case ipc.StateNeedsYou, ipc.StateStalled:
		return "it waits on you"
	default:
		return "it is " + strings.ReplaceAll(st, "_", " ")
	}
	if !c.reopenable(x).Resume {
		return "it has no conversation to carry on"
	}
	if auto {
		if c.selected == x.id || c.gab.installs[x.id] || c.inFocus(true, c.cfg().Arch.Gabriel.Word, now)[x.id] {
			return "it is in your focus"
		}
		if now.Sub(lastActive(x)) < idleEnough() {
			return "it was at work a moment ago"
		}
	}
	return ""
}

// lastActive is when session x was last at work or in your hands: its
// last hook, the end of its last turn, your last word to it, or its start.
func lastActive(x *session) time.Time {
	t := x.created
	for _, at := range []time.Time{x.track.heard, x.track.stopped, x.userWord, x.track.requested} {
		if at.After(t) {
			t = at
		}
	}
	return t
}

// restShort says whether memory is short enough for Raffi to rest cards,
// m being the machine's memory, pressure the kernel's level, warned how
// long it has warned of pressure, and room the swap's room on disk; was is
// what it said last time. It also says how many agents may be ending at
// once.
func restShort(m localmodel.Machine, pressure int, warned time.Duration, room sysload.Room, was bool) (short bool, atOnce int) {
	free := 100
	if m.Total > 0 {
		free = int(m.Available * 100 / m.Total)
	}
	swapStuck := room.Starved() && room.SwapUsed > 0
	dire := pressure >= sysload.PressureCritical || swapStuck
	warn := pressure >= sysload.PressureWarn && (warned >= restWarnFor || was)
	short = dire || warn || free < restBelow || was && free < restUntil
	switch {
	case !short:
		return false, 0
	case dire:
		return true, restAtOnceDire
	}
	return true, restAtOnce
}

// restUnderPressure rests the next cards while memory is short: the
// longest idle first, as many at once as restShort allows. It runs after
// each memory sample. The caller holds c.mu.
func (c *Core) restUnderPressure(now time.Time) {
	r := c.cfg().Arch.Raphael
	if !r.On || !r.Rest || restOff() {
		c.rest.short = false
		return
	}
	switch {
	case c.load.Pressure < sysload.PressureWarn:
		c.rest.warnSince = time.Time{}
	case c.rest.warnSince.IsZero():
		c.rest.warnSince = now
	}
	var warned time.Duration
	if !c.rest.warnSince.IsZero() {
		warned = now.Sub(c.rest.warnSince)
	}
	short, atOnce := restShort(c.machine, c.load.Pressure, warned, c.room, c.rest.short)
	if short != c.rest.short {
		c.rest.short, c.rest.shortSince = short, now
		if short {
			c.log.Info("rest: memory is short, so "+brand.Raffi+" rests idle cards", "available", c.machine.Available, "total", c.machine.Total, "pressure", c.load.Pressure, "disk_free", c.room.DiskFree, "swap", c.room.SwapUsed)
		} else {
			c.log.Info("rest: memory has room again", "available", c.machine.Available, "total", c.machine.Total)
		}
	}
	if !short {
		return
	}
	for len(c.rest.ending) < atOnce {
		x := c.restPick(now)
		if x == nil {
			if now.Sub(c.rest.stuck) >= time.Hour {
				c.rest.stuck = now
				c.log.Warn("rest: memory is short, and no card may rest: those left are at work, in your focus or wait on you")
			}
			return
		}
		if err := c.restLocked(x, ipc.RestMemory, event.SourceIrie); err != nil {
			c.log.Warn("rest", "session", x.id, "err", err)
			return
		}
	}
}

// restPick is the card Raffi rests next: of those that may rest, the one
// idle longest. The caller holds c.mu.
func (c *Core) restPick(now time.Time) *session {
	var best *session
	for _, id := range c.fullOrder() {
		x := c.sessions[id]
		if x == nil || c.restable(x, true, now) != "" {
			continue
		}
		if best == nil || lastActive(x).Before(lastActive(best)) {
			best = x
		}
	}
	return best
}

// restBack marks session x, which reopen brought back resting from closed
// record r, as resting since a restore, and frozen again if it was: its
// thaw wakes it then. Its pane ran no agent (launch). The caller holds
// c.mu.
func (c *Core) restBack(x *session, r closedRecord) {
	c.record(x, event.Event{Source: event.SourceIrie, Kind: event.RestOn, Data: map[string]any{"why": ipc.RestRestore, "frozen": r.Frozen}})
	if r.Frozen {
		x.refreeze = false
		c.record(x, event.Event{Source: event.SourceIrie, Kind: event.FreezeOn, Data: map[string]any{"whole": true, "working": false, "restore": true}})
	}
	c.log.Info("rest", "session", x.id, "name", x.name, "why", ipc.RestRestore, "frozen", r.Frozen)
}

// restLocked rests session x: it records why, and the memory its agent
// takes, then ends the agent as a close does, and leaves the pane. The
// caller holds c.mu.
func (c *Core) restLocked(x *session, why, source string) error {
	p := c.snap.Pane(x.pane)
	if p == nil {
		return ipc.Errorf(ipc.CodeNotFound, "session %s has no pane", x.name)
	}
	c.record(x, event.Event{Source: source, Kind: event.RestOn, Data: map[string]any{
		"why": why, "freed": x.memory, "frozen": x.track.frozen != nil,
	}})
	// A frozen agent goes on to end gently: the host and the hook shim
	// hold nothing for a session without freeze.json. It stays frozen in
	// the journal, and its thaw wakes it.
	os.Remove(c.paths.FreezeFile(x.id))
	c.restEnd(*p)
	c.log.Info("rest", "session", x.id, "name", x.name, "why", why, "freed", x.memory)
	return nil
}

// restEnd ends what pane p runs as close's end does, and leaves the pane,
// dead, in its window: tmux keeps it (remain-on-exit), and the card with
// it. The caller holds c.mu.
func (c *Core) restEnd(p tmux.Pane) {
	if c.rest.ending == nil {
		c.rest.ending = map[string]chan struct{}{}
	}
	id := p.IrieSession
	if c.rest.ending[id] != nil {
		return
	}
	done := make(chan struct{})
	c.rest.ending[id] = done
	running := !p.Dead && p.PID > 1
	go func() {
		defer func() {
			c.mu.Lock()
			if c.rest.ending[id] == done {
				delete(c.rest.ending, id)
			}
			c.mu.Unlock()
			close(done)
		}()
		if !running {
			return
		}
		signalAll(p.PID, syscall.SIGCONT)
		if err := syscall.Kill(p.PID, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			c.log.Warn("rest: SIGTERM", "pane", p.ID, "pid", p.PID, "err", err)
		}
		for deadline := time.Now().Add(closeWait); alive(p.PID) && time.Now().Before(deadline); {
			time.Sleep(50 * time.Millisecond)
		}
		if alive(p.PID) && c.hosts(p) {
			c.log.Warn("rest: the agent did not exit after SIGTERM, so it is killed", "session", id, "pid", p.PID, "wait", closeWait)
			signalAll(p.PID, syscall.SIGKILL)
			for i := 0; i < 40 && alive(p.PID); i++ {
				time.Sleep(50 * time.Millisecond)
			}
		}
	}()
}

// restOp rests one session at your word, or with Idle every one that may
// rest now, the longest idle first, as Raffi would.
func (c *Core) restOp(a ipc.RestArgs) (ipc.RestResult, error) {
	var r ipc.RestResult
	c.mu.Lock()
	if err := c.refresh(); err != nil {
		c.mu.Unlock()
		return r, err
	}
	now := time.Now()
	var targets []*session
	switch {
	case a.Idle:
		for _, id := range c.fullOrder() {
			if x := c.sessions[id]; x != nil && c.restable(x, true, now) == "" {
				targets = append(targets, x)
			}
		}
		slices.SortStableFunc(targets, func(a, b *session) int { return lastActive(a).Compare(lastActive(b)) })
	default:
		x, err := c.findSession(a.Session)
		if err != nil {
			c.mu.Unlock()
			return r, err
		}
		if why := c.restable(x, false, now); why != "" {
			c.mu.Unlock()
			return r, ipc.Errorf(ipc.CodeBusy, "%s cannot rest: %s", x.name, why)
		}
		targets = append(targets, x)
	}
	r.Sessions = []string{}
	for _, x := range targets {
		if err := c.restLocked(x, ipc.RestYou, event.SourceUser); err != nil {
			c.mu.Unlock()
			return r, err
		}
		r.Sessions, r.Freed = append(r.Sessions, x.id), r.Freed+x.memory
	}
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	return r, nil
}

// wakeOp wakes a resting session at your word. One that rests frozen
// wakes at its thaw.
func (c *Core) wakeOp(a ipc.RestArgs) (ipc.RestResult, error) {
	c.mu.Lock()
	x, err := c.findSession(a.Session)
	id := ""
	if err == nil {
		id = x.id
	}
	c.mu.Unlock()
	if err != nil {
		return ipc.RestResult{}, err
	}
	if err := c.wake(id, "you", false); err != nil {
		return ipc.RestResult{}, err
	}
	return ipc.RestResult{Sessions: []string{id}}, nil
}

// findSession is the session ref names, by id or by name. The caller holds
// c.mu.
func (c *Core) findSession(ref string) (*session, error) {
	if x := c.sessions[ref]; x != nil {
		return x, nil
	}
	var found []*session
	for _, x := range c.sessions {
		if ref != "" && strings.EqualFold(x.name, ref) {
			found = append(found, x)
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return nil, ipc.Errorf(ipc.CodeNotFound, "no session %q", ref)
	}
	return nil, ipc.Errorf(ipc.CodeAmbiguous, "more than one session is called %q: name it by id", ref)
}

// resting says whether session id rests, and whether frozen too. The
// caller holds c.mu.
func (c *Core) resting(id string) (rests, frozen bool) {
	x := c.sessions[id]
	if x == nil || x.track.rest == nil {
		return false, false
	}
	return true, x.track.frozen != nil
}

// wakeFor wakes session id, if it rests, before a message is typed to it,
// and waits for its agent's prompt. One that rests frozen stays so: its
// thaw wakes it.
func (c *Core) wakeFor(id, why string) error {
	c.mu.Lock()
	rests, frozen := c.resting(id)
	name := ""
	if x := c.sessions[id]; x != nil {
		name = x.name
	}
	c.mu.Unlock()
	switch {
	case !rests:
		return nil
	case frozen:
		return ipc.Errorf(ipc.CodeBusy, "%s is frozen and resting: thaw it first", name)
	}
	if err := c.wake(id, why, false); err != nil {
		return err
	}
	// Its agent starts, then draws its prompt: what is typed before it
	// listens would be lost.
	for until := time.Now().Add(restPromptWait); time.Now().Before(until); time.Sleep(250 * time.Millisecond) {
		c.mu.Lock()
		x := c.sessions[id]
		started := x == nil || x.track.started || x.track.failed != nil || x.track.exited != nil
		c.mu.Unlock()
		if started {
			break
		}
	}
	c.awaitPrompt(id, limitPromptWait)
	return nil
}

// wakeLater wakes session id in the background, as a show does, and logs
// what went wrong.
func (c *Core) wakeLater(id, why string) {
	go func() {
		if err := c.wake(id, why, false); err != nil {
			c.log.Warn("wake", "session", id, "why", why, "err", err)
		}
	}()
}

// wake starts resting session id's agent again in its own pane, carrying
// its conversation on, once its agent has ended: one spawn, reopen or wake
// at a time, as for a reopen. thawing is a thaw's wake, which may wake a
// session that rests frozen; the thaw records freeze.off itself. A session
// awake already is left be.
func (c *Core) wake(id, why string, thawing bool) error {
	c.spawnMu.Lock()
	defer c.spawnMu.Unlock()
	c.mu.Lock()
	x := c.sessions[id]
	switch {
	case x == nil:
		c.mu.Unlock()
		return ipc.Errorf(ipc.CodeNotFound, "no session %q", id)
	case x.track.rest == nil:
		c.mu.Unlock()
		return nil
	case x.track.frozen != nil && !thawing:
		c.mu.Unlock()
		return ipc.Errorf(ipc.CodeBusy, "%s is frozen and resting: thaw it to start its agent again", x.name)
	case c.rest.waking[id]:
		c.mu.Unlock()
		return nil
	}
	if c.rest.waking == nil {
		c.rest.waking = map[string]bool{}
	}
	c.rest.waking[id] = true
	defer func() {
		c.mu.Lock()
		delete(c.rest.waking, id)
		c.mu.Unlock()
	}()
	r := c.reopenable(x)
	r.Name = x.name
	ending := c.rest.ending[id]
	c.mu.Unlock()
	// Two agents must never share one conversation.
	if ending != nil {
		<-ending
	}
	if !r.Resume {
		return ipc.Errorf(ipc.CodeBusy, "%s has no conversation to carry on", r.Name)
	}
	j, err := c.reopenJob(r)
	if err == nil {
		err = c.freeConversation(r, j)
	}
	if err != nil {
		return err
	}

	c.mu.Lock()
	err = c.respawn(id, why, j, r.Name)
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	return err
}

// respawn starts session id's agent again in its pane for job j, as wake
// does. The caller holds c.mu.
func (c *Core) respawn(id, why string, j job, name string) error {
	if err := c.refresh(); err != nil {
		return err
	}
	x := c.sessions[id]
	if x == nil || x.track.rest == nil {
		return nil
	}
	p := c.snap.Pane(x.pane)
	if p == nil {
		return ipc.Errorf(ipc.CodeNotFound, "%s has no pane", name)
	}
	if !p.Dead {
		return ipc.Errorf(ipc.CodeBusy, "%s's agent has not ended yet", name)
	}
	profile, h := j.run, j.h
	// While its model's own usage limit holds, it starts on the fallback,
	// as a spawn does (fallback.go).
	warnings := slices.Clone(j.probe.Warnings)
	if pr, ok := c.profile(j.profile); ok && profile.Model == pr.Model {
		if m, why := c.startsOn(profile, time.Now()); m != profile.Model {
			profile.Model, warnings = m, append(warnings, why)
		}
	}
	x.vendorID, x.model, x.local, x.warnings = j.resume, profile.Model, profile.Local != nil, warnings
	os.Remove(c.paths.ExitFile(id))
	os.Remove(c.paths.FreezeFile(id))
	s, argv, files, spec, err := c.prepareRun(j, x, profile, h)
	if err != nil {
		return err
	}
	if _, err := c.tmux.RunList([][]string{
		{"respawn-pane", "-k", "-t", p.ID, "-c", x.cwd, "--", c.bin, "host", "--spec", spec},
		{"set-option", "-p", "-t", p.ID, tmux.OptVendorID, x.vendorID},
	}); err != nil {
		return fmt.Errorf("cannot wake %s: %w", name, err)
	}
	c.record(x, event.Event{Source: event.SourceIrie, Kind: event.RestOff, Data: map[string]any{"why": why, "resume": j.resume}})
	if err := c.refresh(); err != nil {
		c.log.Warn("wake", "session", id, "err", err)
	}
	// Its agent lock: its setup as its agent starts again (locks.go).
	c.lockLater(x, j, s, argv, files)
	c.log.Info("woke", "session", id, "name", x.name, "why", why, "resume", j.resume)
	return nil
}
