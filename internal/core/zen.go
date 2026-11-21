package core

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"time"

	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/zen"
)

// Zen, a calm place (docs/proposals/zen.md): the core keeps your zen and its
// versions (internal/zen), hears from the app when zen starts, ends or goes
// on to another session, asks each agent zen shows to be gentle, and offers
// one thing at a time, a step of the path, a rest or a word at night,
// through the header, which every client draws.

// zenEvery is how often, in ticks, the core looks at the moment while zen
// is on.
const zenEvery = 10

// zenGone is how long zen stays on after the last app window went: an
// install relaunches the app within seconds, and its zen comes back.
const zenGone = 2 * time.Minute

// zenState is zen as the core keeps it, under c.mu.
type zenState struct {
	loaded     bool
	book       zen.Book
	profile    ipc.ZenProfile // your zen: the newest version's, or the first
	head       string
	on         bool
	session    string          // the session zen shows
	since      time.Time       // zen on since
	gone       time.Time       // the app went while zen was on
	windowText int             // the window's text size, as the app last said
	trial      *ipc.ZenProfile // a step's change on trial
	trying     string          // that step's id
	offer      *ipc.ZenOffer
	answered   time.Time       // the last offer was answered
	desk       time.Time       // at the computer since; zero while away
	snooze     time.Time       // no rest before
	saved      zenSaved        // what a restarted core keeps
	told       map[string]bool // sessions told zen is on
}

// zenSaved is the state file's: how far along the path the person is, the
// night zen last spoke, and the sessions told zen is on, so a core that
// restarts tells no agent twice. Nothing about how anyone feels.
type zenSaved struct {
	Path  zen.Progress `json:"path"`
	Night string       `json:"night,omitempty"`
	Told  []string     `json:"told,omitempty"`
}

// loadZen reads your zen's newest version and the state file, at start.
func (c *Core) loadZen() {
	c.zen.book = zen.Open(c.paths.Zen())
	c.zen.profile = zen.Default()
	if v, ok, err := c.zen.book.Head(); err != nil {
		c.log.Warn("zen", "err", err)
	} else if ok {
		c.zen.profile, c.zen.head = v.Zen, v.Name
	}
	c.zen.saved.Path = zen.Progress{}
	if b, err := os.ReadFile(c.paths.ZenState()); err == nil {
		if err := json.Unmarshal(b, &c.zen.saved); err != nil {
			c.log.Warn("zen state", "err", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		c.log.Warn("zen state", "err", err)
	}
	if c.zen.saved.Path == nil {
		c.zen.saved.Path = zen.Progress{}
	}
	c.zen.told = map[string]bool{}
	for _, id := range c.zen.saved.Told {
		c.zen.told[id] = true
	}
	c.zen.loaded = true
}

// saveZen writes the state file. The caller holds c.mu.
func (c *Core) saveZen() {
	s := &c.zen.saved
	s.Told = s.Told[:0]
	for id := range c.zen.told {
		s.Told = append(s.Told, id)
	}
	slices.Sort(s.Told)
	b, err := json.MarshalIndent(s, "", "  ")
	if err == nil {
		err = filetx.WriteFile(c.paths.ZenState(), append(b, '\n'), 0o600)
	}
	if err != nil {
		c.log.Warn("zen state", "err", err)
	}
}

// zenHeader is zen as the header gives it. The caller holds c.mu.
func (c *Core) zenHeader() *ipc.Zen {
	z := &c.zen
	if !z.loaded {
		return nil
	}
	h := &ipc.Zen{Profile: z.profile, Head: z.head, On: z.on, Trying: z.trying}
	if z.trial != nil {
		h.Profile = *z.trial
	}
	if z.offer != nil {
		o := *z.offer
		h.Offer = &o
	}
	return h
}

// zenOp is the zen op.
func (c *Core) zenOp(a ipc.ZenArgs) (ipc.ZenResult, error) {
	now := time.Now()
	c.mu.Lock()
	// A core loads your zen as it starts; one that hasn't, as in a test,
	// does now, so nothing is written but where it belongs.
	if !c.zen.loaded {
		c.loadZen()
	}
	var res ipc.ZenResult
	var err error
	switch {
	case a.Restore != "":
		res.Version, err = c.zenRestore(a.Restore, now)
	case a.Set != nil:
		res.Version, err = c.zenSet(*a.Set, a.Why, now)
	case a.Answer != "":
		res.Version, err = c.zenAnswer(a.Offer, a.Answer, now)
	case a.On != nil:
		c.zenShow(*a.On, a.Session, a.WindowText, now)
	}
	if err == nil && a.Log > 0 {
		res.Versions, err = c.zen.book.Log(min(a.Log, ipc.MaxZenLog))
	}
	if h := c.zenHeader(); h != nil {
		res.Zen = *h
	}
	c.mu.Unlock()
	c.publishHeader()
	return res, err
}

// zenShow is the app saying zen started or ended, or went on to another
// session. Each session zen shows is told, once, to be gentle. The caller
// holds c.mu.
func (c *Core) zenShow(on bool, session string, windowText int, now time.Time) {
	z := &c.zen
	if windowText >= zen.MinText && windowText <= 96 {
		z.windowText = windowText
	}
	if !on {
		if z.on {
			c.zenLeave()
		} else if len(z.told) > 0 {
			// Zen ended while no core heard it, as across a restart.
			c.zenLeave()
		}
		return
	}
	z.gone = time.Time{}
	if !z.on {
		z.on, z.since, z.desk = true, now, now
	}
	z.session = session
	c.zenTell(session)
}

// zenTell queues the gentle note for the agent of a session zen shows,
// once while zen is on, if your zen asks for it and its harness takes
// notes at its hooks. The caller holds c.mu.
func (c *Core) zenTell(id string) {
	z := &c.zen
	x := c.sessions[id]
	if x == nil || !x.observed || z.told[id] || !z.profile.Gentle {
		return
	}
	if z.told == nil {
		z.told = map[string]bool{}
	}
	z.told[id] = true
	x.zenNote = zen.GentleNote()
	c.saveZen()
}

// zenLeave ends zen: a change on trial goes, as it was never kept, and so
// does what zen offered; each agent told zen was on is told it ended,
// unless its note had not gone out yet, which is then taken back. The
// caller holds c.mu.
func (c *Core) zenLeave() {
	z := &c.zen
	z.on, z.session, z.gone = false, "", time.Time{}
	z.trial, z.trying, z.offer = nil, "", nil
	for id := range z.told {
		if x := c.sessions[id]; x != nil {
			if x.zenNote == zen.GentleNote() {
				x.zenNote = ""
			} else {
				x.zenNote = zen.LeftNote()
			}
		}
	}
	clear(z.told)
	c.saveZen()
}

// zenStartNote is the gentle note again for a session that starts afresh
// while zen shows it, after a /clear or a compaction, which lose the first
// (startNote). The caller holds c.mu.
func (c *Core) zenStartNote(x *session) string {
	if !c.zen.on || !c.zen.told[x.id] || x.zenNote != "" {
		return ""
	}
	return zen.GentleNote()
}

// zenCommit makes p your zen as a new version, if it differs from the
// newest. The caller holds c.mu.
func (c *Core) zenCommit(p ipc.ZenProfile, why string, now time.Time) (*ipc.ZenVersion, error) {
	v, made, err := c.zen.book.Commit(p, why, now)
	if err != nil {
		return nil, err
	}
	c.zen.profile, c.zen.head = v.Zen, v.Name
	if !made {
		return nil, nil
	}
	c.log.Info("zen", "version", v.Name, "why", v.Why)
	return &v, nil
}

// zenSet changes your zen as a new version. A step's change on trial keeps
// on top of it. The caller holds c.mu.
func (c *Core) zenSet(ch ipc.ZenChange, why string, now time.Time) (*ipc.ZenVersion, error) {
	z := &c.zen
	p := zen.Apply(z.profile, ch)
	if err := zen.Check(p); err != nil {
		return nil, ipc.Errorf(ipc.CodeBadArgs, "zen: %v", err)
	}
	if z.trial != nil {
		if t := zen.Apply(*z.trial, ch); zen.Check(t) == nil {
			z.trial = &t
		}
	}
	if why = zen.CleanWhy(why); why == "" {
		why = zen.Why(z.profile, p)
	}
	return c.zenCommit(p, why, now)
}

// zenRestore goes back to the zen of an earlier version, as a new version,
// so the history is never rewritten. The caller holds c.mu.
func (c *Core) zenRestore(name string, now time.Time) (*ipc.ZenVersion, error) {
	z := &c.zen
	v, err := z.book.Find(name)
	switch {
	case zen.IsNone(err):
		return nil, ipc.Errorf(ipc.CodeNotFound, "zen: %v", err)
	case err != nil:
		return nil, ipc.Errorf(ipc.CodeBadArgs, "zen: %v", err)
	}
	z.trial, z.trying = nil, ""
	if z.offer != nil && z.offer.Kind == ipc.ZenKindKeep {
		z.offer = nil
	}
	return c.zenCommit(v.Zen, "Back to your zen of "+v.Created.Local().Format("2 Jan 15:04"), now)
}

// zenAnswer takes the person's answer to what zen offers. Trying a change
// shows it at once, as a trial, and asks to keep it; keeping it makes a
// version. The caller holds c.mu.
func (c *Core) zenAnswer(id, answer string, now time.Time) (*ipc.ZenVersion, error) {
	z := &c.zen
	o := z.offer
	if o == nil || o.ID != id {
		return nil, ipc.Errorf(ipc.CodeStale, "zen offers nothing called %q now", id)
	}
	if answer != ipc.ZenYes && answer != ipc.ZenNo {
		return nil, ipc.Errorf(ipc.CodeBadArgs, "zen: answer %q: must be %s or %s", answer, ipc.ZenYes, ipc.ZenNo)
	}
	yes := answer == ipc.ZenYes
	z.offer, z.answered = nil, now
	switch o.Kind {
	case ipc.ZenKindRest:
		if yes {
			z.desk = now
		} else {
			z.snooze = now.Add(zen.Snooze)
		}
	case ipc.ZenKindNight:
		z.saved.Night = zen.NightOf(now)
		c.saveZen()
	case ipc.ZenKindLearn:
		how := zen.Done
		if !yes && o.No != "" {
			how = zen.Later
		}
		c.zenAnswered(id, how, now)
	case ipc.ZenKindTry:
		s, ok := zen.StepByID(id)
		if !ok {
			break
		}
		if !yes {
			c.zenAnswered(id, zen.Later, now)
			break
		}
		t := s.Apply(z.profile, z.windowText)
		k := s.KeepOffer()
		z.trial, z.trying, z.offer = &t, id, &k
	case ipc.ZenKindKeep:
		s, _ := zen.StepByID(id)
		t := z.trial
		z.trial, z.trying = nil, ""
		c.zenAnswered(id, zen.Done, now)
		if yes && t != nil {
			return c.zenCommit(*t, s.Why, now)
		}
	}
	return nil, nil
}

// zenAnswered records a step's answer. The caller holds c.mu.
func (c *Core) zenAnswered(id, how string, now time.Time) {
	a := c.zen.saved.Path[id]
	if how == zen.Later {
		a.Times++
	}
	a.How, a.At = how, now
	c.zen.saved.Path[id] = a
	c.saveZen()
}

// zenSweep looks at the moment while zen is on: whether the person is at
// the computer, and what zen may offer, one thing at a time.
func (c *Core) zenSweep() {
	c.mu.Lock()
	on := c.zen.on
	idle, fresh := c.gab.idle, time.Since(c.gab.idleRead) < 30*time.Second
	c.mu.Unlock()
	if !on {
		return
	}
	// Gabriel's reading when it is fresh; else ioreg, outside the lock.
	if !fresh {
		idle = 0
		if d, ok := machineIdle(); ok {
			idle = d
		}
	}
	c.mu.Lock()
	c.zenMoment(time.Now(), idle)
	c.mu.Unlock()
	c.publishHeader()
}

// zenMoment brings what zen knows up to now, and offers what is due: a
// rest first, then the word at night, then a step of the path. The caller
// holds c.mu.
func (c *Core) zenMoment(now time.Time, idle time.Duration) {
	z := &c.zen
	if !z.on {
		return
	}
	if c.appViews == 0 {
		if z.gone.IsZero() {
			z.gone = now
		} else if now.Sub(z.gone) >= zenGone {
			c.zenLeave()
		}
		return
	}
	z.gone = time.Time{}
	// Away from the computer counts as a rest: the count starts again
	// when the person comes back, and a rest offered meanwhile goes.
	if idle >= zen.Away {
		z.desk = time.Time{}
		if z.offer != nil && z.offer.Kind == ipc.ZenKindRest {
			z.offer = nil
		}
		return
	}
	if z.desk.IsZero() {
		z.desk = now
	}
	if z.offer != nil {
		return
	}
	m := zen.Moment{Now: now, On: now.Sub(z.since), Answered: z.answered, Today: z.saved.Path.Today(now),
		Idle: idle, AtDesk: now.Sub(z.desk)}
	if x := c.sessions[z.session]; x != nil {
		st := x.wire().State
		m.NeedsYou, m.Working = st == ipc.StateNeedsYou, st == ipc.StateWorking
	}
	var o ipc.ZenOffer
	switch {
	case !now.Before(z.snooze) && zen.RestDue(z.profile, m):
		o = zen.RestOffer()
	case zen.NightDue(z.profile, m, z.saved.Night) && now.Sub(z.answered) >= time.Minute:
		o = zen.NightOffer()
	case zen.MayStep(m):
		s, ok := zen.Next(z.profile, z.windowText, z.saved.Path)
		if !ok {
			return
		}
		o = s.Offer()
	default:
		return
	}
	z.offer = &o
}
