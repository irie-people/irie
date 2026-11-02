package perf

import (
	"math/rand/v2"
	"sync"
	"testing"
	"time"
)

func TestBucketsCoverEveryDuration(t *testing.T) {
	for i := 0; i < Buckets-1; i++ {
		if Low(i) >= High(i) {
			t.Fatalf("bucket %d: low %d is not below high %d", i, Low(i), High(i))
		}
		for _, ns := range []int64{Low(i), High(i) - 1} {
			if b := Bucket(ns); b != i {
				t.Fatalf("Bucket(%d) = %d, want %d", ns, b, i)
			}
		}
	}
	if b := Bucket(-5); b != 0 {
		t.Errorf("Bucket(-5) = %d, want 0", b)
	}
	if b := Bucket(1 << 62); b != Buckets-1 {
		t.Errorf("a huge duration went in bucket %d, want the last", b)
	}
	// A bucket is at most an eighth of its low bound wide.
	for i := 16; i < Buckets-1; i++ {
		if w := High(i) - Low(i); w*8 > Low(i) {
			t.Fatalf("bucket %d is %d ns wide at %d ns", i, w, Low(i))
		}
	}
}

func TestQuantiles(t *testing.T) {
	var h Hist
	for i := 1; i <= 1000; i++ {
		h.Add(time.Duration(i) * time.Microsecond)
	}
	got := h.Times()
	if got.N != 1000 || got.Max != int64(time.Millisecond) {
		t.Fatalf("n %d, max %d", got.N, got.Max)
	}
	for _, c := range []struct {
		q    float64
		want time.Duration
	}{{0.5, 500 * time.Microsecond}, {0.99, 990 * time.Microsecond}} {
		v := time.Duration(got.Quantile(c.q))
		if v < c.want || float64(v) > float64(c.want)*1.13 {
			t.Errorf("q%.2f = %v, want %v to 13%% over", c.q, v, c.want)
		}
	}
	if got.P50 != got.Quantile(0.5) || got.P99 != got.Quantile(0.99) {
		t.Errorf("p50 %d, p99 %d not filled in", got.P50, got.P99)
	}
	if (Times{}).Quantile(0.5) != 0 || (Times{}).Mean() != 0 {
		t.Error("no durations should read as 0")
	}
}

func TestMinusIsTheWindow(t *testing.T) {
	var h Hist
	for range 100 {
		h.Add(time.Millisecond)
	}
	before := h.Times()
	for range 10 {
		h.Add(time.Second)
	}
	w := h.Times().Minus(before)
	if w.N != 10 || w.Sum != int64(10*time.Second) {
		t.Fatalf("window n %d sum %d", w.N, w.Sum)
	}
	if p := time.Duration(w.P50); p < time.Second || p > 1100*time.Millisecond {
		t.Errorf("window p50 %v, want about 1s: the earlier milliseconds must not count", p)
	}
	if e := h.Times().Minus(h.Times()); e.N != 0 || e.Max != 0 || len(e.Buckets) != 0 {
		t.Errorf("an empty window: %+v", e)
	}
}

func TestMutexCountsWaits(t *testing.T) {
	var m Mutex
	m.Lock()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		m.Lock()
		m.Unlock()
	}()
	time.Sleep(20 * time.Millisecond)
	m.Unlock()
	wg.Wait()
	s := m.Stats()
	if s.Locks != 2 || s.Contended != 1 {
		t.Fatalf("locks %d, contended %d, want 2 and 1", s.Locks, s.Contended)
	}
	if w := time.Duration(s.Wait.Max); w < 15*time.Millisecond {
		t.Errorf("the wait was counted as %v, want about 20ms", w)
	}
	if s.Hold.N != 2 || time.Duration(s.Hold.Max) < 15*time.Millisecond {
		t.Errorf("holds: %+v", s.Hold)
	}
}

func TestSetIsSafeForConcurrentUse(t *testing.T) {
	var s Set
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 1000 {
				s.Add("frames", 1)
				s.Hist([]string{"a", "b"}[(g+i)%2]).Add(time.Duration(rand.IntN(1000)))
			}
		}()
	}
	wg.Wait()
	if n := s.Counts()["frames"]; n != 8000 {
		t.Errorf("frames %d, want 8000", n)
	}
	ts := s.Times()
	if ts["a"].N+ts["b"].N != 8000 {
		t.Errorf("times %d + %d, want 8000", ts["a"].N, ts["b"].N)
	}
	if got := Names(ts); len(got) != 2 || got[0] != "a" {
		t.Errorf("names %v", got)
	}
}
