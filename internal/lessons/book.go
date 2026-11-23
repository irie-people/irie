package lessons

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
)

// MostKept is the most lessons every session hears: each costs every
// session its words, at each start.
const MostKept = 10

// What the user said to a drafted lesson, its Said.
const (
	Waiting = ""
	Kept    = "kept"
	No      = "no"
)

// A Lesson is one sentence for agents, drafted from the kinds of failure
// it cites, which are its evidence, and what the user said to it.
type Lesson struct {
	N       int       `json:"n"`
	Text    string    `json:"text"`
	Kinds   []Kind    `json:"kinds"`
	Days    int       `json:"days"` // the stretch its evidence covers
	Drafted time.Time `json:"drafted"`
	By      string    `json:"by"`             // the local model that drafted it
	Said    string    `json:"said,omitempty"` // Kept or No; Waiting for the user's word
	At      time.Time `json:"at,omitzero"`    // when the user said it
	// Since is what came of a kept lesson, by the latest evidence: how
	// many calls of its kinds failed in the SinceDays after it was kept,
	// beside its own evidence's Kinds' calls over Days. Nil until a draft
	// after it was kept reads the evidence again.
	Since     *int    `json:"since,omitempty"`
	SinceDays float64 `json:"since_days,omitempty"`
}

// Calls are the calls of the kinds the lesson cites.
func (l Lesson) Calls() int {
	n := 0
	for _, k := range l.Kinds {
		n += k.Calls
	}
	return n
}

// KindNames are the names of the kinds the lesson cites.
func (l Lesson) KindNames() []string {
	var out []string
	for _, k := range l.Kinds {
		out = append(out, k.Kind)
	}
	return out
}

// Book is the user's lessons: the latest draft's, each waiting for their
// word until they say it or a new draft comes, and those kept, which
// every session hears as it starts, oldest first.
type Book struct {
	Drafted time.Time `json:"drafted,omitzero"`
	By      string    `json:"by,omitempty"`    // the local model that drafted the latest
	Asked   string    `json:"asked,omitempty"` // the session that asked for it, which may relay the user's word
	// Evidence is what the latest draft read.
	Evidence *Evidence `json:"evidence,omitempty"`
	Problem  string    `json:"problem,omitempty"` // why the latest draft brought none
	Drafts   []Lesson  `json:"drafts,omitempty"`
	Kept     []Lesson  `json:"kept,omitempty"`
}

// Waiting are the drafts the user has said nothing to.
func (b Book) Waiting() []Lesson {
	var out []Lesson
	for _, l := range b.Drafts {
		if l.Said == Waiting {
			out = append(out, l)
		}
	}
	return out
}

// Texts are the kept lessons' words.
func (b Book) Texts() []string {
	var out []string
	for _, l := range b.Kept {
		out = append(out, l.Text)
	}
	return out
}

// ErrNotFound says a lesson named is not there.
var ErrNotFound = errors.New("no such lesson")

// ErrStale says the lessons named were of a draft a newer one replaced.
var ErrStale = errors.New("a newer draft came since: answer its lessons instead")

// Pending checks that the drafts ns number, of the draft made at of (the
// latest when zero), wait for the user's word.
func (b Book) Pending(ns []int, of time.Time) error {
	_, err := b.drafts(ns, of)
	return err
}

// drafts are the drafts ns number, each once, waiting for the user's word.
func (b Book) drafts(ns []int, of time.Time) ([]int, error) {
	if len(ns) == 0 {
		return nil, errors.New("name the lessons by their numbers")
	}
	if !of.IsZero() && !of.Equal(b.Drafted) {
		return nil, ErrStale
	}
	var out []int
	for _, n := range ns {
		if n < 1 || n > len(b.Drafts) {
			return nil, fmt.Errorf("%w: the latest draft has no lesson %d; it has %d", ErrNotFound, n, len(b.Drafts))
		}
		switch b.Drafts[n-1].Said {
		case Kept:
			return nil, fmt.Errorf("lesson %d is kept already", n)
		case No:
			return nil, fmt.Errorf("lesson %d was turned down", n)
		}
		if !containsInt(out, n) {
			out = append(out, n)
		}
	}
	return out, nil
}

// Keep keeps the drafts ns number, of the draft made at of (the latest
// when zero), on the user's word: every session hears them from its next
// start. A kept lesson gets a number of its own, which never changes. It
// refuses past MostKept.
func (b *Book) Keep(ns []int, of, now time.Time) error {
	ix, err := b.drafts(ns, of)
	if err != nil {
		return err
	}
	if len(b.Kept)+len(ix) > MostKept {
		return fmt.Errorf("every session hears %d lessons already, and %d is the most, since each costs every session its words: drop one first", len(b.Kept), MostKept)
	}
	next := 1
	for _, l := range b.Kept {
		next = max(next, l.N+1)
	}
	for _, n := range ix {
		b.Drafts[n-1].Said, b.Drafts[n-1].At = Kept, now
		l := b.Drafts[n-1]
		l.N, next = next, next+1
		b.Kept = append(b.Kept, l)
	}
	return nil
}

// No turns down the drafts ns number, of the draft made at of (the latest
// when zero), on the user's word.
func (b *Book) No(ns []int, of, now time.Time) error {
	ix, err := b.drafts(ns, of)
	if err != nil {
		return err
	}
	for _, n := range ix {
		b.Drafts[n-1].Said, b.Drafts[n-1].At = No, now
	}
	return nil
}

// Drop stops every session hearing the kept lessons ns number.
func (b *Book) Drop(ns []int) error {
	if len(ns) == 0 {
		return errors.New("name the kept lessons by their numbers")
	}
	for _, n := range ns {
		if !slices.ContainsFunc(b.Kept, func(l Lesson) bool { return l.N == n }) {
			return fmt.Errorf("%w: no kept lesson is numbered %d", ErrNotFound, n)
		}
	}
	b.Kept = slices.DeleteFunc(b.Kept, func(l Lesson) bool { return containsInt(ns, l.N) })
	return nil
}

// Take takes a new draft in: the drafts before it that wait lapse, and
// what came of each kept lesson is counted again from ev.
func (b *Book) Take(ls []Lesson, ev Evidence, by, asked string, now time.Time) {
	b.Drafted, b.By, b.Asked, b.Problem, b.Drafts = now, by, asked, "", ls
	b.Watch(ev)
}

// Watch counts again from ev what came of each kept lesson: its kinds'
// calls that failed since it was kept, within ev's stretch.
func (b *Book) Watch(ev Evidence) {
	e := ev
	e.failed = nil
	b.Evidence = &e
	for i, l := range b.Kept {
		from := l.At
		if from.Before(ev.From) {
			from = ev.From
		}
		if !ev.To.After(from) {
			continue
		}
		n := ev.Since(l.KindNames(), from)
		b.Kept[i].Since, b.Kept[i].SinceDays = &n, ev.To.Sub(from).Hours()/24
	}
}

// Note is what every session hears of the kept lessons as it starts: a
// line each, or nothing for none.
func Note(kept []Lesson) string {
	if len(kept) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("[" + brand.Name + "] Lessons from what agents got wrong most often, which your user picked; follow them:")
	for _, l := range kept {
		b.WriteString("\n- " + l.Text)
	}
	return b.String()
}

func containsInt(l []int, n int) bool {
	for _, x := range l {
		if x == n {
			return true
		}
	}
	return false
}
