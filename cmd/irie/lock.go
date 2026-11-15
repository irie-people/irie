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

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/lock"
)

// lockUsage is the part of usage for the agent locks (DESIGN §4.21).
const lockUsage = `  ` + brand.Slug + ` lock [SESSION] [--check] [--json]
                          each session's agent lock: its setup as its agent
                          started, one line a part; --check first asks
                          OSV.dev about their harness and package versions,
                          their names and versions alone, and prints each
                          advisory that affects one
`

// lockCmd is irie lock: the locks of the open sessions, or of one, and
// with --check what OSV.dev says of their versions.
func lockCmd(args []string) error {
	fs := flag.NewFlagSet("lock", flag.ContinueOnError)
	check := fs.Bool("check", false, "ask OSV.dev about the locks' harness and package versions first")
	asJSON := fs.Bool("json", false, "print the locks as JSON")
	// The session may come before the flags or after them.
	var rest []string
	for {
		if err := fs.Parse(args); err != nil {
			return err
		}
		if fs.NArg() == 0 {
			break
		}
		rest = append(rest, fs.Arg(0))
		args = fs.Args()[1:]
	}
	if len(rest) > 1 {
		return errors.New("usage: " + brand.Slug + " lock [SESSION] [--check] [--json]")
	}
	a := ipc.LockArgs{Check: *check}
	if len(rest) == 1 {
		a.Session = rest[0]
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	var r ipc.LockResult
	// A check waits on OSV.dev, for two minutes at most.
	if err := ipc.Call(sock, 3*time.Minute, ipc.OpLock, a, &r); err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(r)
	}
	if *check {
		return printLockCheck(os.Stdout, r)
	}
	return printLocks(os.Stdout, r, time.Now())
}

// printLocks writes each session's lock, a line a part, then when OSV.dev
// was last asked.
func printLocks(w io.Writer, r ipc.LockResult, now time.Time) error {
	if len(r.Sessions) == 0 {
		fmt.Fprintln(w, "No session is open.")
	}
	found := map[string]ipc.LockPackage{}
	for _, s := range r.Sessions {
		for _, p := range s.Packages {
			found[lock.Key(p)] = p
		}
	}
	status := func(p *ipc.LockPackage) string {
		if p == nil {
			return ""
		}
		marked, ok := found[lock.Key(*p)]
		if !ok {
			marked = *p
		}
		return p.Ecosystem + " " + p.Name + ": " + lock.Found(marked)
	}
	for i, s := range r.Sessions {
		if i > 0 {
			fmt.Fprintln(w)
		}
		l := s.Lock
		head := fmt.Sprintf("%s (%s)", s.Name, s.ID)
		switch {
		case l != nil:
			head += fmt.Sprintf(", locked at its %s %s", l.At, agoFor(now.Sub(l.Written)))
		case s.Host != "":
			head += ", on " + s.Host + ", whose lock is there"
		case !s.Open:
			head += ", closed"
		}
		fmt.Fprintln(w, head)
		var table strings.Builder
		tw := tabwriter.NewWriter(&table, 0, 4, 2, ' ', 0)
		switch {
		case l != nil:
			part := func(kind string, p ipc.LockPart, extra ...string) {
				name := p.Name
				if p.From != "" {
					name += " (" + p.From + ")"
				}
				what := strings.TrimSpace(strings.Join([]string{p.Version, p.Command}, "  "))
				if len(p.Events) > 0 {
					what = strings.TrimSpace(what + fmt.Sprintf("  %d events", len(p.Events)))
				}
				var tail []string
				for _, e := range append(extra, shortHash(p.Hash)) {
					if e != "" {
						tail = append(tail, e)
					}
				}
				fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", kind, name, what, strings.Join(tail, "  "))
			}
			part(brand.Slug, l.Irie)
			part("harness", l.Harness, status(l.Harness.Package))
			fmt.Fprintf(tw, "  profile\t+%s\t\t\n", l.Profile)
			if l.Model != "" {
				fmt.Fprintf(tw, "  model\t%s\t\t\n", l.Model)
			}
			fmt.Fprintf(tw, "  permissions\t%s\t\t\n", or(l.PermissionMode, "the CLI's default"))
			for _, p := range l.MCP {
				part("mcp", p, status(p.Package))
			}
			for _, p := range l.Plugins {
				part("plugin", p)
			}
			for _, p := range l.Skills {
				part("skill", p)
			}
			for _, p := range l.Hooks {
				kind := "hook"
				if len(p.Events) > 0 {
					kind = "hooks"
				}
				part(kind, p)
			}
			for _, n := range l.Notes {
				fmt.Fprintf(tw, "  note\t%s\t\t\n", n)
			}
		case s.Host != "":
			for _, p := range s.Packages {
				fmt.Fprintf(tw, "  package\t%s\t%s %s %s\t%s\n", p.Part, p.Ecosystem, p.Name, p.Version, lock.Found(p))
			}
		default:
			fmt.Fprintf(tw, "  %s\n", s.None)
		}
		tw.Flush()
		// A line whose last cells are empty ends in the table's padding.
		for _, line := range strings.SplitAfter(table.String(), "\n") {
			if line != "" {
				fmt.Fprintln(w, strings.TrimRight(line, " \n"))
			}
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, checkedWords(r, now))
	return nil
}

// printLockCheck writes what a check found: each version with an advisory,
// its sessions and its advisories, each with its id, summary and fix.
func printLockCheck(w io.Writer, r ipc.LockResult) error {
	var hit []ipc.LockFound
	for _, f := range r.Packages {
		if len(f.Advisories) > 0 {
			hit = append(hit, f)
		}
	}
	versions := fmt.Sprintf("%d package version", len(r.Packages))
	if len(r.Packages) != 1 {
		versions += "s"
	}
	switch {
	case len(r.Packages) == 0:
		fmt.Fprintln(w, "The locks name no package version to ask OSV.dev about.")
		return nil
	case len(hit) == 0:
		fmt.Fprintf(w, "Asked OSV.dev about %s: none has a known advisory.\n", versions)
		return nil
	}
	fmt.Fprintf(w, "Asked OSV.dev about %s: %d with known advisories.\n", versions, len(hit))
	for _, f := range hit {
		fmt.Fprintln(w)
		who := ""
		if n := len(f.Sessions); n > 0 {
			who = fmt.Sprintf(", in %d session", n)
			if n != 1 {
				who += "s"
			}
			who += ": " + oneLine(strings.Join(f.Sessions, ", "), 80)
		}
		fmt.Fprintf(w, "%s %s (%s %s)%s\n", or(f.Part, f.Name), f.Version, f.Ecosystem, f.Name, who)
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		for _, a := range f.Advisories {
			fix := "no fix yet"
			if a.Fixed != "" {
				fix = "fixed in " + a.Fixed
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\n", a.ID, oneLine(or(a.Summary, "no summary"), 90), fix)
		}
		tw.Flush()
	}
	return nil
}

// checkedWords say when OSV.dev was last asked, and how to ask it.
func checkedWords(r ipc.LockResult, now time.Time) string {
	how := brand.Slug + " lock --check asks it now, sending only names and versions"
	if r.Checked.IsZero() {
		return "OSV.dev has not been asked about these locks: " + how + "."
	}
	when := "OSV.dev was asked " + agoFor(now.Sub(r.Checked))
	if r.Daily {
		return when + ", and is asked once a day (lock.check = \"daily\")."
	}
	return when + "; " + how + "."
}

// agoFor is d in a few words: "just now", "5 minutes ago", "2 days ago".
func agoFor(d time.Duration) string {
	unit := func(n int, s string) string {
		if n == 1 {
			return "1 " + s + " ago"
		}
		return fmt.Sprintf("%d %ss ago", n, s)
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return unit(int(d.Minutes()), "minute")
	case d < 48*time.Hour:
		return unit(int(d.Hours()), "hour")
	}
	return unit(int(d.Hours()/24), "day")
}

// shortHash is a hash in a few characters: sha256:9f2c1a3b4c5d.
func shortHash(h string) string {
	if i := strings.LastIndexByte(h, ':'); i >= 0 && len(h)-i > 13 {
		return h[:i+13]
	}
	return h
}
