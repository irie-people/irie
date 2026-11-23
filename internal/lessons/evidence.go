// Package lessons turns what Irie's agents fail at most into lessons that
// the user picks, Blitz's harness learning (retail-cortex/blitz, spec 048)
// made Irie's (DESIGN §4.11, Raphael). The evidence comes first, with no
// model call: the calls that failed, by kind, each with how many sessions
// share it and what the agent did next that worked, from the journal and,
// for the time before it kept errors and for the calls hooks refused,
// which never reach it, from the transcripts of the sessions Irie ran. A
// local model then drafts at most five one-sentence lessons from it, each
// citing the kinds it comes from. The user keeps or turns down each, and
// every session hears the ones kept as it starts. Nothing is kept without
// the user's word.
package lessons

import (
	"sort"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/habits"
)

// Who stopped a call that failed, a Kind's By.
const (
	ByCommand    = "command"    // a shell command ended in an error
	ByTool       = "tool"       // the tool itself, such as an edit of a file not read first
	ByIrie       = "irie"       // one of Irie's own hooks: an arch agent or the trod bot
	ByPermission = "permission" // Claude Code's permission checks, its auto mode's among them
	ByUser       = "user"       // the user turned the call down
	ByHook       = "hook"       // a hook of the user's own, whose refusals are theirs to weigh
	ByNoVerdict  = "unanswered" // the auto mode's check gave no verdict: no one's mistake
)

// By says who stopped a failed call, from what its error said on one line
// (claude.ErrorLine). A hook's refusal is Irie's own when it starts with
// Irie's mark, as Gabriel's and the trod bot's do, or is an arch agent's
// door, whose prompt Claude Code shows in brackets; any other hook is the
// user's own.
func By(tool, err string) string {
	switch {
	case strings.Contains(err, " hook error: "):
		_, msg, _ := strings.Cut(err, " hook error: ")
		if strings.HasPrefix(msg, "["+brand.Name+"]") || strings.HasPrefix(msg, "[You are ") && strings.Contains(msg[:min(len(msg), 80)], "arch agent") {
			return ByIrie
		}
		return ByHook
	case strings.HasPrefix(err, "The server-side auto mode classifier gave no verdict"):
		return ByNoVerdict
	case strings.HasPrefix(err, "Permission for this ") || strings.HasPrefix(err, "Permission to use "):
		return ByPermission
	case strings.HasPrefix(err, "The user doesn't want to "):
		return ByUser
	case tool == "Bash" && strings.HasPrefix(err, "exit "):
		return ByCommand
	}
	return ByTool
}

// Kind is a kind of failure, as habits.FailureKind names it: the family
// and what its error said, with paths, numbers and quoted words left out;
// who stopped it; how many calls, in how many sessions; and the family of
// the agent's next call that did not fail, the commonest, what worked
// next. N numbers it in its evidence, for a lesson to cite. An example of
// its error and of what worked next goes to the model that drafts, and is
// kept nowhere: it can name a file or a word of any repository.
type Kind struct {
	N        int    `json:"n"`
	Kind     string `json:"kind"`
	By       string `json:"by"`
	Calls    int    `json:"calls"`
	Sessions int    `json:"sessions"`
	Then     string `json:"then,omitempty"`
	example  string // an error as one call gave it, the shortest
	thenEx   string // a call of Then's family that worked next, the shortest
}

// Evidence is what agents' calls failed at over a stretch: the kinds
// worth a lesson, most calls first, and the share left out.
type Evidence struct {
	From        time.Time `json:"from"`
	To          time.Time `json:"to"`
	Calls       int       `json:"calls"`
	Sessions    int       `json:"sessions"`
	Transcripts int       `json:"transcripts"` // transcripts read
	Failed      int       `json:"failed"`
	// Kinds are those two sessions or more share, or that the user
	// turned down, which cost them their time once: the patterns.
	Kinds []Kind `json:"kinds"`
	// Yours are the kinds hooks of the user's own refused, most first,
	// and YoursCalls all their refusals: their share, which no lesson is
	// drafted from, since a guard of theirs says what it is for, and
	// whether it should refuse is theirs to weigh.
	Yours      []Kind `json:"yours,omitempty"`
	YoursCalls int    `json:"yours_calls,omitempty"`
	// Unanswered is how often Claude Code's auto mode gave no verdict, a
	// hiccup of its service rather than a mistake.
	Unanswered int `json:"unanswered,omitempty"`
	// failed are the calls that failed, with their errors, for Since.
	failed []habits.Call
}

// MostKinds is the most kinds evidence lists, and a model reads.
const MostKinds = 15

// Build reads what failed in calls, which follow one another per session
// and agent, as habits.Calls gives them.
func Build(calls []habits.Call) Evidence {
	var ev Evidence
	sessions := map[string]bool{}
	type agg struct {
		k        *Kind
		sessions map[string]bool
		then     map[string]int
		thenEx   map[string]string
	}
	kinds := map[string]*agg{}
	byAgent := map[string][]int{}
	var order []string
	for i, c := range calls {
		sessions[c.Session] = true
		a := c.Session + "/" + c.Agent
		if _, ok := byAgent[a]; !ok {
			order = append(order, a)
		}
		byAgent[a] = append(byAgent[a], i)
	}
	ev.Calls, ev.Sessions = len(calls), len(sessions)
	for _, a := range order {
		ix := byAgent[a]
		for k, i := range ix {
			c := calls[i]
			if !c.Failed {
				continue
			}
			ev.Failed++
			if c.Error = untag(c.Error); c.Error == "" {
				continue
			}
			ev.failed = append(ev.failed, c)
			by := By(c.Tool, c.Error)
			if by == ByNoVerdict {
				ev.Unanswered++
				continue
			}
			name := habits.FailureKind(c)
			g := kinds[name]
			if g == nil {
				g = &agg{k: &Kind{Kind: name, By: by}, sessions: map[string]bool{}, then: map[string]int{}, thenEx: map[string]string{}}
				kinds[name] = g
			}
			g.k.Calls++
			g.sessions[c.Session] = true
			if ex := habits.Example(c.Error); g.k.example == "" || len(ex) < len(g.k.example) {
				g.k.example = ex
			}
			for _, j := range ix[k+1:] {
				if n := calls[j]; !n.Failed {
					g.then[n.Family]++
					ex := n.Tool
					if n.Cmd != "" {
						ex = habits.Example(n.Cmd)
					}
					if old, ok := g.thenEx[n.Family]; !ok || len(ex) < len(old) {
						g.thenEx[n.Family] = ex
					}
					break
				}
			}
		}
	}
	for _, g := range kinds {
		g.k.Sessions = len(g.sessions)
		best := 0
		for fam, n := range g.then {
			if n > best || n == best && fam < g.k.Then {
				g.k.Then, best = fam, n
			}
		}
		g.k.thenEx = g.thenEx[g.k.Then]
		switch {
		case g.k.By == ByHook:
			ev.YoursCalls += g.k.Calls
			ev.Yours = append(ev.Yours, *g.k)
		case g.k.Sessions >= 2 || g.k.By == ByUser:
			ev.Kinds = append(ev.Kinds, *g.k)
		}
	}
	for _, l := range []*[]Kind{&ev.Kinds, &ev.Yours} {
		sort.Slice(*l, func(i, j int) bool {
			a, b := (*l)[i], (*l)[j]
			if a.Calls != b.Calls {
				return a.Calls > b.Calls
			}
			if a.Sessions != b.Sessions {
				return a.Sessions > b.Sessions
			}
			return a.Kind < b.Kind
		})
		*l = (*l)[:min(len(*l), MostKinds)]
	}
	for i := range ev.Kinds {
		ev.Kinds[i].N = i + 1
	}
	for i := range ev.Yours {
		ev.Yours[i].example, ev.Yours[i].thenEx = "", ""
	}
	return ev
}

// untag takes off the tags Claude Code puts around a tool's own error in a
// transcript, so that it reads as the journal keeps it.
func untag(err string) string {
	err = strings.TrimSpace(err)
	if s, ok := strings.CutPrefix(err, "<tool_use_error>"); ok {
		err = strings.TrimSpace(strings.TrimSuffix(s, "</tool_use_error>"))
	}
	return err
}

// Since counts the calls of the kinds named that failed at t or after, so
// that a lesson kept at t shows what came of it.
func (ev Evidence) Since(kinds []string, t time.Time) int {
	n := 0
	for _, c := range ev.failed {
		if !c.At.Before(t) && containsKind(kinds, habits.FailureKind(c)) {
			n++
		}
	}
	return n
}

func containsKind(kinds []string, k string) bool {
	for _, x := range kinds {
		if x == k {
			return true
		}
	}
	return false
}
