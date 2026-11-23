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
	"github.com/irie-people/irie/internal/ledger"
)

// costsUsage is the part of ctlUsage for the ledger (DESIGN §6.3).
const costsUsage = `  ` + brand.Slug + ` ctl costs [--json]                what the model calls of your sessions and
                                         of ` + brand.Name + ` itself cost at the list price
                                         of their day: today, this week and in 30
                                         days, by purpose and model, with the calls
                                         that have no price counted apart, and the
                                         sessions that cost most, beside what
                                         Claude Code's status line said
`

// costsTop is how many purposes, models and sessions the report lists.
const costsTop = 6

// ctlCosts is the ledger's report.
func ctlCosts(args []string) error {
	fs := flag.NewFlagSet("ctl costs", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the report as JSON, with every session")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New("usage: " + brand.Slug + " ctl costs [--json]")
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	var r ipc.CostsResult
	if err := ipc.Call(sock, 30*time.Second, ipc.OpCosts, nil, &r); err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(r)
	}
	return printCosts(os.Stdout, r)
}

// spanTitles name the report's spans.
var spanTitles = map[string]string{"today": "today", "week": "this week", "month": "30 days"}

// printCosts writes the report: a line for each span, then the week's
// purposes and models and the costliest sessions of the 30 days.
func printCosts(w io.Writer, r ipc.CostsResult) error {
	fmt.Fprintln(w, "Model calls at the list price of their day:")
	var week *ipc.CostsSpan
	for i, s := range r.Spans {
		fmt.Fprintf(w, "  %-10s %s\n", spanTitles[s.Name], callWords(s.Total))
		if s.Name == "week" {
			week = &r.Spans[i]
		}
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if week != nil && week.Total.Calls > 0 {
		for _, part := range []struct {
			title string
			parts []ipc.CostsPart
		}{{"THIS WEEK, BY PURPOSE", week.Purposes}, {"THIS WEEK, BY MODEL", week.Models}} {
			fmt.Fprintf(tw, "\n%s\tCALLS\tCOST\n", part.title)
			for _, p := range part.parts[:min(len(part.parts), costsTop)] {
				fmt.Fprintf(tw, "%s\t%d\t%s\n", oneLine(p.Name, 40), p.Calls, p.Words)
			}
		}
	}
	if len(r.Sessions) > 0 {
		fmt.Fprintf(tw, "\nCOSTLIEST SESSIONS, 30 DAYS\tCALLS\tCOST\tCLAUDE CODE SAID\n")
		for _, s := range r.Sessions[:min(len(r.Sessions), costsTop)] {
			name := s.Name
			if !s.Open {
				name += " (closed)"
			}
			said := "-"
			if s.Claude != nil {
				said = ledger.Dollars(*s.Claude)
			}
			fmt.Fprintf(tw, "%s\t%d\t%s\t%s\n", oneLine(name, 40), s.Ledger.Calls, s.Ledger.Words, said)
		}
	}
	return tw.Flush()
}

// callWords sum a span up: "1203 calls: $12.40, plus 3 calls with no
// price", "41 calls: free", or "no calls".
func callWords(t ipc.CostsTally) string {
	switch t.Calls {
	case 0:
		return "no calls"
	case 1:
		return "1 call: " + t.Words
	}
	return fmt.Sprintf("%d calls: %s", t.Calls, t.Words)
}
