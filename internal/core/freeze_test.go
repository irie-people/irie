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

	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/proc"
	"github.com/irie-hq/irie/internal/testutil"
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
	c.t.Setenv("IRIE_BIN", wrapper)
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
	if d := time.Since(start); d > 100*time.Millisecond && !demoted(t) {
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

	// The agent comes to its tool call and waits there.
	testutil.Eventually(t, 10*time.Second, "the row never said the agent was held", func() bool {
		row, _ := c.row(s.ID)
		return row.State == ipc.StateFrozen && !row.Freezing
	})
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
	if err := c.call(ipc.OpThaw, ipc.FreezeArgs{All: true}, &r); err != nil || len(r.Sessions) != 2 {
		t.Fatalf("thaw all: %v, %v", r.Sessions, err)
	}
	for _, s := range c.list() {
		if s.State == ipc.StateFrozen {
			t.Errorf("%s is still frozen", s.Name)
		}
	}
	if err := c.call(ipc.OpFreeze, ipc.FreezeArgs{}, nil); err == nil {
		t.Error("a freeze of nothing went through")
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

func TestParkKeepsThePlaceThroughARestartAndPromoteUnparks(t *testing.T) {
	c := setup(t)
	for _, n := range []string{"alpha", "beta", "gamma"} {
		c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Name: n})
	}
	id := map[string]string{}
	for _, s := range c.list() {
		id[s.Name] = s.ID
	}
	order := []string{"alpha", "beta", "gamma"}
	for _, n := range []string{"alpha", "gamma"} {
		if err := c.call(ipc.OpPark, ipc.ParkArgs{Session: id[n]}, nil); err != nil {
			t.Fatal(err)
		}
	}
	// Parking twice changes nothing.
	if err := c.call(ipc.OpPark, ipc.ParkArgs{Session: id["gamma"]}, nil); err != nil {
		t.Fatal(err)
	}
	rows := c.list()
	if got := parked(rows); !slices.Equal(got, []string{"alpha", "gamma"}) {
		t.Errorf("parked = %v, want alpha and gamma", got)
	}
	// Parking leaves the order be, so unparking brings a session back to
	// its place.
	if got := names(rows); !slices.Equal(got, order) {
		t.Errorf("order = %v, want %v", got, order)
	}
	c.restartCore()
	if got := parked(c.list()); !slices.Equal(got, []string{"alpha", "gamma"}) {
		t.Errorf("after a restart, parked = %v", got)
	}
	if err := c.call(ipc.OpUnpark, ipc.ParkArgs{Session: id["alpha"]}, nil); err != nil {
		t.Fatal(err)
	}
	if got := parked(c.list()); !slices.Equal(got, []string{"gamma"}) {
		t.Errorf("after unparking alpha, parked = %v", got)
	}
	// Promoting a parked session unparks it (DESIGN §4.3).
	if err := c.call(ipc.OpPriority, ipc.PriorityArgs{Session: id["gamma"], Level: 1}, nil); err != nil {
		t.Fatal(err)
	}
	rows = c.list()
	if got := parked(rows); len(got) > 0 {
		t.Errorf("after promoting gamma, parked = %v", got)
	}
	if got := names(rows); !slices.Equal(got, []string{"gamma", "alpha", "beta"}) {
		t.Errorf("after promoting gamma: %v", got)
	}
	if err := c.call(ipc.OpPriority, ipc.PriorityArgs{Session: id["gamma"], Level: 0}, nil); err != nil {
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

func TestPromoteGoesToTheTopAndPutsTheOthersInTheBackground(t *testing.T) {
	c := setup(t)
	var all []ipc.Session
	for _, n := range []string{"alpha", "beta", "gamma"} {
		s := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,kids", Name: n})
		c.kids(s)
		all = append(all, s)
	}
	gamma := all[2]
	if err := c.call(ipc.OpPriority, ipc.PriorityArgs{Session: gamma.ID, Level: 1, Reason: "demo at noon"}, nil); err != nil {
		t.Fatal(err)
	}
	rows := c.list()
	if got := names(rows); !slices.Equal(got, []string{"gamma", "alpha", "beta"}) {
		t.Errorf("order = %v, want gamma on top", got)
	}
	if rows[0].Priority != 1 || rows[1].Priority != 0 {
		t.Errorf("priorities = %d, %d", rows[0].Priority, rows[1].Priority)
	}
	if !proc.CanBackground {
		t.Skip("no background state here")
	}
	// While a session is promoted, the running Irie keeps the others' trees
	// in the background, a test one of them runs and all it starts as well,
	// so nothing here could show a difference.
	if pri(t, os.Getpid()) == "4" {
		t.Skip("the test runs in the background, as a session's processes do while another is promoted")
	}
	// At once: the call returns once it is done.
	inBackground := func(s ipc.Session) (in, out []string) {
		for _, p := range tree(t, c.hostPID(s)) {
			if pri(t, p.PID) == "4" {
				in = append(in, p.Name)
			} else {
				out = append(out, p.Name)
			}
		}
		return in, out
	}
	for _, s := range all[:2] {
		if _, out := inBackground(s); len(out) > 0 {
			t.Errorf("%s: %v not in the background", s.Name, out)
		}
	}
	if in, _ := inBackground(gamma); len(in) > 0 {
		t.Errorf("the promoted session's %v are in the background", in)
	}
	// A session that starts meanwhile goes in the background too.
	delta := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,kids", Name: "delta"})
	c.kids(delta)
	testutil.Eventually(t, 3*time.Second, "a new session is not in the background", func() bool {
		_, out := inBackground(delta)
		return len(out) == 0
	})

	if err := c.call(ipc.OpPriority, ipc.PriorityArgs{Session: gamma.ID, Level: 0}, nil); err != nil {
		t.Fatal(err)
	}
	for _, s := range append(all, delta) {
		if in, _ := inBackground(s); len(in) > 0 {
			t.Errorf("%s: %v still in the background", s.Name, in)
		}
	}
	if got := names(c.list()); !slices.Equal(got, []string{"gamma", "alpha", "beta", "delta"}) {
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
