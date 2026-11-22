package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/proc"
	"github.com/irie-people/irie/internal/sysload"
	"github.com/irie-people/irie/internal/testutil"
)

// withOldHosts makes the core run its agents under the host of before
// freeze (testutil/oldhost), as a running Irie's older agents do.
func (c *coreEnv) withOldHosts() {
	c.t.Helper()
	old := testutil.BuildOldHost(c.t)
	wrapper := filepath.Join(c.t.TempDir(), "irie")
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = host ]; then exec %q \"$@\"; fi\nexec %q \"$@\"\n", old, c.bin)
	if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil {
		c.t.Fatal(err)
	}
	c.t.Setenv(brand.Env("BIN"), wrapper)
}

func (c *coreEnv) hostPID(s ipc.Session) int {
	c.t.Helper()
	pid, err := strconv.Atoi(c.Tmux(c.t, "display-message", "-p", "-t", s.Pane, "#{pane_pid}"))
	if err != nil {
		c.t.Fatal(err)
	}
	return pid
}

// tree is what a host runs, from the process table.
func tree(t *testing.T, host int) []proc.Proc {
	t.Helper()
	tb, err := proc.Read()
	if err != nil {
		t.Fatal(err)
	}
	return tb.Tree(host)
}

// kids waits for the processes of the fake agent's kids step and returns
// the agent's pid. They are killed when the test ends, the group of their
// own included, which the end of the tmux server does not reach.
func (c *coreEnv) kids(s ipc.Session) (agent int) {
	c.t.Helper()
	var pids []int
	testutil.Eventually(c.t, 10*time.Second, "the agent started no kids", func() bool {
		for _, r := range c.agentLog(s.ID) {
			if r["t"] == "start" {
				agent = int(r["pid"].(float64))
			}
			if r["t"] == "kids" {
				for _, p := range r["pids"].([]any) {
					pids = append(pids, int(p.(float64)))
				}
			}
		}
		return len(pids) == 2
	})
	c.t.Cleanup(func() {
		syscall.Kill(-pids[1], syscall.SIGKILL)
		syscall.Kill(-pids[1], syscall.SIGCONT)
		syscall.Kill(pids[0], syscall.SIGKILL)
		syscall.Kill(pids[0], syscall.SIGCONT)
	})
	host := c.hostPID(s)
	// The agent, its two children and the shell's own child.
	testutil.Eventually(c.t, 5*time.Second, "the kids' tree is not complete", func() bool {
		ps := tree(c.t, host)
		return len(ps) >= 4 && slices.ContainsFunc(ps, func(p proc.Proc) bool { return p.PPID == pids[1] })
	})
	for _, p := range tree(c.t, host) {
		if p.PID == pids[1] && p.PGID != pids[1] {
			c.t.Fatalf("the second child is in group %d, not its own", p.PGID)
		}
	}
	return agent
}

func (c *coreEnv) freeze(id string, on bool) {
	c.t.Helper()
	op := ipc.OpThaw
	if on {
		op = ipc.OpFreeze
	}
	var r ipc.FreezeResult
	if err := c.call(op, ipc.FreezeArgs{Session: id}, &r); err != nil || !slices.Equal(r.Sessions, []string{id}) {
		c.t.Fatalf("%s %s: %v, %v", op, id, r.Sessions, err)
	}
}

// stoppedOf says which processes of ps are stopped and which run.
func stoppedOf(ps []proc.Proc) (stopped, running []string) {
	for _, p := range ps {
		d := fmt.Sprintf("%d %s", p.PID, p.Name)
		if p.Stopped {
			stopped = append(stopped, d)
		} else if !p.Zombie {
			running = append(running, d)
		}
	}
	return stopped, running
}

func TestFreezeStopsTheWholeTreeAndThawContinuesIt(t *testing.T) {
	c := setup(t)
	s := c.spawn("prompt,kids,sleep:1500ms,tool:Bash,stop")
	c.kids(s)
	host := c.hostPID(s)

	start := time.Now()
	c.freeze(s.ID, true)
	if _, running := stoppedOf(tree(t, host)); len(running) > 0 {
		t.Errorf("%v after freezing still run %v", time.Since(start), running)
	}
	if d := time.Since(start); d > 100*time.Millisecond && !slow(t) {
		t.Errorf("freezing took %v", d)
	}
	row, _ := c.row(s.ID)
	if row.State != ipc.StateFrozen || row.Frozen == nil || row.Freezing {
		t.Errorf("row = %s, frozen %v, freezing %v", row.State, row.Frozen, row.Freezing)
	}
	if off := c.Tmux(t, "display-message", "-p", "-t", s.Pane, "#{pane_input_off}"); off != "1" {
		t.Errorf("the frozen pane takes input: pane_input_off %q", off)
	}
	// The host leaves the agent stopped; Irie's tick changes nothing.
	time.Sleep(1500 * time.Millisecond)
	if _, running := stoppedOf(tree(t, host)); len(running) > 0 {
		t.Errorf("after 1.5 s these run again: %v", running)
	}

	c.freeze(s.ID, false)
	if stopped, _ := stoppedOf(tree(t, host)); len(stopped) > 0 {
		t.Errorf("after the thaw these are still stopped: %v", stopped)
	}
	if _, err := os.Stat(c.paths.FreezeFile(s.ID)); !os.IsNotExist(err) {
		t.Errorf("freeze.json is still there: %v", err)
	}
	if off := c.Tmux(t, "display-message", "-p", "-t", s.Pane, "#{pane_input_off}"); off != "0" {
		t.Errorf("the thawed pane takes no input: pane_input_off %q", off)
	}
	// It was working, so its next tool call hears how long it was frozen.
	c.waitState(s.ID, ipc.StateDone)
	if !slices.ContainsFunc(c.agentLog(s.ID), func(r map[string]any) bool {
		out, _ := r["stdout"].(string)
		return r["event"] == "PreToolUse" && strings.Contains(out, "The user froze this session for")
	}) {
		t.Error("the agent was not told it was frozen")
	}
}

func TestFreezeWithAHostFromBeforeFreeze(t *testing.T) {
	// Such a host continues every stop of its agent that it sees, and tmux
	// continues a stopped host, so its agent runs on while everything under
	// it stops, and the shim holds the agent at its next tool call.
	c := prepare(t, agentConfig{})
	c.withOldHosts()
	c.up()
	s := c.spawn("prompt,kids,sleep:1500ms,tool:Bash,stop")
	agent := c.kids(s)
	host := c.hostPID(s)
	if _, err := os.Stat(c.paths.HostFile(s.ID)); !os.IsNotExist(err) {
		t.Fatalf("the old host wrote host.json: %v", err)
	}

	c.freeze(s.ID, true)
	check := func(when string) {
		t.Helper()
		for _, p := range tree(t, host) {
			switch {
			case p.PID == agent && p.Stopped:
				t.Errorf("%s: the agent is stopped, which its host would undo", when)
			case p.PID != agent && !p.Stopped && !p.Zombie && p.Name != "irie" && p.Name != "sh":
				t.Errorf("%s: %d %s runs", when, p.PID, p.Name)
			}
		}
	}
	check("at once")
	if row, _ := c.row(s.ID); row.State != ipc.StateFrozen || !row.Freezing {
		t.Errorf("row = %s, freezing %v; want frozen, its agent still on its way to a tool call", row.State, row.Freezing)
	}
	time.Sleep(500 * time.Millisecond)
	check("after 0.5 s")

	// The agent comes to its tool call and waits there. Its hook tells the
	// core before it holds the agent, unless the core's next tick stops the
	// hook first (keepFrozen stops what starts in a frozen session), as a
	// busy machine's slow start of the hook allows: the agent still waits,
	// on its stopped hook, but the row says freezing until the thaw.
	held := func() bool {
		row, _ := c.row(s.ID)
		return row.State == ipc.StateFrozen && !row.Freezing
	}
	for end := time.Now().Add(10 * time.Second); !held() && time.Now().Before(end); {
		time.Sleep(50 * time.Millisecond)
	}
	if !held() && !slow(t) {
		t.Error("after 10s: the row never said the agent was held")
	}
	hooked := func(event string) bool {
		return slices.ContainsFunc(c.agentLog(s.ID), func(r map[string]any) bool { return r["t"] == "hook" && r["event"] == event })
	}
	time.Sleep(500 * time.Millisecond)
	if hooked("PreToolUse") {
		t.Fatal("the frozen agent's tool call went through")
	}
	check("while held")

	c.freeze(s.ID, false)
	testutil.Eventually(t, 10*time.Second, "the thawed agent did not finish its turn", func() bool { return hooked("Stop") })
	if stopped, _ := stoppedOf(tree(t, host)); len(stopped) > 0 {
		t.Errorf("after the thaw these are still stopped: %v", stopped)
	}
}

func TestAFrozenSessionStaysFrozenThroughACoreRestart(t *testing.T) {
	c := setup(t)
	s := c.spawn("prompt,kids")
	c.kids(s)
	host := c.hostPID(s)
	c.freeze(s.ID, true)
	c.restartCore()
	time.Sleep(1500 * time.Millisecond)
	if row, _ := c.row(s.ID); row.State != ipc.StateFrozen {
		t.Errorf("after a restart the row is %s", row.State)
	}
	if _, running := stoppedOf(tree(t, host)); len(running) > 0 {
		t.Errorf("after a restart these run: %v", running)
	}
	c.freeze(s.ID, false)
	if stopped, _ := stoppedOf(tree(t, host)); len(stopped) > 0 {
		t.Errorf("after the thaw these are still stopped: %v", stopped)
	}
	if row := c.waitState(s.ID, ipc.StateWorking); row.Frozen != nil {
		t.Errorf("the thawed row is frozen since %v", row.Frozen)
	}
}

func TestFreezeAllAndThawAll(t *testing.T) {
	c := setup(t)
	a, b := c.spawn("prompt"), c.spawn("prompt")
	c.waitState(a.ID, ipc.StateWorking)
	c.waitState(b.ID, ipc.StateWorking)
	var r ipc.FreezeResult
	if err := c.call(ipc.OpFreeze, ipc.FreezeArgs{All: true}, &r); err != nil || len(r.Sessions) != 2 {
		t.Fatalf("freeze all: %v, %v", r.Sessions, err)
	}
	for _, s := range c.list() {
		if s.State != ipc.StateFrozen {
			t.Errorf("%s is %s", s.Name, s.State)
		}
	}
	// Two thaw in turn: the first at once, the other a gap later
	// (TestAThawOfSeveralGoesInTurn).
	if err := c.call(ipc.OpThaw, ipc.FreezeArgs{All: true}, &r); err != nil || len(r.Sessions) != 1 || len(r.Queued) != 1 {
		t.Fatalf("thaw all: %+v, %v", r, err)
	}
	c.waitState(r.Sessions[0], ipc.StateWorking)
	if err := c.call(ipc.OpFreeze, ipc.FreezeArgs{}, nil); err == nil {
		t.Error("a freeze of nothing went through")
	}
}

// A thaw of several goes in turn, one session every thawGap, so that each
// agent frozen long finds the sign-in the one before renewed (DESIGN
// §4.3): the first at once and the next a gap later, which its row says
// while it waits, even across a restart of the core, since state.json
// keeps the queue.
func TestAThawOfSeveralGoesInTurn(t *testing.T) {
	c := setup(t)
	a := c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Name: "alpha"})
	b := c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Name: "beta"})
	c.waitState(a.ID, ipc.StateWorking)
	c.waitState(b.ID, ipc.StateWorking)
	var r ipc.FreezeResult
	if err := c.call(ipc.OpFreeze, ipc.FreezeArgs{All: true}, &r); err != nil {
		t.Fatal(err)
	}
	c.waitState(a.ID, ipc.StateFrozen)
	c.waitState(b.ID, ipc.StateFrozen)
	if err := c.call(ipc.OpThaw, ipc.FreezeArgs{All: true}, &r); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.Sessions, []string{a.ID}) || !slices.Equal(r.Queued, []string{b.ID}) {
		t.Fatalf("thaw all = %+v; want alpha now and beta in turn", r)
	}
	c.waitState(a.ID, ipc.StateWorking)
	waits := func(when string) {
		t.Helper()
		if s, _ := c.row(b.ID); s.State != ipc.StateFrozen || !strings.Contains(s.Detail, thawsInTurn) {
			t.Errorf("%s, beta is %s, %q; want frozen, waiting its turn", when, s.State, s.Detail)
		}
	}
	waits("at first")
	c.restartCore()
	waits("after a restart")
	testutil.Eventually(t, thawGap+20*time.Second, "beta never thawed", func() bool {
		s, _ := c.row(b.ID)
		return s.State == ipc.StateWorking
	})
	var at []time.Time
	for _, s := range []ipc.Session{a, b} {
		for _, e := range c.events(s.ID) {
			if e.Kind == event.FreezeOff {
				at = append(at, e.Time())
			}
		}
	}
	// The core looks once a second, and stamps a thaw once its processes
	// go on, so allow a second.
	if len(at) != 2 || at[1].Sub(at[0]) < thawGap-time.Second {
		t.Errorf("thawed at %v; want two, %v apart", at, thawGap)
	}
}

// A thaw in turn waits thawGap after the last thaw, and holds while an
// agent's turn has failed for want of a sign-in since it began, which its
// rows say; a session gone or thawed by another hand leaves the queue, and
// one waiting its turn counts as thawed. Nothing here gets as far as a
// thaw, so it needs no tmux.
func TestAThawInTurnWaitsItsTurnAndForASignIn(t *testing.T) {
	c := newPeerCore(t)
	now := time.Now().UTC()
	frozen := func(id string) *session {
		x := &session{id: id, name: id}
		x.track.frozen = &event.Event{TS: event.Stamp(now.Add(-time.Hour)), Kind: event.FreezeOn}
		c.sessions[id] = x
		return x
	}
	a, b := frozen("s1"), frozen("s2")
	c.mu.Lock()
	defer c.mu.Unlock()
	c.steps.thaws = thawQueue{IDs: []string{"s1", "s2", "s9"}, Since: now.Add(-time.Minute)}
	c.lastThaw = now.Add(-10 * time.Second)
	if got := c.thawNext(now); len(got) != 0 || !slices.Equal(c.steps.thaws.IDs, []string{"s1", "s2"}) {
		t.Fatalf("too soon: thawed %v, queue %v; want none, and s9, which is gone, out", got, c.steps.thaws.IDs)
	}
	if w := c.thawWait(a); w != thawsInTurn {
		t.Errorf("a row waiting its turn says %q", w)
	}
	if c.frozenNow(a) || !c.frozenNow(&session{id: "s7", track: track{frozen: a.track.frozen}}) {
		t.Error("a session waiting its turn counts as frozen, or one frozen as not")
	}

	// Its turn has come, but an agent signed out since the thaw began.
	login := &session{id: "s3", name: "login"}
	login.track.needs = map[string]event.Event{event.Main: {TS: event.Stamp(now.Add(-30 * time.Second)), Kind: event.TurnFailed,
		Data: map[string]any{"error": "authentication_failed"}}}
	c.sessions[login.id] = login
	if got := c.thawNext(now.Add(time.Hour)); len(got) != 0 || len(c.steps.thaws.IDs) != 2 {
		t.Errorf("signed out: thawed %v, queue %v; want none, both waiting", got, c.steps.thaws.IDs)
	}
	if w := c.thawWait(b); w != thawsHeld {
		t.Errorf("a row held for a sign-in says %q", w)
	}
	// A sign-in that failed before the thaw began holds nothing.
	login.track.needs[event.Main] = event.Event{TS: event.Stamp(now.Add(-2 * time.Minute)), Kind: event.TurnFailed,
		Data: map[string]any{"error": "authentication_failed"}}
	if w := c.thawWait(b); w != thawsInTurn {
		t.Errorf("after an older failure the row says %q", w)
	}

	// Thawed by another hand, it leaves the queue; frozen by hand, so does
	// the other, and the queue is done.
	b.track.frozen = nil
	c.thawNext(now)
	if !slices.Equal(c.steps.thaws.IDs, []string{"s1"}) || c.thawWait(b) != "" {
		t.Errorf("queue %v after beta thawed by hand", c.steps.thaws.IDs)
	}
	c.unqueue("s1")
	if len(c.steps.thaws.IDs) != 0 || !c.steps.thaws.Since.IsZero() || !c.frozenNow(a) {
		t.Errorf("after a freeze by hand: %+v", c.steps.thaws)
	}
}

// names are the rows' names in the list's order.
func names(rows []ipc.Session) []string {
	var out []string
	for i, s := range rows {
		if s.Order != i {
			out = append(out, fmt.Sprintf("%s@%d", s.Name, s.Order))
			continue
		}
		out = append(out, s.Name)
	}
	return out
}

func TestReorderKeepsOneOrderThroughARestart(t *testing.T) {
	c := setup(t)
	for _, n := range []string{"alpha", "beta", "gamma"} {
		c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Name: n})
	}
	id := map[string]string{}
	for _, s := range c.list() {
		id[s.Name] = s.ID
	}
	if got := names(c.list()); !slices.Equal(got, []string{"alpha", "beta", "gamma"}) {
		t.Fatalf("order = %v, want the order they started", got)
	}
	if err := c.call(ipc.OpReorder, ipc.ReorderArgs{Session: id["gamma"], Before: id["alpha"]}, nil); err != nil {
		t.Fatal(err)
	}
	if got := names(c.list()); !slices.Equal(got, []string{"gamma", "alpha", "beta"}) {
		t.Errorf("after gamma before alpha: %v", got)
	}
	if err := c.call(ipc.OpReorder, ipc.ReorderArgs{Session: id["alpha"]}, nil); err != nil {
		t.Fatal(err)
	}
	want := []string{"gamma", "beta", "alpha"}
	if got := names(c.list()); !slices.Equal(got, want) {
		t.Errorf("after alpha last: %v", got)
	}
	// A state change moves nothing; a new session goes last.
	c.waitState(id["beta"], ipc.StateWorking)
	c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Name: "delta"})
	want = append(want, "delta")
	if got := names(c.list()); !slices.Equal(got, want) {
		t.Errorf("with a new session: %v, want %v", got, want)
	}
	c.restartCore()
	if got := names(c.list()); !slices.Equal(got, want) {
		t.Errorf("after a restart: %v, want %v", got, want)
	}
	for _, a := range []ipc.ReorderArgs{{Session: "s000000000000"}, {Session: id["beta"], Before: "s000000000000"}} {
		if err := c.call(ipc.OpReorder, a, nil); err == nil || !strings.Contains(err.Error(), "no session") {
			t.Errorf("reorder %+v: %v", a, err)
		}
	}
}

// parked are the names of the parked rows, in the list's order.
func parked(rows []ipc.Session) []string {
	var out []string
	for _, s := range rows {
		if s.Parked {
			out = append(out, s.Name)
		}
	}
	return out
}

// unparked are the names of the rows that are not parked, in the list's
// order.
func unparked(rows []ipc.Session) []string {
	var out []string
	for _, s := range rows {
		if !s.Parked {
			out = append(out, s.Name)
		}
	}
	return out
}

// park parks a session (on) or unparks it, as the sidebars' z does.
func (c *coreEnv) park(id string, on bool) {
	c.t.Helper()
	op := ipc.OpUnpark
	if on {
		op = ipc.OpPark
	}
	if err := c.call(op, ipc.ParkArgs{Session: id}, nil); err != nil {
		c.t.Fatalf("%s %s: %v", op, id, err)
	}
}

// Parking a session puts it last, in Parked, and unparking it puts it last
// among the rest, just above Parked, not back where it was. Both last
// through a restart, and boosting a parked session leaves it where it is.
func TestParkAndUnparkPutASessionLastAndABoostMovesNone(t *testing.T) {
	c := setup(t)
	for _, n := range []string{"alpha", "beta", "gamma", "delta"} {
		c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Name: n})
	}
	id := map[string]string{}
	for _, s := range c.list() {
		id[s.Name] = s.ID
	}
	for _, n := range []string{"beta", "alpha"} {
		c.park(id[n], true)
	}
	// Parking twice changes nothing, and moves nothing.
	c.park(id["beta"], true)
	want := []string{"gamma", "delta", "beta", "alpha"}
	rows := c.list()
	if got := parked(rows); !slices.Equal(got, []string{"beta", "alpha"}) {
		t.Errorf("parked = %v, want beta then alpha, the last parked last", got)
	}
	if got := names(rows); !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
	c.restartCore()
	rows = c.list()
	if got := parked(rows); !slices.Equal(got, []string{"beta", "alpha"}) {
		t.Errorf("after a restart, parked = %v", got)
	}
	if got := names(rows); !slices.Equal(got, want) {
		t.Errorf("after a restart, order = %v, want %v", got, want)
	}
	// Unparked, beta comes last among the rest, below delta, and unparking
	// it twice moves nothing.
	c.park(id["beta"], false)
	c.park(id["beta"], false)
	rows = c.list()
	if got := parked(rows); !slices.Equal(got, []string{"alpha"}) {
		t.Errorf("after unparking beta, parked = %v", got)
	}
	if got := unparked(rows); !slices.Equal(got, []string{"gamma", "delta", "beta"}) {
		t.Errorf("after unparking beta, the rest = %v", got)
	}
	// Boosting a parked session leaves it parked, where it is (DESIGN §4.3).
	before := names(c.list())
	if err := c.call(ipc.OpPriority, ipc.PriorityArgs{Session: id["alpha"], Level: 1}, nil); err != nil {
		t.Fatal(err)
	}
	rows = c.list()
	if got := parked(rows); !slices.Equal(got, []string{"alpha"}) {
		t.Errorf("after boosting alpha, parked = %v", got)
	}
	if got := names(rows); !slices.Equal(got, before) {
		t.Errorf("after boosting alpha: %v, want %v", got, before)
	}
	if err := c.call(ipc.OpPriority, ipc.PriorityArgs{Session: id["alpha"], Level: 0}, nil); err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{ipc.OpPark, ipc.OpUnpark} {
		if err := c.call(op, ipc.ParkArgs{Session: "s000000000000"}, nil); err == nil || !strings.Contains(err.Error(), "no session") {
			t.Errorf("%s of no session: %v", op, err)
		}
	}
}

// pri is a process's scheduling priority, which macOS's background state
// lowers to 4.
func pri(t *testing.T, pid int) string {
	t.Helper()
	out, _ := exec.Command("ps", "-o", "pri=", "-p", strconv.Itoa(pid)).Output()
	return strings.TrimSpace(string(out))
}

// demoted says whether the test runs in the background, as a session's
// processes do while another is promoted. macOS throttles its CPU and I/O
// then, so a bound on time would measure the machine rather than Irie.
func demoted(t *testing.T) bool {
	return proc.CanBackground && pri(t, os.Getpid()) == "4"
}

// slow reports whether a time bound cannot hold here, because the scheduler
// rather than Irie sets the time: the test runs in macOS's background state,
// or the machine has more work waiting than CPUs to run it. Gabriel never
// holds back an install (tools/live.sh), so on a busy machine an install's
// tests meet only the second.
func slow(t *testing.T) bool {
	if demoted(t) {
		return true
	}
	l, err := sysload.Read()
	if err != nil || l.RunQueue <= float64(l.CPUs) {
		return false
	}
	t.Logf("not timed: %.1f CPUs' worth of work waiting on %d CPUs", l.RunQueue, l.CPUs)
	return true
}

// skipIfDemoted skips a test that looks for processes out of the
// background once the test itself went there, as when the machine got busy
// meanwhile and Gabriel put the test's session in the background (an
// agent's own go test; an install's never is): what it starts from then on
// starts there too.
func skipIfDemoted(t *testing.T) {
	t.Helper()
	if demoted(t) {
		t.Skip("the test went into the background, as a session's processes do while another is promoted or Gabriel holds it back")
	}
}

// backState is where a session's processes run, by ps: whether its agent
// is in the background, and which of the rest are and are not. The hook
// shims the agent runs are left out, as they come and go.
func (c *coreEnv) backState(s ipc.Session, agent int) (agentBack bool, back, up []string) {
	c.t.Helper()
	for _, p := range tree(c.t, c.hostPID(s)) {
		in := pri(c.t, p.PID) == "4"
		switch {
		case p.PID == agent:
			agentBack = in
		case p.Name == filepath.Base(c.bin):
		case in:
			back = append(back, p.Name)
		default:
			up = append(up, p.Name)
		}
	}
	return agentBack, back, up
}

// A boost gives a session's tree the machine first and moves no card. The
// other sessions' tools step back, a session's that starts meanwhile too,
// but never their agents, which keep their network (DESIGN §4.3).
func TestABoostMovesNoCardAndPutsTheOthersToolsInTheBackground(t *testing.T) {
	c := setup(t)
	var all []ipc.Session
	agents := map[string]int{}
	for _, n := range []string{"alpha", "beta", "gamma"} {
		s := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,kids", Name: n})
		agents[s.ID] = c.kids(s)
		all = append(all, s)
	}
	gamma := all[2]
	if err := c.call(ipc.OpPriority, ipc.PriorityArgs{Session: gamma.ID, Level: 1, Reason: "demo at noon"}, nil); err != nil {
		t.Fatal(err)
	}
	rows := c.list()
	if got := names(rows); !slices.Equal(got, []string{"alpha", "beta", "gamma"}) {
		t.Errorf("order = %v, want it as it was: a boost moves no card", got)
	}
	if rows[2].Priority != 1 || rows[0].Priority != 0 {
		t.Errorf("priorities = %d, %d", rows[2].Priority, rows[0].Priority)
	}
	if !proc.CanBackground {
		t.Skip("no background state here")
	}
	// While a session is boosted, the running Irie keeps the others' tools
	// in the background, a test one of them runs and all it starts as well,
	// so nothing here could show a difference.
	if pri(t, os.Getpid()) == "4" {
		t.Skip("the test runs in the background, as a session's processes do while another is boosted")
	}
	// At once: the call returns once it is done.
	for _, s := range all[:2] {
		agentBack, _, up := c.backState(s, agents[s.ID])
		if agentBack {
			t.Errorf("%s: its agent is in the background", s.Name)
		}
		if len(up) > 0 {
			t.Errorf("%s: %v not in the background", s.Name, up)
		}
	}
	skipIfDemoted(t)
	if agentBack, back, _ := c.backState(gamma, agents[gamma.ID]); agentBack || len(back) > 0 {
		t.Errorf("the boosted session's %v are in the background (its agent: %v)", back, agentBack)
	}
	// A session that starts meanwhile: its tools, which its agent starts
	// at normal priority, step back at the core's next tick.
	delta := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,kids", Name: "delta"})
	agents[delta.ID] = c.kids(delta)
	testutil.Eventually(t, 3*time.Second, "a new session's tools are not in the background", func() bool {
		agentBack, back, up := c.backState(delta, agents[delta.ID])
		return !agentBack && len(back) > 0 && len(up) == 0
	})

	if err := c.call(ipc.OpPriority, ipc.PriorityArgs{Session: gamma.ID, Level: 0}, nil); err != nil {
		t.Fatal(err)
	}
	skipIfDemoted(t)
	for _, s := range append(all, delta) {
		if agentBack, back, _ := c.backState(s, agents[s.ID]); agentBack || len(back) > 0 {
			t.Errorf("%s: %v still in the background (its agent: %v)", s.Name, back, agentBack)
		}
	}
	if got := names(c.list()); !slices.Equal(got, []string{"alpha", "beta", "gamma", "delta"}) {
		t.Errorf("putting it back moved it: %v", got)
	}
	if err := c.call(ipc.OpPriority, ipc.PriorityArgs{Session: gamma.ID, Level: 2}, nil); err == nil {
		t.Error("level 2 went through")
	}
}

func TestTurnsServeAPromotedRequestFirst(t *testing.T) {
	var q turns
	q.Lock()
	var mu sync.Mutex
	var got []int
	var wg sync.WaitGroup
	for i, level := range []int{0, 0, 1, 0, 1} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			q.LockAt(level)
			mu.Lock()
			got = append(got, i)
			mu.Unlock()
			q.Unlock()
		}()
		testutil.Eventually(t, time.Second, "the request did not queue", func() bool {
			q.mu.Lock()
			defer q.mu.Unlock()
			return len(q.waiting) == i+1
		})
	}
	q.Unlock()
	wg.Wait()
	if want := []int{2, 4, 0, 1, 3}; !slices.Equal(got, want) {
		t.Errorf("turns went to %v, want %v", got, want)
	}
}
