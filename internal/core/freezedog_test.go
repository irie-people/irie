package core

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/proc"
	"github.com/irie-people/irie/internal/testutil"
)

// fakeWindows stands in for the processes the freeze dog watches and for
// what it does to them: sample, kill and open.
type fakeWindows struct {
	mu      sync.Mutex
	procs   map[int]proc.Proc
	sampled []string // the reports' paths
	killed  []int
	opened  []openCall
	done    chan struct{} // a restart has ended
	watched int           // the real process a test watches
}

type openCall struct {
	bundle        string
	front, hidden bool
	stuck         time.Duration
}

// dogCore is a core with no tmux behind it whose freeze dog acts on fakes,
// with state in a temporary folder.
func dogCore(t *testing.T) (*Core, *fakeWindows) {
	t.Helper()
	c := testCore(t)
	home := t.TempDir()
	p, err := paths.New(func(k string) string { return map[string]string{"HOME": home}[k] }, os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	c.paths = p
	f := &fakeWindows{procs: map[int]proc.Proc{}, done: make(chan struct{}, 8)}
	c.fdog.lookup = func(pid int) (proc.Proc, bool) {
		f.mu.Lock()
		defer f.mu.Unlock()
		p, ok := f.procs[pid]
		return p, ok
	}
	c.fdog.sample = func(pid int, file string) bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.sampled = append(f.sampled, file)
		return true
	}
	c.fdog.kill = func(pid int) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.killed = append(f.killed, pid)
		delete(f.procs, pid)
		return nil
	}
	c.fdog.open = func(bundle string, front, hidden bool, stuck time.Duration) error {
		f.mu.Lock()
		f.opened = append(f.opened, openCall{bundle, front, hidden, stuck})
		f.mu.Unlock()
		f.done <- struct{}{}
		return nil
	}
	return c, f
}

func (f *fakeWindows) add(pid int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.procs[pid] = proc.Proc{PID: pid, Start: int64(pid) * 10, Name: "Irie"}
}

func (f *fakeWindows) calls() (killed []int, opened []openCall, sampled []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.killed...), append([]openCall(nil), f.opened...), append([]string(nil), f.sampled...)
}

// watchFor subscribes as the window pid of bundle.
func watchFor(t *testing.T, c *Core, pid int, bundle string) <-chan ipc.Frame {
	t.Helper()
	raw, _ := json.Marshal(ipc.SubscribeArgs{View: ipc.ViewWatch, PID: pid, Bundle: bundle})
	ch, cancel, err := c.Subscribe(raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cancel)
	return ch
}

// pinged takes the ping the dog just sent.
func pinged(t *testing.T, ch <-chan ipc.Frame) uint64 {
	t.Helper()
	select {
	case f := <-ch:
		if f.T != ipc.FramePing || f.Ping == 0 {
			t.Fatalf("frame %+v, want a ping", f)
		}
		return f.Ping
	default:
		t.Fatal("no ping")
		return 0
	}
}

// restarting counts the windows the dog has begun to restart. The dog
// marks one as it decides, before the restart's own goroutine runs.
func restarting(c *Core) int {
	c.fdog.mu.Lock()
	defer c.fdog.mu.Unlock()
	n := 0
	for _, w := range c.fdog.watches {
		if w.acting {
			n++
		}
	}
	return n
}

// waitSpared waits until the dog has said it leaves pid's window be.
func waitSpared(t *testing.T, c *Core, pid int) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		c.fdog.mu.Lock()
		spared := false
		for _, w := range c.fdog.watches {
			spared = spared || w.pid == pid && w.spared && !w.acting
		}
		c.fdog.mu.Unlock()
		if spared {
			return
		}
		if time.Now().After(end) {
			t.Fatalf("the dog did not leave %d be", pid)
		}
	}
}

func answer(c *Core, ch <-chan ipc.Frame, p ipc.Pong) {
	line, _ := json.Marshal(p)
	c.Hear(ch, line)
}

// quiet fails when the dog sends another frame.
func quiet(t *testing.T, ch <-chan ipc.Frame) {
	t.Helper()
	select {
	case f := <-ch:
		t.Fatalf("frame %+v while a ping waits for its answer", f)
	default:
	}
}

func TestTheFreezeDogPingsAWindowThatAnswers(t *testing.T) {
	c, f := dogCore(t)
	f.add(101)
	ch := watchFor(t, c, 101, "/Users/alice/Applications/Irie.app")
	t0 := time.Now()

	c.lookAtWindows(t0)
	n := pinged(t, ch)
	// No second ping while the first waits, however long it waits short
	// of ui.freeze_dog.
	c.lookAtWindows(t0.Add(time.Second))
	quiet(t, ch)
	answer(c, ch, ipc.Pong{Pong: n, Front: true})
	for i := 2; i < 60; i++ {
		c.lookAtWindows(t0.Add(time.Duration(i) * time.Second))
		m := pinged(t, ch)
		if m <= n {
			t.Fatalf("ping %d after ping %d", m, n)
		}
		n = m
		answer(c, ch, ipc.Pong{Pong: n, Front: true})
	}
	if killed, _, _ := f.calls(); len(killed) > 0 {
		t.Errorf("a window that answers was force quit: %v", killed)
	}
}

func TestTheFreezeDogRestartsAWindowThatStopsAnswering(t *testing.T) {
	c, f := dogCore(t)
	f.add(202)
	app := "/Users/alice/Applications/Irie.app"
	ch := watchFor(t, c, 202, app)
	t0 := time.Now()

	c.lookAtWindows(t0)
	// It last answered as an app behind the one in front.
	answer(c, ch, ipc.Pong{Pong: pinged(t, ch), Front: false})
	c.lookAtWindows(t0.Add(time.Second))
	pinged(t, ch)
	// Then nothing, for ui.freeze_dog, which is 10s.
	for i := 2; i <= 10; i++ {
		c.lookAtWindows(t0.Add(time.Duration(i) * time.Second))
		quiet(t, ch)
	}
	if n := restarting(c); n > 0 {
		t.Fatalf("a restart began %v after the last ping", 9*time.Second)
	}
	c.lookAtWindows(t0.Add(11 * time.Second))
	select {
	case <-f.done:
	case <-time.After(5 * time.Second):
		t.Fatal("no restart 10s after the last ping")
	}
	killed, opened, sampled := f.calls()
	if len(killed) != 1 || killed[0] != 202 {
		t.Errorf("killed %v, want 202", killed)
	}
	if want := []openCall{{app, false, false, 10 * time.Second}}; len(opened) != 1 || opened[0] != want[0] {
		t.Errorf("opened %+v, want %+v", opened, want)
	}
	if len(sampled) != 1 || filepath.Dir(sampled[0]) != c.paths.Freezes() || !strings.HasSuffix(sampled[0], "-202.txt") {
		t.Errorf("report %v, want one for pid 202 in %s", sampled, c.paths.Freezes())
	}
	// A restart in progress is not started again.
	c.lookAtWindows(t0.Add(12 * time.Second))
	if killed, _, _ := f.calls(); len(killed) != 1 {
		t.Errorf("killed %v after the restart", killed)
	}
}

func TestTheFreezeDogOpensTheAppAsItStood(t *testing.T) {
	for _, tc := range []struct {
		name          string
		pong          *ipc.Pong
		front, hidden bool
	}{
		{"in front", &ipc.Pong{Front: true}, true, false},
		{"hidden", &ipc.Pong{Hidden: true}, false, true},
		{"never answered, as when it froze opening", nil, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, f := dogCore(t)
			f.add(303)
			ch := watchFor(t, c, 303, "/Applications/Irie.app")
			t0 := time.Now()
			c.lookAtWindows(t0)
			n := pinged(t, ch)
			at := t0
			if tc.pong != nil {
				p := *tc.pong
				p.Pong = n
				answer(c, ch, p)
				at = t0.Add(time.Second)
				c.lookAtWindows(at)
				pinged(t, ch)
			}
			for i := 1; i <= 10; i++ {
				c.lookAtWindows(at.Add(time.Duration(i) * time.Second))
			}
			select {
			case <-f.done:
			case <-time.After(5 * time.Second):
				t.Fatal("no restart")
			}
			if _, opened, _ := f.calls(); len(opened) != 1 || opened[0].front != tc.front || opened[0].hidden != tc.hidden {
				t.Errorf("opened %+v, want front %v and hidden %v", opened, tc.front, tc.hidden)
			}
		})
	}
}

func TestTheFreezeDogBlamesNoWindowWhenTheCoreWasHeldUp(t *testing.T) {
	c, f := dogCore(t)
	f.add(404)
	ch := watchFor(t, c, 404, "/Applications/Irie.app")
	t0 := time.Now()
	c.lookAtWindows(t0)
	pinged(t, ch)
	// The core was stopped, or the machine slept, for 30 s: the answer may
	// be waiting unread, so the ping's time starts again.
	held := t0.Add(30 * time.Second)
	c.lookAtWindows(held)
	for i := 1; i < 10; i++ {
		c.lookAtWindows(held.Add(time.Duration(i) * time.Second))
	}
	if n := restarting(c); n > 0 {
		t.Fatal("a restart began because the core was held up")
	}
	c.lookAtWindows(held.Add(10 * time.Second))
	select {
	case <-f.done:
	case <-time.After(5 * time.Second):
		t.Fatal("no restart 10s after the core came back")
	}
}

func TestTheFreezeDogLeavesBe(t *testing.T) {
	for _, tc := range []struct {
		name  string
		limit time.Duration
		proc  func(p *proc.Proc)
	}{
		{"a window under a debugger", 10 * time.Second, func(p *proc.Proc) { p.Traced = true }},
		{"any window while it is off", 0, nil},
	} {
		// A window under a debugger is looked at, and then left be.
		t.Run(tc.name, func(t *testing.T) {
			c, f := dogCore(t)
			cfg := *c.cfg()
			cfg.UI.FreezeDog = tc.limit
			c.useConfig(cfg)
			f.add(505)
			if tc.proc != nil {
				p := f.procs[505]
				tc.proc(&p)
				f.procs[505] = p
			}
			ch := watchFor(t, c, 505, "/Applications/Irie.app")
			t0 := time.Now()
			c.lookAtWindows(t0)
			pinged(t, ch)
			for i := 1; i <= 10; i++ {
				c.lookAtWindows(t0.Add(time.Duration(i) * time.Second))
			}
			if tc.limit == 0 {
				if n := restarting(c); n > 0 {
					t.Fatal("a restart began while the dog is off")
				}
			} else {
				waitSpared(t, c, 505)
			}
			if killed, _, _ := f.calls(); len(killed) > 0 {
				t.Errorf("force quit %v", killed)
			}
		})
	}
}

func TestTheFreezeDogRestartsThreeTimesInTenMinutesAtMost(t *testing.T) {
	c, f := dogCore(t)
	t0 := time.Now()
	freeze := func(pid int, at time.Time) {
		t.Helper()
		f.add(pid)
		ch := watchFor(t, c, pid, "/Applications/Irie.app")
		c.lookAtWindows(at)
		pinged(t, ch)
		for i := 1; i <= 10; i++ {
			c.lookAtWindows(at.Add(time.Duration(i) * time.Second))
		}
	}
	for i, pid := range []int{601, 602, 603} {
		freeze(pid, t0.Add(time.Duration(i)*time.Minute))
		select {
		case <-f.done:
		case <-time.After(5 * time.Second):
			t.Fatalf("restart %d did not happen", i+1)
		}
	}
	freeze(604, t0.Add(3*time.Minute))
	waitSpared(t, c, 604)
	if killed, _, _ := f.calls(); len(killed) != 3 {
		t.Errorf("killed %v, want the first three only", killed)
	}
}

func TestAWatchNeedsALivingProcess(t *testing.T) {
	c, f := dogCore(t)
	f.add(707)
	for _, a := range []ipc.SubscribeArgs{
		{View: ipc.ViewWatch},
		{View: ipc.ViewWatch, PID: 708},
		{View: ipc.ViewWatch, PID: 707, Bundle: "Applications/Irie.app"},
	} {
		raw, _ := json.Marshal(a)
		if _, _, err := c.Subscribe(raw); err == nil {
			t.Errorf("%+v was watched", a)
		}
	}
}

func TestAnAnswerToAnotherPingCountsForNothing(t *testing.T) {
	c, f := dogCore(t)
	f.add(808)
	ch := watchFor(t, c, 808, "/Applications/Irie.app")
	t0 := time.Now()
	c.lookAtWindows(t0)
	n := pinged(t, ch)
	answer(c, ch, ipc.Pong{Pong: n + 1, Front: true})
	c.Hear(ch, []byte("not json"))
	for i := 1; i <= 10; i++ {
		c.lookAtWindows(t0.Add(time.Duration(i) * time.Second))
	}
	select {
	case <-f.done:
	case <-time.After(5 * time.Second):
		t.Fatal("a wrong answer kept a frozen window from its restart")
	}
}

func TestOnlyTheNewestFreezeReportsAreKept(t *testing.T) {
	dir := t.TempDir()
	for i := range 12 {
		os.WriteFile(filepath.Join(dir, "2026-10-31T10-00-"+twoDigits(i)+"Z-1.txt"), nil, 0o600)
	}
	os.WriteFile(filepath.Join(dir, "notes.md"), nil, 0o600)
	pruneReports(dir, freezeReports-1)
	ents, _ := os.ReadDir(dir)
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	if len(names) != freezeReports {
		t.Fatalf("left %v", names)
	}
	if names[0] != "2026-10-31T10-00-03Z-1.txt" || names[len(names)-1] != "notes.md" {
		t.Errorf("left %v, want the newest nine reports and notes.md", names)
	}
}

func twoDigits(i int) string { return string([]byte{byte('0' + i/10), byte('0' + i%10)}) }

// TestTheFreezeDogWatchesOverTheSocket is the whole round over a real
// socket: a watch subscription gets pings, and the client's pongs, sent on
// the same connection, reach the dog.
func TestTheFreezeDogWatchesOverTheSocket(t *testing.T) {
	c, _ := dogCore(t)
	c.fdog.lookup = lookupProc
	sock := filepath.Join(testutil.ShortDir(t), "core.sock")
	l, err := ipc.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go ipc.Serve(l, c)

	cl, err := ipc.Dial(sock, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	frames, err := cl.Subscribe(ipc.SubscribeArgs{View: ipc.ViewWatch, PID: os.Getpid(), Bundle: "/Applications/Irie.app"})
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Now()
	var w *watch
	for i := 0; i < 3; i++ {
		c.lookAtWindows(t0.Add(time.Duration(i) * time.Second))
		var f ipc.Frame
		select {
		case f = <-frames:
		case <-time.After(5 * time.Second):
			t.Fatalf("ping %d did not arrive", i+1)
		}
		if f.T != ipc.FramePing {
			t.Fatalf("frame %+v, want a ping", f)
		}
		if err := cl.Send(ipc.Pong{Pong: f.Ping, Hidden: true}); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			c.fdog.mu.Lock()
			for _, x := range c.fdog.watches {
				w = x
			}
			answered := w != nil && !w.waiting && w.hidden
			c.fdog.mu.Unlock()
			if answered {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("the answer to ping %d did not reach the dog", f.Ping)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	// The watch ends with its connection.
	cl.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		c.fdog.mu.Lock()
		n := len(c.fdog.watches)
		c.fdog.mu.Unlock()
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the watch outlived its connection")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestTheFreezeDogForceQuitsARealProcess has the dog force quit a real
// process that stands in for a frozen window, and wait until it is gone
// before it opens the app again: Launch Services would bring back an app
// that still runs.
func TestTheFreezeDogForceQuitsARealProcess(t *testing.T) {
	c, f := dogCore(t)
	// It dies a moment after the kill, as on a busy machine.
	c.fdog.lookup = lookupProc
	c.fdog.kill = func(pid int) error {
		time.AfterFunc(100*time.Millisecond, func() { killProc(pid) })
		return nil
	}
	var openedLive bool
	open := c.fdog.open
	c.fdog.open = func(bundle string, front, hidden bool, stuck time.Duration) error {
		pid := 0
		f.mu.Lock()
		pid = f.watched
		f.mu.Unlock()
		if p, ok := lookupProc(pid); ok && !p.Zombie {
			openedLive = true
		}
		return open(bundle, front, hidden, stuck)
	}
	stuck := exec.Command("sleep", "60")
	if err := stuck.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- stuck.Wait() }()
	t.Cleanup(func() { stuck.Process.Kill() })

	f.mu.Lock()
	f.watched = stuck.Process.Pid
	f.mu.Unlock()
	ch := watchFor(t, c, stuck.Process.Pid, "/Applications/Irie.app")
	t0 := time.Now()
	c.lookAtWindows(t0)
	pinged(t, ch)
	for i := 1; i <= 10; i++ {
		c.lookAtWindows(t0.Add(time.Duration(i) * time.Second))
	}
	select {
	case err := <-exited:
		ws, ok := stuck.ProcessState.Sys().(syscall.WaitStatus)
		if !ok || !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
			t.Errorf("the process ended with %v, want SIGKILL", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the frozen process still runs")
	}
	select {
	case <-f.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the app was not opened again")
	}
	if openedLive {
		t.Error("the app was opened again while the frozen one still ran")
	}
}
