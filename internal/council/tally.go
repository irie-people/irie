package council

import (
	"cmp"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/habits"
)

// Kinds are the journal's event kinds a tally counts, besides every
// spawn.requested, which names each session's repository and profile.
var Kinds = []string{
	event.ToolStarted, event.ToolFinished, event.ToolFailed, event.InputNeeded, event.PromptSubmitted,
	event.TurnFailed, event.CacheRebuilt, event.DogNudged, event.RouteFailover, event.RouteFallback,
	event.SpawnRequested,
}

// Tally is what Raguel counts of a stretch of the journal, beside the
// stretch before it, for a council to read (DESIGN §4.11). Nothing in it
// costs a token.
type Tally struct {
	Days    int       `json:"days"`              // how long each stretch is
	Now     Stretch   `json:"now"`               // the stretch that ends now
	Before  Stretch   `json:"before"`            // the stretch before it, of as many days
	Earlier []Earlier `json:"earlier,omitempty"` // the plans of earlier councils, newest first
}

// Stretch is the tally of one stretch of days, by seat.
type Stretch struct {
	From     time.Time `json:"from"`
	To       time.Time `json:"to"`
	Sessions int       `json:"sessions"` // the sessions with an event in it
	Calls    int       `json:"calls"`    // their tool calls

	// Ragu's seat: cost. A session's cost is Claude Code's, from its
	// start, at list price; it counts in the stretch of its last event.
	USD       float64 `json:"usd"`
	ByRepo    []Row   `json:"by_repo"`    // sessions and what they cost, by repository
	ByProfile []Row   `json:"by_profile"` // the same, by profile
	Costliest []Row   `json:"costliest"`  // the sessions that cost the most, with their repository and profile
	Rebuilds  Row     `json:"rebuilds"`   // prompt-cache rebuilds and their price (DESIGN §6.3)
	RebuiltBy []Row   `json:"rebuilt_by"` // the same, by what woke the session
	Tools     []Row   `json:"tools"`      // calls by tool
	Commands  []Row   `json:"commands"`   // calls by kind, as tools/habits.sh names them, with two examples each
	Finds     []Row   `json:"finds"`      // searches (Of) and how many a read of what they found followed within two calls (N)
	Repeats   []Row   `json:"repeats"`    // the same command three or more times in a row
	Delegated Row     `json:"delegated"`  // tasks agents handed on (irie ctl delegate) and what they cost

	// Gabriel's seat: time.
	Waits   Row   `json:"waits"`   // calls with a hand-written wait, and the minutes they took
	Longest []Row `json:"longest"` // the kinds of call that took the most time
	Asks    []Row `json:"asks"`    // the user's asks by reason (Of), those answered within the stretch (N), how long they waited in all and the longest wait

	// Raffi's seat: what breaks.
	ToolFailures []Row `json:"tool_failures"` // failed calls by kind (N), of its calls (Of)
	TurnFailures []Row `json:"turn_failures"` // failed turns, by error
	Tests        Row   `json:"tests"`         // runs of tests or checks that failed (N), of the runs (Of)
	TestsBy      []Row `json:"tests_by"`      // the same, by kind
	Nudges       []Row `json:"nudges"`        // turns the jot dog sent back, by what was left
	Failovers    int   `json:"failovers"`     // tasks carried on by another session at a usage limit
	Fallbacks    int   `json:"fallbacks"`     // sessions switched to their fallback model
}

// Row is one line of a tally: what it counts and its numbers.
type Row struct {
	Name     string   `json:"name"`
	N        int      `json:"n"`                  // how many: calls, sessions, runs or asks, as its list says
	Of       int      `json:"of,omitempty"`       // out of how many, for a share
	Sessions int      `json:"sessions,omitempty"` // in how many sessions
	Minutes  float64  `json:"minutes,omitempty"`  // the time they took, in all
	Most     float64  `json:"most,omitempty"`     // the longest one, in minutes
	USD      float64  `json:"usd,omitempty"`      // what they cost, at list price
	Note     string   `json:"note,omitempty"`     // more about it, such as a session's repository and profile
	Examples []string `json:"examples,omitempty"` // two commands of the kind, the shortest (habits.Example)
}

// Earlier is a plan of an earlier council, as the next tally shows it.
type Earlier struct {
	Council string `json:"council"` // its council's date
	N       int    `json:"n"`
	Seat    string `json:"seat"`
	Title   string `json:"title"`
	Said    string `json:"said"` // Waiting, Yes, No or Lapsed
	Why     string `json:"why,omitempty"`
	Jot     string `json:"jot,omitempty"`   // the jot a yes started
	State   string `json:"state,omitempty"` // where that jot stands now
	Watch   string `json:"watch"`
	Then    string `json:"then,omitempty"` // its watch line in its council's tally
	Now     string `json:"now,omitempty"`  // the same line in this one
}

// Input is what a tally counts.
type Input struct {
	Now    time.Time
	Days   int                // how long a stretch is; 7 when 0
	Events []event.Event      // the journal's Kinds from the stretch before's start on, in its order
	Spawns []event.Event      // every spawn.requested the journal holds
	Cost   map[string]float64 // what each session cost, by its status line, at list price
	Names  map[string]string  // each session's name
	Home   string             // the user's home folder, which paths show as ~
}

// How many rows a list keeps.
const (
	topCommands = 25
	topTools    = 15
	topList     = 10
)

// Count tallies the stretch of in.Days that ends at in.Now, and the
// stretch of as many days before it.
func Count(in Input) Tally {
	days := in.Days
	if days <= 0 {
		days = 7
	}
	span := time.Duration(days) * 24 * time.Hour
	to := in.Now
	from := to.Add(-span)
	start := from.Add(-span)
	meta := map[string]spawned{}
	for _, evs := range [][]event.Event{in.Spawns, in.Events} {
		for _, e := range evs {
			if e.Kind == event.SpawnRequested {
				meta[e.Session] = spawned{repo: e.Str("repo"), profile: e.Str("profile"), parent: e.Str("parent")}
			}
		}
	}
	var now, before []event.Event
	last := map[string]time.Time{}
	for _, e := range in.Events {
		at := e.Time()
		switch {
		case at.Before(start) || at.After(to):
			continue
		case at.Before(from):
			before = append(before, e)
		default:
			now = append(now, e)
		}
		if e.Session != "" && at.After(last[e.Session]) {
			last[e.Session] = at
		}
	}
	t := Tally{
		Days:   days,
		Now:    in.stretch(now, from, to, meta, last),
		Before: in.stretch(before, start, from, meta, last),
	}
	a, b := &t.Now, &t.Before
	for _, l := range []struct {
		now, before *[]Row
		top         int
	}{
		{&a.ByRepo, &b.ByRepo, topList}, {&a.ByProfile, &b.ByProfile, topList}, {&a.RebuiltBy, &b.RebuiltBy, topList},
		{&a.Tools, &b.Tools, topTools}, {&a.Commands, &b.Commands, topCommands}, {&a.Finds, &b.Finds, topList},
		{&a.Repeats, &b.Repeats, topList}, {&a.Longest, &b.Longest, topList}, {&a.Asks, &b.Asks, topList},
		{&a.ToolFailures, &b.ToolFailures, topList}, {&a.TurnFailures, &b.TurnFailures, topList},
		{&a.TestsBy, &b.TestsBy, topList}, {&a.Nudges, &b.Nudges, topList},
	} {
		*l.now, *l.before = cut(*l.now, *l.before, l.top)
	}
	a.Costliest, b.Costliest = a.Costliest[:min(len(a.Costliest), topList)], b.Costliest[:min(len(b.Costliest), topList)]
	return t
}

// cut keeps the first top rows of now, and of before those rows' own,
// to show beside them, and its first top: a row only before had is news
// too. The rows before keep no examples.
func cut(now, before []Row, top int) ([]Row, []Row) {
	now = now[:min(len(now), top)]
	keep := map[string]bool{}
	for _, r := range now {
		keep[r.Name] = true
	}
	var out []Row
	for i, r := range before {
		if i < top || keep[r.Name] {
			r.Examples = nil
			out = append(out, r)
		}
	}
	return now, out
}

// spawned is what a session's spawn.requested says of it.
type spawned struct{ repo, profile, parent string }

// stretch counts the events of one stretch, from to to: the sessions
// whose last event falls in it, for their cost, are those in last.
func (in Input) stretch(evs []event.Event, from, to time.Time, meta map[string]spawned, last map[string]time.Time) Stretch {
	s := Stretch{From: from, To: to}
	sessions := map[string]bool{}
	for _, e := range evs {
		if e.Session != "" {
			sessions[e.Session] = true
		}
	}
	s.Sessions = len(sessions)

	// Cost. A session's last event falls in the stretch from its start on,
	// and before its end, or up to it for the stretch that ends now.
	repos, profiles := rows{}, rows{}
	for id, at := range last {
		if at.Before(from) || !at.Before(to) && !to.Equal(in.Now) {
			continue
		}
		m := meta[id]
		usd := in.Cost[id]
		repo := in.tilde(m.repo)
		if repo == "" {
			repo = "(no repository)"
		}
		profile := m.profile
		if profile == "" {
			profile = "(not known)"
		}
		for _, r := range []*Row{repos.of(repo), profiles.of("+" + strings.TrimPrefix(profile, "+"))} {
			r.N++
			r.USD += usd
		}
		s.USD += usd
		if usd > 0 {
			name := in.Names[id]
			if name == "" {
				name = id
			}
			s.Costliest = append(s.Costliest, Row{Name: name, USD: usd, Note: repo + " · +" + strings.TrimPrefix(profile, "+")})
		}
	}
	s.ByRepo, s.ByProfile = repos.byUSD(), profiles.byUSD()
	slices.SortFunc(s.Costliest, func(a, b Row) int { return cmp.Or(cmp.Compare(b.USD, a.USD), strings.Compare(a.Name, b.Name)) })

	// Calls.
	cs := habits.Calls(evs)
	s.Calls = len(cs)
	r := habits.Build(cs, 0)
	for _, k := range r.Families {
		s.Commands = append(s.Commands, kindRow(k))
	}
	s.Longest = slices.Clone(s.Commands)
	slices.SortFunc(s.Longest, func(a, b Row) int { return cmp.Or(cmp.Compare(b.Minutes, a.Minutes), strings.Compare(a.Name, b.Name)) })
	for _, f := range r.Finds {
		s.Finds = append(s.Finds, Row{Name: f.Family, N: f.ThenRead, Of: f.Calls})
	}
	for _, k := range r.Repeats {
		s.Repeats = append(s.Repeats, Row{Name: k.Family, N: k.Calls, Sessions: k.Sessions, Examples: k.Examples})
	}
	s.Waits = kindRow(r.Waits)
	s.Waits.Name = "Hand-written waits"
	tools, failed, tests := rows{}, rows{}, rows{}
	toolSessions := map[string]map[string]bool{}
	for _, c := range cs {
		t := tools.of(c.Tool)
		t.N++
		t.Minutes += c.MS / 60000
		if toolSessions[c.Tool] == nil {
			toolSessions[c.Tool] = map[string]bool{}
		}
		toolSessions[c.Tool][c.Session] = true
		f := failed.of(c.Family)
		f.Of++
		if c.Failed {
			f.N++
		}
		if c.Tests != "" {
			x := tests.of(c.Family)
			x.Of++
			s.Tests.Of++
			if c.Tests == "failed" {
				x.N++
				s.Tests.N++
			}
		}
	}
	for name, r := range tools {
		r.Sessions = len(toolSessions[name])
	}
	s.Tools = tools.byN()
	for name, r := range failed {
		if r.N == 0 {
			delete(failed, name)
		}
	}
	s.ToolFailures, s.TestsBy = failed.byN(), tests.byN()
	s.Tests.Name = "Test runs"

	// The rest, from the events.
	rebuilt, turns, nudges, asks := rows{}, rows{}, rows{}, rows{}
	type ask struct {
		at     time.Time
		reason string
	}
	pending := map[string]ask{}
	for _, e := range evs {
		switch e.Kind {
		case event.CacheRebuilt:
			usd, _ := e.Data["usd"].(float64)
			s.Rebuilds.N++
			s.Rebuilds.USD += usd
			cause := e.Str("cause")
			if cause == "" {
				cause = "not known"
			}
			x := rebuilt.of(cause)
			x.N++
			x.USD += usd
		case event.TurnFailed:
			why := oneLine(e.Str("error"), 60)
			if why == "" {
				why = "not known"
			}
			turns.of(why).N++
		case event.DogNudged:
			left := false
			if n, _ := e.Int("dirty"); n > 0 {
				nudges.of("files not committed").N++
				left = true
			}
			if n, _ := e.Int("ahead"); n > 0 {
				nudges.of("commits not landed").N++
				left = true
			}
			if e.Str("unproven") != "" {
				nudges.of("said the tests pass when they failed").N++
				left = true
			}
			if !left {
				nudges.of("other").N++
			}
		case event.RouteFailover:
			s.Failovers++
		case event.RouteFallback:
			s.Fallbacks++
		case event.SpawnRequested:
			if e.Str("parent") != "" {
				s.Delegated.N++
				s.Delegated.USD += in.Cost[e.Session]
			}
		case event.InputNeeded:
			switch reason := e.Str("reason"); reason {
			case event.ReasonPermission, event.ReasonQuestion, event.ReasonPlan:
				if _, ok := pending[e.Session]; !ok {
					pending[e.Session] = ask{e.Time(), reason}
					asks.of(reason).Of++
				}
			}
		case event.PromptSubmitted, event.ToolFinished:
			if a, ok := pending[e.Session]; ok {
				delete(pending, e.Session)
				waited := e.Time().Sub(a.at).Minutes()
				x := asks.of(a.reason)
				x.N++
				x.Minutes += waited
				x.Most = max(x.Most, waited)
			}
		}
	}
	s.Delegated.Name = "Tasks handed on"
	s.Rebuilds.Name = "Prompt-cache rebuilds"
	s.RebuiltBy, s.TurnFailures, s.Nudges, s.Asks = rebuilt.byN(), turns.byN(), nudges.byN(), asks.byN()
	return s
}

// kindRow is a kind of call as a tally's row.
func kindRow(k habits.Kind) Row {
	return Row{Name: k.Family, N: k.Calls, Sessions: k.Sessions, Minutes: k.Minutes, Examples: k.Examples}
}

// rows are a list's rows by name, as they are counted.
type rows map[string]*Row

func (r rows) of(name string) *Row {
	x := r[name]
	if x == nil {
		x = &Row{Name: name}
		r[name] = x
	}
	return x
}

// byN is the list, the most first.
func (r rows) byN() []Row {
	return r.sorted(func(a, b Row) int { return cmp.Or(cmp.Compare(b.N, a.N), cmp.Compare(b.Of, a.Of)) })
}

// byUSD is the list, the costliest first.
func (r rows) byUSD() []Row {
	return r.sorted(func(a, b Row) int { return cmp.Or(cmp.Compare(b.USD, a.USD), cmp.Compare(b.N, a.N)) })
}

func (r rows) sorted(by func(a, b Row) int) []Row {
	out := []Row{}
	for _, x := range r {
		out = append(out, *x)
	}
	slices.SortFunc(out, func(a, b Row) int { return cmp.Or(by(a, b), strings.Compare(a.Name, b.Name)) })
	return out
}

// tilde is a path with the user's home folder as ~.
func (in Input) tilde(p string) string {
	if in.Home != "" && (p == in.Home || strings.HasPrefix(p, in.Home+string(filepath.Separator))) {
		return "~" + p[len(in.Home):]
	}
	return p
}

// oneLine is s's first line, cut to most characters.
func oneLine(s string, most int) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	if r := []rune(s); len(r) > most {
		return string(r[:most-1]) + "…"
	}
	return s
}

// Earliers are the plans of the councils cs, the newest of them first,
// as the tally t shows them: what the user said, where each yes's jot
// stands by state, and each watch line then and in t. It takes the plans
// of the last most councils.
func Earliers(t Tally, cs []Council, state func(jot string) string, most int) []Earlier {
	var out []Earlier
	for i := len(cs) - 1; i >= 0 && i >= len(cs)-most; i-- {
		for _, p := range cs[i].Plans {
			e := Earlier{Council: cs[i].Date, N: p.N, Seat: p.Seat, Title: p.Title, Said: p.Said, Why: p.Why, Jot: p.Jot, Watch: p.Watch, Then: p.Then}
			if p.Jot != "" && state != nil {
				e.State = state(p.Jot)
			}
			e.Now = Line(t, p.Watch)
			out = append(out, e)
		}
	}
	return out
}
