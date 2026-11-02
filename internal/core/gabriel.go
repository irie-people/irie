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

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/focus"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/localmodel"
	"github.com/irie-people/irie/internal/proc"
	"github.com/irie-people/irie/internal/sysload"
)

// Gabriel (DESIGN §4.11) carries the user's focus to the machine. While the
// user is at the computer and it is busy, what they work on comes first:
// the session on show while Irie is in front, a session they messaged or
// started lately, a promoted one, and the app in front. Every other
// session's process tree steps back into macOS's background state, as for
// a promotion (priority.go), and so do the user's other heavy apps outside
// Irie. All of it comes back once the machine is calm again, the user
// walks away, Gabriel is turned off, or the core stops; a core that starts
// first takes back what a core before it left, from gabriel.json.

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
const irieBundleID = "io.github.irie-hq.irie"

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
	outside   map[int]outProc // the apps outside Irie it slowed, by pid
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
func gabrielOff() bool { return os.Getenv("IRIE_GABRIEL") == "off" }

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
// they messaged or started within word, and the one on show while Irie is
// in front. The caller holds c.mu.
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

	on := g.On && proc.CanBackground && !gabrielOff()
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
	// A boost holds back as a busy machine does, until its time is up;
	// while the user is away it waits like the rest.
	boosting := now.Before(st.boostUntil)
	st.holding = on && st.present && (st.busy || boosting)
	st.frontOK, st.irieFront = false, false
	if st.holding {
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
	var installs map[string]bool
	if st.holding {
		installs = c.installing()
	}
	c.mu.Lock()
	st.focus = c.inFocus(st.irieFront, g.Word, now)
	for id := range installs {
		st.focus[id] = true
	}
	trees := c.sessionRoots()
	held := maps.Clone(c.gab.outside)
	c.mu.Unlock()

	outside := c.slowOutside(held, st, g, trees)

	c.mu.Lock()
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
		case p.Path == c.bin || tmuxBin != "" && p.Path == tmuxBin || strings.Contains(p.Path, "/Irie.app/"):
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
	return r, nil
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
		On: g.On && proc.CanBackground && !gabrielOff(), Outside: g.Outside, Present: st.present, Idle: st.idle.Round(time.Second).String(),
		Busy: st.busy, Holding: st.holding, CPUs: c.load.CPUs, RunQueue: c.load.RunQueue, Pressure: c.load.Pressure,
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
	for _, id := range slices.Sorted(maps.Keys(c.sessions)) {
		if x := c.sessions[id]; x.demoted && x.track.priority == 0 && !st.focus[id] && st.holding {
			r.Held = append(r.Held, ipc.GabrielSession{ID: id, Name: x.name})
		}
	}
	for _, pid := range slices.Sorted(maps.Keys(st.outside)) {
		o := st.outside[pid]
		r.Apps = append(r.Apps, ipc.GabrielApp{PID: pid, Name: filepath.Base(o.Path), CPU: o.CPU})
	}
	return r
}

// focusNote is what an agent is told when Gabriel turns away a call that
// would take the user's focus.
const focusNote = "[Irie] Gabriel turned this call away: it would bring an app, a window or a page to the front while your user works on something else, and take their focus. Do it without the front: open -g (and --background for Irie.app), a browser headless (--headless=new keeps the profile and its logins) or new pages in the background with no bringToFront, a Mac view checked with Irie.app's --snapshot. If your user must see it, say so in a line and let them open it."

var (
	// shellSplit cuts a shell command at the places a new one starts.
	shellSplit = regexp.MustCompile("[;&|\\n]+|\\$\\(|`")
	// appActivates is AppleScript that brings an app forward or types into
	// one.
	appActivates = regexp.MustCompile(`(?i)\b(activate|reopen|keystroke|key code|open location)\b`)
)

// stealsFocus reports whether a shell command would bring an app, a window
// or a page to the front: open(1) without -g or -j, AppleScript that
// activates an app or types into it, or an app's own executable started
// straight, short of a headless browser, Irie.app's --snapshot and
// --background, and the like. It reads the command roughly, as a person
// would, and errs toward letting it run.
func stealsFocus(cmd string) bool {
	for _, seg := range shellSplit.Split(cmd, -1) {
		words := strings.Fields(seg)
		for len(words) > 0 && (strings.Contains(words[0], "=") && !strings.HasPrefix(words[0], "-") || slices.Contains([]string{"sudo", "env", "nohup", "exec", "command", "time", "("}, words[0])) {
			words = words[1:]
		}
		if len(words) == 0 {
			continue
		}
		prog := strings.Trim(words[0], `"'`)
		switch {
		case prog == "open" || prog == "/usr/bin/open":
			if openInFront(words[1:]) {
				return true
			}
		case filepath.Base(prog) == "osascript":
			if appActivates.MatchString(seg) {
				return true
			}
		case strings.Contains(seg, ".app/Contents/MacOS/"):
			quiet := []string{"--headless", "--snapshot", "--render-conversation", "--background", "--version", "--help"}
			if !slices.ContainsFunc(quiet, func(q string) bool { return strings.Contains(seg, q) }) {
				return true
			}
		}
	}
	return false
}

// openInFront reports whether open(1) with args opens something in front:
// it has something to open, and neither -g (in the background) nor -j
// (hidden).
func openInFront(args []string) bool {
	target := false
	for i := 0; i < len(args); i++ {
		a := strings.Trim(args[i], `"'`)
		switch {
		case a == "--args":
			return target
		case a == "-a" || a == "-b" || a == "-s" || a == "--env" || a == "--stdin" || a == "--stdout" || a == "--stderr":
			i++ // its value
			if a == "-a" || a == "-b" {
				target = true
			}
		case strings.HasPrefix(a, "--"):
		case strings.HasPrefix(a, "-") && len(a) > 1:
			if strings.ContainsAny(a[1:], "gj") {
				return false
			}
		default:
			target = true
		}
	}
	return target
}

// focusGuard says what to tell the agent when a Bash call it is about to
// make would take the user's focus while they look at something else: not
// at the session that makes it, with Irie in front. "" lets the call run.
// It holds no lock when it is called.
func (c *Core) focusGuard(id, tool string, input json.RawMessage, evs []event.Event) string {
	g := c.cfg().Arch.Gabriel
	if tool != "Bash" || !g.On || !g.Guard || gabrielOff() || !slices.ContainsFunc(evs, func(e event.Event) bool { return e.Kind == event.ToolStarted }) {
		return ""
	}
	var in struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(input, &in) != nil || !stealsFocus(in.Command) {
		return ""
	}
	// The user watching this very session may want what it opens.
	if a, ok, err := focus.Front(); err == nil && ok && (a.BundleID == irieBundleID || slices.Contains(terminalApps, a.BundleID)) {
		c.mu.Lock()
		watching := c.selected == id
		c.mu.Unlock()
		if watching {
			return ""
		}
	}
	return focusNote
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
