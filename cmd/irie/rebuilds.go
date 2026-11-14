package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/rebuild"
)

// rebuildsUsage is the part of ctlUsage for the prompt-cache meter
// (DESIGN §6.3).
const rebuildsUsage = `  ` + brand.Slug + ` ctl rebuilds [--json]             the prompt-cache meter: how often each
                                         session wrote its whole context into
                                         its cache again, today and this week,
                                         what woke it and what that cost at
                                         list price, what cost most first
`

// rebuildsTop is how many sessions and causes each span lists.
const rebuildsTop = 5

// ctlRebuilds is the prompt-cache meter's report: the sessions and the
// causes that rebuilt caches most today and in the last seven days.
func ctlRebuilds(args []string) error {
	fs := flag.NewFlagSet("ctl rebuilds", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the report as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New("usage: " + brand.Slug + " ctl rebuilds [--json]")
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	var r ipc.RebuildsResult
	if err := ipc.Call(sock, 10*time.Second, ipc.OpRebuilds, nil, &r); err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(r)
	}
	return printRebuilds(os.Stdout, r)
}

// printRebuilds writes the report: a line for today and the week, then
// each span's causes and sessions, what cost most first.
func printRebuilds(w io.Writer, r ipc.RebuildsResult) error {
	if !r.On {
		fmt.Fprintln(w, "The prompt-cache meter is off (usage.rebuilds).")
		return nil
	}
	fmt.Fprintf(w, "Prompt-cache rebuilds at list price: today %s; this week %s.\n", spanWords(r.Today), spanWords(r.Week))
	if r.Week.Count == 0 {
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, s := range []struct {
		title string
		span  ipc.RebuildsSpan
	}{{"TODAY", r.Today}, {"THIS WEEK", r.Week}} {
		if s.span.Count == 0 {
			continue
		}
		fmt.Fprintf(tw, "\n%s, WHAT WOKE THEM\tREBUILDS\tTOKENS\tCOST\n", s.title)
		for _, p := range top(s.span.Causes) {
			fmt.Fprintf(tw, "%s\t%d\t%s\t%s\n", p.Words, p.Count, rebuild.Tokens(p.Tokens), rebuild.Dollars(p.USD))
		}
		fmt.Fprintf(tw, "%s, BY SESSION\t\t\t\n", s.title)
		for _, p := range top(s.span.Sessions) {
			name := p.Name
			if !p.Open {
				name += " (closed)"
			}
			fmt.Fprintf(tw, "%s\t%d\t%s\t%s\n", oneLine(name, 40), p.Count, rebuild.Tokens(p.Tokens), rebuild.Dollars(p.USD))
		}
	}
	return tw.Flush()
}

// spanWords sums a span up: "5, $6.20", or "none".
func spanWords(s ipc.RebuildsSpan) string {
	if s.Count == 0 {
		return "none"
	}
	return fmt.Sprintf("%d, %s", s.Count, rebuild.Dollars(s.USD))
}

// top is the first rebuildsTop of parts, ranked already.
func top(parts []ipc.RebuildsPart) []ipc.RebuildsPart {
	return parts[:min(len(parts), rebuildsTop)]
}
