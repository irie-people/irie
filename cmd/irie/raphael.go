package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

const raphaelUsage = "usage: irie ctl raphael [look|fix N [--yes]|mine N|on|off] [--json]"

// ctlRaphael is Raphael's report (DESIGN §4.11): what his last look at the
// computer found, with his advice, and what he restarted lately. look has
// him look again first; fix N runs finding N's fix, once you have read
// what it runs and said OK; mine N tells him a startup item is yours, so he
// no longer flags it; on and off turn him on or off (arch.raphael.on).
func ctlRaphael(args []string) error {
	fs := flag.NewFlagSet("ctl raphael", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the report as JSON")
	yes := fs.Bool("yes", false, "with fix: run it without asking, once you have read what it runs")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var words []string
	for fs.NArg() > 0 {
		words = append(words, fs.Arg(0))
		if err := fs.Parse(fs.Args()[1:]); err != nil {
			return err
		}
	}
	verb, arg := "", ""
	if len(words) > 0 {
		verb = words[0]
	}
	if len(words) > 1 {
		arg = words[1]
	}
	switch {
	case len(words) > 2,
		verb != "" && verb != "look" && verb != "fix" && verb != "mine" && verb != "on" && verb != "off",
		(verb == "fix" || verb == "mine") != (arg != ""),
		*yes && verb != "fix":
		return errors.New(raphaelUsage)
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	var r ipc.RaphaelResult
	switch verb {
	case "on", "off":
		set := ipc.ConfigArgs{Set: []ipc.ConfigSet{{Key: "arch.raphael.on", Value: json.RawMessage(fmt.Sprint(verb == "on"))}}}
		if err := ipc.Call(sock, 10*time.Second, ipc.OpConfig, set, nil); err != nil {
			return err
		}
	case "look":
		fmt.Fprintln(os.Stderr, "Raffi is looking over your Mac: it takes a minute or two, longer on a busy Mac…")
		if err := ipc.Call(sock, 25*time.Minute, ipc.OpRaphael, ipc.RaphaelArgs{Check: true, Wait: true}, &r); err != nil {
			return err
		}
	case "fix", "mine":
		if err := ipc.Call(sock, 10*time.Second, ipc.OpRaphael, nil, &r); err != nil {
			return err
		}
		f, err := pickFinding(r, arg)
		if err != nil {
			return err
		}
		if verb == "mine" {
			if !f.Mine {
				return fmt.Errorf("%d is not a startup item, so there is nothing to mark as yours", findingNumber(r, f))
			}
			if err := ipc.Call(sock, 10*time.Second, ipc.OpRaphael, ipc.RaphaelArgs{Mine: f.ID}, &r); err != nil {
				return err
			}
			fmt.Println("Raffi no longer flags it: arch.raphael.known lists it now.")
			return nil
		}
		if f.Fix == nil {
			return fmt.Errorf("Raffi has no fix for that one; his advice: %s", f.Advice)
		}
		fmt.Printf("%s\n\nRaffi will run:\n", f.Text)
		for _, line := range f.Fix.Run {
			fmt.Printf("    %s\n", line)
		}
		fmt.Printf("\n%s\n", f.Fix.About)
		if f.Fix.Undo != "" {
			fmt.Printf("To undo it: %s\n", f.Fix.Undo)
		}
		if !*yes {
			ok, err := askOK("\nRun it? [y/N] ")
			if err != nil {
				return err
			}
			if !ok {
				fmt.Println("Left as it is.")
				return nil
			}
		}
		if err := ipc.Call(sock, 50*time.Minute, ipc.OpRaphael, ipc.RaphaelArgs{Fix: f.ID, OK: true, Wait: true}, &r); err != nil {
			return err
		}
		if *asJSON {
			return json.NewEncoder(os.Stdout).Encode(r)
		}
		fmt.Println(r.Did)
		return nil
	}
	if r.Findings == nil {
		if err := ipc.Call(sock, 10*time.Second, ipc.OpRaphael, nil, &r); err != nil {
			return err
		}
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(r)
	}
	return printRaphael(os.Stdout, r)
}

// pickFinding is finding arg of the report: by its number, as the report
// prints it, or by its id.
func pickFinding(r ipc.RaphaelResult, arg string) (ipc.RaphaelFinding, error) {
	if n, err := strconv.Atoi(arg); err == nil {
		if n < 1 || n > len(r.Findings) {
			return ipc.RaphaelFinding{}, fmt.Errorf("Raffi's report has %d findings: give one of their numbers, as `irie ctl raphael` prints them", len(r.Findings))
		}
		return r.Findings[n-1], nil
	}
	for _, f := range r.Findings {
		if f.ID == arg {
			return f, nil
		}
	}
	return ipc.RaphaelFinding{}, fmt.Errorf("Raffi has no finding %q now: `irie ctl raphael` lists them", arg)
}

func findingNumber(r ipc.RaphaelResult, f ipc.RaphaelFinding) int {
	for i, x := range r.Findings {
		if x.ID == f.ID {
			return i + 1
		}
	}
	return 0
}

// askOK asks the person at the terminal, and reports whether they said
// yes. It reads the terminal itself, so a pipe can't answer for them.
func askOK(q string) (bool, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false, errors.New("Raffi needs your OK: run this in your own terminal, or add --yes once you have read what it runs")
	}
	defer tty.Close()
	fmt.Fprint(tty, q)
	line, _ := bufio.NewReader(tty).ReadString('\n')
	a := strings.ToLower(strings.TrimSpace(line))
	return a == "y" || a == "yes", nil
}

// printRaphael writes Raphael's report in plain words.
func printRaphael(w io.Writer, r ipc.RaphaelResult) error {
	head := "❤️‍🩹 Raffi (Raphael)"
	switch {
	case !r.On:
		head += ": off, so nothing restarts a part of Irie that stops answering. `irie ctl raphael on` turns him on."
	case r.Checking:
		head += ": looking over your Mac now."
	case r.Checked == nil:
		head += ": he has not looked over your Mac yet. `irie ctl raphael look` has him look now."
	default:
		head += fmt.Sprintf(": looked over your Mac at %s, in %s.", r.Checked.Local().Format("15:04 on 2 Jan"), r.Took)
	}
	fmt.Fprintln(w, head)
	levels := []struct{ level, title string }{{"warn", "Needs care"}, {"note", "Worth a look"}}
	for _, l := range levels {
		first := true
		for i, f := range r.Findings {
			if f.Level != l.level {
				continue
			}
			if first {
				fmt.Fprintf(w, "\n%s:\n", l.title)
				first = false
			}
			fmt.Fprintf(w, "%3d. %s\n", i+1, f.Text)
			if f.Advice != "" {
				fmt.Fprintf(w, "     %s\n", f.Advice)
			}
			for _, e := range f.Evidence {
				fmt.Fprintf(w, "     - %s\n", e)
			}
			if f.Sure != "" {
				fmt.Fprintf(w, "     %s\n", f.Sure)
			}
			var can []string
			if f.Fix != nil {
				prefix := ""
				if r.Fixing == f.ID {
					prefix = "working on it now; "
				}
				can = append(can, fmt.Sprintf("%s`irie ctl raphael fix %d` (%s)", prefix, i+1, strings.Join(f.Fix.Run, "; ")))
			}
			if f.Mine {
				can = append(can, fmt.Sprintf("yours? `irie ctl raphael mine %d`", i+1))
			}
			if len(can) > 0 {
				fmt.Fprintf(w, "     %s\n", strings.Join(can, " · "))
			}
		}
	}
	if len(r.Fine) > 0 {
		fmt.Fprintln(w, "\nWell:")
		for _, l := range r.Fine {
			fmt.Fprintf(w, "  - %s\n", l)
		}
	}
	if len(r.Restarts) > 0 {
		fmt.Fprintln(w, "\nRestarted when it stopped answering:")
		for _, x := range r.Restarts {
			fmt.Fprintf(w, "  - %s, %s (%s)\n", restartName(x.What), x.At.Local().Format("15:04 on 2 Jan"), x.Report)
		}
	}
	if len(r.Fixes) > 0 {
		fmt.Fprintln(w, "\nFixes you said OK to:")
		for _, d := range r.Fixes {
			fmt.Fprintf(w, "  - %s %s\n", d.At.Local().Format("15:04 on 2 Jan"), d.Said)
		}
	}
	if r.Next != nil && !r.Checking {
		fmt.Fprintf(w, "\nHe looks again by himself at %s.\n", r.Next.Local().Format("15:04 on 2 Jan"))
	}
	return nil
}

func restartName(what string) string {
	switch what {
	case "core":
		return "the core"
	case "sidebar":
		return "a sidebar"
	}
	return "the Mac app"
}
