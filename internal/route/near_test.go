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
	if _, n := s.Time("dory", Small); n != 0 {
		t.Errorf("no answers, yet %d", n)
	}
	t0 := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	for i, d := range []time.Duration{time.Minute, 7 * time.Minute, 30 * time.Minute} {
		s.AddTime("dory", Small, t0.Add(time.Duration(i)*time.Hour), d)
	}
	if d, n := s.Time("dory", Small); d != 7*time.Minute || n != 3 {
		t.Errorf("median of three = %v of %d", d, n)
	}
	// A class with no answers takes the profile's answers on every class.
	if d, n := s.Time("dory", Hard); d != 7*time.Minute || n != 3 {
		t.Errorf("another class = %v of %d", d, n)
	}
	if _, n := s.Time("haiku", Small); n != 0 {
		t.Errorf("another profile has %d answers", n)
	}
	// Only the latest answers count: ten quick ones outweigh the old.
	for i := range 10 {
		s.AddTime("dory", Small, t0.Add(time.Duration(10+i)*time.Hour), 20*time.Second)
	}
	if d, n := s.Time("dory", Small); d != 20*time.Second || n != timesKept {
		t.Errorf("after quick answers = %v of %d", d, n)
	}
	s.AddTime("haiku", Small, t0, 10*time.Second)
	s.AddTime("haiku", Small, t0.Add(time.Minute), 30*time.Second)
	if d, _ := s.Time("haiku", Small); d != 20*time.Second {
		t.Errorf("median of two = %v", d)
	}
}
