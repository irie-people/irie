package core

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/proc"
)

// The freeze dog (DESIGN §2.4, docs/MAC.md) watches Irie's own windows for
// a freeze. A window that subscribes with view "watch" gets a ping a second
// on that connection, and answers each from its main loop, so an answer
// says the window still answers its user. When a ping waits unanswered for
// ui.freeze_dog, the dog has sample(1) write down where the window's
// process is stuck, force quits it, and opens the Mac app again as it
// stood. Agents never notice: a window keeps no state of its own (DESIGN
// §2.1, principle 6).
//
// The dog takes no lock but its own, so neither a ping nor its answer waits
// for a busy core, and a core that was itself held up blames no window.

var (
	// freezeTick is how often the dog pings and looks.
	freezeTick = time.Second
	// freezeSampleWait is the longest the dog waits for sample to read the
	// stacks before it force quits the window anyway.
	freezeSampleWait = 3 * time.Second
	// freezeGoneWait is the longest it waits for a window it killed to go
	// before it opens the app again.
	freezeGoneWait = 5 * time.Second
)

const (
	// The dog restarts windows freezeRestarts times in freezeWindow at
	// most, so that an app that freezes as it starts is not opened again
	// and again; past that, it leaves a frozen window be.
	freezeRestarts = 3
	freezeWindow   = 10 * time.Minute
	// freezeReports is how many reports of where a window was stuck the
	// dog keeps, the newest.
	freezeReports = 10
)

// watch is one window the freeze dog watches.
type watch struct {
	pid    int
	start  int64  // when its process started, so a later one with its pid is never taken for it
	bundle string // the Mac app to open again; "" for none
	out    chan ipc.Frame

	// The rest is guarded by freezeDog.mu.
	ping    uint64    // the last ping sent
	sent    time.Time // when it went out
	waiting bool      // it is not answered yet
	front   bool      // the window's app was the active one at its last answer
	hidden  bool      // its app was hidden
	acting  bool      // the dog is restarting it
	spared  bool      // the dog has said that it leaves this one be
}

// freezeDog is the freeze dog's state.
type freezeDog struct {
	mu       sync.Mutex
	watches  map[<-chan ipc.Frame]*watch
	next     uint64      // the last ping's number
	last     time.Time   // the dog's last look
	restarts []time.Time // its restarts in the last freezeWindow

	// What it does to a window, which tests replace.
	lookup func(pid int) (proc.Proc, bool)
	sample func(pid int, file string) bool
	kill   func(pid int) error
	open   func(bundle string, front, hidden bool, stuck time.Duration) error
}

func newFreezeDog() *freezeDog {
	return &freezeDog{
		watches: map[<-chan ipc.Frame]*watch{},
		lookup:  lookupProc, sample: sampleStacks, kill: killProc, open: openApp,
	}
}

// watchWindow starts the freeze dog's watch of a window: a subscribe with
// view "watch".
func (c *Core) watchWindow(a ipc.SubscribeArgs) (<-chan ipc.Frame, func(), error) {
	if a.PID <= 0 {
		return nil, nil, ipc.Errorf(ipc.CodeBadArgs, "a watch needs the window's pid")
	}
	if a.Bundle != "" && !filepath.IsAbs(a.Bundle) {
		return nil, nil, ipc.Errorf(ipc.CodeBadArgs, "bundle %q is not an absolute path", a.Bundle)
	}
	d := c.fdog
	p, ok := d.lookup(a.PID)
	if !ok || p.Zombie {
		return nil, nil, ipc.Errorf(ipc.CodeNotFound, "no process %d", a.PID)
	}
	w := &watch{pid: a.PID, start: p.Start, bundle: a.Bundle, out: make(chan ipc.Frame, 1), front: true}
	d.mu.Lock()
	// A window that watches itself again replaces its last watch, whose
	// connection is on its way out.
	for k, old := range d.watches {
		if old.pid == w.pid && old.start == w.start {
			delete(d.watches, k)
		}
	}
	d.watches[w.out] = w
	d.mu.Unlock()
	c.log.Info("freeze dog: watching", "pid", w.pid, "bundle", w.bundle)
	return w.out, func() {
		d.mu.Lock()
		delete(d.watches, w.out)
		d.mu.Unlock()
	}, nil
}

// Hear takes a watched window's answer to its ping (ipc.Hearer).
func (c *Core) Hear(frames <-chan ipc.Frame, line []byte) {
	var p ipc.Pong
	if json.Unmarshal(line, &p) != nil || p.Pong == 0 {
		return
	}
	d := c.fdog
	d.mu.Lock()
	defer d.mu.Unlock()
	if w := d.watches[frames]; w != nil && w.waiting && p.Pong == w.ping {
		w.waiting, w.spared = false, false
		w.front, w.hidden = p.Front, p.Hidden
	}
}

// watchFreezes runs the freeze dog until the core stops.
func (c *Core) watchFreezes() {
	t := time.NewTicker(freezeTick)
	defer t.Stop()
	for range t.C {
		c.lookAtWindows(time.Now())
	}
}

// lookAtWindows pings each watched window that answered its last ping, and
// restarts one that has left a ping unanswered for ui.freeze_dog.
func (c *Core) lookAtWindows(now time.Time) {
	limit := c.cfg().UI.FreezeDog
	d := c.fdog
	d.mu.Lock()
	defer d.mu.Unlock()
	// A late look means the core itself was held up: stopped, starved, or
	// asleep with the machine. An answer may be waiting unread, so every
	// unanswered ping starts its time again.
	held := !d.last.IsZero() && now.Sub(d.last) > 3*freezeTick
	d.last = now
	for _, w := range d.watches {
		switch {
		case w.acting:
		case !w.waiting:
			select {
			case w.out <- ipc.Frame{T: ipc.FramePing, Ping: d.next + 1}:
				d.next++
				w.ping, w.sent, w.waiting = d.next, now, true
			default: // the last ping has not gone out yet
			}
		case held:
			w.sent = now
		case limit > 0 && now.Sub(w.sent) >= limit:
			w.acting = true
			go c.restartWindow(w, now.Sub(w.sent))
		}
	}
}

// restartWindow force quits a window that has not answered for stuck, and
// opens the Mac app again as it stood. sample first writes down where the
// window's process is stuck, in a report in the freezes folder.
func (c *Core) restartWindow(w *watch, stuck time.Duration) {
	d := c.fdog
	d.mu.Lock()
	pid, start, bundle, front, hidden := w.pid, w.start, w.bundle, w.front, w.hidden
	d.mu.Unlock()
	log := c.log.With("pid", pid, "stuck", stuck.Round(100*time.Millisecond), "bundle", bundle)
	// spare leaves the window be: the dog looks again once another
	// ui.freeze_dog has passed without an answer.
	spare := func(why string) {
		d.mu.Lock()
		if !w.spared {
			log.Warn("freeze dog: a window stopped answering, but the dog leaves it be", "why", why)
		}
		w.acting, w.spared, w.sent = false, true, time.Now()
		d.mu.Unlock()
	}
	p, ok := d.lookup(pid)
	switch {
	case !ok || p.Start != start || p.Zombie:
		return // gone: its stream ends, and its watch with it
	case p.Traced:
		spare("a debugger holds it")
		return
	case !d.allow(time.Now()):
		spare(fmt.Sprintf("the dog restarted %d windows in the last %v", freezeRestarts, freezeWindow))
		return
	}
	report := c.freezeReport(pid)
	if err := d.kill(pid); err != nil {
		spare("it could not be force quit: " + err.Error())
		return
	}
	// Launch Services would bring back an app that still runs instead of
	// opening it again.
	if !d.gone(pid, start) {
		log.Warn("freeze dog: the window is still there after a force quit")
	}
	if bundle == "" {
		log.Warn("freeze dog: force quit a window that stopped answering", "report", report)
		return
	}
	if err := d.open(bundle, front, hidden, stuck); err != nil {
		log.Error("freeze dog: force quit a window that stopped answering, but could not open it again", "report", report, "err", err)
		return
	}
	log.Warn("freeze dog: restarted a window that stopped answering", "report", report, "front", front, "hidden", hidden)
}

// allow counts a restart at now, unless the dog has made freezeRestarts in
// the freezeWindow before it.
func (d *freezeDog) allow(now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.restarts = slices.DeleteFunc(d.restarts, func(t time.Time) bool { return now.Sub(t) >= freezeWindow })
	if len(d.restarts) >= freezeRestarts {
		return false
	}
	d.restarts = append(d.restarts, now)
	return true
}

// gone waits up to freezeGoneWait for the process to be gone.
func (d *freezeDog) gone(pid int, start int64) bool {
	for end := time.Now().Add(freezeGoneWait); ; time.Sleep(20 * time.Millisecond) {
		if p, ok := d.lookup(pid); !ok || p.Start != start || p.Zombie {
			return true
		}
		if time.Now().After(end) {
			return false
		}
	}
}

// freezeReport has sample write down where pid is stuck, and returns the
// report's path once the stacks are read, or "" for none. The report itself
// is written a moment later, which no longer needs pid.
func (c *Core) freezeReport(pid int) string {
	dir := c.paths.Freezes()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	pruneReports(dir, freezeReports-1)
	file := filepath.Join(dir, time.Now().UTC().Format("2006-01-02T15-04-05Z")+"-"+strconv.Itoa(pid)+".txt")
	if !c.fdog.sample(pid, file) {
		return ""
	}
	return file
}

// pruneReports removes all but the newest keep reports in dir. Their names
// start with the time, so they sort by it.
func pruneReports(dir string, keep int) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".txt") {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	for len(names) > max(keep, 0) {
		os.Remove(filepath.Join(dir, names[0]))
		names = names[1:]
	}
}

// lookupProc finds a process in the process table.
func lookupProc(pid int) (proc.Proc, bool) {
	t, err := proc.Read()
	if err != nil {
		return proc.Proc{}, false
	}
	return t.Get(pid)
}

// killProc force quits a process.
func killProc(pid int) error { return syscall.Kill(pid, syscall.SIGKILL) }

// sampleStacks starts sample(1) on pid, which writes its report to file,
// and returns once sample has read the stacks, or after freezeSampleWait;
// false where there is no sample. sample says on its stderr when it has
// read them, and then writes the report, which needs pid no longer. Its
// path is whole: a shell may put another tool first on its PATH.
func sampleStacks(pid int, file string) bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	cmd := exec.CommandContext(ctx, "/usr/bin/sample", strconv.Itoa(pid), "1", "-file", file)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return false
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return false
	}
	read := make(chan struct{})
	go func() {
		defer cancel()
		var once sync.Once
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "Sampling completed") {
				once.Do(func() { close(read) })
			}
		}
		once.Do(func() { close(read) })
		cmd.Wait()
	}()
	select {
	case <-read:
	case <-time.After(freezeSampleWait):
	}
	return true
}

// openApp opens the Mac app at bundle as it stood: in front, behind the app
// in front, or hidden (open(1)'s -g and -j). --freeze-dog tells the app why
// it opened, and --background keeps it from taking the focus (IrieApp.swift).
func openApp(bundle string, front, hidden bool, stuck time.Duration) error {
	if runtime.GOOS != "darwin" {
		return errors.New("only macOS opens an app")
	}
	var args []string
	if hidden {
		args = append(args, "-j")
	}
	if !front {
		args = append(args, "-g")
	}
	args = append(args, bundle, "--args", "--freeze-dog", strconv.Itoa(max(1, int(stuck.Round(time.Second)/time.Second))))
	if !front {
		args = append(args, "--background")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "/usr/bin/open", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
