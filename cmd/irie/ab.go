package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/irie-people/irie/internal/ab"
	"github.com/irie-people/irie/internal/brand"
)

const abUsage = `usage: ` + brand.Slug + ` ab [-n RUNS] [--warmup N] [--ignore REGEXP]... [--stdin FILE]
          [--timeout DURATION] [--json] GOLD NEW [-- ARG...]

Runs two builds of a program on the same arguments and input, to check a
rewrite: GOLD, the build you trust, such as the last release, and NEW. They
take turns, RUNS times each (default 10) after --warmup runs each (default
1), and the report says whether NEW's output is the same as GOLD's, and how
its time, CPU and peak memory compare: the median run, with the quickest
and the slowest. Standard output and the exit status are compared, line by
line after --ignore blanks out what may differ, such as a version or a
date; standard error is not. Naming one build twice shows how far the
numbers move on their own. Exits 0 when the outputs are the same, 1 when
they differ, 2 on a usage error and 3 when a build could not run.
`

// abCmd checks a rewrite against the build it replaces: the same output, and
// whether it is faster (DESIGN §4.24).
func abCmd(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("ab", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, aligned(abUsage)) }
	var o ab.Options
	fs.IntVar(&o.Runs, "n", 10, "timed runs of each build")
	fs.IntVar(&o.Warmup, "warmup", 1, "runs of each build before the timed ones, compared but not timed")
	fs.StringVar(&o.Stdin, "stdin", "", "a file both builds read as standard input")
	fs.DurationVar(&o.Timeout, "timeout", 10*time.Minute, "the longest one run may take")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	fs.Func("ignore", "a regular expression for what may differ within a line, such as a version", func(s string) error {
		re, err := regexp.Compile(s)
		if err != nil {
			return err
		}
		o.Ignore = append(o.Ignore, re)
		return nil
	})
	if err := fs.Parse(args); err != nil {
		return exitCode{2, err}
	}
	rest := fs.Args()
	if len(rest) < 2 {
		return exitCode{2, errors.New("ab: name two builds, GOLD and NEW (" + brand.Slug + " ab --help)")}
	}
	o.Gold, o.New = rest[0], rest[1]
	if len(rest) > 2 {
		if rest[2] != "--" {
			return exitCode{2, fmt.Errorf("ab: put the program's arguments after --, as in %s ab GOLD NEW -- %s", brand.Slug, strings.Join(rest[2:], " "))}
		}
		o.Args = rest[3:]
	}
	if o.Runs < 1 || o.Warmup < 0 {
		return exitCode{2, errors.New("ab: -n wants 1 or more, and --warmup 0 or more")}
	}
	r, err := ab.Run(context.Background(), o)
	if errors.Is(err, ab.ErrRun) {
		return exitCode{3, fmt.Errorf("ab: %w", err)}
	} else if err != nil {
		return fmt.Errorf("ab: %w", err)
	}
	if *asJSON {
		e := json.NewEncoder(stdout)
		e.SetIndent("", "  ")
		if err := e.Encode(r); err != nil {
			return err
		}
	} else {
		printAB(stdout, r)
	}
	switch {
	case r.Varies == "gold":
		return exitCode{1, fmt.Errorf("ab: gold's own output changes from run to run, from run %d, so a difference from new means nothing: --ignore what changes", r.VariesAt)}
	case r.Varies == "new":
		return exitCode{1, fmt.Errorf("ab: new's own output changes from run to run, from run %d, where gold's stays the same", r.VariesAt)}
	case !r.Same:
		return exitCode{1, errors.New("ab: new's output differs from gold's, so its times are for different work")}
	}
	return nil
}

// printAB writes a comparison as a person reads it.
func printAB(w io.Writer, r ab.Report) {
	runs := fmt.Sprintf("%d each, taking turns", r.Runs)
	if r.Warmup > 0 {
		runs += fmt.Sprintf(", after %d each to warm up", r.Warmup)
	}
	fmt.Fprintf(w, "gold    %s\nnew     %s\n", r.Gold, r.New)
	if len(r.Args) > 0 {
		fmt.Fprintf(w, "args    %s\n", strings.Join(r.Args, " "))
	}
	fmt.Fprintf(w, "runs    %s\n\n", runs)
	switch {
	case r.Varies != "":
		fmt.Fprintf(w, "output  %s's run %d differs from its first\n\n", r.Varies, r.VariesAt)
	case r.Diff != nil && r.Diff.Line == 0:
		fmt.Fprintf(w, "output  the same lines, but gold ends with %s and new with %s\n\n", r.Diff.Gold, r.Diff.New)
	case r.Diff != nil:
		fmt.Fprintf(w, "output  differs at line %d:\n  gold  %s\n  new   %s\n\n", r.Diff.Line, r.Diff.Gold, r.Diff.New)
	default:
		out := fmt.Sprintf("%s in %d lines, exit %d", sizeOf(r.GoldOut.Bytes), r.GoldOut.Lines, r.GoldOut.Exit)
		if r.NewOut.Bytes != r.GoldOut.Bytes {
			out += fmt.Sprintf(" (new's is %s, after --ignore)", sizeOf(r.NewOut.Bytes))
		}
		fmt.Fprintf(w, "output  the same: %s\n\n", out)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "\tgold\tnew\tchange")
	row := func(name string, m ab.Metric, show func(float64) string) {
		if m.Gold.Median == 0 && m.New.Median == 0 {
			return
		}
		stat := func(s ab.Stats) string {
			return fmt.Sprintf("%s (%s–%s)", show(s.Median), show(s.Min), show(s.Max))
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", name, stat(m.Gold), stat(m.New), changeOf(m))
	}
	secs := func(s float64) string { return dur(int64(s * 1e9)) }
	row("time", r.Time, secs)
	row("cpu", r.CPU, secs)
	row("memory", r.Memory, memOf)
	tw.Flush()
	// The times measure the machine as much as the builds when it holds
	// them back: say so before the verdict.
	if r.HeldBack {
		fmt.Fprintf(w, "\nnote: this ran in macOS's background state, as when "+brand.Gabriel+" holds a session back, so both builds ran slower and varied more (%s ctl gabriel says why)\n", brand.Slug)
	}
	if r.RunQueue > 0 {
		fmt.Fprintf(w, "\nnote: %.1f CPUs' worth of work waited on %d CPUs, so the times varied more than the builds do\n", r.RunQueue, r.CPUs)
	}
	if r.Same {
		verdict := "neither clearly faster nor slower"
		switch r.Time.Change {
		case ab.Lower:
			verdict = "faster"
		case ab.Higher:
			verdict = "slower"
		}
		fmt.Fprintf(w, "\nnew is %s, with the same output\n", verdict)
	}
}

// changeOf says how far new's median is from gold's, and whether that is
// more than noise.
func changeOf(m ab.Metric) string {
	p := math.Abs(m.Percent)
	way := "less"
	if m.Percent > 0 {
		way = "more"
	}
	switch {
	case m.Change == ab.Same && p < 0.5:
		return "no clear change"
	case m.Change == ab.Same:
		return fmt.Sprintf("no clear change (%.0f%% %s)", p, way)
	}
	return fmt.Sprintf("%.0f%% %s", p, way)
}

// memOf is a peak memory in bytes, to a tenth of a megabyte.
func memOf(b float64) string {
	switch {
	case b < 1<<20:
		return fmt.Sprintf("%.0fK", b/(1<<10))
	case b < 1<<30:
		return fmt.Sprintf("%.1fM", b/(1<<20))
	}
	return fmt.Sprintf("%.2fG", b/(1<<30))
}

// sizeOf is an output's size.
func sizeOf(n int64) string {
	if n < 1<<10 {
		return fmt.Sprintf("%d bytes", n)
	}
	return memOf(float64(n))
}
