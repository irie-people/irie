package core

// The lid (DESIGN §4.12). macOS sleeps a MacBook the moment its lid closes,
// whatever caffeinate holds, and every agent at work stops with it. So
// while sessions work, the core turns the Mac's sleep off, through the rule
// `irie setup lid` installs. Once nothing has worked for lid.grace it turns
// sleep back on, and if the lid is closed by then with no display beside
// it, it sleeps the Mac, as closing the lid would have. On battery under
// lid.battery, or once the Mac runs hot, it lets the Mac sleep as well.
// The state dir's lid.json says what it did. While sleep is off the core
// touches that file at every look, and the launchd agent's check (lid.Check)
// turns sleep back on once no core has touched it for a while.

import (
	"os"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/lid"
)

// lidEvery is how often, in seconds, the core looks at the lid.
const lidEvery = 5

// lidRetry is how long the core waits to try again after it could not
// turn sleep off.
const lidRetry = 5 * time.Minute

// lidVerify is how often the core checks with pmset that sleep is still
// off while it keeps it so.
const lidVerify = time.Minute

// lidRule is the sudoers rule the core needs before it tries sudo, so that
// a Mac without it is not asked for a password at every look; tests point
// it elsewhere.
var lidRule = lid.Rule

// lidHold is what the core did to the Mac's sleep, and what it saw.
type lidHold struct {
	off      bool      // the core turned sleep off and keeps it so
	since    time.Time // when off last changed
	why      string    // why it did
	worked   time.Time // when a session last had work under way
	retry    time.Time // when the core may try again to turn sleep off
	verified time.Time // when it last checked with pmset that sleep is off
	warned   string    // what it last logged it could not do, logged once
	stopped  bool      // the core let go as it stops: no look holds again
}

// lidOff reports whether this core leaves the Mac's sleep alone: a test
// core (IRIE_LID=off, internal/testutil) would turn the user's own Mac's
// sleep off and on.
func lidOff() bool { return os.Getenv("IRIE_LID") == "off" }

// lidAdopt takes on the hold a core that stopped without letting go left
// behind, as one that crashed does, so that this core turns sleep back on
// once the work is through. It gives the sessions lid.grace to show their
// work first.
func (c *Core) lidAdopt() {
	if !lid.Supported() || lidOff() {
		return
	}
	s, _, err := lid.Read(c.paths.LidFile())
	if err != nil {
		c.log.Warn("lid", "err", err)
		return
	}
	c.lidMu.Lock()
	defer c.lidMu.Unlock()
	c.lid.off, c.lid.since, c.lid.why = s.Off, s.Since, s.Why
	if s.Off {
		c.lid.worked = time.Now()
	}
}

// lidWork reports whether a session has work under way that a closed lid
// would stop: one that works, starts or is stalled at work, or one whose
// own background work, or a wakeup, will wake it. A session whose harness
// reports no turns cannot say, and does not count. The caller holds c.mu.
func (c *Core) lidWork() bool {
	for _, x := range c.sessions {
		t := &x.track
		if !x.observed || x.dead || t.parked || t.frozen != nil {
			continue
		}
		switch t.state(x.dead) {
		case ipc.StateWorking, ipc.StateStarting, ipc.StateStalled:
			return true
		case ipc.StateDone, ipc.StateIdle:
			if t.waiting > 0 {
				return true
			}
		}
	}
	return false
}

// lidSweep is the core's look at the lid: sleep off while there is work,
// or always by lid.awake, and back on once there has been none for
// lid.grace, the battery is under lid.battery or the Mac is hot. The
// trod bot's keeping the Mac awake counts as work. One look runs at a
// time.
func (c *Core) lidSweep() {
	if !lid.Supported() || lidOff() || !c.lidSweeping.CompareAndSwap(false, true) {
		return
	}
	defer c.lidSweeping.Store(false)
	n := c.cfg().Lid
	now := time.Now()
	c.mu.Lock()
	work := c.lidWork()
	c.mu.Unlock()
	work = work || c.awakeNow()

	c.lidMu.Lock()
	defer c.lidMu.Unlock()
	h := &c.lid
	if h.stopped {
		return
	}
	if work {
		h.worked = now
	}
	f := lid.Facts{Work: !h.worked.IsZero() && now.Sub(h.worked) <= n.Grace, Power: lid.Power{Percent: -1}}
	var m lid.Mac
	want, why := lid.Want(n, f)
	if want {
		// What may still let the Mac sleep is read only when it would
		// stay awake.
		if p, err := m.Power(); err == nil {
			f.Power = p
		}
		f.Heat, _ = m.Heat()
		want, why = lid.Want(n, f)
	}
	switch {
	case want && !h.off:
		c.lidHoldLocked(m, now, why)
	case !want && h.off:
		c.lidReleaseLocked(m, now, why, true)
	case h.off:
		c.lidKeepLocked(m, now)
	}
}

// lidHoldLocked turns the Mac's sleep off. Sleep that something else
// turned off, such as the user with pmset, is theirs: the core neither
// takes it nor turns it back on. Nor does it hold in a dark wake, when a
// sleeping Mac is up for a moment on its own: its sessions still look as
// they did when it slept, and a hold would keep it from sleeping again.
// The caller holds c.lidMu.
func (c *Core) lidHoldLocked(m lid.Mac, now time.Time, why string) {
	h := &c.lid
	if now.Before(h.retry) {
		return
	}
	if dark, err := m.Dark(); err == nil && dark {
		return
	}
	// Without the rule nothing is tried, so the look that follows `irie
	// setup lid` holds at once.
	if _, err := os.Stat(lidRule); err != nil {
		c.lidWarnLocked("the Mac sleeps when its lid closes: " + lid.ErrNoRule.Error())
		return
	}
	off, err := m.IsOff()
	if err == nil && off {
		h.retry = now.Add(lidRetry)
		c.lidWarnLocked("sleep is already off, and not by Irie: Irie leaves it so")
		return
	}
	if err == nil {
		err = m.SleepOff(true)
	}
	if err != nil {
		h.retry = now.Add(lidRetry)
		c.lidWarnLocked("the Mac sleeps when its lid closes: " + err.Error())
		return
	}
	h.off, h.since, h.why, h.verified, h.warned = true, now, why, now, ""
	c.lidWriteLocked()
	c.log.Info("lid: the Mac stays awake with its lid closed", "why", why)
}

// lidReleaseLocked turns the Mac's sleep back on. With sleep, when it is
// the look that lets go, it also sleeps the Mac if its lid is closed with
// no display beside it. The caller holds c.lidMu.
func (c *Core) lidReleaseLocked(m lid.Mac, now time.Time, why string, sleep bool) {
	h := &c.lid
	if err := m.SleepOff(false); err != nil {
		c.lidWarnLocked("sleep stays off: " + err.Error())
		return
	}
	h.off, h.since, h.why, h.warned = false, now, why, ""
	c.lidWriteLocked()
	c.log.Info("lid: the Mac may sleep again", "why", why)
	if !sleep {
		return
	}
	if slept, err := m.SleepIfClosed(); err != nil {
		c.log.Warn("lid: sleep the Mac", "err", err)
	} else if slept {
		c.log.Info("lid: its lid is closed, so the Mac sleeps")
	}
}

// lidKeepLocked is the core's word that it still keeps sleep off: it
// touches lid.json, and now and then checks with pmset that sleep is still
// off. Sleep that something else turned back on, such as the launchd
// agent's check while this core was stuck, is turned off again at the
// next look. The caller holds c.lidMu.
func (c *Core) lidKeepLocked(m lid.Mac, now time.Time) {
	h := &c.lid
	if now.Sub(h.verified) >= lidVerify {
		h.verified = now
		if off, err := m.IsOff(); err == nil && !off {
			h.off, h.since, h.why = false, now, "something else turned sleep back on"
			c.lidWriteLocked()
			c.log.Info("lid: " + h.why)
			return
		}
	}
	if err := lid.Touch(c.paths.LidFile(), now); err != nil {
		c.lidWriteLocked()
	}
}

// lidLetGo turns the Mac's sleep back on as the core stops, without
// sleeping it: a core that comes back keeps the Mac awake again at its
// first look, and macOS sleeps a Mac whose lid is already closed only at
// the next reason it has to.
func (c *Core) lidLetGo() {
	if !lid.Supported() || lidOff() {
		return
	}
	c.lidMu.Lock()
	defer c.lidMu.Unlock()
	c.lid.stopped = true
	if c.lid.off {
		c.lidReleaseLocked(lid.Mac{}, time.Now(), "the core stopped", false)
	}
}

// lidWriteLocked writes what the core did to lid.json. The caller holds
// c.lidMu.
func (c *Core) lidWriteLocked() {
	h := c.lid
	if err := lid.Write(c.paths.LidFile(), lid.State{Off: h.off, Since: h.since, Why: h.why}); err != nil {
		c.log.Warn("lid: write lid.json", "err", err)
	}
}

// lidWarnLocked logs what the core could not do, once until it can. The
// caller holds c.lidMu.
func (c *Core) lidWarnLocked(msg string) {
	if c.lid.warned != msg {
		c.lid.warned = msg
		c.log.Warn("lid: " + msg)
	}
}
