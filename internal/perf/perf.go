// Package perf is the yardstick's counting (docs/proposals/optimisation.md,
// iteration 1): durations kept in buckets so that any window's percentiles
// follow from two reads, a mutex that times its waits and holds, and named
// counters. The core counts with it and `irie ctl perf` reads the counts;
// tools/perf takes the difference of two reads.
package perf

import (
	"math/bits"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// Buckets is how many buckets a Hist has: 8 to each doubling, each about 9%
// wide, from 1 ns up to about 2.4 hours. Longer durations go in the last.
const Buckets = 8 * 41

// Bucket is the bucket of a duration in nanoseconds. Below 16 ns each
// nanosecond has its own; above, each doubling is cut in 8.
func Bucket(ns int64) int {
	if ns < 16 {
		return int(max(ns, 0))
	}
	e := bits.Len64(uint64(ns)) - 1 // 2^e <= ns < 2^(e+1)
	return min((e-2)*8+int(uint64(ns)>>(e-3)&7), Buckets-1)
}

// Low is the shortest duration, in nanoseconds, of bucket i.
func Low(i int) int64 {
	if i < 16 {
		return int64(i)
	}
	return int64(8+i%8) << (i/8 - 1)
}

// High is the bound above bucket i: the shortest duration of the next.
func High(i int) int64 { return Low(i + 1) }

// Hist counts durations. It is safe for concurrent use and takes no lock.
type Hist struct {
	n, sum, max atomic.Int64
	b           [Buckets]atomic.Uint64
}

// Add counts one duration.
func (h *Hist) Add(d time.Duration) {
	ns := max(int64(d), 0)
	h.n.Add(1)
	h.sum.Add(ns)
	for {
		m := h.max.Load()
		if ns <= m || h.max.CompareAndSwap(m, ns) {
			break
		}
	}
	h.b[Bucket(ns)].Add(1)
}

// Since counts the time since t.
func (h *Hist) Since(t time.Time) { h.Add(time.Since(t)) }

// Times reads the counts.
func (h *Hist) Times() Times {
	t := Times{N: uint64(h.n.Load()), Sum: h.sum.Load(), Max: h.max.Load()}
	for i := range h.b {
		if n := h.b[i].Load(); n > 0 {
			t.Buckets = append(t.Buckets, [2]uint64{uint64(i), n})
		}
	}
	t.fill()
	return t
}

// Times is a Hist as it was read: how many durations, their sum and the
// longest in nanoseconds, the median and 99th percentile, and every
// non-empty bucket as [bucket, count], in order.
type Times struct {
	N       uint64      `json:"n"`
	Sum     int64       `json:"sum_ns"`
	Max     int64       `json:"max_ns"`
	P50     int64       `json:"p50_ns"`
	P99     int64       `json:"p99_ns"`
	Buckets [][2]uint64 `json:"buckets,omitempty"`
}

// fill works out the percentiles from the buckets.
func (t *Times) fill() {
	t.P50, t.P99 = t.Quantile(0.5), t.Quantile(0.99)
}

// Quantile is the duration, in nanoseconds, below which a share q of the
// durations fall: the upper bound of the bucket it falls in, or the
// longest duration when that is less. It is 0 for no durations.
func (t Times) Quantile(q float64) int64 {
	var total uint64
	for _, b := range t.Buckets {
		total += b[1]
	}
	if total == 0 {
		return 0
	}
	rank := uint64(q*float64(total-1)) + 1
	var seen uint64
	for _, b := range t.Buckets {
		seen += b[1]
		if seen >= rank {
			v := High(int(b[0]))
			if t.Max > 0 && t.Max < v {
				v = t.Max
			}
			return v
		}
	}
	return t.Max
}

// Mean is the mean duration in nanoseconds, 0 for none.
func (t Times) Mean() int64 {
	if t.N == 0 {
		return 0
	}
	return t.Sum / int64(t.N)
}

// Minus is what was counted between old and t, two reads of one Hist, old
// first. The longest is t's, since a Hist keeps no history of it.
func (t Times) Minus(old Times) Times {
	out := Times{N: t.N - min(old.N, t.N), Sum: t.Sum - old.Sum, Max: t.Max}
	was := map[uint64]uint64{}
	for _, b := range old.Buckets {
		was[b[0]] = b[1]
	}
	for _, b := range t.Buckets {
		if n := b[1] - min(was[b[0]], b[1]); n > 0 {
			out.Buckets = append(out.Buckets, [2]uint64{b[0], n})
		}
	}
	if out.N == 0 {
		out.Max = 0
	}
	out.fill()
	return out
}

// Mutex is a sync.Mutex that counts how often it is taken, how often a
// caller has to wait for it, how long those waits take and how long it is
// held. Taking it free costs a TryLock and a clock read.
type Mutex struct {
	mu   sync.Mutex
	at   time.Time // when its holder took it; only the holder touches it
	n    atomic.Uint64
	held atomic.Uint64 // takes that had to wait
	wait Hist
	hold Hist
}

func (m *Mutex) Lock() {
	if m.mu.TryLock() {
		m.at = time.Now()
		m.n.Add(1)
		return
	}
	t := time.Now()
	m.mu.Lock()
	m.at = time.Now()
	m.n.Add(1)
	m.held.Add(1)
	m.wait.Add(m.at.Sub(t))
}

// TryLock takes the mutex if it is free, as sync.Mutex's does, and says
// whether it did.
func (m *Mutex) TryLock() bool {
	if !m.mu.TryLock() {
		return false
	}
	m.at = time.Now()
	m.n.Add(1)
	return true
}

func (m *Mutex) Unlock() {
	m.hold.Since(m.at)
	m.mu.Unlock()
}

// Lock is a Mutex as it was read.
type Lock struct {
	Locks     uint64 `json:"locks"`
	Contended uint64 `json:"contended"` // takes that had to wait
	Wait      Times  `json:"wait"`      // those waits
	Hold      Times  `json:"hold"`      // how long each take held it
}

// Stats reads the mutex's counts.
func (m *Mutex) Stats() Lock {
	return Lock{Locks: m.n.Load(), Contended: m.held.Load(), Wait: m.wait.Times(), Hold: m.hold.Times()}
}

// Minus is what was counted between old and l, old first.
func (l Lock) Minus(old Lock) Lock {
	return Lock{Locks: l.Locks - min(old.Locks, l.Locks), Contended: l.Contended - min(old.Contended, l.Contended),
		Wait: l.Wait.Minus(old.Wait), Hold: l.Hold.Minus(old.Hold)}
}

// Set is named Hists and counters, made on first use. It is safe for
// concurrent use.
type Set struct {
	mu     sync.RWMutex
	hists  map[string]*Hist
	counts map[string]*atomic.Uint64
}

// Hist is the Hist named name.
func (s *Set) Hist(name string) *Hist {
	s.mu.RLock()
	h := s.hists[name]
	s.mu.RUnlock()
	if h != nil {
		return h
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if h = s.hists[name]; h == nil {
		if s.hists == nil {
			s.hists = map[string]*Hist{}
		}
		h = &Hist{}
		s.hists[name] = h
	}
	return h
}

// Since counts the time since t in the Hist named name.
func (s *Set) Since(name string, t time.Time) { s.Hist(name).Since(t) }

// Add adds n to the counter named name.
func (s *Set) Add(name string, n uint64) {
	s.mu.RLock()
	c := s.counts[name]
	s.mu.RUnlock()
	if c == nil {
		s.mu.Lock()
		if c = s.counts[name]; c == nil {
			if s.counts == nil {
				s.counts = map[string]*atomic.Uint64{}
			}
			c = &atomic.Uint64{}
			s.counts[name] = c
		}
		s.mu.Unlock()
	}
	c.Add(n)
}

// Times reads every Hist, by name.
func (s *Set) Times() map[string]Times {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]Times, len(s.hists))
	for name, h := range s.hists {
		out[name] = h.Times()
	}
	return out
}

// Counts reads every counter, by name.
func (s *Set) Counts() map[string]uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]uint64, len(s.counts))
	for name, c := range s.counts {
		out[name] = c.Load()
	}
	return out
}

// Names is the sorted keys of a read.
func Names[V any](m map[string]V) []string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	slices.Sort(names)
	return names
}
