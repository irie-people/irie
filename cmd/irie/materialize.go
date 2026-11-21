package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/genui"
	"github.com/irie-people/irie/internal/ipc"
)

// materializeUsage is the part of ctlUsage for materialized cards
// (docs/proposals/materialized-cards.md).
const materializeUsage = `  ` + brand.Slug + ` ctl materialize ID PROMPT [--context FILE|-]
      [--every D] [--gap D] [--wait D] [--json]
                                         the card a local model makes for PROMPT,
                                         from the context, kept under ID: drawn at
                                         once from what is kept, and made again
                                         when the context changed or it is as old
                                         as --every, never sooner than --gap
  ` + brand.Slug + ` ctl materialize --forget ID       drop the card kept under ID
`

// cardWidth is how wide ctl materialize draws a card.
const cardWidth = 72

// ctlMaterialize asks the core for a materialized card and draws it as
// text, with a line on where it stands.
func ctlMaterialize(args []string) error {
	fs := flag.NewFlagSet("ctl materialize", flag.ContinueOnError)
	from := fs.String("context", "", "the text to make the card from: a file, or - for standard input")
	every := fs.Duration("every", 0, "the most the card may age, such as 1h; 0 for no limit")
	gap := fs.Duration("gap", 0, "the least time between two cards, such as 5m")
	wait := fs.Duration("wait", 2*time.Minute, "how long to wait while the card is made; 0 answers at once")
	asJSON := fs.Bool("json", false, "print the answer as JSON")
	forget := fs.Bool("forget", false, "drop the card kept under ID")
	usage := errors.New("usage: " + brand.Slug + " ctl materialize ID PROMPT [--context FILE|-] [--every D] [--gap D] [--wait D] [--json], or --forget ID")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// Flags may follow ID and PROMPT too.
	rest := fs.Args()
	need := 2
	if *forget {
		need = 1
	}
	if len(rest) < need {
		return usage
	}
	if err := fs.Parse(rest[need:]); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return usage
	}
	a := ipc.MaterializeArgs{ID: rest[0], Every: *every, Gap: *gap, Wait: min(*wait, ipc.MaxMaterializeWait), Forget: *forget}
	if !*forget {
		a.Prompt = rest[1]
	}
	if *from != "" {
		text, err := readContext(*from)
		if err != nil {
			return err
		}
		a.Context = text
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	var r ipc.MaterializeResult
	if err := ipc.Call(sock, a.Wait+30*time.Second, ipc.OpMaterialize, a, &r); err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(r)
	}
	return printCard(os.Stdout, r)
}

// readContext reads a card's context from a file, or standard input for
// "-", no more of it than an ask may carry.
func readContext(from string) (string, error) {
	var in io.Reader = os.Stdin
	if from != "-" {
		f, err := os.Open(from)
		if err != nil {
			return "", err
		}
		defer f.Close()
		in = f
	}
	b, err := io.ReadAll(io.LimitReader(in, ipc.MaxMaterializeText+1))
	if err != nil {
		return "", err
	}
	if len(b) > ipc.MaxMaterializeText {
		return "", fmt.Errorf("the context is over %d KB; give less of it", ipc.MaxMaterializeText>>10)
	}
	return string(b), nil
}

// printCard draws the card as text, then says where it stands.
func printCard(w io.Writer, r ipc.MaterializeResult) error {
	if len(r.Spec) > 0 {
		c, err := genui.Check(r.Spec)
		if err != nil {
			return fmt.Errorf("the kept card does not draw: %w", err)
		}
		fmt.Fprint(w, genui.Text(c, cardWidth))
		fmt.Fprintln(w)
	}
	made := ""
	if !r.Made.IsZero() {
		made = " Made " + ago(r.Made) + " by " + r.By + "."
	}
	switch r.State {
	case "fresh":
		fmt.Fprintf(w, "Up to date.%s\n", made)
	case "refreshing":
		fmt.Fprintf(w, "Being made again, since what it was made from changed.%s\n", made)
	case "stale":
		fmt.Fprintf(w, "Out of date: the next ask after its gap makes it again.%s\n", made)
	default:
		if r.Began || r.Err == "" {
			fmt.Fprintln(w, "Not made yet: a local model is making it. Ask again, or wait longer with --wait.")
		} else {
			fmt.Fprintln(w, "Not made.")
		}
	}
	if r.Err != "" {
		fmt.Fprintf(w, "The last try failed: %s\n", r.Err)
	}
	return nil
}
