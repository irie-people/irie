package core

// The core's own counters, the optimisation plan's yardstick
// (docs/proposals/optimisation.md, iteration 1). The core counts each
// request and how long it took, the waits on c.mu and how long it was held,
// the frames it sends by type, its ticks, diffs and memory samples, and its
// runs of `claude agents`. `irie ctl perf` reads them (op perf); each
// counts from the core's start, so tools/perf diffs two reads.

import (
	"os"
	"syscall"
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

// perfResult reads the counters.
func (c *Core) perfResult() ipc.PerfResult {
	r := ipc.PerfResult{
		Version: c.version, PID: os.Getpid(), Since: c.started, Now: time.Now(),
		Ops: c.perfOps.Times(), Times: c.perf.Times(), Counts: c.perf.Counts(), Lock: c.mu.Stats(),
		CPU: rusage(syscall.RUSAGE_SELF), Children: rusage(syscall.RUSAGE_CHILDREN),
	}
	c.mu.Lock()
	r.Sessions = len(c.sessions)
	r.Status = make(map[string]uint64, len(c.sessions))
	for id, x := range c.sessions {
		r.Status[id] = x.statusN
	}
	r.Subscribers = map[string]int{}
	for s := range c.subs {
		r.Subscribers[s.view]++
	}
	c.mu.Unlock()
	r.Subscribers[ipc.ViewWatch] = c.fdog.count()
	return r
}

// rusage is the user and system time of who (RUSAGE_SELF or
// RUSAGE_CHILDREN), in microseconds.
func rusage(who int) int64 {
	var u syscall.Rusage
	if syscall.Getrusage(who, &u) != nil {
		return 0
	}
	return u.Utime.Nano()/1000 + u.Stime.Nano()/1000
}

// cpuOf is the user and system time a finished process took.
func cpuOf(ps *os.ProcessState) time.Duration {
	if ps == nil {
		return 0
	}
	return ps.UserTime() + ps.SystemTime()
}
