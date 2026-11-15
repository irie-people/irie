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
	"github.com/irie-people/irie/internal/repo"
)

// worktreesUsage is the part of ctlUsage for the sweep of the closed
// sessions' worktrees (DESIGN §4.5).
const worktreesUsage = `  ` + brand.Slug + ` ctl worktrees [--all] [--json] [--drop]
                                         the closed sessions' worktrees a sweep
                                         would drop, and the room they take: each
                                         whose branch is in its repo's land branch
                                         (dog.land), with nothing uncommitted or
                                         running in it. A dry run; --drop removes
                                         them, keeping each branch, and reopen adds
                                         a worktree back. --all also lists those
                                         that stay, and why
`

// ctlWorktrees is `irie ctl worktrees`: a dry run of the sweep, or with
// --drop the sweep.
func ctlWorktrees(args []string) error {
	fs := flag.NewFlagSet("ctl worktrees", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print what the sweep found as JSON")
	all := fs.Bool("all", false, "list the worktrees that stay too, and why")
	drop := fs.Bool("drop", false, "remove the worktrees that can go, keeping their branches")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New("usage: " + brand.Slug + " ctl worktrees [--all] [--json] [--drop]")
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	var r ipc.WorktreesResult
	if err := ipc.Call(sock, 30*time.Minute, ipc.OpWorktrees, ipc.WorktreesArgs{Drop: *drop}, &r); err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(r)
	}
	home, _ := os.UserHomeDir()
	return printWorktrees(os.Stdout, r, *drop, *all, home)
}

// printWorktrees writes what the sweep found: the worktrees that can go,
// or went, with the room each takes, then with all those that stay and
// why, and a line that adds it up.
func printWorktrees(w io.Writer, r ipc.WorktreesResult, drop, all bool, home string) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	kept := 0
	head := false
	for _, t := range r.Trees {
		if t.Keep != "" && !t.Dropped {
			kept++
			continue
		}
		if !head {
			fmt.Fprintln(tw, "SESSION\tMB\tWORKTREE\tBRANCH")
			head = true
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", t.Session, megabytes(t.Bytes), repo.Tilde(t.Path, home), t.Branch)
	}
	if all && kept > 0 {
		if head {
			fmt.Fprintln(tw, "\t\t\t")
		}
		fmt.Fprintln(tw, "STAYS\tMB\tWORKTREE\tWHY")
		for _, t := range r.Trees {
			if t.Keep != "" && !t.Dropped {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", t.Session, megabytes(t.Bytes), repo.Tilde(t.Path, home), t.Keep)
			}
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if head || all && kept > 0 {
		fmt.Fprintln(w)
	}
	trees := func(n int) string {
		if n == 1 {
			return "1 worktree"
		}
		return fmt.Sprintf("%d worktrees", n)
	}
	switch {
	case drop:
		fmt.Fprintf(w, "Dropped %s of closed sessions, %s MB; their branches stay, and reopen adds each back.", trees(r.Dropped), megabytes(r.Bytes))
	case r.Can == 0:
		fmt.Fprint(w, "No closed session's worktree can go now.")
	default:
		fmt.Fprintf(w, "%s of closed sessions could go, %s MB; their branches stay, and reopen adds each back. `%s ctl worktrees --drop` removes them.",
			trees(r.Can), megabytes(r.Bytes), brand.Slug)
	}
	if kept > 0 {
		stay := trees(kept) + " stay"
		if kept == 1 {
			stay += "s"
		}
		if all {
			fmt.Fprintf(w, " %s.", stay)
		} else {
			fmt.Fprintf(w, " %s (--all says why).", stay)
		}
	}
	fmt.Fprintln(w)
	return nil
}

// megabytes is a size in megabytes, as du counts them, to one place.
func megabytes(n int64) string { return fmt.Sprintf("%.1f", float64(n)/(1<<20)) }
