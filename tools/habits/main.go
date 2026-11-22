// Command habits reads Irie's journal and ranks what agents did by how
// many calls it took them, the measure of what a script could take over:
// each call is a round trip that reads the agent's whole context again.
// It also counts the habits the repository's tools are for, a grep and
// then a read of what it found, and hand-written waits, so that a tool's
// effect can be seen in the next run (AGENTS.md, "Stack"), and ranks what
// failed most, with what the agent did next that worked. The counting is
// internal/habits, which Raguel's tally shares; tools/habits.sh runs it.
//
//	tools/habits.sh [--days N] [--top N] [--json]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/habits"
	"github.com/irie-people/irie/internal/journal"
	"github.com/irie-people/irie/internal/paths"
)

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
	r := habits.Build(habits.Calls(evs), *top)
	r.From, r.To = since.UTC().Format("2006-01-02"), time.Now().UTC().Format("2006-01-02")
	if *asJSON {
		json.NewEncoder(os.Stdout).Encode(r)
		return
	}
	habits.Print(os.Stdout, r)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "habits:", err)
	os.Exit(1)
}
