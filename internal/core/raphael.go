package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/proc"
	"github.com/irie-people/irie/internal/raphael"
)

// Raphael (DESIGN §4.11) heals Irie and the computer it runs on. He is
// plain code in the core, with no model and no tokens.
//
// His watch restarts a part of Irie that stops answering, which never
// disturbs an agent (§2.1, principle 6): the freeze dog (freezedog.go)
// restarts the Mac app, and force quits a sidebar, which its supervisor
// starts again; watchCore has the core leave when its own loop stands
// still for arch.raphael.core, and the core's supervisor starts it again.
//
// His look (internal/raphael) says what slows the computer down, fills it
// up or may have got in, with advice. Every minute he samples the
// processes, the memory and the disk; every arch.raphael.every, when the
// disk is nearly full, or when asked, he weighs the caches, what starts by
// itself and macOS's protections too. A fix runs only on the person's OK,
// one at a time, and never from inside an agent.

// raphaelEvery is how often, in ticks, he samples the processes, the
// memory and the disk.
const raphaelEvery = 60

// raphaelFirstLook is how long after the core starts his first look comes,
// when the last one is older than arch.raphael.every: not at once, as an
// install restarts the core.
const raphaelFirstLook = 10 * time.Minute

// raphaelLowLook is how long after the last look a disk nearly full brings
// another, to say what can go: not with every install's new core.
const raphaelLowLook = 3 * time.Hour

// coreWatchTick is how often watchCore looks at the core's loop.
var coreWatchTick = 5 * time.Second

// The core restarts itself coreRestarts times in coreWindow at most, by its
// reports in the freezes folder, so that a core that sticks as it starts
// is not started again and again.
const (
	coreRestarts = 3
	coreWindow   = 30 * time.Minute
)

// raphState is what Raphael knows. It has a lock of its own, taken after
// c.mu, so that a look never holds the core up.
type raphState struct {
	mu        sync.Mutex
	sampler   raphael.Sampler
	quick     []raphael.Finding // the last sample's: what runs hot, memory and disk
	quickFine []string
	slow      []raphael.Finding // the last look's: caches, startup, protection
	slowFine  []string
	checked   time.Time // when the last look ended
	took      time.Duration
	started   time.Time     // when this core started sampling
	lowLooked bool          // a look has been made since the disk was last found nearly full
	checking  bool          // a look is under way
	looked    chan struct{} // closed when the look under way ends
	fixing    *raphael.Finding
	fixed     chan struct{}     // closed when the fix under way ends
	fixes     []ipc.RaphaelDone // the fixes he ran lately, newest first
	sweeping  atomic.Bool       // a sample is under way
	beat      atomic.Int64      // when the core's loop last went round, in unix nanoseconds
	coreSaid  atomic.Bool       // he said he leaves a stuck core be
	exit      func(code int)    // how the core leaves for its supervisor to start it again; os.Exit unless a test sets it
	runner    raphael.Runner    // what runs his tools; raphael.Exec unless a test sets it
}

// run is what runs Raphael's tools.
func (r *raphState) run() raphael.Runner {
	if r.runner != nil {
		return r.runner
	}
	return raphael.Exec{}
}

// raphaelOff reports whether Raphael may not look here: a test core
// (IRIE_RAPHAEL=off, internal/testutil) would sample the whole machine.
func raphaelOff() bool { return os.Getenv("IRIE_RAPHAEL") == "off" }

// raphaelFile keeps his last look, for the core after this one.
func (c *Core) raphaelFile() string { return filepath.Join(c.paths.State, "raphael.json") }

// raphSaved is what raphael.json keeps.
type raphSaved struct {
	Checked time.Time         `json:"checked"`
	Took    time.Duration     `json:"took"`
	Slow    []raphael.Finding `json:"slow"`
	Fine    []string          `json:"fine"`
	Fixes   []ipc.RaphaelDone `json:"fixes,omitempty"`
}

// loadRaphael reads his last look, which a core before this one saved.
func (c *Core) loadRaphael() {
	r := &c.raph
	r.started = time.Now()
	b, err := os.ReadFile(c.raphaelFile())
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			c.log.Warn("raphael", "err", err)
		}
		return
	}
	var s raphSaved
	if err := json.Unmarshal(b, &s); err != nil {
		c.log.Warn("raphael", "err", err)
		return
	}
	r.mu.Lock()
	r.slow, r.slowFine, r.checked, r.took, r.fixes = s.Slow, s.Fine, s.Checked, s.Took, s.Fixes
	r.mu.Unlock()
}

func (c *Core) saveRaphael() {
	r := &c.raph
	r.mu.Lock()
	s := raphSaved{Checked: r.checked, Took: r.took, Slow: r.slow, Fine: r.slowFine, Fixes: r.fixes}
	r.mu.Unlock()
	b, err := json.Marshal(s)
	if err == nil {
		err = os.MkdirAll(c.paths.State, 0o700)
	}
	if err == nil {
		err = filetx.WriteFile(c.raphaelFile(), b, 0o600)
	}
	if err != nil {
		c.log.Warn("raphael", "err", err)
	}
}

// raphaelSweep samples the processes, the memory and the disk, and starts
// a look when one is due. One runs at a time; the caller holds no lock.
func (c *Core) raphaelSweep() {
	r := &c.raph
	if !r.sweeping.CompareAndSwap(false, true) {
		return
	}
	defer r.sweeping.Store(false)
	cfg := c.cfg().Arch.Raphael
	if !cfg.On || raphaelOff() {
		return
	}
	now := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ps, err := raphael.Procs(ctx, r.run())
	if err != nil {
		c.log.Warn("raphael", "err", err)
		return
	}
	t, err := proc.Read()
	if err != nil {
		c.log.Warn("raphael", "err", err)
		return
	}
	starts := make(map[int]int64, len(ps))
	for _, p := range ps {
		if q, ok := t.Get(p.PID); ok {
			starts[p.PID] = q.Start
		}
	}
	r.mu.Lock()
	r.sampler.Add(now, ps, starts)
	hot := r.sampler.Hot(ps)
	r.mu.Unlock()
	in := c.raphaelInput(ps, hot, t, now)
	disk, _ := raphael.ReadDisk()
	quick, fine := raphael.Quick(in, raphael.ReadMemory(), disk)
	r.mu.Lock()
	r.quick, r.quickFine = quick, fine
	if !disk.Low() {
		r.lowLooked = false
	}
	// Not in a core's first minutes, as an install restarts it.
	idle := !r.checking && r.fixing == nil && now.Sub(r.started) >= raphaelFirstLook
	every := idle && cfg.Every > 0 && now.Sub(r.checked) >= cfg.Every
	// A disk nearly full brings a look, to say what can go.
	low := idle && disk.Low() && !r.lowLooked && now.Sub(r.checked) >= raphaelLowLook
	if low {
		r.lowLooked = true
	}
	r.mu.Unlock()
	if every || low {
		c.raphaelBegin(false)
	}
}

// raphaelInput is what a look needs: the person's home and known startup
// items, whose each process is, and the last sample.
func (c *Core) raphaelInput(ps []raphael.Proc, hot []raphael.Hot, t proc.Table, now time.Time) raphael.Input {
	home, _ := os.UserHomeDir()
	o := raphael.Owner{Session: map[int]string{}, Irie: map[int]bool{os.Getpid(): true, os.Getppid(): true}}
	c.mu.Lock()
	for _, x := range c.sessions {
		if p := c.snap.Pane(x.pane); p != nil && !p.Dead && p.PID > 1 {
			for _, q := range t.Tree(p.PID) {
				o.Session[q.PID] = x.name
			}
		}
	}
	c.mu.Unlock()
	c.fdog.mu.Lock()
	for _, w := range c.fdog.watches {
		o.Irie[w.pid] = true
	}
	c.fdog.mu.Unlock()
	for _, p := range ps {
		if c.bin != "" && p.Path == c.bin {
			o.Irie[p.PID] = true
		}
	}
	return raphael.Input{Home: home, UID: os.Getuid(), Known: c.cfg().Arch.Raphael.Known, Owner: o, Procs: ps, Hot: hot, Now: now}
}

// raphaelBegin starts a look at the caches, what starts by itself and
// macOS's protections, unless one is under way, and returns a channel
// closed once it ends. The look runs in the background state. A thorough
// one, which the person asked for, also asks Homebrew what it would clean
// up, which can take minutes, on a busy Mac more than an hour.
func (c *Core) raphaelBegin(thorough bool) <-chan struct{} {
	r := &c.raph
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.checking {
		r.checking, r.looked = true, make(chan struct{})
		go c.raphaelLook(r.looked, thorough)
	}
	return r.looked
}

// raphaelLook is the look raphaelBegin started, which closes done.
func (c *Core) raphaelLook(done chan struct{}, thorough bool) {
	r := &c.raph
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	ps, _ := raphael.Procs(ctx, r.run())
	t, _ := proc.Read()
	disk, _ := raphael.ReadDisk()
	in := c.raphaelInput(ps, nil, t, start)
	in.Thorough = thorough
	slow, fine := raphael.Slow(ctx, r.run(), in, disk)
	r.mu.Lock()
	r.slow, r.slowFine, r.checked, r.took = slow, fine, time.Now(), time.Since(start).Round(time.Second)
	r.checking = false
	r.mu.Unlock()
	c.saveRaphael()
	close(done)
	c.log.Info("raphael: looked over the computer", "took", time.Since(start).Round(time.Second), "findings", len(slow))
}

// raphaelOp is the raphael op (OpRaphael) from the process peer.
func (c *Core) raphaelOp(peer int, a ipc.RaphaelArgs) (ipc.RaphaelResult, error) {
	r := &c.raph
	switch {
	case a.Fix != "":
		f, ok := c.raphaelFinding(a.Fix)
		switch {
		case c.fromAgent(peer):
			return ipc.RaphaelResult{}, ipc.Errorf(ipc.CodeBadArgs, "An agent can't say OK to Raffi's fixes: they are your user's to say OK to, in Settings > Arch Agents or with `irie ctl raphael fix` in their own terminal.")
		case !ok || f.Fix == nil:
			return ipc.RaphaelResult{}, ipc.Errorf(ipc.CodeNotFound, "Raffi has nothing to fix by %q now: ask him to look again.", a.Fix)
		case !a.OK:
			return ipc.RaphaelResult{}, ipc.Errorf(ipc.CodeBadArgs, "Raffi runs a fix only once you say OK to what it runs: %s.", strings.Join(f.Fix.Run, "; "))
		}
		r.mu.Lock()
		if r.fixing != nil {
			busy := r.fixing.Fix.Label
			r.mu.Unlock()
			return ipc.RaphaelResult{}, ipc.Errorf(ipc.CodeBadArgs, "Raffi is still on one fix (%s): one at a time.", busy)
		}
		fix := f
		r.fixing, r.fixed = &fix, make(chan struct{})
		done := r.fixed
		r.mu.Unlock()
		c.log.Info("raphael: fixing, on the person's OK", "fix", f.Fix.Label, "run", strings.Join(f.Fix.Run, "; "))
		go c.raphaelFix(f)
		if !a.Wait {
			return c.raphaelReport(), nil
		}
		<-done
		res := c.raphaelReport()
		r.mu.Lock()
		if len(r.fixes) > 0 {
			res.Did = r.fixes[0].Said
		}
		r.mu.Unlock()
		return res, nil
	case a.Mine != "":
		if c.fromAgent(peer) {
			return ipc.RaphaelResult{}, ipc.Errorf(ipc.CodeBadArgs, "An agent can't mark a startup item as your user's: that is theirs to say.")
		}
		f, ok := c.raphaelFinding(a.Mine)
		if !ok || f.Key == "" {
			return ipc.RaphaelResult{}, ipc.Errorf(ipc.CodeNotFound, "Raffi has no startup item by %q now.", a.Mine)
		}
		known := slices.Clone(c.cfg().Arch.Raphael.Known)
		if !slices.Contains(known, f.Key) {
			known = append(known, f.Key)
			v, _ := json.Marshal(known)
			if err := c.configure(c.paths.ConfigFile(), []ipc.ConfigSet{{Key: "arch.raphael.known", Value: v}}); err != nil {
				return ipc.RaphaelResult{}, err
			}
		}
		r.mu.Lock()
		r.slow = slices.DeleteFunc(r.slow, func(x raphael.Finding) bool { return x.ID == f.ID })
		r.mu.Unlock()
		c.saveRaphael()
	case a.Check:
		done := c.raphaelBegin(true)
		if a.Wait {
			<-done
		}
	}
	return c.raphaelReport(), nil
}

// raphaelFinding finds a finding by its id.
func (c *Core) raphaelFinding(id string) (raphael.Finding, bool) {
	r := &c.raph
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, f := range append(slices.Clone(r.quick), r.slow...) {
		if f.ID == id {
			return f, true
		}
	}
	return raphael.Finding{}, false
}

// raphaelFix runs a fix the person said OK to, and keeps what came of it.
func (c *Core) raphaelFix(f raphael.Finding) {
	r := &c.raph
	home, _ := os.UserHomeDir()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()
	said, err := raphael.Do(ctx, r.run(), *f.Fix, raphael.DoInput{Home: home, UID: os.Getuid()})
	d := ipc.RaphaelDone{At: time.Now(), Label: f.Fix.Label, Said: said}
	if err != nil {
		d.Said, d.Error = f.Fix.Label+": "+err.Error(), true
		c.log.Warn("raphael: a fix failed", "fix", f.Fix.Label, "err", err)
	} else {
		c.log.Info("raphael: fixed", "fix", f.Fix.Label, "said", said)
	}
	r.mu.Lock()
	r.fixes = append([]ipc.RaphaelDone{d}, r.fixes...)
	if len(r.fixes) > 10 {
		r.fixes = r.fixes[:10]
	}
	if err == nil {
		// What it fixed is gone until a look finds it again.
		gone := func(x raphael.Finding) bool { return x.ID == f.ID }
		r.slow = slices.DeleteFunc(r.slow, gone)
		r.quick = slices.DeleteFunc(r.quick, gone)
	}
	r.fixing = nil
	done := r.fixed
	r.mu.Unlock()
	c.saveRaphael()
	close(done)
}

// raphaelReport is Raphael's report (OpRaphael).
func (c *Core) raphaelReport() ipc.RaphaelResult {
	cfg := c.cfg().Arch.Raphael
	r := &c.raph
	r.mu.Lock()
	rep := raphael.Merge(r.checked, r.took, r.quick, r.slow, r.quickFine, r.slowFine)
	res := ipc.RaphaelResult{On: cfg.On && !raphaelOff(), Checking: r.checking, Findings: []ipc.RaphaelFinding{}, Fine: rep.Fine, Fixes: slices.Clone(r.fixes)}
	if r.fixing != nil {
		res.Fixing = r.fixing.ID
	}
	if !r.checked.IsZero() {
		at := r.checked
		res.Checked, res.Took = &at, r.took.String()
		if cfg.On && cfg.Every > 0 {
			next := at.Add(cfg.Every)
			if first := r.started.Add(raphaelFirstLook); next.Before(first) {
				next = first
			}
			res.Next = &next
		}
	}
	r.mu.Unlock()
	if res.Fine == nil {
		res.Fine = []string{}
	}
	for _, f := range rep.Findings {
		w := ipc.RaphaelFinding{ID: f.ID, Area: f.Area, Level: f.Level, Text: f.Text, Advice: f.Advice, Evidence: f.Evidence, Sure: f.Sure, Size: f.Size, Mine: f.Key != ""}
		if f.Fix != nil {
			w.Fix = &ipc.RaphaelFix{Label: f.Fix.Label, Run: f.Fix.Run, About: f.Fix.About, Undo: f.Fix.Undo}
		}
		res.Findings = append(res.Findings, w)
	}
	res.Restarts = recentRestarts(c.paths.Freezes(), time.Now())
	return res
}

// recentRestarts are the restarts of the last week, newest first, by the
// reports of where each part was stuck: their names start with the time,
// then say what stopped answering (freezeReport, coreReport).
func recentRestarts(dir string, now time.Time) []ipc.RaphaelRestart {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []ipc.RaphaelRestart
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".txt") || len(name) < 20 {
			continue
		}
		at, err := time.Parse("2006-01-02T15-04-05Z", name[:20])
		if err != nil || now.Sub(at) > 7*24*time.Hour {
			continue
		}
		what := "app"
		switch rest := name[20:]; {
		case strings.HasPrefix(rest, "-core"):
			what = "core"
		case strings.HasPrefix(rest, "-sidebar"):
			what = "sidebar"
		}
		out = append(out, ipc.RaphaelRestart{At: at, What: what, Report: filepath.Join(dir, name)})
	}
	slices.SortFunc(out, func(a, b ipc.RaphaelRestart) int { return b.At.Compare(a.At) })
	return out[:min(len(out), 10)]
}

// raphaelNow is Raphael's row in the arch op: what he does now, his look
// as a line each, and what a person can do about each.
func (c *Core) raphaelNow() archNow {
	rep := c.raphaelReport()
	n := archNow{on: rep.On}
	warn, note := 0, 0
	for _, f := range rep.Findings {
		switch f.Level {
		case raphael.Warn:
			warn++
		case raphael.Note:
			note++
		}
	}
	// What he found, in words: the minute's samples count from the start,
	// before his first look at the rest.
	var found string
	switch care, look := plural(warn, "1 thing needs care", "%d things need care"), plural(note, "1 is worth a look", "%d are worth a look"); {
	case warn > 0 && note > 0:
		found = care + ", and " + look
	case warn > 0:
		found = care
	case note > 0:
		found = "nothing needs care, and " + plural(note, "1 thing is worth a look", "%d things are worth a look")
	}
	switch {
	case !rep.On:
		n.status = "Off: nothing restarts a part of Irie that stops answering, and he looks over your Mac only when you ask."
	case rep.Checking:
		n.status = "Looking over your Mac…"
		if found != "" {
			n.status += " So far " + found + "."
		}
	case rep.Checked == nil:
		n.status = "Watching Irie's windows, sidebar and core."
		if found != "" {
			n.status += " So far " + found + "."
		}
		n.status += " He looks over the rest of your Mac soon, or now if you ask."
	default:
		when := rep.Checked.Local().Format("15:04")
		if time.Since(*rep.Checked) > 20*time.Hour {
			when = rep.Checked.Local().Format("2 Jan 15:04")
		}
		if found == "" {
			found = "your Mac is well"
		}
		n.status = fmt.Sprintf("Looked at %s: %s.", when, found)
	}
	if len(rep.Restarts) > 0 && time.Since(rep.Restarts[0].At) < 24*time.Hour {
		x := rep.Restarts[0]
		n.status += fmt.Sprintf(" Restarted %s at %s, when it stopped answering.", restartWhat(x.What), x.At.Local().Format("15:04"))
	}
	if len(rep.Fixes) > 0 && time.Since(rep.Fixes[0].At) < time.Hour {
		n.status += " " + rep.Fixes[0].Said
	}
	if !rep.Checking {
		n.actions = []ipc.ArchAction{{Label: "Look over my Mac now", Op: ipc.OpRaphael, Args: json.RawMessage(`{"check":true}`), Done: "Raffi is looking over your Mac: what he finds shows here in a minute or two."}}
	}
	for _, f := range rep.Findings {
		it := ipc.ArchItem{Text: f.Text, Level: f.Level}
		var detail []string
		if f.Advice != "" {
			detail = append(detail, f.Advice)
		}
		detail = append(detail, f.Evidence...)
		if f.Sure != "" {
			detail = append(detail, f.Sure)
		}
		it.Detail = strings.Join(detail, "\n")
		if f.Fix != nil {
			id, _ := json.Marshal(ipc.RaphaelArgs{Fix: f.ID})
			x := ipc.ArchAction{Label: f.Fix.Label, Op: ipc.OpRaphael, Args: id, Ask: raphaelAsk(f.Fix),
				Done: "Raffi is on it: what came of it shows here once he is done."}
			if rep.Fixing == f.ID {
				x.Label = "Working on it…"
			}
			it.Actions = append(it.Actions, x)
		}
		if f.Mine {
			id, _ := json.Marshal(ipc.RaphaelArgs{Mine: f.ID})
			it.Actions = append(it.Actions, ipc.ArchAction{Label: "It's mine", Op: ipc.OpRaphael, Args: id, Done: "Raffi no longer flags it."})
		}
		n.items = append(n.items, it)
	}
	return n
}

// raphaelAsk is what a person reads before they say OK to a fix: what it
// runs, what it does, and how to undo it.
func raphaelAsk(f *ipc.RaphaelFix) string {
	var b strings.Builder
	b.WriteString("Raffi will run:\n")
	for _, line := range f.Run {
		b.WriteString("    " + line + "\n")
	}
	b.WriteString("\n" + f.About)
	if f.Undo != "" {
		b.WriteString("\n\nTo undo it: " + f.Undo)
	}
	return b.String()
}

// plural is one when n is 1, else many with n in it.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return fmt.Sprintf(many, n)
}

// restartWhat names a part of Irie that Raphael restarted.
func restartWhat(what string) string {
	switch what {
	case "core":
		return "the core"
	case "sidebar":
		return "a sidebar"
	}
	return "the Mac app"
}

// beat says the core's loop went round (watchCore).
func (c *Core) beat() { c.raph.beat.Store(time.Now().UnixNano()) }

// watchCore is the freeze dog's look at the core's own loop, every
// coreWatchTick (watchFreezes); last is when it looked before. The core
// leaves when its loop has stood still for arch.raphael.core: a lock held
// for ever, or a call that never returns. Its supervisor starts it again,
// and the agents never notice (§2.1, principle 6). Before it leaves it
// writes where every goroutine was, in a report in the freezes folder.
func (c *Core) watchCore(now, last time.Time) {
	// A late look means the whole core was held up, or the machine asleep:
	// the loop's time starts again.
	if !c.lookAtCore(now, now.Sub(last) > 3*coreWatchTick) {
		return
	}
	exit := c.raph.exit
	if exit == nil {
		exit = os.Exit
	}
	exit(3)
}

// lookAtCore reports whether the core must leave now, its loop having
// stood still for arch.raphael.core, and writes the report first. A core
// that stuck coreRestarts times in coreWindow is left be, and said so once.
func (c *Core) lookAtCore(now time.Time, held bool) bool {
	r := &c.raph
	if r.beat.Load() == 0 {
		return false // the loop has not started: the core is starting
	}
	cfg := c.cfg().Arch.Raphael
	if held || !cfg.On || cfg.Core <= 0 || raphaelOff() {
		c.beat()
		return false
	}
	still := now.Sub(time.Unix(0, r.beat.Load()))
	if still < cfg.Core {
		r.coreSaid.Store(false)
		return false
	}
	dir := c.paths.Freezes()
	if n := len(coreReportsSince(dir, now.Add(-coreWindow))); n >= coreRestarts {
		if !r.coreSaid.Swap(true) {
			c.log.Error("raphael: the core's loop stands still, but it was restarted often of late, so he leaves it be", "still", still.Round(time.Second), "restarts", n)
		}
		return false
	}
	report := c.coreReport(still)
	c.log.Error("raphael: the core's loop stood still, so he restarts the core", "still", still.Round(time.Second), "report", report)
	return true
}

// coreReport writes where every goroutine of the core is, in the freezes
// folder, and returns its path; "" when it could not.
func (c *Core) coreReport(still time.Duration) string {
	dir := c.paths.Freezes()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	pruneReports(dir, freezeReports-1)
	buf := make([]byte, 16<<20)
	n := runtime.Stack(buf, true)
	file := filepath.Join(dir, time.Now().UTC().Format("2006-01-02T15-04-05Z")+"-core.txt")
	head := fmt.Sprintf("Raphael restarted the core (pid %d, %s): its loop had stood still for %v.\n\n", os.Getpid(), c.version, still.Round(time.Second))
	if err := os.WriteFile(file, append([]byte(head), buf[:n]...), 0o600); err != nil {
		return ""
	}
	return file
}

// coreReportsSince are the reports of core restarts since then.
func coreReportsSince(dir string, since time.Time) []string {
	var out []string
	for _, x := range recentRestarts(dir, time.Now()) {
		if x.What == "core" && !x.At.Before(since.Truncate(time.Second)) {
			out = append(out, x.Report)
		}
	}
	return out
}
