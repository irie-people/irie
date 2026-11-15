package route

import (
	"cmp"
	"math"
	"slices"
	"strings"
	"time"
)

// Labelled is a task someone gave a class, with its embedding: the
// examples a new task is sorted by (DESIGN §6.4).
type Labelled struct {
	Text  string
	Class string
	Vec   []float32 // unit length; nil until the task is embedded
}

// Unit scales v to unit length, so that a dot product of two is their
// cosine. A zero vector stays zero.
func Unit(v []float64) []float32 {
	n := 0.0
	for _, x := range v {
		n += x * x
	}
	n = math.Sqrt(n)
	out := make([]float32, len(v))
	if n == 0 {
		return out
	}
	for i, x := range v {
		out[i] = float32(x / n)
	}
	return out
}

// Nearest returns the labelled task most like v, by cosine, and the
// cosine. v and every label's Vec are unit length; a label without a
// vector, or with one of another length, is passed over.
//
// The one nearest decides. On 153 of a user's tasks with their classes
// given by hand, the class of the one nearest by embeddinggemma-2 270M was
// right on 71%, where 3 to 9 nearest voting were right on 64–67%, Haiku
// on 68% and route.words on 50%.
func Nearest(v []float32, labels []Labelled) (Labelled, float64, bool) {
	best, sim := -1, math.Inf(-1)
	for i, l := range labels {
		if len(l.Vec) == 0 || len(l.Vec) != len(v) {
			continue
		}
		s := 0.0
		for j, x := range l.Vec {
			s += float64(x) * float64(v[j])
		}
		if s > sim {
			best, sim = i, s
		}
	}
	if best < 0 {
		return Labelled{}, 0, false
	}
	return labels[best], sim, true
}

// timesKept is how many of a profile's latest answers on a class make its
// time: a model's speed changes when it moves, as to another machine.
const timesKept = 9

type timed struct {
	at      time.Time
	d       time.Duration
	atLeast bool // it had not answered by then
}

// AddTime notes how long a session took to answer a task of class on
// profile: from its spawn, at, to the end of its first turn; or, atLeast,
// the wait it ran past, which it took at least.
func (s *Stats) AddTime(profile, class string, at time.Time, d time.Duration, atLeast bool) {
	if s.times == nil {
		s.times = map[[2]string][]timed{}
	}
	k := [2]string{profile, class}
	s.times[k] = append(s.times[k], timed{at, d, atLeast})
}

// Time is how long profile takes to answer a task of class: the median of
// its latest answers on that class, else of its latest on every class,
// since a model's speed is mostly its own; how many answers that is; and
// whether the median is only a least, a wait the session ran past.
func (s Stats) Time(profile, class string) (time.Duration, int, bool) {
	ts := s.times[[2]string{profile, class}]
	if len(ts) == 0 {
		for k, more := range s.times {
			if k[0] == profile {
				ts = append(ts, more...)
			}
		}
	}
	if len(ts) == 0 {
		return 0, 0, false
	}
	ts = slices.Clone(ts)
	slices.SortFunc(ts, func(a, b timed) int { return b.at.Compare(a.at) })
	ts = ts[:min(len(ts), timesKept)]
	slices.SortStableFunc(ts, func(a, b timed) int { return cmp.Compare(a.d, b.d) })
	n := len(ts)
	if n%2 == 1 {
		return ts[n/2].d, n, ts[n/2].atLeast
	}
	return (ts[n/2-1].d + ts[n/2].d) / 2, n, ts[n/2].atLeast
}

// Took renders how long something took as people say it: 8s and 1m30s
// under ten minutes, 25m and 1h30m past that.
func Took(d time.Duration) string {
	if d < 10*time.Minute {
		d = d.Round(time.Second)
	} else {
		d = d.Round(time.Minute)
	}
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}
