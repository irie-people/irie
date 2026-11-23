package core

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/focus"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/localmodel"
	"github.com/irie-people/irie/internal/proc"
	"github.com/irie-people/irie/internal/skills"
	"github.com/irie-people/irie/internal/sysload"
)

// Gabriel (DESIGN §4.11) carries the user's focus to the machine. While the
// user is at the computer and it is busy, what they work on comes first:
// the session on show while Irie is in front, a session they messaged or
// started lately, a promoted one, the subtasks these delegated, and the app
// in front. Every other session's process tree steps back into macOS's
// background state, as for a promotion (priority.go), and so do the user's
// other heavy apps outside Irie. All of it comes back once the machine is
// calm again, the user walks away, Gabriel is turned off, or the core
// stops; a core that starts first takes back what a core before it left,
// from gabriel.json.
//
// The tab the user is on comes first of all: the top of the focus never
// steps back, not even for a promotion, and while it works the machine
// every other session but an install steps back for it, the rest of the
// focus too. macOS has one background state a user may undo, so the order
// top, rest of the focus, the rest is kept by when each steps back.

// gabrielIdleEvery is how often the machine's idle time is read: ioreg is a
// process to start.
const gabrielIdleEvery = 10 * time.Second

// gabrielCalm is how long the machine must have room to spare before it
// counts as calm again, so that trees do not flap.
const gabrielCalm = 30 * time.Second

// gabrielMostOutside is the most apps outside Irie Gabriel slows at once,
// the heaviest first.
const gabrielMostOutside = 8

// irieBundleID is Irie.app's (mac/Info.plist).
const irieBundleID = brand.BundleID

// terminalApps are terminals Irie's own window may be in: with one in
// front, the session on show may be what the user looks at.
var terminalApps = []string{"com.mitchellh.ghostty", "com.apple.Terminal", "com.googlecode.iterm2", "com.github.wez.wezterm", "net.kovidgoyal.kitty", "org.alacritty", "dev.warp.Warp-Stable"}

// systemDirs hold macOS's own programs, which Gabriel never slows.
var systemDirs = []string{"/System/", "/usr/bin/", "/usr/sbin/", "/usr/libexec/", "/usr/lib/", "/bin/", "/sbin/", "/Library/Apple/", "/private/"}

// gabState is what Gabriel knows and holds back. The sweep writes it under
// c.mu; background reads holding and focus there.
type gabState struct {
	holding   bool            // the user is at the computer and the machine is busy
	present   bool            // the user is at the computer
	idle      time.Duration   // since their last keyboard or mouse input
	idleRead  time.Time       // when idle was read
	busy      bool            // the machine is busy
	calmSince time.Time       // since when it has had room to spare, while busy
	front     focus.App       // the app in front, when read
	frontOK   bool            // whether front is known
	irieFront bool            // Irie, or a terminal it may be in, is in front
	focus     map[string]bool // the sessions in the user's focus
	installs  map[string]bool // the sessions that ran an install at the last sweep, in the focus too
	outside   map[int]outProc // the apps outside Irie it slowed, by pid
	// top is the tab the user is on, with the subtasks it delegated, while
	// anything steps back (onTop): it never does.
	top map[string]bool
	// topBusy says the top works the machine (topBusyNext): every other
	// session but an install steps back for it, the rest of the focus too.
	topBusy bool
	topCalm time.Time // since when the top has run light, while topBusy
	topOf   string    // the session on show that topBusy was read for
	// shownAt is when the user last turned to a tab in Irie (refocus): they
	// were at the computer then, with Irie in front.
	shownAt time.Time
	// boostUntil is when the boost on ends: until then the user's focus
	// gets the machine first whether it is busy or not.
	boostUntil time.Time
}

// outProc is an app outside Irie that Gabriel slowed.
type outProc struct {
	Start int64   `json:"start"` // tells it from a later process with its pid
	Path  string  `json:"path"`
	CPU   float64 `json:"cpu"` // its share of a CPU when it was slowed, in percent
}

// gabrielOff reports whether Gabriel may not run in this core: a test core
// (IRIE_GABRIEL=off, internal/testutil) would take the user's own agents
// for apps outside it.
func gabrielOff() bool { return os.Getenv(brand.Env("GABRIEL")) == "off" }

// gabrielOn reports whether Gabriel works in this core: he is on, the
// system has a background state, and it is not a test core.
func gabrielOn(g config.Gabriel) bool { return g.On && proc.CanBackground && !gabrielOff() }

// holds says whether Gabriel holds back: while he is on and the user is at
// the computer, when the machine is busy or a boost is on, and at all times
// then with arch.gabriel.always. While the user is away nothing is held
// back, so work left running goes at full speed.
func holds(on, present, busy, boosting, always bool) bool {
	return on && present && (busy || boosting || always)
}

// busyNext says whether the machine is busy now, given whether it was:
// busy as soon as the work waiting to run leaves less than a CPU free or
// memory is under pressure; calm again only after gabrielCalm with two
// CPUs free and no pressure.
func busyNext(l sysload.Load, was bool, calmSince, now time.Time) (bool, time.Time) {
	if l.CPUs == 0 {
		return false, time.Time{}
	}
	cpus := float64(l.CPUs)
	if l.RunQueue >= cpus-1 || l.Pressure >= sysload.PressureWarn {
		return true, time.Time{}
	}
	if !was {
		return false, time.Time{}
	}
	if l.RunQueue > cpus-2 {
		return true, time.Time{}
	}
	if calmSince.IsZero() {
		calmSince = now
	}
	if now.Sub(calmSince) >= gabrielCalm {
		return false, time.Time{}
	}
	return true, calmSince
}

// inFocus is the set of sessions in the user's focus: promoted ones, those
// they messaged or started within word, the one on show while Irie is in
// front, and the subtasks these delegated. The caller holds c.mu.
func (c *Core) inFocus(irieFront bool, word time.Duration, now time.Time) map[string]bool {
	f := map[string]bool{}
	for id, x := range c.sessions {
		if x.track.priority > 0 || !x.userWord.IsZero() && now.Sub(x.userWord) < word {
			f[id] = true
		}
	}
	if irieFront && c.selected != "" {
		f[c.selected] = true
	}
	return c.withDelegates(f)
}

// withDelegates adds to set every live session that one in it delegated
// (ctl delegate), and theirs in turn: a session waits on the subtasks it
// hands out, so they go where it goes. The caller holds c.mu.
func (c *Core) withDelegates(set map[string]bool) map[string]bool {
	for grew := true; grew; {
		grew = false
		for id, x := range c.sessions {
			if !set[id] && !x.dead && x.out != nil && x.out.parent != "" && set[x.out.parent] {
				set[id] = true
				grew = true
			}
		}
	}
	return set
}

// onTop is the tab the user is on: the session on show while Irie, or a
// terminal it may be in, is in front, with the subtasks it delegated. It
// comes first of all, and never steps back. The caller holds c.mu.
func (c *Core) onTop(irieFront bool) map[string]bool {
	x := c.sessions[c.selected]
	if !irieFront || x == nil || x.dead {
		return nil
	}
	return c.withDelegates(map[string]bool{x.id: true})
}

// topBusyNext says whether the top works the machine now, given whether it
// did: at once when its trees use heavy of a CPU between them, as the last
// sample read them, or memory is critically short; light again only after
// gabrielCalm with neither, so the rest does not flap between the top's
// tool calls.
func topBusyNext(cpu, heavy float64, critical, was bool, calmSince, now time.Time) (bool, time.Time) {
	if cpu >= heavy || critical {
		return true, time.Time{}
	}
	if !was {
		return false, time.Time{}
	}
	if calmSince.IsZero() {
		calmSince = now
	}
	if now.Sub(calmSince) >= gabrielCalm {
		return false, time.Time{}
	}
	return true, calmSince
}

// placeTop works out st's top and whether it works the machine. There is a
// top whenever the user is at the computer, so that a promotion, which
// puts the others in the background at once, never puts it there; whether
// it works the machine counts only while Gabriel holds back. A tab the user
// turns to starts afresh. The caller holds c.mu.
func (c *Core) placeTop(st *gabState, now time.Time) {
	st.top = nil
	if st.present {
		st.top = c.onTop(st.irieFront)
	}
	if !st.holding || len(st.top) == 0 {
		st.topBusy, st.topCalm, st.topOf = false, time.Time{}, ""
		return
	}
	if st.topOf != c.selected {
		st.topBusy, st.topCalm, st.topOf = false, time.Time{}, c.selected
	}
	var cpu float64
	for id := range st.top {
		cpu += c.sessions[id].cpu
	}
	st.topBusy, st.topCalm = topBusyNext(cpu, c.cfg().Arch.Gabriel.Heavy, c.load.Pressure >= sysload.PressureCritical, st.topBusy, st.topCalm, now)
}

// refocus takes the session now on show into the user's focus, and to the
// top of it, and moves the trees before it returns: a show never waits for
// Gabriel's next sweep, up to sampleEvery ticks away, with the session the
// user turned to still in the background. A show is the user's own doing:
// they are at the computer, with Irie in front, whatever the last sweep
// read. The caller holds c.mu.
func (c *Core) refocus() {
	g := c.cfg().Arch.Gabriel
	if !gabrielOn(g) {
		return
	}
	now := time.Now()
	if !c.gab.present {
		// Back from away: the next sweep reads the idle time afresh, and
		// holds back again as it says.
		c.gab.idleRead = time.Time{}
	}
	c.gab.shownAt, c.gab.present, c.gab.irieFront = now, true, true
	c.gab.focus = c.focusNow(true, c.gab.installs, g.Word, now)
	c.placeTop(&c.gab, now)
	c.background()
}

// focusNow is the user's focus (inFocus), and every session that runs an
// install, which is everyone's and never steps back. The caller holds
// c.mu.
func (c *Core) focusNow(irieFront bool, installs map[string]bool, word time.Duration, now time.Time) map[string]bool {
	f := c.inFocus(irieFront, word, now)
	for id := range installs {
		f[id] = true
	}
	return f
}

// markWord notes that the user just messaged or started session id: it
// stays in their focus for arch.gabriel.word.
func (c *Core) markWord(id string) {
	c.mu.Lock()
	if x := c.sessions[id]; x != nil {
		x.userWord = time.Now()
	}
	c.mu.Unlock()
}

// gabrielSweep reads the user's focus and the machine, and holds back or
// lets go as they say. One runs at a time; the caller holds no lock.
func (c *Core) gabrielSweep() {
	if !c.gabSweeping.CompareAndSwap(false, true) {
		return
	}
	defer c.gabSweeping.Store(false)
	g := c.cfg().Arch.Gabriel
	now := time.Now()
	c.mu.Lock()
	st := c.gab
	load := c.load
	c.mu.Unlock()

	on := gabrielOn(g)
	if on {
		if now.Sub(st.idleRead) >= gabrielIdleEvery {
			if idle, ok := machineIdle(); ok {
				st.idle = idle
			} else {
				st.idle = 0 // no reading: take the user to be at the computer
			}
			st.idleRead = now
		}
		st.present = g.Away == 0 || st.idle < g.Away
		st.busy, st.calmSince = busyNext(load, st.busy, st.calmSince, now)
	} else {
		st.present, st.busy, st.calmSince = false, false, time.Time{}
	}
	// A boost holds back as a busy machine does, until its time is up, and
	// arch.gabriel.always for good; while the user is away they wait like
	// the rest.
	st.holding = holds(on, st.present, st.busy, now.Before(st.boostUntil), g.Always)
	st.frontOK, st.irieFront = false, false
	// What is in front says what the user looks at: the tab they are on
	// never steps back (placeTop), not even for a promotion, which comes at
	// once and needs it known.
	if st.present {
		if a, ok, err := focus.Front(); err == nil && ok {
			st.front, st.frontOK = a, true
			st.irieFront = a.BundleID == irieBundleID || slices.Contains(terminalApps, a.BundleID)
		} else {
			// Not knowing what is in front, Irie may be: the session on
			// show keeps its place.
			st.irieFront = true
		}
	}

	// An install in progress is everyone's: it never steps back.
	st.installs = nil
	if st.holding {
		st.installs = c.installing()
	}
	c.mu.Lock()
	trees := c.sessionRoots()
	held := maps.Clone(c.gab.outside)
	c.mu.Unlock()

	outside := c.slowOutside(held, st, g, trees)

	c.mu.Lock()
	// A show made during the sweep wins: the user turned to a tab in Irie,
	// so they are at the computer with Irie in front, and the idle time is
	// read afresh next time (refocus). The focus and its top are read last,
	// so it keeps the session it brought there, and the top goes on from
	// where the show left it.
	if c.gab.shownAt.After(st.shownAt) {
		st.shownAt, st.present, st.irieFront, st.idleRead = c.gab.shownAt, true, true, time.Time{}
	}
	st.topBusy, st.topCalm, st.topOf = c.gab.topBusy, c.gab.topCalm, c.gab.topOf
	st.focus = c.focusNow(st.irieFront, st.installs, g.Word, now)
	c.placeTop(&st, now)
	st.outside = outside
	// A boost started or stopped meanwhile wins; one whose time is up ends.
	st.boostUntil = c.gab.boostUntil
	if !st.boostUntil.IsZero() && !now.Before(st.boostUntil) {
		st.boostUntil = time.Time{}
	}
	c.gab = st
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	// A new Irie.app that waits for the user to look away (appswap.go).
	c.swapApp()
}

// sessionRoots are the pids of the sessions' panes, whose trees are the
// sessions' own. The caller holds c.mu.
func (c *Core) sessionRoots() []int {
	var roots []int
	for _, x := range c.sessions {
		if p := c.snap.Pane(x.pane); p != nil && !p.Dead && p.PID > 1 {
			roots = append(roots, p.PID)
		}
	}
	return roots
}

// slowOutside puts the user's heavy apps outside Irie in the background
// while Gabriel holds back, and takes out those that no longer should be
// there: when he stops holding back, when one comes to the front or is
// spared, or is gone. It returns what is slowed now, and keeps it in
// gabriel.json for a core that comes after. It holds no lock.
func (c *Core) slowOutside(held map[int]outProc, st gabState, g config.Gabriel, roots []int) map[int]outProc {
	if held == nil {
		held = map[int]outProc{}
	}
	want := map[int]outProc{}
	var t proc.Table
	if st.holding && g.Outside || len(held) > 0 {
		var err error
		if t, err = proc.Read(); err != nil {
			c.log.Warn("gabriel", "err", err)
			return held
		}
	}
	if st.holding && g.Outside {
		if ps, err := focus.Procs(); err == nil {
			awake, _ := focus.Awake()
			want = c.heavyOutside(ps, t, awake, st, g, roots, held)
		} else {
			c.log.Warn("gabriel", "err", err)
		}
	}
	changed := false
	for pid, o := range held {
		if w, keep := want[pid]; keep && w.Start == o.Start {
			continue
		}
		// It may have started children while slowed, which took its
		// state: they come out too.
		for _, q := range t.Tree(pid) {
			if q.PID == pid && q.Start != o.Start {
				break
			}
			proc.SetBackground(q.PID, false)
		}
		delete(held, pid)
		changed = true
	}
	for pid, o := range want {
		if h, ok := held[pid]; ok && h.Start == o.Start {
			continue
		}
		if err := proc.SetBackground(pid, true); err == nil {
			held[pid] = o
			changed = true
		}
	}
	if changed {
		c.saveOutside(held)
	}
	return held
}

// heavyOutside picks the user's apps outside Irie to slow: their own
// processes over arch.gabriel.heavy, and those already slowed while they
// are still around, but never one in a session's tree or Irie's own, one of
// the app in front, one macOS keeps awake for (sound, video, a call), one
// in a system folder, or a spared app. The heaviest come first, up to
// gabrielMostOutside.
func (c *Core) heavyOutside(ps []focus.Proc, t proc.Table, awake map[int]bool, st gabState, g config.Gabriel, roots []int, held map[int]outProc) map[int]outProc {
	mine := os.Getuid()
	inTree := map[int]bool{}
	for _, r := range roots {
		for _, q := range t.Tree(r) {
			inTree[q.PID] = true
		}
	}
	front := map[int]bool{}
	if st.frontOK && st.front.PID > 0 {
		for _, q := range t.Tree(st.front.PID) {
			front[q.PID] = true
		}
	}
	tmuxBin := ""
	if c.tmux != nil {
		tmuxBin = c.tmux.Bin
	}
	var picks []focus.Proc
	for _, p := range ps {
		o, wasHeld := held[p.PID]
		q, alive := t.Get(p.PID)
		// A process that took a slowed one's pid is new.
		wasHeld = wasHeld && alive && q.Start == o.Start
		switch {
		case p.UID != mine || p.PID == os.Getpid() || !alive:
		case !wasHeld && p.CPU < g.Heavy:
		case inTree[p.PID] || front[p.PID] || awake[p.PID]:
		case p.Path == c.bin || tmuxBin != "" && p.Path == tmuxBin || strings.Contains(p.Path, "/"+brand.Name+".app/"):
		case st.frontOK && st.front.BundlePath != "" && strings.HasPrefix(p.Path, st.front.BundlePath+"/"):
		case systemPath(p.Path) || spared(p.Path, g.Spare):
		default:
			picks = append(picks, p)
		}
	}
	slices.SortFunc(picks, func(a, b focus.Proc) int {
		switch {
		case a.CPU > b.CPU:
			return -1
		case a.CPU < b.CPU:
			return 1
		}
		return a.PID - b.PID
	})
	want := map[int]outProc{}
	for _, p := range picks[:min(len(picks), gabrielMostOutside)] {
		q, _ := t.Get(p.PID)
		cpu := p.CPU
		if o, ok := held[p.PID]; ok && o.Start == q.Start {
			cpu = o.CPU
		}
		want[p.PID] = outProc{Start: q.Start, Path: p.Path, CPU: cpu}
	}
	return want
}

// systemPath reports whether path is one of macOS's own programs.
func systemPath(path string) bool {
	return slices.ContainsFunc(systemDirs, func(d string) bool { return strings.HasPrefix(path, d) })
}

// spared reports whether the program at path is one of the spared apps,
// by its app's name or its own.
func spared(path string, spare []string) bool {
	base := filepath.Base(path)
	for _, s := range spare {
		if s == "" {
			continue
		}
		if strings.EqualFold(base, s) || strings.Contains(strings.ToLower(path), "/"+strings.ToLower(s)+".app/") {
			return true
		}
	}
	return false
}

// outsideFile keeps what Gabriel slowed outside Irie, for the core after
// this one.
func (c *Core) outsideFile() string { return filepath.Join(c.paths.State, "gabriel.json") }

func (c *Core) saveOutside(held map[int]outProc) {
	if len(held) == 0 {
		os.Remove(c.outsideFile())
		return
	}
	b, err := json.Marshal(held)
	if err == nil {
		err = filetx.WriteFile(c.outsideFile(), b, 0o600)
	}
	if err != nil {
		c.log.Warn("gabriel", "err", err)
	}
}

// releaseOutside takes every app Gabriel slowed out of the background: those
// this core holds, and those a core before it left in gabriel.json. A core
// calls it as it starts and as it stops, so nothing stays slowed.
func (c *Core) releaseOutside() {
	c.mu.Lock()
	held := maps.Clone(c.gab.outside)
	c.gab.outside = nil
	c.mu.Unlock()
	if b, err := os.ReadFile(c.outsideFile()); err == nil {
		var left map[int]outProc
		if json.Unmarshal(b, &left) == nil {
			if held == nil {
				held = map[int]outProc{}
			}
			maps.Copy(held, left)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		c.log.Warn("gabriel", "err", err)
	}
	if len(held) > 0 && proc.CanBackground {
		if t, err := proc.Read(); err == nil {
			for pid, o := range held {
				if q, ok := t.Get(pid); ok && q.Start == o.Start {
					for _, d := range t.Tree(pid) {
						proc.SetBackground(d.PID, false)
					}
				}
			}
		}
	}
	os.Remove(c.outsideFile())
}

// gabrielOp starts or stops a boost, then reports (OpGabriel). A boost
// lasts For, or arch.gabriel.boost, at most config.MaxBoost; starting one
// lets the idle models of Irie's own Ollama go, to free their memory. It
// changes only which processes macOS runs first, never a clock or a fan.
func (c *Core) gabrielOp(a ipc.GabrielArgs) (ipc.GabrielResult, error) {
	var freed []string
	switch {
	case a.Boost && a.Stop:
		return ipc.GabrielResult{}, ipc.Errorf(ipc.CodeBadArgs, "boost or stop, not both")
	case a.Boost:
		d := c.cfg().Arch.Gabriel.Boost
		if a.For != "" {
			var err error
			if d, err = time.ParseDuration(a.For); err != nil || d < time.Minute || d > config.MaxBoost {
				return ipc.GabrielResult{}, ipc.Errorf(ipc.CodeBadArgs, "for %q: give from 1m to %v, such as 30m", a.For, config.MaxBoost)
			}
		}
		c.mu.Lock()
		c.gab.boostUntil = time.Now().Add(d)
		c.mu.Unlock()
		freed = c.freeModels()
		go c.gabrielSweep()
	case a.Stop:
		c.mu.Lock()
		c.gab.boostUntil = time.Time{}
		c.mu.Unlock()
		go c.gabrielSweep()
	}
	r := c.gabrielReport()
	r.Freed = freed
	if a.Boost || a.Stop {
		// Every client hears at once (the header's boost).
		c.publishHeader()
	}
	return r, nil
}

// boostHeader is Gabriel's boost for the header while one is on: when a
// boost started by hand ends, and arch.gabriel.always; nil while none is,
// or while he is off. The caller holds c.mu.
func (c *Core) boostHeader(now time.Time) *ipc.Boost {
	g := c.cfg().Arch.Gabriel
	if !g.On || !proc.CanBackground || gabrielOff() {
		return nil
	}
	b := ipc.Boost{Always: g.Always}
	if now.Before(c.gab.boostUntil) {
		until := c.gab.boostUntil
		b.Until = &until
	}
	if b.Until == nil && !b.Always {
		return nil
	}
	return &b
}

// freeModels lets the models of Irie's own Ollama go that no live session
// runs on, and returns their names. It holds no lock.
func (c *Core) freeModels() []string {
	c.mu.Lock()
	inUse := map[string]bool{}
	for _, x := range c.sessions {
		if x.local && !x.dead && x.model != "" {
			inUse[x.model] = true
		}
	}
	own := false
	if c.cfg().Local.URL == "" {
		rt := c.runtimePane()
		own = rt != nil && !rt.Dead
	}
	c.mu.Unlock()
	if !own {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	o := localmodel.Ollama{URL: c.ownURL(), HTTP: c.http}
	loaded, err := o.Running(ctx)
	if err != nil {
		return nil
	}
	var freed []string
	for _, l := range loaded {
		if inUse[l.Name] {
			continue
		}
		if err := o.Unload(ctx, l.Name); err != nil {
			c.log.Warn("gabriel: unload", "model", l.Name, "err", err)
			continue
		}
		freed = append(freed, l.Name)
	}
	return freed
}

// gabrielReport is `irie ctl gabriel`: what Gabriel sees and holds back.
func (c *Core) gabrielReport() ipc.GabrielResult {
	g := c.cfg().Arch.Gabriel
	lowPower := focus.LowPower()
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.gab
	r := ipc.GabrielResult{
		On: gabrielOn(g), Outside: g.Outside, Present: st.present, Idle: st.idle.Round(time.Second).String(),
		Busy: st.busy, Holding: st.holding, Always: g.Always, CPUs: c.load.CPUs, RunQueue: c.load.RunQueue, Pressure: c.load.Pressure,
	}
	if st.frontOK {
		r.Front = st.front.Name
	}
	if time.Now().Before(st.boostUntil) {
		until := st.boostUntil
		r.BoostUntil = &until
	}
	r.LowPower = lowPower
	for _, id := range slices.Sorted(maps.Keys(st.focus)) {
		if x := c.sessions[id]; x != nil {
			r.Focus = append(r.Focus, ipc.GabrielSession{ID: id, Name: x.name})
		}
	}
	// The tab the user is on first, then what it delegated.
	top := slices.SortedFunc(maps.Keys(st.top), func(a, b string) int {
		switch {
		case a == c.selected:
			return -1
		case b == c.selected:
			return 1
		}
		return strings.Compare(a, b)
	})
	for _, id := range top {
		if x := c.sessions[id]; x != nil {
			r.Top = append(r.Top, ipc.GabrielSession{ID: id, Name: x.name})
			r.TopCPU += x.cpu
		}
	}
	r.TopBusy = st.topBusy
	for _, id := range slices.Sorted(maps.Keys(c.sessions)) {
		x := c.sessions[id]
		switch {
		case !x.demoted || st.top[id]:
		case x.track.priority == 0 && !st.focus[id] && st.holding:
			r.Held = append(r.Held, ipc.GabrielSession{ID: id, Name: x.name})
		case st.focus[id] && st.topBusy:
			r.Back = append(r.Back, ipc.GabrielSession{ID: id, Name: x.name})
		}
	}
	for _, pid := range slices.Sorted(maps.Keys(st.outside)) {
		o := st.outside[pid]
		r.Apps = append(r.Apps, ipc.GabrielApp{PID: pid, Name: filepath.Base(o.Path), CPU: o.CPU})
	}
	return r
}

// focusNote is what an agent is told when Gabriel turns away a call that
// would put a window where the user works.
var focusNote = "[" + brand.Name + "] " + brand.Gabriel + " turned this call away: it would put a window where your user works, in front of what they look at, or on the desktop they are on even behind it. Work without a window where you can: a browser headless (--headless=new keeps the profile and its logins), pages in the background with no bringToFront, a Mac view checked with " + brand.Name + ".app's --snapshot. A window you must open goes through \"$" + brand.Env("BIN") + "\" away -- COMMAND, with open -g inside it, which puts each window it opens on a desktop they are not looking at; never a tab in their own browser. The " + skills.Plugin + ":out-of-sight skill says more. If your user must see it, say so in a line and let them open it."

var (
	// appActivates is AppleScript that brings an app forward or types into
	// one.
	appActivates = regexp.MustCompile(`(?i)\b(activate|reopen|keystroke|key code|open location)\b`)
	// hereDoc starts a here-document: <<END, <<-END, <<'END' or <<"END".
	hereDoc = regexp.MustCompile(`<<-?[ \t]*['"]?([A-Za-z_][A-Za-z0-9_]*)['"]?`)
	// redirect is a word that sends a stream to a file or reads one: >,
	// 2>, >>out, <in, &>out, 2>&1.
	redirect = regexp.MustCompile(`^(\d*|&)(>>?|<)`)
	// assignment sets a variable for the command after it: PATH=/usr/bin.
	assignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
)

// shells run the script they are given with -c.
var shells = []string{"sh", "bash", "zsh", "dash", "ksh"}

// quietFlags keep an app started as the command, or that open(1) passes
// them to, from showing a window where the user works: Irie.app's own runs,
// which draw out of sight or put the window back on its desktop
// (Desktops.swift), and a browser headless.
var quietFlags = []string{"--snapshot", "--render-conversation", "--render-onboarding", "--record", "--bench", "--background", "--away", "--away-helper", "--version", "--help", "--headless"}

// prefixes run the command after them, as sudo or nice do, or start a
// compound command, as then or { do. Each maps to the letters of its
// options that take a value, which is skipped with the option.
var prefixes = map[string]string{
	"sudo": "ughpCDrtUT", "env": "uSP", "nice": "n", "timeout": "sk", "caffeinate": "tw", "xargs": "nILPsEJR",
	"nohup": "", "exec": "a", "command": "", "time": "", "!": "", "{": "",
	"do": "", "then": "", "else": "", "elif": "", "if": "", "while": "", "until": "",
}

// windowEffect is where a shell command would put a window, from best to
// worst.
type windowEffect int

const (
	outOfSight windowEffect = iota // nowhere the user works
	inSight                        // on the desktop the user is on, behind the app in front
	inFront                        // in front: an app, a window or a page brought forward
)

// stealsFocus reports whether a shell command would bring an app, a window
// or a page to the front: open(1) without -g or -j, AppleScript that
// activates an app or types into it, or an app's own executable started as
// the command, short of a headless browser and Irie.app's --snapshot,
// --record, --background, --away and the like.
func stealsFocus(cmd string) bool { return windowOf(cmd) == inFront }

// opensInSight reports whether a shell command would open a window on the
// desktop the user is on, behind the app in front, where it still covers
// what they see: open(1) with -g but not -j, short of what Irie.app keeps
// out of sight itself. irie away is the way to open one (DESIGN §4.11, "Out
// of sight").
func opensInSight(cmd string) bool { return windowOf(cmd) == inSight }

// windowOf is the worst any command a shell command runs does with a
// window. It reads the command as a person would, not as a full shell
// grammar, and errs toward letting it run: the guard stops a reflex, and a
// wrong refusal costs the agent a retry.
func windowOf(cmd string) windowEffect {
	worst := outOfSight
	for _, ws := range commands(withoutText(cmd)) {
		words, piped := command(ws)
		if len(words) == 0 {
			continue
		}
		prog, base := words[0], filepath.Base(words[0])
		e := outOfSight
		switch {
		case prog == "open" || prog == "/usr/bin/open":
			e = openEffect(words[1:], piped)
		case base == "osascript":
			if appActivates.MatchString(strings.Join(words, " ")) {
				e = inFront
			}
		case slices.Contains(shells, base):
			// sh -c 'open …': the script it is given.
			for i, w := range words[1:] {
				if strings.HasPrefix(w, "-") && !strings.HasPrefix(w, "--") && strings.HasSuffix(w, "c") {
					if i+2 < len(words) {
						e = windowOf(words[i+2])
					}
					break
				}
			}
		case base == "eval":
			e = windowOf(strings.Join(words[1:], " "))
		case strings.Contains(prog, ".app/Contents/MacOS/"):
			irie := strings.Contains("/"+prog, "/"+brand.Name+".app/") || strings.Contains("/"+prog, "/"+brand.Name+" Preview.app/")
			if !(irie && quietWords(words[1:]) || slices.ContainsFunc(words[1:], headless)) {
				e = inFront
			}
		}
		worst = max(worst, e)
	}
	return worst
}

// commands splits a shell command into the commands it runs, each as its
// words with the shell's quotes taken off. It splits at ; & | ( ) and new
// lines outside quotes, and where $( and backticks start a command, even
// inside double quotes. Quoted text stays one word, so a message that holds
// "; open -a" is an argument and runs nothing.
func commands(cmd string) [][]string {
	var out [][]string
	var cur []string
	var word strings.Builder
	in := false // a word is under way
	endWord := func() {
		if in {
			cur = append(cur, word.String())
			word.Reset()
			in = false
		}
	}
	endCommand := func() {
		endWord()
		if len(cur) > 0 {
			out = append(out, cur)
			cur = nil
		}
	}
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		switch {
		case c == '\\' && i+1 < len(cmd):
			i++
			if cmd[i] != '\n' {
				word.WriteByte(cmd[i])
				in = true
			}
		case c == '\'':
			j := strings.IndexByte(cmd[i+1:], '\'')
			if j < 0 {
				j = len(cmd) - i - 1
			}
			word.WriteString(cmd[i+1 : i+1+j])
			in = true
			i += j + 1
		case c == '"':
			j := i + 1
			for j < len(cmd) && cmd[j] != '"' && cmd[j] != '`' && !(cmd[j] == '$' && j+1 < len(cmd) && cmd[j+1] == '(') {
				if cmd[j] == '\\' {
					j++
				}
				j++
			}
			j = min(j, len(cmd))
			word.WriteString(cmd[i+1 : j])
			in = true
			if j < len(cmd) && cmd[j] != '"' {
				i = j - 1 // a command inside the quotes: read on from it
				continue
			}
			i = j
		case c == '&' && (i > 0 && (cmd[i-1] == '>' || cmd[i-1] == '<') || i+1 < len(cmd) && cmd[i+1] == '>'):
			word.WriteByte(c) // 2>&1, &>out: a redirect, not a new command
			in = true
		case c == '$' && i+1 < len(cmd) && cmd[i+1] == '(':
			endCommand()
			i++
		case strings.IndexByte(";&|\n()`", c) >= 0:
			endCommand()
		case c == ' ' || c == '\t' || c == '\r':
			endWord()
		default:
			word.WriteByte(c)
			in = true
		}
	}
	endCommand()
	return out
}

// command strips what runs a command, such as sudo -u alice, nice, timeout
// 30s or xargs, what starts a compound one, such as then or {, and the
// variables set for it, down to the command itself. piped says xargs runs
// it, with arguments of its own.
func command(words []string) (rest []string, piped bool) {
	for len(words) > 0 {
		w := words[0]
		if assignment.MatchString(w) {
			words = words[1:]
			continue
		}
		takes, ok := prefixes[w]
		if !ok {
			return words, piped
		}
		piped = piped || w == "xargs"
		words = words[1:]
		for len(words) > 0 && strings.HasPrefix(words[0], "-") && words[0] != "-" {
			o := words[0]
			words = words[1:]
			if o == "--" {
				break
			}
			if len(o) == 2 && strings.ContainsRune(takes, rune(o[1])) && len(words) > 0 {
				words = words[1:]
			}
		}
		if w == "timeout" && len(words) > 0 {
			words = words[1:] // its duration
		}
	}
	return words, piped
}

// withoutText drops here-documents, whose lines are text such as a commit
// message, not commands, and may start with words like open. A
// here-document fed to a shell is commands, and stays.
func withoutText(cmd string) string {
	for from := 0; from < len(cmd); {
		m := hereDoc.FindStringSubmatchIndex(cmd[from:])
		if m == nil {
			break
		}
		for i := range m {
			m[i] += from
		}
		// <<< is a here-string, on its own line.
		if m[0] > 0 && cmd[m[0]-1] == '<' {
			from = m[1]
			continue
		}
		delim := cmd[m[2]:m[3]]
		nl := strings.IndexByte(cmd[m[1]:], '\n')
		if nl < 0 {
			cmd = cmd[:m[0]] + cmd[m[1]:]
			from = m[0]
			continue
		}
		body := m[1] + nl + 1
		stop, end := len(cmd), len(cmd)
		for at := body; at < len(cmd); {
			line, next := cmd[at:], len(cmd)
			if i := strings.IndexByte(line, '\n'); i >= 0 {
				line, next = line[:i], at+i+1
			}
			if strings.TrimLeft(line, "\t") == delim {
				stop, end = at, next
				break
			}
			at = next
		}
		fed := false
		if cs := commands(cmd[strings.LastIndexByte(cmd[:m[0]], '\n')+1 : m[0]]); len(cs) > 0 {
			if w, _ := command(cs[len(cs)-1]); len(w) > 0 {
				fed = slices.Contains(shells, filepath.Base(w[0]))
			}
		}
		if fed {
			cmd = cmd[:m[0]] + cmd[m[1]:stop] + cmd[end:]
		} else {
			cmd = cmd[:m[0]] + cmd[m[1]:body] + cmd[end:]
		}
		from = m[0]
	}
	return cmd
}

// quietWords reports whether words hold a flag that keeps the window out
// of sight, as a word of its own, such as --snapshot or --headless=new.
func quietWords(words []string) bool {
	return slices.ContainsFunc(words, func(w string) bool {
		return slices.ContainsFunc(quietFlags, func(q string) bool { return w == q || strings.HasPrefix(w, q+"=") })
	})
}

// headless reports whether a word starts a browser without a window.
func headless(w string) bool { return w == "--headless" || strings.HasPrefix(w, "--headless=") }

// openEffect is where open(1) with args puts a window: nowhere without
// something to open, or hidden (-j); in front without -g; else on the
// desktop the user is on, behind the app in front. Short of that are
// Irie.app given a flag of its own that keeps its window out of sight, as
// tools/live.sh gives it --background, and a new browser (-n) started
// headless; a browser that runs already drops the arguments. piped says
// xargs gives it what to open.
func openEffect(args []string, piped bool) windowEffect {
	target, background, hidden, fresh, irie := piped, false, false, false, false
	var passed []string // what --args hands the app
	opens := func(v string) {
		target = true
		v = strings.TrimSuffix(v, "/")
		base := strings.TrimSuffix(filepath.Base(v), ".app")
		irie = irie || base == brand.Name || base == brand.Name+" Preview" || strings.HasPrefix(v, brand.BundleID)
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--args":
			passed = args[i+1:]
			i = len(args)
		case redirect.MatchString(a):
			if op := strings.TrimLeft(a, "0123456789"); op == ">" || op == ">>" || op == "<" || op == "&>" || op == "&>>" {
				i++ // the file follows
			}
		case a == "--background":
			background = true
		case a == "--hide":
			hidden = true
		case a == "--new":
			fresh = true
		case a == "--url":
			if i+1 < len(args) {
				i++
				opens(args[i])
			}
		case a == "--arch" || a == "--stdin" || a == "--stdout" || a == "--stderr" || a == "--env":
			i++ // its value
		case strings.HasPrefix(a, "--"):
		case strings.HasPrefix(a, "-") && len(a) > 1:
			flags := a[1:]
			hidden = hidden || strings.Contains(flags, "j")
			background = background || strings.Contains(flags, "g")
			fresh = fresh || strings.Contains(flags, "n")
			// The last of -absuio takes a value, as -na Safari does.
			if last := flags[len(flags)-1:]; strings.Contains("absuio", last) && i+1 < len(args) {
				i++
				if strings.Contains("abu", last) {
					opens(args[i])
				}
			}
		default:
			opens(a)
		}
	}
	switch {
	case !target || hidden:
		return outOfSight
	case !background:
		return inFront
	case irie && quietWords(passed), fresh && slices.ContainsFunc(passed, headless):
		return outOfSight
	}
	return inSight
}

// sweepNote is what an agent is told when Gabriel turns away a search of
// the whole disk or the home folder.
var sweepNote = "[" + brand.Name + "] " + brand.Gabriel + " turned this call away: it searches the whole disk or your user's home folder, and macOS asks them about each private folder it reads (Documents, Desktop, Downloads, iCloud Drive, Mail…), a prompt in front of what they look at, while the search works the disk for minutes. Look where the thing should be: command -v NAME for a program, mdfind -name NAME for a file by name (Spotlight's index, which reads no folder), or a narrower folder, such as the repository, /opt/homebrew, /usr/local or ~/Library/Caches. find with -maxdepth 1 runs. If your user wants the whole search, say so in a line: it runs from the session they watch, with " + brand.Name + " in front."

// sweepRoots are the folders a search may not start from: the whole disk,
// every user's home, the other volumes, and the home folder and its
// Library, which hold the folders macOS guards with a prompt.
func sweepRoots(home string) []string {
	roots := []string{"/", "/Users", "/home", "/Volumes", "/System/Volumes/Data", "~", "$HOME", "${HOME}", "~/Library", "$HOME/Library", "${HOME}/Library"}
	if home = strings.TrimRight(home, "/"); home != "" {
		roots = append(roots, home, home+"/Library")
	}
	return roots
}

// sweepsDisk reports whether a shell command searches every folder under
// the whole disk or the home folder (sweepRoots): macOS then asks the user
// about each private folder it reads, Documents, Desktop, Downloads and the
// rest, and the search works the disk for minutes. Like windowOf it reads
// the commands a person would see, and errs toward letting them run.
func sweepsDisk(cmd, home string) bool {
	roots := sweepRoots(home)
	for _, ws := range commands(withoutText(cmd)) {
		words, _ := command(ws)
		if len(words) == 0 {
			continue
		}
		base := filepath.Base(words[0])
		switch {
		case slices.Contains(shells, base):
			for i, w := range words[1:] {
				if strings.HasPrefix(w, "-") && !strings.HasPrefix(w, "--") && strings.HasSuffix(w, "c") {
					if i+2 < len(words) && sweepsDisk(words[i+2], home) {
						return true
					}
					break
				}
			}
		case base == "eval":
			if sweepsDisk(strings.Join(words[1:], " "), home) {
				return true
			}
		case slices.ContainsFunc(searchedFrom(base, words[1:]), func(p string) bool { return sweepRoot(p, roots) }):
			return true
		}
	}
	return false
}

// searchedFrom is where the program base, run with args, searches every
// folder below: find's, du's and tree's paths, fd's, rg's and grep -r's
// after their pattern, ls -R's. Held to the folder itself (find -maxdepth 1,
// tree -L 1, fd and rg with a depth of 1) it searches nothing below.
func searchedFrom(base string, args []string) []string {
	shallow := func(v string) bool { n, err := strconv.Atoi(v); return err == nil && n <= 1 }
	has := func(opts map[string]string, names ...string) bool {
		return slices.ContainsFunc(names, func(n string) bool { _, ok := opts[n]; return ok })
	}
	switch base {
	case "find", "gfind":
		// find [-H | -L | -P] [-EXdsx] [-f path] path ... [expression]
		var paths []string
		i := 0
		for ; i < len(args); i++ {
			if args[i] == "-f" && i+1 < len(args) {
				i++
				paths = append(paths, args[i])
				continue
			}
			if len(args[i]) < 2 || args[i][0] != '-' || strings.Trim(args[i][1:], "HLPEXdsx") != "" {
				break
			}
		}
		for ; i < len(args) && !strings.HasPrefix(args[i], "-") && args[i] != "(" && args[i] != "!"; i++ {
			paths = append(paths, args[i])
		}
		for ; i+1 < len(args); i++ {
			if args[i] == "-maxdepth" && shallow(args[i+1]) {
				return nil
			}
		}
		return paths
	case "fd", "fdfind":
		opts, rest := argv(args, "dEetSxXjco", []string{"max-depth", "exact-depth", "exclude", "extension", "type", "size", "exec", "exec-batch", "threads", "color", "owner", "changed-within", "changed-before", "search-path", "base-directory", "ignore-file", "max-results", "path-separator", "format"})
		if shallow(opts["-d"]) || shallow(opts["--max-depth"]) || shallow(opts["--exact-depth"]) || len(rest) == 0 {
			return nil
		}
		return append(rest[1:], opts["--search-path"], opts["--base-directory"])
	case "rg":
		opts, rest := argv(args, "efgtTmABCjMdEr", []string{"regexp", "file", "glob", "iglob", "type", "type-not", "type-add", "max-count", "after-context", "before-context", "context", "threads", "max-columns", "max-depth", "replace", "encoding", "engine", "sort", "sortr", "max-filesize", "pre", "pre-glob", "color", "colors", "path-separator", "context-separator", "ignore-file"})
		if shallow(opts["-d"]) || shallow(opts["--max-depth"]) {
			return nil
		}
		if !has(opts, "-e", "-f", "--regexp", "--file", "--files") && len(rest) > 0 {
			rest = rest[1:] // the pattern
		}
		return rest
	case "grep", "egrep", "fgrep", "ggrep":
		opts, rest := argv(args, "efmABCdD", []string{"regexp", "file", "max-count", "after-context", "before-context", "context", "directories", "devices", "label", "color", "colour"})
		if !has(opts, "-r", "-R", "--recursive", "--dereference-recursive") && opts["-d"] != "recurse" && opts["--directories"] != "recurse" {
			return nil
		}
		if !has(opts, "-e", "-f", "--regexp", "--file") && len(rest) > 0 {
			rest = rest[1:] // the pattern
		}
		return rest
	case "du", "gdu":
		_, rest := argv(args, "dBtXI", []string{"block-size", "threshold", "exclude-from", "time-style", "files0-from"})
		return rest
	case "tree":
		opts, rest := argv(args, "LPIoHT", []string{"filelimit", "timefmt", "charset", "sort", "infile"})
		if shallow(opts["-L"]) {
			return nil
		}
		return rest
	case "ls", "gls":
		opts, rest := argv(args, "", nil)
		if !has(opts, "-R", "--recursive") {
			return nil
		}
		return rest
	}
	return nil
}

// argv reads a command's arguments: its options, each with its value when
// it takes one (the short options in takes, the long ones in long, or given
// with =), and the rest in order. -- ends the options.
func argv(args []string, takes string, long []string) (opts map[string]string, rest []string) {
	opts = map[string]string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return opts, append(rest, args[i+1:]...)
		case strings.HasPrefix(a, "--"):
			name, val, set := strings.Cut(a[2:], "=")
			if !set && slices.Contains(long, name) && i+1 < len(args) {
				i++
				val = args[i]
			}
			opts["--"+name] = val
		case len(a) > 1 && a[0] == '-':
			for j := 1; j < len(a); j++ {
				if !strings.ContainsRune(takes, rune(a[j])) {
					opts["-"+a[j:j+1]] = ""
					continue
				}
				val := a[j+1:]
				if val == "" && i+1 < len(args) {
					i++
					val = args[i]
				}
				opts["-"+a[j:j+1]] = val
				break
			}
		default:
			rest = append(rest, a)
		}
	}
	return opts, rest
}

// sweepRoot reports whether path, as written, names one of roots, with or
// without a slash at its end, or everything in one (/*, ~/*).
func sweepRoot(path string, roots []string) bool {
	path = strings.TrimSuffix(path, "*")
	if p := strings.TrimRight(path, "/"); p != "" {
		path = p
	} else if path != "" {
		path = "/"
	}
	return path != "" && slices.Contains(roots, path)
}

// focusGuard says what to tell the agent when a Bash call it is about to
// make would put a window where the user works: on the desktop they are on,
// always, since nobody asked for a window behind them; in front, unless
// they watch the session that makes it, with Irie in front, and may want
// what it opens. A search of the whole disk or the home folder is in front
// too: macOS asks the user about each private folder it reads. "" lets the
// call run. It holds no lock when it is called.
func (c *Core) focusGuard(id, tool string, input json.RawMessage, evs []event.Event) string {
	g := c.cfg().Arch.Gabriel
	if tool != "Bash" || !g.On || !g.Guard || gabrielOff() || !slices.ContainsFunc(evs, func(e event.Event) bool { return e.Kind == event.ToolStarted }) {
		return ""
	}
	var in struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(input, &in) != nil {
		return ""
	}
	home, _ := os.UserHomeDir()
	effect, sweep := windowOf(in.Command), sweepsDisk(in.Command, home)
	switch {
	case effect == inSight:
		return focusNote
	case effect == outOfSight && !sweep:
		return ""
	}
	if a, ok, err := focus.Front(); err == nil && ok && (a.BundleID == irieBundleID || slices.Contains(terminalApps, a.BundleID)) {
		c.mu.Lock()
		watching := c.selected == id
		c.mu.Unlock()
		if watching {
			return ""
		}
	}
	if effect == inFront {
		return focusNote
	}
	return sweepNote
}

// installing is the sessions that run tools/live.sh: every session's work
// waits on an install, so Gabriel never holds one back. It reads ps once,
// and only while he holds back. It holds no lock when it is called.
func (c *Core) installing() map[string]bool {
	out, err := exec.Command("/bin/ps", "-Ao", "pid=,command=").Output()
	if err != nil {
		return nil
	}
	pids := installPIDs(string(out))
	if len(pids) == 0 {
		return nil
	}
	t, err := proc.Read()
	if err != nil {
		return nil
	}
	c.mu.Lock()
	roots := map[int]string{}
	for id, x := range c.sessions {
		if p := c.snap.Pane(x.pane); p != nil && !p.Dead && p.PID > 1 {
			roots[p.PID] = id
		}
	}
	c.mu.Unlock()
	return sessionsOf(pids, t, roots)
}

// installPIDs are the processes in ps output that run tools/live.sh.
func installPIDs(ps string) []int {
	var pids []int
	for _, line := range strings.Split(ps, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || !strings.Contains(line, "tools/live.sh") {
			continue
		}
		if pid, err := strconv.Atoi(f[0]); err == nil {
			pids = append(pids, pid)
		}
	}
	return pids
}

// sessionsOf are the sessions whose trees hold pids: each pid's ancestors,
// up to a session's pane.
func sessionsOf(pids []int, t proc.Table, roots map[int]string) map[string]bool {
	found := map[string]bool{}
	for _, pid := range pids {
		q, ok := t.Get(pid)
		for i := 0; ok && i < 64; i++ {
			if id, root := roots[q.PID]; root {
				found[id] = true
				break
			}
			if q.PPID <= 1 {
				break
			}
			q, ok = t.Get(q.PPID)
		}
	}
	return found
}
