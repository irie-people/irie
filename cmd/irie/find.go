package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
)

const findUsage = "usage: " + brand.Slug + " ctl find [--json] [-n N] WORDS..."

// findTimeout is how long a find may take: the core waits a few seconds at
// most for the meaning of the words looked up.
const findTimeout = 30 * time.Second

// ctlFind looks the sessions up by what they did: their names, their tasks
// and what their agents said (DESIGN §4.5).
func ctlFind(args []string) error {
	fs := flag.NewFlagSet("ctl find", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print what it found as JSON")
	n := fs.Int("n", 0, "show at most this many (5 when 0)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	q := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if q == "" {
		return errors.New(findUsage)
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	// The session it runs in is left out: its own task has these words.
	a := ipc.FindArgs{Query: q, Limit: *n, From: os.Getenv(brand.Env("SESSION"))}
	var r ipc.FindResult
	if err := ipc.Call(sock, findTimeout, ipc.OpFind, a, &r); err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(r)
	}
	printFound(os.Stdout, r)
	return nil
}

// printFound prints what a find found: each session, whether it is open or
// when it closed, its name, and under them the start of what its agent
// said, or its task.
func printFound(w io.Writer, r ipc.FindResult) {
	switch {
	case len(r.Found) == 0:
		fmt.Fprintln(w, "no session matches")
	case !r.Close:
		fmt.Fprintln(w, "Nothing close to that. The nearest:")
	}
	for _, f := range r.Found {
		state := f.State
		if !f.Closed.IsZero() {
			state += " " + f.Closed.Local().Format("Jan 2 15:04")
		}
		fmt.Fprintf(w, "%-13s  %-18s  %s\n", f.ID, state, f.Name)
		if line := f.Said; line != "" {
			fmt.Fprintf(w, "    %s\n", line)
		} else if f.Task != "" {
			fmt.Fprintf(w, "    task: %s\n", f.Task)
		}
	}
	if !r.Meaning && r.Note != "" {
		fmt.Fprintf(w, "By words alone: %s.\n", r.Note)
	}
	if r.Reading > 0 {
		sessions := "sessions"
		if r.Reading == 1 {
			sessions = "session"
		}
		fmt.Fprintf(w, "Still reading %d %s for meaning: until then they rank by their words.\n", r.Reading, sessions)
	}
	if len(r.Found) == 0 {
		return
	}
	top := r.Found[0]
	if top.File != "" {
		fmt.Fprintf(w, "All its agent said: %s\n", top.File)
	}
	if top.Reopen {
		fmt.Fprintf(w, "Bring it back: %s ctl reopen %s\n", brand.Slug, top.ID)
	}
}
