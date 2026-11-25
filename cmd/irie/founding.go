//go:build !nofounding

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
	"github.com/irie-people/irie/internal/chain"
	"github.com/irie-people/irie/internal/founding"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/repo"
)

const foundingUsage = `  ` + brand.Slug + ` ctl charters [--json]             the charters, texts of rights from Magna Carta
                                         on, how they bear on one another, and their
                                         local chain, every version checked
  ` + brand.Slug + ` ctl charters log [-n N] [--json]  the chain's versions, newest first
  ` + brand.Slug + ` ctl charters export               every version's bytes, oldest first, one a line
  ` + brand.Slug + ` ctl charters graph                where the Charters Graph page is: the texts,
                                         their parts and the rights they name, as a map
                                         to explore, in one file that loads nothing
  ` + brand.Slug + ` ctl schools [--json]              where the Common Schools page is: America's
                                         public schools as pies that open into smaller
                                         ones, in one file that loads nothing
`

// ctlFounding is `irie ctl charters` (and `irie ctl founding`, its first
// name): the charters and their local chain (DESIGN §4.17). The core
// records the texts and links this build carries on it, then reads back
// every version and checks it.
func ctlFounding(args []string) error {
	sub := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("ctl charters", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the answer as JSON")
	n := fs.Int("n", 20, "how many versions")
	if err := fs.Parse(args); err != nil {
		return err
	}
	usage := errors.New("usage: " + brand.Slug + " ctl charters [--json], log [-n N] [--json], export [--json], or graph [--json]")
	var a ipc.FoundingArgs
	switch {
	case fs.NArg() > 0:
		return usage
	case sub == "log" && *n > 0:
		a.Log = *n
	case sub == "export":
		a.Export = true
	case sub == "graph":
		a.Graph = true
	case sub != "":
		return usage
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	var r ipc.FoundingResult
	if err := ipc.Call(sock, 30*time.Second, ipc.OpFounding, a, &r); err != nil {
		return err
	}
	if *asJSON {
		switch sub {
		case "log":
			return json.NewEncoder(os.Stdout).Encode(r.Versions)
		case "export":
			return json.NewEncoder(os.Stdout).Encode(r.Export)
		case "graph":
			return json.NewEncoder(os.Stdout).Encode(struct {
				Page string `json:"page"`
			}{r.Page})
		}
		type text struct {
			ID, Title, When, Kind, SHA512 string
		}
		out := struct {
			Chain *ipc.FoundingChain `json:"chain"`
			Texts []text             `json:"texts"`
			Links []ipc.FoundingLink `json:"links"`
		}{Chain: r.Chain, Links: r.Links}
		for _, d := range r.Docs {
			out.Texts = append(out.Texts, text{d.ID, d.Title, d.When, d.Kind, d.SHA512})
		}
		return json.NewEncoder(os.Stdout).Encode(out)
	}
	switch sub {
	case "log":
		printFoundingLog(os.Stdout, r.Versions)
	case "export":
		// Canonical JSON holds no line break, so each line is one
		// version's bytes, whole.
		for _, v := range r.Export {
			fmt.Println(v)
		}
	case "graph":
		// The path alone, so a script can open it; the Mac app shows it in
		// Window › Charters Graph.
		fmt.Println(r.Page)
	default:
		printFounding(os.Stdout, r)
	}
	return nil
}

// printFounding says how the chain checked, with ● when every version did
// and ! when one didn't, so colour never carries it alone, then each
// text's date and fingerprint, then how the texts bear on one another.
func printFounding(w io.Writer, r ipc.FoundingResult) {
	home, _ := os.UserHomeDir()
	if c := r.Chain; c != nil {
		switch {
		case c.Verified:
			versions := "1 version"
			if c.Versions != 1 {
				versions = fmt.Sprintf("%d versions", c.Versions)
			}
			fmt.Fprintf(w, "● On your Charters chain: %s, each read back and matching its SHA-512; the newest, made %s, holds %d texts and %d links.\n", versions, c.Created, c.Texts, c.Links)
			fmt.Fprintf(w, "  Newest: %s\n", c.Head)
		default:
			fmt.Fprintf(w, "! The Charters chain doesn't check: %s\n", c.Problem)
		}
		fmt.Fprintf(w, "  Kept in %s\n", repo.Tilde(c.Folder, home))
	}
	fmt.Fprintln(w, "The texts, by date, each with its SHA-512:")
	for _, d := range r.Docs {
		fmt.Fprintf(w, "  %-10s %-44s sha512:%s…\n", d.When, d.Title, d.SHA512[:min(16, len(d.SHA512))])
	}
	if len(r.Links) > 0 {
		fmt.Fprintln(w, "How they bear on one another, the graph's edges (--json gives each one's why):")
		printFoundingLinks(w, r)
	}
}

// printFoundingLinks prints one line for each text that bears on earlier
// ones, in the texts' order, such as "Bill of Rights: drew on Magna Carta,
// English Bill of Rights, Virginia Declaration; amends Constitution".
func printFoundingLinks(w io.Writer, r ipc.FoundingResult) {
	short := map[string]string{}
	for _, d := range r.Docs {
		short[d.ID] = d.Short
	}
	name := func(id string) string {
		if s := short[id]; s != "" {
			return s
		}
		return id
	}
	for _, d := range r.Docs {
		to := map[string][]string{}
		for _, l := range r.Links {
			if l.From == d.ID {
				to[l.How] = append(to[l.How], name(l.To))
			}
		}
		var parts []string
		for _, how := range founding.Hows {
			if len(to[how]) > 0 {
				parts = append(parts, how+" "+strings.Join(to[how], ", "))
			}
		}
		if len(parts) > 0 {
			fmt.Fprintf(w, "  %s: %s\n", name(d.ID), strings.Join(parts, "; "))
		}
	}
}

// printFoundingLog lists versions, newest first: each one's short name,
// when it was made, its links, its form when it is the first build's, and
// its texts.
func printFoundingLog(w io.Writer, vs []ipc.FoundingVersion) {
	if len(vs) == 0 {
		fmt.Fprintln(w, "Nothing is on the chain yet.")
		return
	}
	for _, v := range vs {
		form := ""
		if v.Format == founding.FormatFirst {
			form = "  the first build's form, " + v.Format
		}
		links := fmt.Sprintf("%d links", v.Links)
		if v.Links == 1 {
			links = "1 link"
		}
		fmt.Fprintf(w, "%s  %s  %s%s\n", chain.Short(v.Name), v.Created, links, form)
		for _, f := range v.Files {
			file, digest, _ := strings.Cut(f, " ")
			fmt.Fprintf(w, "  %-44s %s…\n", file, digest[:min(len(chain.Prefix)+16, len(digest))])
		}
	}
}

// ctlSchools is `irie ctl schools`: where the Common Schools page is
// (docs/proposals/common-schools.md), which the core writes as it's asked.
func ctlSchools(args []string) error {
	fs := flag.NewFlagSet("ctl schools", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the answer as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New("usage: " + brand.Slug + " ctl schools [--json]")
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	var r ipc.FoundingResult
	if err := ipc.Call(sock, 30*time.Second, ipc.OpFounding, ipc.FoundingArgs{Schools: true}, &r); err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(struct {
			Page string `json:"page"`
		}{r.Page})
	}
	fmt.Println(r.Page)
	return nil
}
