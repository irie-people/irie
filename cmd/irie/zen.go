package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/chain"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/zen"
)

// zenUsage is the part of ctlUsage for your zen (docs/proposals/zen.md).
const zenUsage = `  ` + brand.Slug + ` ctl zen [--json]                  your zen: its settings, its newest version,
                                         and what zen offers now
  ` + brand.Slug + ` ctl zen set KEY=VALUE... [--why WORDS]
                                         change your zen, as a new version: layout
                                         (centred, edge, watch), columns (80, 100,
                                         120), text_size (0 for the window's, or 9
                                         to 28), full_screen, gentle and night (on
                                         or off), breaks (minutes, 15 to 180, or
                                         off)
  ` + brand.Slug + ` ctl zen log [-n N] [--json]       its versions, newest first
  ` + brand.Slug + ` ctl zen restore VERSION           back to an earlier zen, by the start of its
                                         hash, as a new version
`

// ctlZen is `irie ctl zen`.
func ctlZen(args []string) error {
	sub := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("ctl zen", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the answer as JSON")
	why := fs.String("why", "", "what the change is for, in plain words")
	n := fs.Int("n", 20, "how many versions")
	usage := errors.New("usage: " + brand.Slug + " ctl zen [--json], set KEY=VALUE... [--why WORDS], log [-n N] [--json], or restore VERSION")
	var rest []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return err
		}
		if args = fs.Args(); len(args) > 0 {
			rest, args = append(rest, args[0]), args[1:]
		}
	}
	var a ipc.ZenArgs
	switch sub {
	case "":
		if len(rest) > 0 {
			return usage
		}
	case "set":
		ch, err := parseZenSet(rest)
		if err != nil {
			return err
		}
		a.Set, a.Why = &ch, *why
	case "log":
		if len(rest) > 0 || *n < 1 {
			return usage
		}
		a.Log = *n
	case "restore":
		if len(rest) != 1 {
			return usage
		}
		a.Restore = rest[0]
	default:
		return usage
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	var r ipc.ZenResult
	if err := ipc.Call(sock, 10*time.Second, ipc.OpZen, a, &r); err != nil {
		return err
	}
	if *asJSON {
		if sub == "log" {
			return json.NewEncoder(os.Stdout).Encode(r.Versions)
		}
		return json.NewEncoder(os.Stdout).Encode(r)
	}
	switch sub {
	case "log":
		printZenLog(os.Stdout, r.Versions)
	case "set", "restore":
		if r.Version == nil {
			fmt.Println("Your zen is like that already; no new version.")
			return nil
		}
		fmt.Printf("Your zen is now version %s: %s.\n", chain.Short(r.Version.Name), r.Version.Why)
	default:
		printZen(os.Stdout, r.Zen)
	}
	return nil
}

// parseZenSet reads KEY=VALUE pairs into a change.
func parseZenSet(pairs []string) (ipc.ZenChange, error) {
	var ch ipc.ZenChange
	if len(pairs) == 0 {
		return ch, errors.New("zen set: name a key and its value, such as breaks=50")
	}
	for _, kv := range pairs {
		k, v, ok := strings.Cut(kv, "=")
		k, v = strings.ToLower(strings.TrimSpace(k)), strings.ToLower(strings.TrimSpace(v))
		if !ok || v == "" {
			return ch, fmt.Errorf("zen set: %q: give KEY=VALUE", kv)
		}
		var err error
		switch strings.ReplaceAll(k, "-", "_") {
		case "layout":
			v = strings.NewReplacer(" ", "", "-", "", "_", "").Replace(v)
			switch v {
			case "edgetoedge":
				v = "edge"
			case "keepwatch":
				v = "watch"
			case "centered":
				v = "centred"
			}
			ch.Layout = &v
		case "columns":
			ch.Columns, err = zenInt(k, v)
		case "text_size", "text":
			if v == "window" {
				v = "0"
			}
			ch.TextSize, err = zenInt(k, strings.TrimSuffix(strings.TrimSpace(strings.TrimSuffix(v, "pt")), " "))
		case "breaks", "rests":
			if v == "off" || v == "none" || v == "no" {
				v = "0"
			}
			ch.Breaks, err = zenInt(k, strings.TrimSuffix(v, "m"))
		case "full_screen", "fullscreen":
			ch.FullScreen, err = zenBool(k, v)
		case "gentle":
			ch.Gentle, err = zenBool(k, v)
		case "night":
			ch.Night, err = zenBool(k, v)
		default:
			return ch, fmt.Errorf("zen set: no key %q; the keys are layout, columns, text_size, full_screen, gentle, breaks and night", k)
		}
		if err != nil {
			return ch, err
		}
	}
	return ch, nil
}

func zenInt(k, v string) (*int, error) {
	n, err := strconv.Atoi(v)
	if err != nil {
		return nil, fmt.Errorf("zen set: %s=%s: a whole number", k, v)
	}
	return &n, nil
}

func zenBool(k, v string) (*bool, error) {
	switch v {
	case "on", "true", "yes", "1":
		b := true
		return &b, nil
	case "off", "false", "no", "0":
		b := false
		return &b, nil
	}
	return nil, fmt.Errorf("zen set: %s=%s: on or off", k, v)
}

// printZen says your zen in plain words.
func printZen(w io.Writer, z ipc.Zen) {
	p := z.Profile
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	layout := zen.LayoutName(p.Layout)
	if p.Layout == "centred" {
		layout += fmt.Sprintf(", %d columns", p.Columns)
	}
	text := "the window's"
	if p.TextSize > 0 {
		text = fmt.Sprintf("%d pt", p.TextSize)
	}
	rests := "off"
	if p.Breaks > 0 {
		rests = fmt.Sprintf("every %d minutes", p.Breaks)
	}
	onOff := func(b bool) string {
		if b {
			return "on"
		}
		return "off"
	}
	fmt.Fprintf(tw, "layout\t%s\n", layout)
	fmt.Fprintf(tw, "text\t%s\n", text)
	fmt.Fprintf(tw, "full screen\t%s\n", onOff(p.FullScreen))
	fmt.Fprintf(tw, "gentle agent\t%s\n", onOff(p.Gentle))
	fmt.Fprintf(tw, "rests\t%s\n", rests)
	fmt.Fprintf(tw, "word at night\t%s\n", onOff(p.Night))
	tw.Flush()
	switch {
	case z.Head == "":
		fmt.Fprintln(w, "\nYour zen hasn't changed yet, so it has no versions.")
	default:
		fmt.Fprintf(w, "\nVersion %s (%s ctl zen log lists them).\n", chain.Short(z.Head), brand.Slug)
	}
	if z.Trying != "" {
		fmt.Fprintln(w, "A step's change is on trial; the settings above show it.")
	}
	if z.On {
		fmt.Fprintln(w, "Zen is on.")
	}
	if o := z.Offer; o != nil {
		fmt.Fprintf(w, "Zen offers: %s\n", o.Text)
	}
}

// printZenLog lists versions, newest first: each's short hash, when it was
// made and why.
func printZenLog(w io.Writer, vs []ipc.ZenVersion) {
	if len(vs) == 0 {
		fmt.Fprintln(w, "Your zen hasn't changed yet, so it has no versions.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, v := range vs {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", chain.Short(v.Name), v.Created.Local().Format("2 Jan 15:04"), v.Why)
	}
	tw.Flush()
}
