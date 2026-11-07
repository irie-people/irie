package ipc

import (
	"time"

	"github.com/irie-people/irie/internal/perf"
)

// OpPerf reads the core's own counters, the yardstick of the optimisation
// plan (docs/proposals/optimisation.md, iteration 1). Each counts from the
// core's start, so tools/perf takes two reads and their difference.
const OpPerf = "perf"

// PerfResult is what the core counted since it started.
type PerfResult struct {
	Version string    `json:"version"`
	PID     int       `json:"pid"`
	Since   time.Time `json:"since"` // when the core started counting
	Now     time.Time `json:"now"`
	// CPU is the core's own CPU time, and Children that of the processes it
	// ran and waited for, such as tmux, ps and `claude agents`, in
	// microseconds of user and system time.
	CPU      int64 `json:"cpu_us"`
	Children int64 `json:"children_cpu_us"`
	// Ops are the requests it answered on its socket, by op: how many and
	// how long each took to answer. A subscription counts once, when it
	// starts.
	Ops map[string]perf.Times `json:"ops"`
	// Times are how long its own work took: "tick" for each tick,
	// "tmux.snapshot" for each `tmux list-panes`, "changes" for each diff of
	// the rows, "sample" for each memory sample and "outside.run" for each
	// run of `claude agents`.
	Times map[string]perf.Times `json:"times"`
	// Counts are events by name: "frame.<type>" for each frame sent,
	// "status.unchanged" for a status update that changed nothing,
	// "header.changed" for a header that subscribers were told of, and
	// "outside.<why>" for each run of `claude agents` with why it ran.
	Counts map[string]uint64 `json:"counts"`
	// Lock is the core's one lock, c.mu.
	Lock perf.Lock `json:"lock"`
	// Status counts each live session's status line updates, by session.
	Status map[string]uint64 `json:"status"`
	// Sessions is how many sessions the core has, and Subscribers how many
	// subscriptions are open, by view: "app", "watch", "session" (one
	// session's) and "all" (every session's, as the sidebar's).
	Sessions    int            `json:"sessions"`
	Subscribers map[string]int `json:"subscribers"`
}
