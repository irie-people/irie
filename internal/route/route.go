// Package route picks the profile a task runs on (DESIGN §6.4): the one
// with the lowest cost per successful task, among those accurate enough for
// the task's class. A cheap model that gets a task wrong costs a retry and
// the user's attention, so cost is normalised by accuracy, and a profile
// below the accuracy floor is never chosen: hard work, such as
// orchestrating a feature, stays on the strongest model.
//
// Accuracy starts from guesses in the config and moves with the user's own
// outcomes: a session rated good or bad, and a task the user ran again on a
// more accurate profile, which says the first one fell short.
package route

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"
)

// A task's class: how hard it is.
const (
	Small  = "small"  // mechanical: a typo, a rename, a docstring, a summary
	Medium = "medium" // ordinary coding: a function, a flag, a bug in one place
	Hard   = "hard"   // design, debugging, review, orchestrating work over many files
)

// Classes are the classes, easiest first.
var Classes = []string{Small, Medium, Hard}

// Words are what make a task small, medium or hard.
type Words struct {
	Small  []string `toml:"small"`
	Medium []string `toml:"medium"`
	Hard   []string `toml:"hard"`
}

// DefaultWords are the starting word lists. A word matches at the start of
// a word, so "debug" matches "debugging" too.
var DefaultWords = Words{
	Small: []string{
		"typo", "spelling", "rename", "comment", "docstring", "doc comment", "readme", "changelog", "commit message",
		"format", "lint", "whitespace", "indent", "sort imports", "unused import", "bump", "version number",
		"translate", "summarize", "summarise", "summary", "describe", "explain", "list the", "title", "one line",
		"one paragraph", "small", "tiny", "quick", "simple", "trivial",
	},
	Medium: []string{
		"implement", "add a", "add an", "feature", "fix", "bug", "test", "endpoint", "flag", "option", "function",
		"method", "component", "handler", "parse", "validate", "update", "error", "fail", "broken",
	},
	Hard: []string{
		"design", "architect", "refactor", "rewrite", "migrate", "migration", "debug", "investigate", "root cause",
		"why", "race", "deadlock", "concurren", "crash", "flaky", "intermittent", "leak", "performance", "optimi",
		"security", "vulnerab", "audit", "review", "plan", "orchestrat", "subagent", "in parallel", "end to end",
		"end-to-end", "milestone", "across the", "every file", "codebase", "whole", "entire", "from scratch", "system",
	},
}

// Weights of the signals, and where the classes start.
const (
	smallWeight  = -2
	mediumWeight = 1
	hardWeight   = 3
	hardFrom     = 3  // a score of this or more is hard
	smallBelow   = -1 // a score of this or less is small
)

// Classify says how hard a task is, and why, from its words and its size:
// a long task, or one with a list of steps, does more than a short one.
func Classify(task string, w Words) (string, string) {
	text := strings.ToLower(task)
	if len(text) > 4000 {
		text = text[:4000]
	}
	score := 0
	var why []string
	for _, group := range []struct {
		words  []string
		weight int
	}{{w.Hard, hardWeight}, {w.Medium, mediumWeight}, {w.Small, smallWeight}} {
		for _, word := range group.words {
			if word != "" && hasWord(text, strings.ToLower(word)) {
				score += group.weight
				why = append(why, word)
			}
		}
	}
	if n := len(task); n > 150 {
		extra := (n - 150) / 300
		if extra > 0 {
			score += min(extra+1, 4)
			why = append(why, fmt.Sprintf("%d characters", n))
		}
	}
	if steps := listItems(task); steps > 2 {
		score += steps - 2
		why = append(why, fmt.Sprintf("%d steps", steps))
	}
	class := Medium
	switch {
	case score >= hardFrom:
		class = Hard
	case score <= smallBelow:
		class = Small
	}
	if len(why) == 0 {
		why = []string{"no telling words"}
	}
	return class, strings.Join(why, ", ")
}

// hasWord reports whether text has word at the start of one of its words.
func hasWord(text, word string) bool {
	for i := 0; ; {
		j := strings.Index(text[i:], word)
		if j < 0 {
			return false
		}
		at := i + j
		if at == 0 || !isWordRune(rune(text[at-1])) {
			return true
		}
		i = at + 1
	}
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

var listItem = regexp.MustCompile(`(?m)^\s*(?:[-*•]|\d+[.)])\s+\S`)

// listItems counts the lines of a task that are list items.
func listItems(task string) int { return len(listItem.FindAllStringIndex(task, -1)) }

// Fingerprint names a task's text, ignoring case and spacing, so the same
// task spawned again is known to be the same.
func Fingerprint(task string) string {
	norm := strings.Join(strings.Fields(strings.ToLower(task)), " ")
	if len(norm) > 2000 {
		norm = norm[:2000]
	}
	sum := sha256.Sum256([]byte(norm))
	return hex.EncodeToString(sum[:8])
}

// Prior is where a profile starts: how often it gets a task of each class
// right the first time, and what a task costs on it, relative to one on
// the strongest profile.
type Prior struct {
	Accuracy map[string]float64
	Cost     float64
}

// Counts are a profile's outcomes on one class.
type Counts struct {
	Good, Bad int
	cost      float64 // dollars, over the costed outcomes
	costed    int
}

// Stats are the user's outcomes, by profile and class.
type Stats struct {
	by map[[2]string]*Counts
}

// Add notes one outcome. cost is what the session cost in dollars, at list
// price, when its status line said.
func (s *Stats) Add(profile, class string, good bool, cost *float64) {
	if s.by == nil {
		s.by = map[[2]string]*Counts{}
	}
	k := [2]string{profile, class}
	c := s.by[k]
	if c == nil {
		c = &Counts{}
		s.by[k] = c
	}
	if good {
		c.Good++
	} else {
		c.Bad++
	}
	if cost != nil && *cost > 0 {
		c.cost += *cost
		c.costed++
	}
}

// Counts returns a profile's outcomes on a class.
func (s Stats) Counts(profile, class string) Counts {
	if c := s.by[[2]string{profile, class}]; c != nil {
		return *c
	}
	return Counts{}
}

// MeanCost is what a task of a class cost on a profile, in dollars, when at
// least minCosted outcomes say.
func (s Stats) MeanCost(profile, class string) (float64, bool) {
	c := s.Counts(profile, class)
	if c.costed < minCosted {
		return 0, false
	}
	return c.cost / float64(c.costed), true
}

// minCosted is how many costed outcomes make a mean cost.
const minCosted = 3

// Accuracy is a profile's accuracy on a class: the prior, counted as weight
// outcomes, moved by the user's own (the mean of a beta distribution).
func Accuracy(prior float64, weight int, c Counts) float64 {
	w := float64(max(weight, 0))
	n := float64(c.Good + c.Bad)
	if w+n == 0 {
		return prior
	}
	return (prior*w + float64(c.Good)) / (w + n)
}

// Candidate is one profile weighed for a task.
type Candidate struct {
	Profile    string  `json:"profile"`
	Accuracy   float64 `json:"accuracy"`    // on the task's class, 0 to 1
	Outcomes   int     `json:"outcomes"`    // how many of the user's outcomes moved it
	Cost       float64 `json:"cost"`        // per attempt, relative to the strongest profile
	PerSuccess float64 `json:"per_success"` // Cost over Accuracy
	Usable     bool    `json:"usable"`      // it can run the task now
	Note       string  `json:"note,omitempty"`
}

// Choice is the profile a task runs on, and why.
type Choice struct {
	Class      string      `json:"class"`
	Why        string      `json:"why"` // what made the class
	Profile    string      `json:"profile"`
	Met        bool        `json:"met"` // the profile reaches the accuracy floor
	Reason     string      `json:"reason"`
	Candidates []Candidate `json:"candidates"` // cheapest per success first
}

// Choose picks, of the usable candidates whose accuracy reaches floor, the
// one with the lowest cost per success, the more accurate one on a tie.
// When none reaches the floor it takes the most accurate usable one.
func Choose(class, why string, cands []Candidate, floor float64) Choice {
	cands = slices.Clone(cands)
	for i := range cands {
		c := &cands[i]
		c.PerSuccess = math.Inf(1)
		if c.Accuracy > 0 {
			c.PerSuccess = c.Cost / c.Accuracy
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		if a.PerSuccess != b.PerSuccess {
			return a.PerSuccess < b.PerSuccess
		}
		return a.Accuracy > b.Accuracy
	})
	ch := Choice{Class: class, Why: why, Candidates: cands}
	for _, c := range cands {
		if c.Usable && c.Accuracy >= floor {
			ch.Profile, ch.Met = c.Profile, true
			ch.Reason = fmt.Sprintf("%s: %s is the cheapest per success of those at %s or more (%s, %s a success)", class, c.Profile, Percent(floor), Percent(c.Accuracy), Relative(c.PerSuccess))
			return ch
		}
	}
	best := -1
	for i, c := range cands {
		if c.Usable && (best < 0 || c.Accuracy > cands[best].Accuracy) {
			best = i
		}
	}
	if best >= 0 {
		ch.Profile = cands[best].Profile
		ch.Reason = fmt.Sprintf("%s: none reaches %s, so the most accurate, %s (%s)", class, Percent(floor), ch.Profile, Percent(cands[best].Accuracy))
	} else {
		ch.Reason = class + ": no profile can run it now"
	}
	return ch
}

// Percent renders an accuracy: 85%.
func Percent(p float64) string { return fmt.Sprintf("%.0f%%", p*100) }

// Relative renders a cost relative to the strongest profile: 0.25×, or
// "free" for nothing.
func Relative(c float64) string {
	switch {
	case math.IsInf(c, 1):
		return "never"
	case c == 0:
		return "free"
	case c < 0.01:
		return fmt.Sprintf("%.3f×", c)
	}
	return fmt.Sprintf("%.2f×", c)
}

// Cheaper returns the profiles whose prior cost is below that of profile,
// in the order given. A profile without a prior counts as costing what the
// dearest does.
func Cheaper(profile string, profiles []string, priors map[string]Prior) []string {
	cost := 0.0
	if top, ok := priors[profile]; ok {
		cost = top.Cost
	} else {
		for _, pr := range priors {
			cost = max(cost, pr.Cost)
		}
	}
	var out []string
	for _, p := range profiles {
		if pr, ok := priors[p]; ok && p != profile && pr.Cost < cost {
			out = append(out, p)
		}
	}
	return out
}
