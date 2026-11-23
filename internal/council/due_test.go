package council

import (
	"github.com/irie-people/irie/internal/brand"
	"testing"
	"time"
)

// TestDueKeepsItsContract holds Due to what any rhythm must keep, however
// it weighs the rest: never without one (every 0), never sooner than every
// after the last council, and a council at last when one is overdue, the
// user is away and the week has room.
func TestDueKeepsItsContract(t *testing.T) {
	now := time.Date(2026, 11, 22, 3, 0, 0, 0, time.UTC)
	week := 7 * 24 * time.Hour
	away, room := 2*time.Hour, 0.3
	for _, c := range []struct {
		name  string
		last  time.Time
		every time.Duration
		idle  time.Duration
		used  float64
		want  bool
	}{
		{"no rhythm set", time.Time{}, 0, away, room, false},
		{"no rhythm set, a month after the last", now.Add(-30 * 24 * time.Hour), 0, away, room, false},
		{"a day after the last", now.Add(-24 * time.Hour), week, away, room, false},
		{"a minute before the rhythm comes round", now.Add(-week + time.Minute), week, away, room, false},
		{"the first council, the user away, room in the week", time.Time{}, week, away, room, true},
		{"three weeks overdue, the user away, room in the week", now.Add(-3 * week), week, away, room, true},
	} {
		if got := Due(now, c.last, c.every, c.idle, c.used); got != c.want {
			t.Errorf("%s: Due = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestDueWaitsForAQuietTime pins how Due weighs a council that has come
// due: while the user is at the computer, or the week is nearly spent, it
// waits, but not past a day's grace, unless the week is all but spent.
func TestDueWaitsForAQuietTime(t *testing.T) {
	now := time.Date(2026, 11, 22, 15, 0, 0, 0, time.UTC)
	week := 7 * 24 * time.Hour
	due, late := now.Add(-week-time.Hour), now.Add(-week-DueGrace)
	for _, c := range []struct {
		name string
		last time.Time
		idle time.Duration
		used float64
		want bool
	}{
		{"due, the user at the computer", due, 2 * time.Minute, 0.3, false},
		{"due, the user just past " + brand.Gabriel + "'s away", due, 6 * time.Minute, 0.3, false},
		{"due, the user away", due, DueAway, 0.3, true},
		{"due, away, the week nearly spent", due, time.Hour, 0.85, false},
		{"due, away, no word of the week", due, time.Hour, -1, true},
		{"a day late, the user at the computer", late, 0, 0.3, true},
		{"a day late, the week nearly spent", late, 0, 0.9, true},
		{"a day late, the week all but spent", late, time.Hour, 0.97, false},
		{"the first council, the user at the computer", time.Time{}, 0, 0.3, false},
	} {
		if got := Due(now, c.last, week, c.idle, c.used); got != c.want {
			t.Errorf("%s: Due = %v, want %v", c.name, got, c.want)
		}
	}
}
