package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/council"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/repo"
)

// councilUsage is the part of ctlUsage for Raguel's council (DESIGN §4.11).
const councilUsage = `  ` + brand.Slug + ` ctl council [tally [--days N]|convene|propose|done|yes N...|no N [--why TEXT]] [--json]
                                         Raguel (Ragu): the latest council, its
                                         plans and when the next is due. tally
                                         counts what the agents did and what it
                                         cost over N days (7), beside as many
                                         before, with no tokens; convene holds a
                                         council now, one +raguel session that
                                         drafts plans; yes N starts plan N as a
                                         jot, no N turns it down. propose and
                                         done read one JSON object on stdin, a
                                         plan or {"summary": ...}, and only the
                                         council's own session may send them
`

const councilUsageLine = "usage: " + brand.Slug + " ctl council [tally [--days N]|convene|propose|done|yes N...|no N [--why TEXT]] [--json]"

// ctlCouncil is Raguel's report and his council (DESIGN §4.11).
func ctlCouncil(args []string) error {
	fs := flag.NewFlagSet("ctl council", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the result as JSON")
	days := fs.Int("days", 0, "with tally: how many days a stretch is (7)")
	why := fs.String("why", "", "with no: your reason")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var words []string
	for fs.NArg() > 0 {
		words = append(words, fs.Arg(0))
		if err := fs.Parse(fs.Args()[1:]); err != nil {
			return err
		}
	}
	verb := ""
	if len(words) > 0 {
		verb, words = words[0], words[1:]
	}
	a := ipc.CouncilArgs{Do: verb}
	timeout := 10 * time.Second
	switch verb {
	case ipc.CouncilReport:
		if len(words) > 0 {
			return errors.New(councilUsageLine)
		}
	case ipc.CouncilTally:
		// A fortnight of the journal is about 100 MB to read.
		a.Days, timeout = *days, time.Minute
	case ipc.CouncilConvene:
		// Typing it is the OK the Arch Agents tab asks for.
		a.OK, timeout = true, 2*time.Minute
	case ipc.CouncilPropose:
		var p council.Plan
		if err := readStdinJSON(&p); err != nil {
			return fmt.Errorf("a plan on stdin: %w", err)
		}
		a.Plan, timeout = &p, 30*time.Second
	case ipc.CouncilDone:
		var d struct {
			Summary string `json:"summary"`
		}
		if err := readStdinJSON(&d); err != nil {
			return fmt.Errorf(`{"summary": "..."} on stdin: %w`, err)
		}
		a.Summary, timeout = d.Summary, 30*time.Second
	case ipc.CouncilYes, ipc.CouncilNo:
		for _, w := range words {
			n, err := strconv.Atoi(strings.TrimPrefix(w, "#"))
			if err != nil {
				return fmt.Errorf("%q is not a plan's number\n%s", w, councilUsageLine)
			}
			a.Plans = append(a.Plans, n)
		}
		words = nil
		if len(a.Plans) == 0 {
			return errors.New(councilUsageLine)
		}
		// Each yes starts a jot, which may make a worktree.
		a.OK, a.Why, timeout = verb == ipc.CouncilYes, *why, 2*time.Minute
	default:
		return errors.New(councilUsageLine)
	}
	switch {
	case len(words) > 0, *days != 0 && verb != ipc.CouncilTally, *why != "" && verb != ipc.CouncilNo:
		return errors.New(councilUsageLine)
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	var r ipc.CouncilResult
	if err := ipc.Call(sock, timeout, ipc.OpCouncil, a, &r); err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(r)
	}
	switch verb {
	case ipc.CouncilTally:
		fmt.Print(r.Markdown)
	case ipc.CouncilConvene:
		fmt.Printf("Ragu holds a council in %s (%s), in %s: his plans show in `%s ctl council` and the Arch Agents tab as he puts them in.\n", r.Session.Name, r.Session.ID, repo.Tilde(r.Session.Cwd, homeDir()), brand.Slug)
	case ipc.CouncilPropose:
		fmt.Printf("Plan %d is in.\n", r.Plan)
	case ipc.CouncilDone:
		fmt.Printf("The minutes are written: %s\n", strings.Join(r.Minutes, ", "))
	case ipc.CouncilYes:
		for _, j := range r.Jots {
			fmt.Printf("Plan %d started as a jot: %s (%s), in %s.\n", j.Plan, j.Name, j.Session, repo.Tilde(j.Cwd, homeDir()))
		}
	case ipc.CouncilNo:
		fmt.Println("Ragu lets it go.")
	default:
		printCouncil(os.Stdout, r)
	}
	return nil
}

// readStdinJSON reads one JSON object from stdin into v, refusing a field
// v lacks, so a misspelt one is named rather than lost.
func readStdinJSON(v any) error {
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

// printCouncil writes Raguel's report in plain words.
func printCouncil(w io.Writer, r ipc.CouncilResult) {
	fmt.Fprintln(w, council.Seat("raguel")+" holds the council.")
	switch {
	case !r.On:
		fmt.Fprintln(w, "He is off: no tally and no council (arch.raguel.on).")
		return
	case r.Latest == nil && r.Running:
		fmt.Fprintln(w, "A council is being convened now.")
	case r.Latest == nil:
		fmt.Fprintln(w, "No council yet: `"+brand.Slug+" ctl council convene` holds one.")
	default:
		l := r.Latest
		state := "done"
		switch {
		case r.Running:
			state = "sitting now"
		case l.Done.IsZero():
			state = "ended before it said it was done"
		}
		fmt.Fprintf(w, "The latest council: %s, in %s, %s", l.Date, l.Name, state)
		if l.CostUSD != nil {
			fmt.Fprintf(w, ", cost $%.2f", *l.CostUSD)
		}
		fmt.Fprintln(w, ".")
		for _, f := range l.Minutes {
			if strings.HasSuffix(f, ".html") {
				fmt.Fprintln(w, "Minutes: "+repo.Tilde(f, homeDir()))
			}
		}
		if len(l.Plans) == 0 {
			fmt.Fprintln(w, "No plans.")
		}
		for _, p := range l.Plans {
			said := "waiting for your word"
			switch p.Said {
			case council.Yes:
				said = "yes, started as " + p.Jot
			case council.No:
				said = "no"
				if p.Why != "" {
					said += ": " + p.Why
				}
			case council.Lapsed:
				said = "lapsed"
			}
			fmt.Fprintf(w, "  %d. %s [%s]: saves %s; effort %s. %s.\n", p.N, p.Title, council.Seat(p.Seat), p.Saves, p.Effort, strings.ToUpper(said[:1])+said[1:])
		}
		if len(l.Waiting()) > 0 {
			fmt.Fprintf(w, "Answer with `%s ctl council yes N...` or `%s ctl council no N --why TEXT`.\n", brand.Slug, brand.Slug)
		}
	}
	switch {
	case r.Next == nil:
		fmt.Fprintln(w, "He holds a council when you ask (arch.raguel.every is 0s).")
	case r.Next.After(time.Now()):
		fmt.Fprintf(w, "The next is due from %s, at a quiet time.\n", r.Next.Local().Format("2 Jan 15:04"))
	default:
		fmt.Fprintln(w, "The next is due at the next quiet time.")
	}
}
