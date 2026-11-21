package core

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"slices"
	"time"

	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/keyring"
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

// zenKeyName names the key that opens your zen's vault (internal/keyring):
// in the login keychain, the item "Irie vault: zen".
const zenKeyName = "zen"

// zenVaultWait is how long a starting core waits for your zen's vault to
// open before going on without it: its key comes from the keychain, which
// can take a moment, or ask for the login password.
const zenVaultWait = 2 * time.Second

// zenRetry is how soon after a try that failed a change of your zen tries
// the vault again: a locked keychain asks for its password each time.
const zenRetry = time.Minute

// errZenOpening is why your zen's vault isn't open while its key is on its
// way.
var errZenOpening = errors.New("it is still opening")

// zenState is zen as the core keeps it, under c.mu.
type zenState struct {
	loaded     bool
	keys       keyring.Source // where the vault's key is kept
	book       zen.Book       // sealed once its vault is open
	locked     error          // why the vault isn't open
	opening    bool           // its key is on its way
	tried      time.Time      // the vault was last tried
	profile    ipc.ZenProfile // your zen: the newest version's, or the first
	head       string
	on         bool               // a client holds zen
	holds      map[string]zenHold // each client's zen, by ipc.ZenArgs' Client
	session    string             // the session zen shows: the one a client said last
	since      time.Time          // zen on since
	gone       time.Time          // the app went while it held zen
	windowText int                // the window's text size, as the app last said
	trial      *ipc.ZenProfile    // a step's change on trial
	trying     string             // that step's id
	offer      *ipc.ZenOffer
	answered   time.Time       // the last offer was answered
	desk       time.Time       // at the computer since; zero while away
	snooze     time.Time       // no rest before
	saved      zenSaved        // what a restarted core keeps
	told       map[string]bool // sessions told zen is on
}

// zenHold is a client's zen: the session it shows, and when it last said
// it was in zen.
type zenHold struct {
	Session string    `json:"session,omitempty"`
	Said    time.Time `json:"said"`
}

// zenSaved is the state file's: how far along the path the person is, the
// night zen last spoke, the sessions told zen is on, so a core that
// restarts tells no agent twice, and your zen as it is, for a core whose
// vault isn't open yet. Nothing about how anyone feels, and none of the
// history, which stays in the vault.
type zenSaved struct {
	Path    zen.Progress       `json:"path"`
	Night   string             `json:"night,omitempty"`
	Told    []string           `json:"told,omitempty"`
	Profile *ipc.ZenProfile    `json:"profile,omitempty"`
	Head    string             `json:"head,omitempty"`
	Holds   map[string]zenHold `json:"holds,omitempty"` // the clients in zen, which a restarted core keeps until they lapse
}

// loadZen reads the state file, with your zen as it was, as the core
// starts (startZen), or as a core that never started first takes an op.
// The vault opens after (openZen). The caller holds c.mu, or nothing runs
// beside it yet.
func (c *Core) loadZen() {
	z := &c.zen
	if z.keys == nil {
		z.keys = keyring.Default(c.paths.Keys())
	}
	z.book, z.locked = zen.Open(c.paths.Zen()), errZenOpening
	z.profile = zen.Default()
	z.saved.Path = zen.Progress{}
	if b, err := os.ReadFile(c.paths.ZenState()); err == nil {
		if err := json.Unmarshal(b, &z.saved); err != nil {
			c.log.Warn("zen state", "err", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		c.log.Warn("zen state", "err", err)
	}
	if z.saved.Path == nil {
		z.saved.Path = zen.Progress{}
	}
	// Your zen as the state file keeps it, until the vault opens; or, from
	// a core before the state file kept it, the newest version kept plain.
	if p := z.saved.Profile; p != nil && zen.Check(*p) == nil {
		z.profile, z.head = *p, z.saved.Head
	} else if v, ok, err := z.book.Head(); err != nil && !zen.IsLocked(err) {
		c.log.Warn("zen", "err", err)
	} else if ok {
		z.profile, z.head = v.Zen, v.Name
	}
	c.zen.told = map[string]bool{}
	for _, id := range c.zen.saved.Told {
		c.zen.told[id] = true
	}
	// The clients that were in zen are still, until they lapse or say
	// otherwise: each that restarts with the core comes back into zen.
	z.holds = map[string]zenHold{}
	for client, h := range z.saved.Holds {
		if zenClient(client) {
			z.holds[client] = h
		}
	}
	if len(z.holds) > 0 {
		now := time.Now()
		z.on, z.since, z.desk, z.session = true, now, now, z.latest()
	}
	c.zen.loaded = true
}

// zenClient says whether a client may hold zen.
func zenClient(client string) bool {
	switch client {
	case ipc.ZenApp, ipc.ZenSidebar, ipc.ZenPhone:
		return true
	}
	return false
}

// latest is the session of the client that said last it was in zen.
func (z *zenState) latest() string {
	var last zenHold
	for _, h := range z.holds {
		if h.Said.After(last.Said) {
			last = h
		}
	}
	return last.Session
}

// saveZen writes the state file. The caller holds c.mu.
func (c *Core) saveZen() {
	s := &c.zen.saved
	s.Told = s.Told[:0]
	for id := range c.zen.told {
		s.Told = append(s.Told, id)
	}
	slices.Sort(s.Told)
	p := c.zen.profile
	s.Profile, s.Head = &p, c.zen.head
	s.Holds = maps.Clone(c.zen.holds)
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
	if len(z.holds) > 0 {
		h.Clients = slices.Sorted(maps.Keys(z.holds))
	}
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
		// Only a core that never started comes here, as in a test, whose
		// keys are files: fetched under c.mu, they take no time.
		kek, kerr := c.zen.keys.Key(zenKeyName)
		c.openZen(kek, kerr, now)
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
	case a.On != nil && a.Client != "" && !zenClient(a.Client):
		err = ipc.Errorf(ipc.CodeBadArgs, "zen: client %q: must be %s, %s or %s", a.Client, ipc.ZenApp, ipc.ZenSidebar, ipc.ZenPhone)
	case a.On != nil:
		c.zenShow(*a.On, a.Client, a.Session, a.WindowText, now)
	}
	if err == nil && a.Log > 0 {
		res.Versions, err = c.zen.book.Log(min(a.Log, ipc.MaxZenLog))
	}
	if err == nil && a.Export {
		res.Docs, err = c.zen.book.Export()
	}
	if zen.IsLocked(err) {
		c.zenRetryVault(now)
		err = zenLockedErr(c.zen.locked)
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
func (c *Core) zenShow(on bool, client, session string, windowText int, now time.Time) {
	z := &c.zen
	if client == "" {
		client = ipc.ZenApp
	}
	if windowText >= zen.MinText && windowText <= 96 {
		z.windowText = windowText
	}
	if !on {
		_, held := z.holds[client]
		delete(z.holds, client)
		switch {
		case len(z.holds) == 0 && (z.on || len(z.told) > 0):
			// The last client left zen; or zen ended while no core
			// heard it, as across a restart.
			c.zenLeave()
		case held:
			z.session = z.latest()
			c.saveZen()
		}
		return
	}
	if client == ipc.ZenApp {
		z.gone = time.Time{}
	}
	if z.holds == nil {
		z.holds = map[string]zenHold{}
	}
	z.holds[client] = zenHold{Session: session, Said: now}
	if !z.on {
		z.on, z.since, z.desk = true, now, now
	}
	z.session = session
	c.zenTell(session)
	c.saveZen()
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
	clear(z.holds)
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
// newest. With the vault shut it keeps nothing, rather than a version in
// plain, and tries the vault again. The caller holds c.mu.
func (c *Core) zenCommit(p ipc.ZenProfile, why string, now time.Time) (*ipc.ZenVersion, error) {
	z := &c.zen
	if !z.book.Sealed() {
		c.zenRetryVault(now)
		return nil, zenLockedErr(z.locked)
	}
	v, made, err := z.book.Commit(p, why, now)
	if err != nil {
		return nil, err
	}
	z.profile, z.head = v.Zen, v.Name
	if !made {
		return nil, nil
	}
	c.saveZen()
	c.log.Info("zen", "version", v.Name, "why", v.Why)
	return &v, nil
}

// zenLockedErr is what a change of your zen, or a look at its history,
// says while its vault is shut; the log says why.
func zenLockedErr(why error) error {
	if errors.Is(why, errZenOpening) {
		return ipc.Errorf(ipc.CodeBusy, "zen: your zen's vault is still opening, so nothing was kept; try again in a moment")
	}
	return ipc.Errorf(ipc.CodeBusy, "zen: your zen's vault is locked, so nothing was kept; it opens once your keychain gives its key")
}

// startZen loads your zen as the core starts, and opens its vault. Its key
// comes from the keychain, which can take a moment, or ask for the login
// password, so the core waits zenVaultWait at most, and after that the
// vault opens in the background.
func (c *Core) startZen() {
	c.mu.Lock()
	c.loadZen()
	c.zen.opening = true
	c.mu.Unlock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.unlockZen()
	}()
	select {
	case <-done:
	case <-time.After(zenVaultWait):
		c.log.Info("zen: its vault opens in the background")
	}
}

// unlockZen fetches the vault's key, outside c.mu, and opens the vault.
func (c *Core) unlockZen() {
	c.mu.Lock()
	keys := c.zen.keys
	c.mu.Unlock()
	kek, err := keys.Key(zenKeyName)
	c.mu.Lock()
	c.zen.opening = false
	changed := c.openZen(kek, err, time.Now())
	c.mu.Unlock()
	if changed {
		c.publishHeader()
	}
}

// zenRetryVault tries the vault again, in the background, unless it was
// tried a moment ago or is being tried. The caller holds c.mu.
func (c *Core) zenRetryVault(now time.Time) {
	z := &c.zen
	if z.book.Sealed() || z.opening || now.Sub(z.tried) < zenRetry {
		return
	}
	z.opening = true
	go c.unlockZen()
}

// openZen opens your zen's vault with kek, the key the keychain keeps for
// it, or notes kerr, why there is none, and moves in the versions kept
// before the vault. A vault the key doesn't open, as when the keychain
// lost its key, is set aside whole, beside it, where that key could still
// open it, and your zen starts a new vault as it is. It says whether your
// zen changed. The caller holds c.mu.
func (c *Core) openZen(kek []byte, kerr error, now time.Time) bool {
	z := &c.zen
	z.tried = now
	if z.book.Sealed() {
		return false
	}
	if kerr != nil {
		z.locked = kerr
		c.log.Warn("zen: the vault's key", "err", kerr)
		return false
	}
	dir := c.paths.Zen()
	book, err := zen.OpenVault(dir, kek)
	if zen.IsWrongKey(err) {
		aside := dir + "-unreadable-" + now.UTC().Format("20060102T150405Z")
		if rerr := os.Rename(dir, aside); rerr != nil {
			err = errors.Join(err, rerr)
		} else {
			c.log.Warn("zen: the vault's key doesn't open it, so it is set aside and a new one starts", "aside", aside)
			book, err = zen.OpenVault(dir, kek)
		}
	}
	if err != nil {
		z.locked = err
		c.log.Warn("zen: the vault", "err", err)
		return false
	}
	z.book, z.locked = book, nil
	if n, err := book.Seal(); err != nil {
		c.log.Warn("zen: sealing the versions kept before the vault", "err", err)
	} else if n > 0 {
		c.log.Info("zen: sealed the versions kept before the vault", "versions", n)
	}
	changed := false
	switch v, ok, err := book.Head(); {
	case err != nil:
		c.log.Warn("zen", "err", err)
	case ok && v.Name != z.head:
		// The newest version is your zen, over the state file's copy.
		z.profile, z.head, changed = v.Zen, v.Name, true
	case !ok && (z.head != "" || z.profile != zen.Default()):
		// A new vault, after one set aside or a folder emptied: your zen
		// as it is becomes its first version.
		z.head = ""
		if _, err := c.zenCommit(z.profile, "Your zen as it was", now); err != nil {
			c.log.Warn("zen", "err", err)
		}
	}
	if s := z.saved; s.Profile == nil || *s.Profile != z.profile || s.Head != z.head {
		c.saveZen()
	}
	return changed
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
	// A client's zen ends once it has gone: the app's zenGone after its
	// last window, another's ipc.ZenLapse after it last said it was in
	// zen. While only the app holds zen and its windows are gone, zen
	// offers nothing.
	away := false
	if _, ok := z.holds[ipc.ZenApp]; ok && c.appViews == 0 {
		if z.gone.IsZero() {
			z.gone = now
		}
		if now.Sub(z.gone) >= zenGone {
			delete(z.holds, ipc.ZenApp)
			z.gone = time.Time{}
		} else {
			away = true
		}
	} else {
		z.gone = time.Time{}
	}
	for client, h := range z.holds {
		if client != ipc.ZenApp && now.Sub(h.Said) >= ipc.ZenLapse {
			delete(z.holds, client)
		}
	}
	if len(z.holds) == 0 {
		c.zenLeave()
		return
	}
	z.session = z.latest()
	if away && len(z.holds) == 1 {
		return
	}
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
