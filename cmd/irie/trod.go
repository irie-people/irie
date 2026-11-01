package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

const trodUsage = "usage: irie ctl trod [on|off] [--json]"

// ctlTrod is the trod bot's report (DESIGN §4.10): whether it works
// now, the rules and guidelines it gives every agent it sends on, and what
// it did in this night's shift or the last. on and off turn it on or off
// first, in the config file (trod.on).
func ctlTrod(args []string) error {
	fs := flag.NewFlagSet("ctl trod", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the report as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	turn := ""
	if fs.NArg() > 0 {
		turn = fs.Arg(0)
		if err := fs.Parse(fs.Args()[1:]); err != nil {
			return err
		}
	}
	if fs.NArg() > 0 || turn != "" && turn != "on" && turn != "off" {
		return errors.New(trodUsage)
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	if turn != "" {
		set := ipc.ConfigArgs{Set: []ipc.ConfigSet{{Key: "trod.on", Value: json.RawMessage(fmt.Sprint(turn == "on"))}}}
		if err := ipc.Call(sock, 10*time.Second, ipc.OpConfig, set, nil); err != nil {
			return err
		}
	}
	var r ipc.TrodResult
	if err := ipc.Call(sock, 10*time.Second, ipc.OpTrod, nil, &r); err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(r)
	}
	return printTrod(os.Stdout, r, time.Now())
}

// printTrod writes the trod bot's report in plain words.
func printTrod(w io.Writer, r ipc.TrodResult, now time.Time) error {
	hours := r.Start + "–" + r.End
	if r.Start == r.End {
		hours = "around the clock"
	}
	switch {
	case !r.On:
		fmt.Fprintf(w, "Trod bot: off (%s when on). `irie ctl trod on` turns it on.\n", hours)
	case !r.In:
		fmt.Fprintf(w, "Trod bot: on, %s; out of its hours until %s.\n", hours, r.From.AddDate(0, 0, 1).Local().Format("Mon 15:04"))
	case r.Works:
		fmt.Fprintf(w, "Trod bot: on, %s, and working: you are away%s.\n", hours, idleWords(r))
	default:
		fmt.Fprintf(w, "Trod bot: on, %s, and waiting: you were at the Mac%s.\n", hours, idleWords(r))
	}
	if r.Awake {
		fmt.Fprintln(w, "It keeps the Mac awake while sessions have work left.")
	}
	list := func(title string, items []string) {
		fmt.Fprintln(w)
		if len(items) == 0 {
			fmt.Fprintln(w, title+": none")
			return
		}
		fmt.Fprintln(w, title+":")
		for i, it := range items {
			fmt.Fprintf(w, "  %d. %s\n", i+1, it)
		}
	}
	list("Rules every agent it sends on must follow", r.Rules)
	list("Guidelines for when they decide", r.Guidelines)
	fmt.Fprintln(w)
	if len(r.Sessions) == 0 {
		fmt.Fprintf(w, "It has sent nothing since %s.\n", r.From.Local().Format("Mon 15:04"))
		return nil
	}
	fmt.Fprintf(w, "Since %s:\n", r.From.Local().Format("Mon 15:04"))
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "SESSION\tSTATE\tNAME\tSENT ON\tLEFT FOR YOU")
	for _, s := range r.Sessions {
		var sent []string
		for _, e := range s.Sent {
			sent = append(sent, e.At.Local().Format("15:04")+" "+e.Reason)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", s.ID, s.State, s.Name, strings.Join(sent, ", "), s.Left)
	}
	return tw.Flush()
}

// idleWords says how long the Mac has been idle, when Irie can tell.
func idleWords(r ipc.TrodResult) string {
	if r.IdleSeconds == nil {
		return ""
	}
	d := time.Duration(*r.IdleSeconds) * time.Second
	if r.Works {
		return " (no keyboard or mouse for " + spokenFor(d) + ")"
	}
	return " " + spokenFor(d) + " ago"
}

// spokenFor says a span the short way: 40s, 12m, 3h05m.
func spokenFor(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}
