package core

import (
	"slices"
	"sync"
)

// turns hands out one turn at a time, as a mutex does, but to the request
// of a promoted session first, and otherwise in the order they came. Spawns
// with their worktrees, and messages typed to agents, take turns this way,
// so a promoted session never waits behind the rest (DESIGN §4.3).
type turns struct {
	mu      sync.Mutex
	busy    bool
	waiting []*waiter // highest level first, then oldest
}

type waiter struct {
	level int
	ready chan struct{}
}

// Lock takes a turn for a request of level 0.
func (q *turns) Lock() { q.LockAt(0) }

// LockAt takes a turn for a request of the given priority level: at once
// when no one has it, else after every waiting request of a higher level
// and those of its own level that came before it.
func (q *turns) LockAt(level int) {
	q.mu.Lock()
	if !q.busy {
		q.busy = true
		q.mu.Unlock()
		return
	}
	w := &waiter{level: level, ready: make(chan struct{})}
	i := len(q.waiting)
	for i > 0 && q.waiting[i-1].level < level {
		i--
	}
	q.waiting = slices.Insert(q.waiting, i, w)
	q.mu.Unlock()
	<-w.ready
}

// Unlock hands the turn to the first waiting request, or frees it.
func (q *turns) Unlock() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.waiting) == 0 {
		q.busy = false
		return
	}
	w := q.waiting[0]
	q.waiting = q.waiting[1:]
	close(w.ready)
}

// levelOf is the highest priority level among the sessions ids names, such
// as the one that delegates a spawn. It takes c.mu.
func (c *Core) levelOf(ids ...string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	level := 0
	for _, id := range ids {
		if x := c.sessions[id]; x != nil {
			level = max(level, x.track.priority)
		}
	}
	return level
}
