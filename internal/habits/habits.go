// Package habits reads the tool calls in Irie's journal and ranks what
// agents did by how many calls it took them, the measure of what a script
// could take over: each call is a round trip that reads the agent's whole
// context again. It also counts the habits the repository's tools are for,
// a grep and then a read of what it found, and hand-written waits, so that
// a tool's effect can be seen in the next count (AGENTS.md, "Stack").
// tools/habits.sh prints its report, and Raguel's tally counts with it
// (DESIGN §4.11).
package habits

import (
	"cmp"
	"fmt"
	"io"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/event"
)

// A Call is one tool call from the journal.
type Call struct {
	Session string    // the Irie session
	Agent   string    // main or sub:<id>: calls follow one another per agent
	Tool    string    // such as Bash or Read
	Cmd     string    // a shell command; empty for other tools
	Family  string    // what kind of call it is: Family's name for a shell command, else the tool
	At      time.Time // when it started
	MS      float64   // how long it took; 0 when the journal has no end for it
	Failed  bool      // it ended in tool.failed
	Tests   string    // for a run of tests or checks, passed, failed or ran (DESIGN §4.8)
}

// Report is what habits prints, or writes as JSON.
type Report struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Calls    int    `json:"calls"`
	Sessions int    `json:"sessions"`
	Families []Kind `json:"families"`
	Finds    []Find `json:"finds"`
	Waits    Kind   `json:"waits"`
	Repeats  []Kind `json:"repeats"`
	Tools    []Kind `json:"tools"`
}

// Kind is a kind of call, a family: how many, in how many sessions, the
// minutes they took in all, and two of its commands for an example.
type Kind struct {
	Family   string   `json:"family"`
	Calls    int      `json:"calls"`
	Sessions int      `json:"sessions"`
	Minutes  float64  `json:"minutes"`
	Examples []string `json:"examples,omitempty"` // the two shortest distinct commands, as Example shows them
}

// Find is a kind of search and how often a read of what it found came
// within the next two calls.
type Find struct {
	Family   string `json:"family"`
	Calls    int    `json:"calls"`
	ThenRead int    `json:"then_read"`
}

// Calls joins each tool call's start to its end, by the call's
// vendor.tool_use_id, in the journal's order.
func Calls(evs []event.Event) []Call {
	var out []Call
	at := map[string]int{}
	for _, e := range evs {
		id := ""
		if e.Vendor != nil {
			id = e.Vendor.ToolUseID
		}
		switch e.Kind {
		case event.ToolStarted:
			tool := e.Str("tool")
			c := Call{Session: e.Session, Agent: e.Agent, Tool: tool, Family: tool, At: e.Time()}
			if tool == "Bash" {
				c.Cmd = e.Str("summary")
				c.Family = Family(c.Cmd)
			}
			if id != "" {
				at[id] = len(out)
			}
			out = append(out, c)
		case event.ToolFinished, event.ToolFailed:
			if i, ok := at[id]; ok {
				out[i].MS, _ = e.Data["ms"].(float64)
				out[i].Failed = e.Kind == event.ToolFailed
				out[i].Tests = e.Str("tests")
			}
		}
	}
	return out
}

// finds are the searches, and reads what reads a file, by family.
var (
	finds = map[string]bool{"grep": true, "egrep": true, "rg": true, "git grep": true, "Grep": true, "Glob": true, "tools/code.sh grep": true, "tools/code.sh refs": true}
	reads = map[string]bool{"sed -n": true, "sed": true, "cat": true, "head": true, "tail": true, "awk": true, "nl": true, "Read": true}
)

// sleepRE finds a fixed wait written into a command.
var sleepRE = regexp.MustCompile(`\bsleep\s+[0-9]`)

// Build counts the calls: the top kinds by calls, or every kind when top
// is 0 or less.
func Build(cs []Call, top int) Report {
	r := Report{Calls: len(cs)}
	fam := map[string]*Kind{}
	famSessions := map[string]map[string]bool{}
	ex := map[string]*examples{}
	var waits examples
	sessions := map[string]bool{}
	byAgent := map[string][]int{}
	var order []string
	for i, c := range cs {
		sessions[c.Session] = true
		f := fam[c.Family]
		if f == nil {
			f = &Kind{Family: c.Family}
			fam[c.Family] = f
			famSessions[c.Family] = map[string]bool{}
			ex[c.Family] = &examples{}
		}
		f.Calls++
		f.Minutes += c.MS / 60000
		famSessions[c.Family][c.Session] = true
		ex[c.Family].add(c.Cmd)
		a := c.Session + "/" + c.Agent
		if _, ok := byAgent[a]; !ok {
			order = append(order, a)
		}
		byAgent[a] = append(byAgent[a], i)
		if c.Cmd != "" && sleepRE.MatchString(c.Cmd) {
			r.Waits.Calls++
			r.Waits.Minutes += c.MS / 60000
			waits.add(c.Cmd)
		}
	}
	r.Waits.Examples = waits.list()
	r.Sessions = len(sessions)
	for k, f := range fam {
		f.Sessions = len(famSessions[k])
		f.Examples = ex[k].list()
		r.Families = append(r.Families, *f)
		if strings.HasPrefix(k, "tools/") || strings.HasPrefix(k, "mac/") {
			r.Tools = append(r.Tools, *f)
		}
	}
	byCalls := func(l []Kind) {
		sort.Slice(l, func(i, j int) bool {
			if l[i].Calls != l[j].Calls {
				return l[i].Calls > l[j].Calls
			}
			return l[i].Family < l[j].Family
		})
	}
	byCalls(r.Families)
	byCalls(r.Tools)
	if top > 0 && len(r.Families) > top {
		r.Families = r.Families[:top]
	}

	found := map[string]*Find{}
	rep := map[string]*Kind{}
	repSessions := map[string]map[string]bool{}
	for _, a := range order {
		ix := byAgent[a]
		for k, i := range ix {
			c := cs[i]
			if finds[c.Family] {
				f := found[c.Family]
				if f == nil {
					f = &Find{Family: c.Family}
					found[c.Family] = f
				}
				f.Calls++
				for _, j := range ix[k+1 : min(k+3, len(ix))] {
					if reads[cs[j].Family] {
						f.ThenRead++
						break
					}
				}
			}
		}
		// Runs of three or more of the same command, other than reading
		// and editing, which are work: polls, and steps done one by one.
		for k := 0; k < len(ix); {
			j := k
			for j+1 < len(ix) && cs[ix[j+1]].Family == cs[ix[k]].Family {
				j++
			}
			f := cs[ix[k]].Family
			if n := j - k + 1; n >= 3 && !reads[f] && !finds[f] && !work[f] {
				x := rep[f]
				if x == nil {
					x = &Kind{Family: f, Examples: ex[f].list()}
					rep[f] = x
					repSessions[f] = map[string]bool{}
				}
				x.Calls += n
				repSessions[f][cs[ix[k]].Session] = true
			}
			k = j + 1
		}
	}
	for _, f := range found {
		r.Finds = append(r.Finds, *f)
	}
	sort.Slice(r.Finds, func(i, j int) bool {
		if r.Finds[i].Calls != r.Finds[j].Calls {
			return r.Finds[i].Calls > r.Finds[j].Calls
		}
		return r.Finds[i].Family < r.Finds[j].Family
	})
	for k, f := range rep {
		f.Sessions = len(repSessions[k])
		r.Repeats = append(r.Repeats, *f)
	}
	byCalls(r.Repeats)
	if len(r.Repeats) > 10 {
		r.Repeats = r.Repeats[:10]
	}
	return r
}

// work is the calls that change files or look things up, which a run of
// them does not make a habit to script.
var work = map[string]bool{"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true, "WebFetch": true, "WebSearch": true, "python3": true, "perl": true, "sed -i": true}

// MaxExample is the most characters an example command shows.
const MaxExample = 160

// secretRE finds what looks like a secret written into a command: a
// variable named for a token, a key or a password set to a value, a bearer
// token, and the shapes of well-known tokens. Example masks it, so that a
// report never carries one.
var secretRE = []struct {
	re   *regexp.Regexp
	with string
}{
	{regexp.MustCompile(`(?i)\b([A-Z0-9_]*(?:TOKEN|SECRET|PASSWORD|PASSWD|API_?KEY|AUTH)[A-Z0-9_]*\s*=\s*)("[^"]*"|'[^']*'|\S+)`), "${1}…"},
	{regexp.MustCompile(`(?i)\b(bearer\s+)[A-Za-z0-9._~+/=-]{8,}`), "${1}…"},
	{regexp.MustCompile(`\b(?:gh[oprsu]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|sk-[A-Za-z0-9_-]{16,}|xox[abposr]-[A-Za-z0-9-]{10,}|AKIA[0-9A-Z]{16})\b`), "…"},
}

// Example is a command as a report shows it: on one line, with its
// newlines shown as ⏎, what looks like a secret masked, and cut to
// MaxExample characters.
func Example(cmd string) string {
	s := strings.TrimSpace(strings.ReplaceAll(cmd, "\r", ""))
	for _, x := range secretRE {
		s = x.re.ReplaceAllString(s, x.with)
	}
	s = strings.ReplaceAll(s, "\n", "⏎")
	if r := []rune(s); len(r) > MaxExample {
		s = string(r[:MaxExample-1]) + "…"
	}
	return s
}

// examples keeps a kind's two shortest distinct commands, by the length of
// the command, as Example shows them.
type examples struct {
	l []example
}

type example struct {
	n     int // the command's length
	shown string
}

func (e *examples) add(cmd string) {
	if strings.TrimSpace(cmd) == "" || len(e.l) == 2 && len(cmd) > e.l[1].n {
		return
	}
	x := example{len(cmd), Example(cmd)}
	for _, y := range e.l {
		if y.shown == x.shown {
			return
		}
	}
	e.l = append(e.l, x)
	slices.SortFunc(e.l, func(a, b example) int { return cmp.Or(cmp.Compare(a.n, b.n), strings.Compare(a.shown, b.shown)) })
	e.l = e.l[:min(len(e.l), 2)]
}

func (e *examples) list() []string {
	if e == nil {
		return nil
	}
	var out []string
	for _, x := range e.l {
		out = append(out, x.shown)
	}
	return out
}

// Print writes the report for a person or an agent to read at a glance.
func Print(w io.Writer, r Report) {
	fmt.Fprintf(w, "%s's journal, %s to %s: %d tool calls by %d sessions.\n", brand.Name, r.From, r.To, r.Calls, r.Sessions)
	fmt.Fprintln(w, "Each call is a round trip that reads the agent's whole context again, so calls are the cost.")
	fmt.Fprintln(w, "\nCalls by kind (calls, sessions, minutes):")
	for _, f := range r.Families {
		fmt.Fprintf(w, "%7d %5d %7.0f  %s\n", f.Calls, f.Sessions, f.Minutes, f.Family)
	}
	fmt.Fprintln(w, "\nA search, then a read of what it found within two calls (tools/code.sh grep --show and def do both):")
	for _, f := range r.Finds {
		pct := 0
		if f.Calls > 0 {
			pct = 100 * f.ThenRead / f.Calls
		}
		fmt.Fprintf(w, "%7d of %6d (%2d%%)  %s\n", f.ThenRead, f.Calls, pct, f.Family)
	}
	fmt.Fprintf(w, "\nHand-written waits (sleep in the command): %d calls, %.0f minutes.\n", r.Waits.Calls, r.Waits.Minutes)
	if len(r.Repeats) > 0 {
		fmt.Fprintln(w, "\nThe same command three or more times in a row (calls, sessions):")
		for _, f := range r.Repeats {
			fmt.Fprintf(w, "%7d %5d  %s\n", f.Calls, f.Sessions, f.Family)
		}
	}
	if len(r.Tools) > 0 {
		fmt.Fprintln(w, "\nThe repository's tools (calls, sessions):")
		for _, f := range r.Tools {
			fmt.Fprintf(w, "%7d %5d  %s\n", f.Calls, f.Sessions, f.Family)
		}
	}
}
