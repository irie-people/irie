package core

import (
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

// The plan's use is the account's, and every session sends what its own
// last request saw of it. An idle session goes on sending old figures, so
// the plan's figure must not follow whichever session spoke last
// (docs/proposals/optimisation.md §2.4).
func TestThePlanKeepsTheNewestWindow(t *testing.T) {
	c := testCore(t)
	resets := time.Date(2026, 11, 1, 21, 10, 0, 0, time.UTC)
	week := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	w := func(used float64, at time.Time) *ipc.Window { return &ipc.Window{Used: used, ResetsAt: at} }
	steps := []struct {
		what     string
		five, wk *ipc.Window
		changed  bool
		wantFive float64
		wantWeek float64
	}{
		{"the first figures", w(37, resets), w(66, week), true, 37, 66},
		{"an idle session's older, lower figures", w(35, resets), w(65, week), false, 37, 66},
		{"the same again", w(37, resets), w(66, week), false, 37, 66},
		{"a session that saw more of the same window", w(38, resets), w(66, week), true, 38, 66},
		{"a session that saw only the week", nil, w(67, week), true, 38, 67},
		{"a session from before the five hours reset", w(90, resets.Add(-5*time.Hour)), nil, false, 38, 67},
		{"the next five hours", w(2, resets.Add(5*time.Hour)), nil, true, 2, 67},
		{"the old window again", w(38, resets), nil, false, 2, 67},
	}
	for _, s := range steps {
		c.mu.Lock()
		changed := c.takePlan(s.five, s.wk)
		p := c.plan
		c.mu.Unlock()
		if changed != s.changed || p.FiveHour == nil || p.FiveHour.Used != s.wantFive || p.SevenDay == nil || p.SevenDay.Used != s.wantWeek {
			t.Errorf("%s: changed %v, plan 5h %v 7d %v; want %v, %v and %v", s.what, changed, p.FiveHour, p.SevenDay, s.changed, s.wantFive, s.wantWeek)
		}
	}
	// A figure that does not say when its window resets cannot be placed,
	// so it is taken as it comes.
	c.mu.Lock()
	changed := c.takePlan(&ipc.Window{Used: 1}, nil)
	c.mu.Unlock()
	if !changed || c.plan.FiveHour.Used != 1 {
		t.Errorf("a figure with no reset: changed %v, plan %v", changed, c.plan.FiveHour)
	}
}
