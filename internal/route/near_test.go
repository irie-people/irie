package route

import (
	"math"
	"testing"
	"time"
)

func TestUnitScalesToLengthOne(t *testing.T) {
	u := Unit([]float64{3, 4})
	if math.Abs(float64(u[0])-0.6) > 1e-6 || math.Abs(float64(u[1])-0.8) > 1e-6 {
		t.Errorf("Unit(3, 4) = %v", u)
	}
	if z := Unit([]float64{0, 0}); z[0] != 0 || z[1] != 0 {
		t.Errorf("a zero vector became %v", z)
	}
}

func TestTheNearestLabelledTaskDecides(t *testing.T) {
	labels := []Labelled{
		{Text: "fix a typo", Class: Small, Vec: Unit([]float64{1, 0, 0})},
		{Text: "design the plugin system", Class: Hard, Vec: Unit([]float64{0, 1, 0})},
		{Text: "not embedded yet", Class: Medium},
		{Text: "another model's", Class: Medium, Vec: Unit([]float64{0, 1})},
	}
	l, sim, ok := Nearest(Unit([]float64{0.2, 0.9, 0.1}), labels)
	if !ok || l.Class != Hard || sim < 0.9 {
		t.Errorf("nearest = %+v at %.2f, %v", l, sim, ok)
	}
	if _, _, ok := Nearest(Unit([]float64{1, 0, 0}), labels[2:]); ok {
		t.Error("a label without a vector of the same length was nearest")
	}
}

func TestATimeIsTheMedianOfTheLatestAnswers(t *testing.T) {
	var s Stats
	if _, n, _ := s.Time("dory", Small); n != 0 {
		t.Errorf("no answers, yet %d", n)
	}
	t0 := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	for i, d := range []time.Duration{time.Minute, 7 * time.Minute, 30 * time.Minute} {
		s.AddTime("dory", Small, t0.Add(time.Duration(i)*time.Hour), d, false)
	}
	if d, n, least := s.Time("dory", Small); d != 7*time.Minute || n != 3 || least {
		t.Errorf("median of three = %v of %d (%v)", d, n, least)
	}
	// A class with no answers takes the profile's answers on every class.
	if d, n, _ := s.Time("dory", Hard); d != 7*time.Minute || n != 3 {
		t.Errorf("another class = %v of %d", d, n)
	}
	if _, n, _ := s.Time("haiku", Small); n != 0 {
		t.Errorf("another profile has %d answers", n)
	}
	// Only the latest answers count: ten quick ones outweigh the old.
	for i := range 10 {
		s.AddTime("dory", Small, t0.Add(time.Duration(10+i)*time.Hour), 20*time.Second, false)
	}
	if d, n, _ := s.Time("dory", Small); d != 20*time.Second || n != timesKept {
		t.Errorf("after quick answers = %v of %d", d, n)
	}
	s.AddTime("haiku", Small, t0, 10*time.Second, false)
	s.AddTime("haiku", Small, t0.Add(time.Minute), 30*time.Second, false)
	if d, _, _ := s.Time("haiku", Small); d != 20*time.Second {
		t.Errorf("median of two = %v", d)
	}
	// A wait a session ran past is only a least, and says so when it is
	// the median.
	s.AddTime("local", Small, t0, 2*time.Minute, true)
	if d, n, least := s.Time("local", Small); d != 2*time.Minute || n != 1 || !least {
		t.Errorf("one wait run past = %v of %d (%v)", d, n, least)
	}
	s.AddTime("local", Small, t0.Add(time.Minute), 10*time.Second, false)
	s.AddTime("local", Small, t0.Add(2*time.Minute), 20*time.Second, false)
	if d, _, least := s.Time("local", Small); d != 20*time.Second || least {
		t.Errorf("with two answers under it = %v (%v)", d, least)
	}
}

func TestTookReadsAsPeopleSayIt(t *testing.T) {
	for d, want := range map[time.Duration]string{
		8 * time.Second:                   "8s",
		90 * time.Second:                  "1m30s",
		2 * time.Minute:                   "2m",
		7*time.Minute + 20*time.Second:    "7m20s",
		10 * time.Minute:                  "10m",
		25*time.Minute + 40*time.Second:   "26m",
		time.Hour:                         "1h",
		90 * time.Minute:                  "1h30m",
		2*time.Hour + 10*time.Millisecond: "2h",
	} {
		if got := Took(d); got != want {
			t.Errorf("Took(%v) = %q, want %q", d, got, want)
		}
	}
}
