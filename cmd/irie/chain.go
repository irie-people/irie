package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/chain"
	"github.com/irie-people/irie/internal/ipc"
)

// chainUsage is the part of ctlUsage for the sidebar's chain (DESIGN
// §4.23).
const chainUsage = `  ` + brand.Slug + ` ctl chain [--json]                the sidebar's chain: every change to how
                                         your sessions are organised, kept as a
                                         version named by its SHA-512, on this
                                         computer only, encrypted. Says where
                                         it is kept, how many versions, the newest,
                                         where its key is, and anything wrong, such
                                         as a version changed or a wrong key
  ` + brand.Slug + ` ctl chain log [--json] [-n N]     the newest N versions (20), newest first:
                                         each one's name, when, what changed and
                                         its cards, groups and spaces
  ` + brand.Slug + ` ctl chain show [--json | --raw] VERSION
                                         a version's organisation, by its name, at
                                         least six hex digits of it, or head;
                                         --raw prints its canonical bytes, which
                                         shasum -a 512 names
  ` + brand.Slug + ` ctl chain restore VERSION         bring a version's organisation back, as a
                                         new version and a step of the history
                                         that one undo takes back; closed sessions
                                         stay closed, and those it doesn't name
                                         stay as they are
  ` + brand.Slug + ` ctl chain head                    the newest version's name, as a tool that
                                         anchors it reads it
`

// chainUse is ctl chain's usage line.
const chainUse = "usage: " + brand.Slug + " ctl chain [log [-n N] | show [--raw] VERSION | restore VERSION | head] [--json]"

// ctlChain is `irie ctl chain`.
func ctlChain(args []string) error {
	do := ipc.ChainStatus
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		do, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("ctl chain", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the result as JSON")
	raw := fs.Bool("raw", false, "show: print the version's canonical bytes")
	n := fs.Int("n", 0, "log: how many versions")
	if err := fs.Parse(args); err != nil {
		return err
	}
	a := ipc.ChainArgs{Do: do, Limit: *n}
	switch do {
	case ipc.ChainStatus, ipc.ChainLog, ipc.ChainHead:
		if fs.NArg() > 0 {
			return errors.New(chainUse)
		}
	case ipc.ChainShow, ipc.ChainRestore:
		if fs.NArg() != 1 {
			return errors.New(chainUse)
		}
		a.Version = fs.Arg(0)
	default:
		return fmt.Errorf("unknown chain command %q\n%s", do, chainUse)
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	var r ipc.ChainResult
	if err := ipc.Call(sock, 20*time.Second, ipc.OpChain, a, &r); err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(r)
	}
	switch do {
	case ipc.ChainHead:
		if r.Problem != "" {
			fmt.Fprintln(os.Stderr, "chain: "+r.Problem)
		}
		if r.Head == "" {
			return errors.New("the sidebar's chain has no version yet")
		}
		fmt.Println(r.Head)
		return nil
	case ipc.ChainShow:
		if *raw && r.Version != nil {
			_, err := io.WriteString(os.Stdout, r.Raw)
			return err
		}
		return printChainShow(os.Stdout, r, time.Local)
	case ipc.ChainLog:
		return printChainLog(os.Stdout, r, time.Local)
	case ipc.ChainRestore:
		return printChainRestore(os.Stdout, r)
	}
	return printChainStatus(os.Stdout, r)
}

// printChainStatus says where the chain is kept and how it stands.
func printChainStatus(w io.Writer, r ipc.ChainResult) error {
	switch {
	case r.Head == "":
		fmt.Fprintf(w, "The sidebar's chain has no version yet. It is kept in %s.\n", r.Dir)
	default:
		fmt.Fprintf(w, "The sidebar's chain: %s in %s.\nNewest: %s\n", plural(r.Count, "version"), r.Dir, r.Head)
	}
	fmt.Fprintln(w, "It stays on this computer, and holds no one's name, account or machine: only ids and the names you gave.")
	if words := vaultWords(r.Vault); words != "" {
		fmt.Fprintln(w, words)
	}
	if r.Problem != "" {
		fmt.Fprintf(w, "Problem: %s.\n", strings.TrimSuffix(r.Problem, "."))
	}
	return nil
}

// printChainLog lists the versions, newest first: each one's name, when,
// what changed and what it holds.
func printChainLog(w io.Writer, r ipc.ChainResult, loc *time.Location) error {
	if r.Problem != "" {
		fmt.Fprintf(w, "Problem: %s.\n", strings.TrimSuffix(r.Problem, "."))
	}
	if len(r.Versions) == 0 {
		fmt.Fprintln(w, "The sidebar's chain has no version yet.")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "VERSION\tWHEN\tWHAT\tHOLDS")
	for _, e := range r.Versions {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", chain.Short(e.Name), e.At.In(loc).Format(time.DateTime), oneLine(e.What, 60), holds(e))
	}
	return tw.Flush()
}

// holds says what a version holds: "12 cards, 3 groups, 2 spaces".
func holds(e ipc.ChainEntry) string {
	return plural(e.Cards, "card") + ", " + plural(e.Groups, "group") + ", " + plural(e.Spaces, "space")
}

// plural is n and noun, with an s past one.
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// printChainShow prints a version: its name, when it was made and by what,
// then each space with its cards, Pinned first and Parked last, and your
// groups as a tree. A name the version keeps is the one you gave; one in
// brackets is the session's name now, which the version does not keep.
func printChainShow(w io.Writer, r ipc.ChainResult, loc *time.Location) error {
	e := r.Version
	if e == nil || e.Doc == nil {
		return errors.New("no version came back")
	}
	v := e.Doc
	fmt.Fprintf(w, "Version %s\n", e.Name)
	fmt.Fprintf(w, "  made %s: %s\n", e.At.In(loc).Format(time.DateTime), e.What)
	if e.Parent != "" {
		fmt.Fprintf(w, "  after %s\n", chain.Short(e.Parent))
	} else {
		fmt.Fprintln(w, "  the first version")
	}
	fmt.Fprintf(w, "  its organisation: %s (%s)\n", chain.Short(e.State), holds(*e))
	name := func(t ipc.ChainCard) string {
		switch {
		case t.Name != "":
			return t.Name
		case e.Names[t.ID] != "":
			return "(" + e.Names[t.ID] + ")"
		}
		return "(" + t.ID + ")"
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	line := func(depth int, t ipc.ChainCard) {
		fmt.Fprintf(tw, "%s%s\t%s\n", strings.Repeat("  ", depth), oneLine(name(t), 50), t.ID)
	}
	for i, s := range v.Org.Spaces {
		key := s.ID
		if i == 0 {
			key = ""
		}
		in := func(t ipc.ChainCard) bool {
			return t.Space == key || i == 0 && !slices.ContainsFunc(v.Org.Spaces[1:], func(o ipc.Space) bool { return o.ID == t.Space })
		}
		fmt.Fprintf(tw, "\n%s\t\n", s.Name)
		var pinned, parked, rest []ipc.ChainCard
		for _, t := range v.Org.Cards {
			switch {
			case !in(t):
			case t.Pinned:
				pinned = append(pinned, t)
			case t.Parked:
				parked = append(parked, t)
			default:
				rest = append(rest, t)
			}
		}
		if len(pinned) > 0 {
			fmt.Fprintln(tw, "  Pinned\t")
			for _, t := range pinned {
				line(2, t)
			}
		}
		// Each group of yours where the first of its cards is, its groups
		// inside it, then the cards of none.
		var walk func(parent string, depth int)
		drawn := map[string]bool{}
		walk = func(parent string, depth int) {
			for _, g := range v.Org.Groups {
				if g.Parent != parent || drawn[g.ID] || !groupHolds(v.Org, g.ID, rest) {
					continue
				}
				drawn[g.ID] = true
				fmt.Fprintf(tw, "%s%s/\t\n", strings.Repeat("  ", depth), g.Name)
				for _, t := range rest {
					if t.Group == g.ID {
						line(depth+1, t)
					}
				}
				walk(g.ID, depth+1)
			}
		}
		walk("", 1)
		for _, t := range rest {
			if t.Group == "" || !slices.ContainsFunc(v.Org.Groups, func(g ipc.Group) bool { return g.ID == t.Group }) {
				line(1, t)
			}
		}
		if len(parked) > 0 {
			fmt.Fprintln(tw, "  Parked\t")
			for _, t := range parked {
				line(2, t)
			}
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if r.Problem != "" {
		fmt.Fprintf(w, "\nProblem: %s.\n", strings.TrimSuffix(r.Problem, "."))
	}
	return nil
}

// groupHolds reports whether any of cards is in group id or a group inside
// it.
func groupHolds(org ipc.ChainOrg, id string, cards []ipc.ChainCard) bool {
	for _, t := range cards {
		g := t.Group
		for range len(org.Groups) + 1 {
			if g == "" {
				break
			}
			if g == id {
				return true
			}
			i := slices.IndexFunc(org.Groups, func(x ipc.Group) bool { return x.ID == g })
			if i < 0 {
				break
			}
			g = org.Groups[i].Parent
		}
	}
	return false
}

// printChainRestore says what a restore did.
func printChainRestore(w io.Writer, r ipc.ChainResult) error {
	x := r.Restored
	if x == nil {
		return errors.New("no restore came back")
	}
	if x.Label == "" {
		fmt.Fprintln(w, "The sidebar is organised as that version has it already, so nothing changed.")
		return nil
	}
	fmt.Fprintf(w, "Done: %s. Cards put back as it had them: %d.\n", x.Label, x.Cards)
	if x.Closed > 0 {
		fmt.Fprintf(w, "Its cards that are closed now, which stay closed: %d.\n", x.Closed)
	}
	if x.Kept > 0 {
		fmt.Fprintf(w, "Sessions it doesn't name, which stay as they are: %d.\n", x.Kept)
	}
	fmt.Fprintf(w, "One undo takes it back. Newest version: %s\n", r.Head)
	if r.Problem != "" {
		fmt.Fprintf(w, "Problem: %s.\n", strings.TrimSuffix(r.Problem, "."))
	}
	return nil
}

// vaultWords says how the chain is kept at rest, in plain words.
func vaultWords(kept string) string {
	const sealed = "Its versions are encrypted on disk (AES-256-GCM), under a key "
	switch kept {
	case chain.KeptInKeychain:
		return sealed + "wrapped by a key in your login Keychain (" + brand.Name + " vault, sidebar)."
	case chain.KeptInSecrets:
		return sealed + "wrapped by a key in the Secret Service, your desktop's keyring."
	case chain.KeptInFile:
		return sealed + "kept in a file only you can read (key, mode 0600), as this computer has no key store: " +
			"anyone who can read your files can read the chain. A passphrase (Argon2id) is to come."
	}
	return ""
}
