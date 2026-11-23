// Package council is Raguel's council (DESIGN §4.11, proposals/council.md):
// a free tally of what the user's agents did and what it cost, by the
// journal, and the plans a council brings the user, with their minutes.
// It is plain code with no state: the core keeps the councils and runs the
// session that holds one.
package council

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/irie-people/irie/internal/arch"
)

// Seats are the arch agents a plan may serve: each part of the tally sits
// with the one whose charge it touches, and Mikael weighs every plan.
var Seats = []string{"raguel", "gabriel", "raphael", "mikael"}

// Efforts are how much work a plan is: an hour of an agent's work, a
// session, or more.
var Efforts = []string{"small", "medium", "large"}

// MaxTitle is the most characters a plan's title holds.
const MaxTitle = 120

// What the user said to a plan, Plan.Said. A plan no one answered before
// the next council sat lapses.
const (
	Waiting = ""
	Yes     = "yes"
	No      = "no"
	Lapsed  = "lapsed"
)

// A Plan is one change a council brings the user: its fields as the
// council puts it in, then what became of it, which the core keeps.
type Plan struct {
	N        int    `json:"n,omitempty"` // its number in its council, from 1, in the order put in
	Seat     string `json:"seat"`        // the arch agent whose charge it serves: one of Seats
	Title    string `json:"title"`       // the change, in a line to take in at a glance
	Evidence string `json:"evidence"`    // the tally's numbers that call for it, and what a closer look found
	Change   string `json:"change"`      // what would be built or changed, and where
	Saves    string `json:"saves"`       // what it saves a week, and how that was counted
	Effort   string `json:"effort"`      // one of Efforts
	Risk     string `json:"risk,omitempty"`
	Watch    string `json:"watch"` // the line of the tally that will show whether it worked
	Where    string `json:"where"` // the folder of the repository or project it changes
	Brief    string `json:"brief"` // the task for the agent who builds it, whole in itself

	Said string    `json:"said,omitempty"`   // Waiting, Yes, No or Lapsed
	Why  string    `json:"why,omitempty"`    // the user's reason for a no
	At   time.Time `json:"said_at,omitzero"` // when it was said, or lapsed
	Jot  string    `json:"jot,omitempty"`    // the session a yes started
	Then string    `json:"then,omitempty"`   // its watch line in its council's tally, as Line found it
}

// A Council is one sitting: the session that ran as Raguel, the tally it
// read, the plans it brought and the minutes it left.
type Council struct {
	Date    string    `json:"date"`    // the day it sat, 2006-01-02
	Started time.Time `json:"started"` // when it was convened
	Days    int       `json:"days"`    // the stretch its tally counts
	Session string    `json:"session"` // the session that runs as Raguel
	Name    string    `json:"name"`    // that session's name
	Folder  string    `json:"folder"`  // where it worked and the minutes go
	Most    int       `json:"most"`    // the most plans it may bring
	Plans   []Plan    `json:"plans"`
	Done    time.Time `json:"done,omitzero"`     // when it said it was done
	Summary string    `json:"summary,omitempty"` // its word on the stretch, at done
	CostUSD *float64  `json:"cost_usd,omitempty"`
	Minutes []string  `json:"minutes,omitempty"` // the files of its minutes
	// Tally is what it read, kept for its minutes and for the watch lines
	// of the plans it brings; only the latest council keeps it.
	Tally *Tally `json:"tally,omitempty"`
}

// Waiting are the plans no one has answered yet.
func (c Council) Waiting() []Plan {
	var out []Plan
	for _, p := range c.Plans {
		if p.Said == Waiting {
			out = append(out, p)
		}
	}
	return out
}

// Check says what is wrong with a plan as the council put it in, naming
// the field, so the council can put it in again; nil when it is whole.
// That its folder exists is the core's to check.
func Check(p Plan) error {
	if !slices.Contains(Seats, p.Seat) {
		return fmt.Errorf("seat %q: must be one of %s", p.Seat, strings.Join(Seats, ", "))
	}
	switch n := utf8.RuneCountInString(strings.TrimSpace(p.Title)); {
	case n == 0:
		return fmt.Errorf("title: is empty")
	case n > MaxTitle:
		return fmt.Errorf("title: %d characters, at most %d", n, MaxTitle)
	case strings.ContainsAny(p.Title, "\n\r"):
		return fmt.Errorf("title: must be one line")
	}
	if !slices.Contains(Efforts, p.Effort) {
		return fmt.Errorf("effort %q: must be one of %s", p.Effort, strings.Join(Efforts, ", "))
	}
	for _, f := range []struct{ name, v string }{
		{"evidence", p.Evidence}, {"change", p.Change}, {"saves", p.Saves},
		{"watch", p.Watch}, {"where", p.Where}, {"brief", p.Brief},
	} {
		if strings.TrimSpace(f.v) == "" {
			return fmt.Errorf("%s: is empty", f.name)
		}
	}
	return nil
}

// Seat is how a seat shows: his nickname, or his name, and his icon, such
// as "Ragu 🤝".
func Seat(name string) string {
	a, ok := arch.Get(name)
	if !ok {
		return name
	}
	s := a.Nickname
	if s == "" {
		s = a.Called
	}
	if a.Icon != "" {
		s += " " + a.Icon
	}
	return s
}

// Due reports whether Raguel holds a council by himself now (DESIGN §4.11).
// The core asks every 10 minutes, while arch.raguel.on and
// arch.raguel.every is above 0 and no council runs; when it says yes, the
// core convenes one at once, as Hold a council now does.
//
//   - now is the time it asks.
//   - last is when the last council was convened, zero before the first.
//   - every is arch.raguel.every, how often he holds one.
//   - idle is how long the computer has had no keyboard or mouse input, 0
//     where Irie cannot tell.
//   - week is how much of the plan's weekly use is spent, from 0 to 1, or
//     -1 when no status line has said.
//
// A council is one Opus session at high effort that reads the tally and
// looks closer where a number calls for it: a few dollars of the week's
// use, and an hour or so of the machine. So it is best held while the user
// is away from the computer, and while the week has room for it, without
// running it late by much, or the council the user set a rhythm for never
// comes.
//
// So once every has passed, he waits for the user to be away DueAway with
// less than DueRoom of the week spent, or with no word of the week at all.
// DueGrace past that, he holds it at any hour, unless the week is all but
// spent (DueFull): the week starts again within days. The first council
// waits for a quiet time, with nothing to be late from.
func Due(now, last time.Time, every, idle time.Duration, week float64) bool {
	if every <= 0 || !last.IsZero() && now.Sub(last) < every {
		return false
	}
	if !last.IsZero() && now.Sub(last) >= every+DueGrace {
		return week < DueFull
	}
	return idle >= DueAway && week < DueRoom
}

// When a council is due by itself, as Due weighs it: how long with no
// input counts as the user away rather than reading, how much of the
// week's use may be spent for a council to sit at a quiet time and when
// late, and how late a council may run while it waits for a quiet time.
const (
	DueAway  = 30 * time.Minute
	DueRoom  = 0.8
	DueFull  = 0.95
	DueGrace = 24 * time.Hour
)
