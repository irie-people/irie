// Command habits reads Irie's journal and ranks what agents did by how
// many calls it took them, the measure of what a script could take over:
// each call is a round trip that reads the agent's whole context again.
// It also counts the habits the repository's tools are for, a grep and
// then a read of what it found, and hand-written waits, so that a tool's
// effect can be seen in the next run (AGENTS.md, "Stack"). tools/habits.sh
// runs it.
//
//	tools/habits.sh [--days N] [--top N] [--json]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/journal"
	"github.com/irie-people/irie/internal/paths"
)

// A call is one tool call from the journal.
type call struct {
	agent  string // session and agent: calls follow one another per agent
	tool   string
	cmd    string // a shell command; empty for other tools
	family string
	ms     float64
}

// Report is what habits prints, or writes as JSON.
type Report struct {
	From     string   `json:"from"`
	To       string   `json:"to"`
	Calls    int      `json:"calls"`
	Sessions int      `json:"sessions"`
	Families []Family `json:"families"`
	Finds    []Find   `json:"finds"`
	Waits    Family   `json:"waits"`
	Repeats  []Family `json:"repeats"`
	Tools    []Family `json:"tools"`
}

// Family is a kind of call: how many, in how many sessions, and the
// minutes they took.
type Family struct {
	Family   string  `json:"family"`
	Calls    int     `json:"calls"`
	Sessions int     `json:"sessions"`
	Minutes  float64 `json:"minutes"`
}

// Find is a kind of search and how often a read of what it found came
// within the next two calls.
type Find struct {
	Family   string `json:"family"`
	Calls    int    `json:"calls"`
	ThenRead int    `json:"then_read"`
}

func main() {
	fs := flag.NewFlagSet("habits", flag.ExitOnError)
	days := fs.Int("days", 14, "read this many days of the journal")
	top := fs.Int("top", 25, "list this many kinds of call")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	fs.Parse(os.Args[1:])
	p, err := paths.New(os.Getenv, os.Getuid())
	if err != nil {
		fail(err)
	}
	since := time.Now().AddDate(0, 0, -*days)
	evs, err := journal.ReadKinds(p.Journal(), since, event.ToolStarted, event.ToolFinished, event.ToolFailed)
	if err != nil {
		fail(err)
	}
	r := build(calls(evs), *top)
	r.From, r.To = since.UTC().Format("2006-01-02"), time.Now().UTC().Format("2006-01-02")
	if *asJSON {
		json.NewEncoder(os.Stdout).Encode(r)
		return
	}
	print(os.Stdout, r)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "habits:", err)
	os.Exit(1)
}

// calls joins each tool call's start to its end, in the journal's order.
func calls(evs []event.Event) []call {
	var out []call
	at := map[string]int{}
	for _, e := range evs {
		id := ""
		if e.Vendor != nil {
			id = e.Vendor.ToolUseID
		}
		switch e.Kind {
		case event.ToolStarted:
			tool, _ := e.Data["tool"].(string)
			c := call{agent: e.Session + "/" + e.Agent, tool: tool, family: tool}
			if tool == "Bash" {
				c.cmd, _ = e.Data["summary"].(string)
				c.family = family(c.cmd)
			}
			if id != "" {
				at[id] = len(out)
			}
			out = append(out, c)
		case event.ToolFinished, event.ToolFailed:
			if i, ok := at[id]; ok {
				out[i].ms, _ = e.Data["ms"].(float64)
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

// build counts the calls.
func build(cs []call, top int) Report {
	r := Report{Calls: len(cs)}
	fam := map[string]*Family{}
	famSessions := map[string]map[string]bool{}
	sessions := map[string]bool{}
	byAgent := map[string][]int{}
	var order []string
	for i, c := range cs {
		s := c.agent[:strings.Index(c.agent, "/")]
		sessions[s] = true
		f := fam[c.family]
		if f == nil {
			f = &Family{Family: c.family}
			fam[c.family] = f
			famSessions[c.family] = map[string]bool{}
		}
		f.Calls++
		f.Minutes += c.ms / 60000
		famSessions[c.family][s] = true
		if _, ok := byAgent[c.agent]; !ok {
			order = append(order, c.agent)
		}
		byAgent[c.agent] = append(byAgent[c.agent], i)
		if c.cmd != "" && sleepRE.MatchString(c.cmd) {
			r.Waits.Calls++
			r.Waits.Minutes += c.ms / 60000
		}
	}
	r.Sessions = len(sessions)
	for k, f := range fam {
		f.Sessions = len(famSessions[k])
		r.Families = append(r.Families, *f)
		if strings.HasPrefix(k, "tools/") || strings.HasPrefix(k, "mac/") {
			r.Tools = append(r.Tools, *f)
		}
	}
	byCalls := func(l []Family) {
		sort.Slice(l, func(i, j int) bool {
			if l[i].Calls != l[j].Calls {
				return l[i].Calls > l[j].Calls
			}
			return l[i].Family < l[j].Family
		})
	}
	byCalls(r.Families)
	byCalls(r.Tools)
	if len(r.Families) > top {
		r.Families = r.Families[:top]
	}

	found := map[string]*Find{}
	rep := map[string]*Family{}
	repSessions := map[string]map[string]bool{}
	for _, a := range order {
		ix := byAgent[a]
		for k, i := range ix {
			c := cs[i]
			if finds[c.family] {
				f := found[c.family]
				if f == nil {
					f = &Find{Family: c.family}
					found[c.family] = f
				}
				f.Calls++
				for _, j := range ix[k+1 : min(k+3, len(ix))] {
					if reads[cs[j].family] {
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
			for j+1 < len(ix) && cs[ix[j+1]].family == cs[ix[k]].family {
				j++
			}
			f := cs[ix[k]].family
			if n := j - k + 1; n >= 3 && !reads[f] && !finds[f] && !work[f] {
				x := rep[f]
				if x == nil {
					x = &Family{Family: f}
					rep[f] = x
					repSessions[f] = map[string]bool{}
				}
				x.Calls += n
				repSessions[f][a[:strings.Index(a, "/")]] = true
			}
			k = j + 1
		}
	}
	for _, f := range found {
		r.Finds = append(r.Finds, *f)
	}
	sort.Slice(r.Finds, func(i, j int) bool { return r.Finds[i].Calls > r.Finds[j].Calls })
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

// print writes the report for a person or an agent to read at a glance.
func print(w io.Writer, r Report) {
	fmt.Fprintf(w, "Irie's journal, %s to %s: %d tool calls by %d sessions.\n", r.From, r.To, r.Calls, r.Sessions)
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
