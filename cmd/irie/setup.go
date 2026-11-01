package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/ready"
)

const setupUsage = "usage: irie setup [--check] [--json] [--yes] | irie setup local [--model MODEL] [--yes]"

func setup(args []string) error {
	if len(args) > 0 && args[0] == "local" {
		return setupLocal(args[1:], os.Stdin, os.Stdout)
	}
	return setupIrie(args, os.Stdin, os.Stdout)
}

// setupIrie is `irie setup`: it checks that this computer has what Irie
// needs (DESIGN §2.7), and offers to fix each part it lacks, one at a
// time. Nothing is installed or changed without a yes.
func setupIrie(args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	check := fs.Bool("check", false, "only check: change nothing")
	asJSON := fs.Bool("json", false, "print the check as JSON, and change nothing")
	yes := fs.Bool("yes", false, "fix what is missing without asking")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New(setupUsage)
	}
	cfg, _, err := load()
	if err != nil {
		return err
	}
	o := readyOptions(cfg)
	if *asJSON {
		r := ready.Check(o)
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(r); err != nil {
			return err
		}
		if !r.Ready() {
			return exitCode{1, errors.New("not ready")}
		}
		return nil
	}
	fmt.Fprintf(stdout, "Checking what Irie needs on this computer…\n\n")
	r := ready.Check(o)
	printReport(stdout, r)
	if !*check {
		r = fixAll(o, r, stdin, stdout, *yes, nil)
	}
	fmt.Fprintln(stdout)
	if !r.Ready() {
		it := "it"
		if len(r.Blocking()) > 1 {
			it = "them"
		}
		next := "Run `irie setup` again once you have " + it + "."
		if *check {
			next = "`irie setup` offers to fix " + it + "."
		}
		return exitCode{1, fmt.Errorf("Irie can't start agents yet: %s. %s", missing(r.Blocking()), next)}
	}
	fmt.Fprintln(stdout, "Everything irie. "+howToOpen())
	fmt.Fprintln(stdout, "Free local models for the small jobs: irie setup local")
	return nil
}

// firstRun is what `irie` does when no Irie is running: it checks this
// computer, and when something is missing, says what and offers each fix,
// as `irie setup` does. Only a missing tmux keeps Irie from starting.
func firstRun(cfg config.Config, stdin io.Reader, stdout io.Writer) error {
	o := readyOptions(cfg)
	r := ready.Check(o)
	if r.Ready() {
		return nil
	}
	fmt.Fprintln(stdout, "Irie needs a few things before it can start agents:")
	fmt.Fprintln(stdout)
	printReport(stdout, r)
	tty := isTerminal(stdin)
	if tty {
		// Only what blocks: `irie setup` offers the rest.
		r = fixAll(o, r, stdin, stdout, false, func(it ready.Item) bool { return it.Blocks })
	} else {
		fmt.Fprintln(stdout, "\n`irie setup`, in a terminal, offers to fix each one.")
	}
	if it, _ := r.Item(ready.TmuxID); it.Status == ready.Missing {
		return exitCode{1, errors.New("Irie can't start without tmux. Run `irie setup` once you have it")}
	}
	if !r.Ready() {
		fmt.Fprintln(stdout, "\nStarting Irie anyway. An agent that needs what is missing won't start; `irie setup` checks again.")
		if tty {
			time.Sleep(2 * time.Second)
		}
	}
	return nil
}

func readyOptions(cfg config.Config) ready.Options {
	self, _ := self()
	return ready.Options{
		Env:    os.Environ(),
		Tmux:   cfg.Tmux.Path,
		Claude: cfg.Profiles["claude"].Command,
		Self:   self,
		SignIn: true,
	}
}

// coreAnswers reports whether an Irie is running.
func coreAnswers(p paths.Paths) bool {
	var r ipc.HelloResult
	return ipc.Call(p.Socket(), 300*time.Millisecond, ipc.OpHello, ipc.HelloArgs{Client: "setup", API: ipc.API}, &r) == nil
}

// printReport writes one line per need: its mark, its name and version,
// and what it is for, or what is wrong.
func printReport(w io.Writer, r ready.Report) {
	width := 0
	label := func(it ready.Item) string {
		if it.Version != "" && it.Status == ready.OK {
			return it.Name + " " + it.Version
		}
		return it.Name
	}
	for _, it := range r.Items {
		width = max(width, len([]rune(label(it))))
	}
	for _, it := range r.Items {
		say := it.For
		if it.Problem != "" {
			say = it.Problem
		}
		l := label(it)
		fmt.Fprintf(w, "  %s %s%s  %s\n", mark(it), l, strings.Repeat(" ", width-len([]rune(l))), say)
	}
}

func mark(it ready.Item) string {
	switch it.Status {
	case ready.OK:
		return "✓"
	case ready.Missing:
		if !it.Blocks {
			return "·"
		}
		return "✕"
	case ready.Warn:
		return "!"
	}
	return "?"
}

// runFix runs a fix's command in the terminal; tests replace it, so that
// no test runs an installer.
var runFix = func(argv []string, stdout io.Writer) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, stdout, os.Stderr
	cmd.Env = ready.PathEnv(os.Environ(), os.Getenv("HOME"))
	return cmd.Run()
}

// fixAll offers the fix of each need that is not met and that only lets
// through (nil is every need), in order, checking again after each, so
// that installing Claude Code brings the offer to sign in. Each fix is
// offered once.
func fixAll(o ready.Options, r ready.Report, stdin io.Reader, stdout io.Writer, yes bool, only func(ready.Item) bool) ready.Report {
	in := bufio.NewReader(stdin)
	ask := func(q string) bool {
		if yes {
			return true
		}
		if !isTerminal(stdin) {
			return false
		}
		fmt.Fprintf(stdout, "%s [Y/n] ", q)
		line, err := in.ReadString('\n')
		if err != nil && line == "" {
			fmt.Fprintln(stdout)
			return false
		}
		a := strings.ToLower(strings.TrimSpace(line))
		return a == "" || a == "y" || a == "yes"
	}
	offered := map[string]bool{}
	for {
		var next *ready.Item
		for i, it := range r.Items {
			if it.Status != ready.OK && it.Fix != nil && !offered[it.ID] && (only == nil || only(it)) {
				next = &r.Items[i]
				break
			}
		}
		if next == nil {
			return r
		}
		offered[next.ID] = true
		f := next.Fix
		fmt.Fprintln(stdout)
		switch {
		case f.Line != "":
			fmt.Fprintln(stdout, headline(*next))
			if !ask(f.Say + "?") {
				fmt.Fprintf(stdout, "  Left as it is. To do it yourself: %s\n", f.Show)
				continue
			}
			if err := ready.AddLine(f.File, f.Line); err != nil {
				fmt.Fprintf(stdout, "  Couldn't: %v\n", err)
				continue
			}
			fmt.Fprintf(stdout, "  Added. Terminals you open from now on find it.\n")
		case f.Argv != nil:
			fmt.Fprintf(stdout, "%s\n  %s\n", headline(*next), f.Show)
			if !ask(f.Say + " now?") {
				fmt.Fprintf(stdout, "  Left as it is.%s\n", linkLine(f.Link))
				continue
			}
			if err := runFix(f.Argv, stdout); err != nil {
				fmt.Fprintf(stdout, "  That didn't work (%v).%s\n", err, linkLine(f.Link))
			}
		default:
			fmt.Fprintf(stdout, "%s %s.%s\n", headline(*next), f.Say, linkLine(f.Link))
			continue
		}
		r = ready.Check(o)
		if it, ok := r.Item(next.ID); ok {
			fmt.Fprintf(stdout, "  %s %s\n", mark(it), statusLine(it))
		}
	}
}

// missing says what is missing: "tmux is missing", "tmux and Claude Code
// are missing".
func missing(items []ready.Item) string {
	var names []string
	for _, it := range items {
		if it.ID == ready.SignInID {
			names = append(names, "a sign-in to Claude Code")
		} else {
			names = append(names, it.Name)
		}
	}
	switch len(names) {
	case 0:
		return "nothing is missing"
	case 1:
		return names[0] + " is missing"
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1] + " are missing"
}

// headline says what is wrong with a need in one sentence: "tmux isn't
// installed.", "Claude Code isn't signed in."
func headline(it ready.Item) string {
	switch {
	case it.Problem == "not installed":
		return it.Name + " isn't installed."
	case strings.Contains(strings.ToLower(it.Problem), strings.ToLower(it.Name)):
		return sentence(it.Problem) + "."
	}
	return it.Name + ": " + it.Problem + "."
}

func statusLine(it ready.Item) string {
	if it.Status == ready.OK {
		if it.Version != "" {
			return it.Name + " " + it.Version
		}
		return it.Name
	}
	return it.Name + ": " + it.Problem
}

func linkLine(link string) string {
	if link == "" {
		return ""
	}
	return " How: " + link
}

// sentence starts s with a capital.
func sentence(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// howToOpen says how to open Irie here: from Spotlight when Irie.app is
// installed, else by typing irie.
func howToOpen() string {
	if runtime.GOOS == "darwin" {
		for _, d := range []string{filepath.Join(os.Getenv("HOME"), "Applications"), "/Applications"} {
			if fi, err := os.Stat(filepath.Join(d, "Irie.app")); err == nil && fi.IsDir() {
				return "Open Irie from Spotlight, or type irie in a terminal."
			}
		}
	}
	return "Type irie to start."
}
