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

	irie "github.com/irie-people/irie/core"
	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/route"
)

// taskArg is a command's task: its words, or stdin for "-".
func taskArg(words []string) (string, error) {
	task := strings.Join(words, " ")
	if task == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", err
		}
		task = string(b)
	}
	return task, nil
}

const routeUsage = "usage: " + brand.Slug + " ctl route [--json] [--wait D] TASK... | --label small|medium|hard TASK... | --import FILE"

func ctlRoute(args []string) error {
	fs := flag.NewFlagSet("ctl route", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the choice as JSON")
	wait := fs.Duration("wait", 0, "as a subtask its caller waits this long for, as irie ctl delegate does")
	label := fs.String("label", "", "give the task this class, which the router sorts tasks by: small, medium or hard")
	from := fs.String("import", "", "give tasks their classes from a file of JSON lines, each with text and class")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *from != "" {
		if len(fs.Args()) > 0 || *label != "" {
			return errors.New(routeUsage)
		}
		ls, err := readLabels(*from)
		if err != nil {
			return err
		}
		return sendLabels(ls)
	}
	task, err := taskArg(fs.Args())
	if err != nil {
		return err
	}
	if strings.TrimSpace(task) == "" {
		return errors.New(routeUsage)
	}
	if *label != "" {
		return sendLabels([]ipc.Label{{Text: task, Class: *label}})
	}
	if err := up(nil); err != nil {
		return err
	}
	_, p, err := load()
	if err != nil {
		return err
	}
	var ch route.Choice
	if err := ipc.Call(p.Socket(), spawnTimeout, ipc.OpRoute, ipc.RouteArgs{Task: task, Wait: *wait}, &ch); err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(ch)
	}
	printChoice(os.Stdout, ch)
	return nil
}

// labelsPerCall is how many labelled tasks one call sends.
const labelsPerCall = 500

// readLabels reads labelled tasks from a file of JSON lines. A line gives
// its task as text, or as fields.text, and its class as class or label, so
// a bench's eval set reads as it is.
func readLabels(path string) ([]ipc.Label, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []ipc.Label
	for i, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var r struct {
			Text   string `json:"text"`
			Class  string `json:"class"`
			Label  string `json:"label"`
			Fields struct {
				Text string `json:"text"`
			} `json:"fields"`
		}
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return nil, fmt.Errorf("%s:%d: %v", path, i+1, err)
		}
		lb := ipc.Label{Text: r.Text, Class: r.Class}
		if lb.Text == "" {
			lb.Text = r.Fields.Text
		}
		if lb.Class == "" {
			lb.Class = r.Label
		}
		if strings.TrimSpace(lb.Text) == "" || lb.Class == "" {
			return nil, fmt.Errorf("%s:%d: a line needs text and class", path, i+1)
		}
		out = append(out, lb)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s has no labelled tasks", path)
	}
	return out, nil
}

// sendLabels gives the core labelled tasks and says where that leaves the
// router.
func sendLabels(ls []ipc.Label) error {
	if err := up(nil); err != nil {
		return err
	}
	_, p, err := load()
	if err != nil {
		return err
	}
	var r ipc.LabelResult
	for len(ls) > 0 {
		n := min(len(ls), labelsPerCall)
		var part ipc.LabelResult
		if err := ipc.Call(p.Socket(), spawnTimeout, ipc.OpLabel, ipc.LabelArgs{Labels: ls[:n]}, &part); err != nil {
			return err
		}
		added := r.Added + part.Added
		r, r.Added = part, added
		ls = ls[n:]
	}
	fmt.Printf("%d added; %d labelled tasks", r.Added, r.Labels)
	if r.Model != "" {
		fmt.Printf(", %d embedded by %s", r.Embedded, r.Model)
	}
	switch {
	case r.Sorting:
		fmt.Println("; the router gives a task the class of the nearest")
	case r.Note != "":
		fmt.Printf("; %s\n", r.Note)
	default:
		fmt.Println()
	}
	return nil
}

// printChoice prints the router's choice and every profile it weighed,
// the cheapest per success first.
func printChoice(w io.Writer, ch route.Choice) {
	fmt.Fprintf(w, "class    %s (%s)\n", ch.Class, ch.Why)
	fmt.Fprintf(w, "choice   %s\n\n", ch.Reason)
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "PROFILE\tACCURACY\tFLOOR\tOUTCOMES\tCOST\tPER SUCCESS\tTIME\tNOTE")
	for _, c := range ch.Candidates {
		note := c.Note
		if !c.Usable && note == "" {
			note = "cannot run it now"
		}
		mark := " "
		if c.Profile == ch.Profile {
			mark = "→"
		}
		floor := "-"
		if c.Floor > 0 {
			floor = route.Percent(c.Floor)
		}
		took := "-"
		if c.Timed > 0 {
			took = answerTime(c.Time)
		}
		fmt.Fprintf(tw, "%s %s\t%s\t%s\t%d\t%s\t%s\t%s\t%s\n", mark, c.Profile, route.Percent(c.Accuracy), floor, c.Outcomes, route.Relative(c.Cost), route.Relative(c.PerSuccess), took, note)
	}
	tw.Flush()
}

// answerTime renders how long a profile takes to answer: 17s, 7m, 1h30m.
func answerTime(d time.Duration) string {
	if d < time.Minute {
		return d.Round(time.Second).String()
	}
	return strings.TrimSuffix(d.Round(time.Minute).String(), "0s")
}

// Exit codes of `irie ctl delegate`.
const (
	exitNeedsStronger = 3
	exitStillRunning  = 4
)

func ctlDelegate(args []string) error {
	fs := flag.NewFlagSet("ctl delegate", flag.ContinueOnError)
	to := fs.String("to", "", "run it on this profile, not the cheapest accurate enough")
	mode := fs.String("mode", "", "its permission mode; default route.delegate_mode")
	timeout := fs.Duration("timeout", 10*time.Minute, "how long to wait for it")
	if err := fs.Parse(args); err != nil {
		return err
	}
	task, err := taskArg(fs.Args())
	if err != nil {
		return err
	}
	if strings.TrimSpace(task) == "" {
		return errors.New("usage: " + brand.Slug + " ctl delegate [--to PROFILE] [--mode MODE] [--timeout D] TASK...")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := up(nil); err != nil {
		return err
	}
	_, p, err := load()
	if err != nil {
		return err
	}
	profile := strings.TrimPrefix(*to, "+")
	if profile == "" {
		profile = config.Auto
	}
	a := ipc.SpawnArgs{Task: task, Cwd: cwd, Profile: profile, Delegate: true, Parent: os.Getenv(brand.Env("SESSION")), PermissionMode: *mode, Wait: *timeout}
	var r ipc.SpawnResult
	if err := ipc.Call(p.Socket(), spawnTimeout, ipc.OpSpawn, a, &r); err != nil {
		var ie *ipc.Error
		if errors.As(err, &ie) && ie.Code == ipc.CodeNeedsStronger {
			return exitCode{exitNeedsStronger, fmt.Errorf("%s; do it yourself", err)}
		}
		return err
	}
	s := r.Session
	fmt.Fprintf(os.Stderr, brand.Slug+": delegated to +%s as %s (%s)\n", s.Profile, s.Name, s.ID)
	err = waitDone(os.Stdout, p.Socket(), s.ID, *timeout)
	var ec exitCode
	if profile != config.Auto || !errors.As(err, &ec) || ec.code != exitStillRunning {
		return err
	}
	// Still at work after the wait: the router stops it and hands the
	// subtask to the next profile accurate enough, which answers in time.
	a.Instead = s.ID
	var h ipc.SpawnResult
	if herr := ipc.Call(p.Socket(), spawnTimeout, ipc.OpSpawn, a, &h); herr != nil {
		// It may have answered meanwhile.
		if waitDone(os.Stdout, p.Socket(), s.ID, time.Second) == nil {
			return nil
		}
		fmt.Fprintf(os.Stderr, brand.Slug+": it could not be handed on (%v)\n", herr)
		return err
	}
	fmt.Fprintf(os.Stderr, brand.Slug+": +%s was still at work after %s, so it handed the subtask on to +%s as %s (%s)\n", s.Profile, answerTime(*timeout), h.Session.Profile, h.Session.Name, h.Session.ID)
	return waitDone(os.Stdout, p.Socket(), h.Session.ID, *timeout)
}

// waitDone waits until a session's turn ends, with no helper of its still
// at work, and prints what its agent said last. A session that fails or
// exits is an error; one still at work at the timeout goes on in Irie.
func waitDone(w io.Writer, sock, id string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	told := ""
	for {
		var lr ipc.ListResult
		if err := ipc.Call(sock, 5*time.Second, ipc.OpList, nil, &lr); err != nil {
			return err
		}
		for _, s := range lr.Sessions {
			if s.ID != id {
				continue
			}
			switch {
			case s.Subs != nil:
				// Its turn is over, but its helpers will wake it with what
				// they found.
			case s.State == ipc.StateDone, s.State == ipc.StateIdle:
				var res ipc.ResultResult
				if err := ipc.Call(sock, 5*time.Second, ipc.OpResult, ipc.ResultArgs{Session: id}, &res); err != nil {
					return err
				}
				fmt.Fprintln(w, strings.TrimRight(res.Text, "\n"))
				return nil
			case s.State == ipc.StateFailed, s.State == ipc.StateExited:
				msg := fmt.Sprintf("%s %s: %s", s.Name, s.State, s.Detail)
				for _, l := range s.Tail {
					msg += "\n  " + l
				}
				return errors.New(msg)
			case s.State == ipc.StateNeedsYou, s.State == ipc.StateStalled:
				if told != s.Detail {
					fmt.Fprintf(os.Stderr, brand.Slug+": %s needs the user in "+brand.Name+": %s\n", s.Name, s.Detail)
					told = s.Detail
				}
			}
		}
		if time.Now().After(deadline) {
			return exitCode{exitStillRunning, fmt.Errorf("session %s is still at work after %v; it goes on in "+brand.Name, id, timeout)}
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func ctlRate(args []string) error {
	if len(args) != 2 || (args[1] != ipc.Good && args[1] != ipc.Bad) {
		return errors.New("usage: " + brand.Slug + " ctl rate SESSION good|bad")
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	return ipc.Call(sock, 5*time.Second, ipc.OpRate, ipc.RateArgs{Session: args[0], Verdict: args[1]}, nil)
}

func ctlBench(args []string) error {
	fs := flag.NewFlagSet("ctl bench", flag.ContinueOnError)
	runs := fs.Int("runs", 0, "how many sessions run the task; default bench.runs")
	judge := fs.String("judge", "", "the profile that judges them; default bench.judge")
	wait := fs.Bool("wait", false, "wait until the bench is judged, and print the verdict")
	asJSON := fs.Bool("json", false, "print as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	task, err := taskArg(fs.Args())
	if err != nil {
		return err
	}
	if err := up(nil); err != nil {
		return err
	}
	_, p, err := load()
	if err != nil {
		return err
	}
	sock := p.Socket()
	if strings.TrimSpace(task) == "" {
		var r ipc.BenchResult
		if err := ipc.Call(sock, 5*time.Second, ipc.OpBench, ipc.BenchArgs{}, &r); err != nil {
			return err
		}
		if *asJSON {
			return json.NewEncoder(os.Stdout).Encode(r.Benches)
		}
		if len(r.Benches) == 0 {
			fmt.Println("no benches yet; " + irie.BenchUsage)
			return nil
		}
		for _, b := range r.Benches {
			fmt.Println(irie.BenchLine(b))
		}
		return nil
	}
	home, _ := os.UserHomeDir()
	first, _, _ := strings.Cut(task, "\n")
	a, err := irie.BenchTask(first, task, home)
	if err != nil {
		return fmt.Errorf("%v\n%s", err, irie.BenchUsage)
	}
	a.Runs, a.Judge = *runs, strings.TrimPrefix(*judge, "+")
	var r ipc.BenchResult
	// Its runs start one after another, and the first may load a local
	// model, which can take minutes.
	if err := ipc.Call(sock, 10*time.Minute, ipc.OpBench, a, &r); err != nil {
		return err
	}
	// A bench's id shows that bench, and with --wait waits for its verdict.
	if r.Shown && r.Bench != nil {
		if b := *r.Bench; !*wait || b.State == ipc.BenchDone || b.State == ipc.BenchFailed {
			if *asJSON {
				return json.NewEncoder(os.Stdout).Encode(b)
			}
			printBench(os.Stdout, b)
			if *wait && b.State == ipc.BenchFailed {
				return errors.New(b.Problem)
			}
			return nil
		}
	} else if !*wait {
		if *asJSON {
			return json.NewEncoder(os.Stdout).Encode(r.Bench)
		}
		fmt.Println(irie.BenchStarted(r.Bench))
		return nil
	}
	id := r.Bench.ID
	for {
		time.Sleep(5 * time.Second)
		var lr ipc.BenchResult
		if err := ipc.Call(sock, 5*time.Second, ipc.OpBench, ipc.BenchArgs{}, &lr); err != nil {
			// The core may be restarting; it carries on with the bench.
			continue
		}
		for _, b := range lr.Benches {
			if b.ID != id || (b.State != ipc.BenchDone && b.State != ipc.BenchFailed) {
				continue
			}
			if *asJSON {
				return json.NewEncoder(os.Stdout).Encode(b)
			}
			printBench(os.Stdout, b)
			if b.State == ipc.BenchFailed {
				return errors.New(b.Problem)
			}
			return nil
		}
	}
}

// printBench prints a judged bench: each run's score and what it missed,
// what the judge made, and the playbook's size.
func printBench(w io.Writer, b ipc.Bench) {
	fmt.Fprintf(w, "bench %s: %s, +%s, %s work\n", b.ID, b.State, b.Profile, b.Class)
	for i, r := range b.Runs {
		score := "-"
		if r.Score != nil {
			score = fmt.Sprint(*r.Score)
		}
		fmt.Fprintf(w, "  run %d  %s  %-4s %s  %s\n", i+1, r.Session, r.Verdict, score, r.Missed)
	}
	if b.JudgeID != "" {
		fmt.Fprintf(w, "  judge  %s (+%s)\n", b.JudgeID, b.Judge)
	}
	if b.Final != "" {
		fmt.Fprintf(w, "  result %s\n", b.Final)
	}
	if b.State == ipc.BenchDone {
		fmt.Fprintf(w, "  playbook: %d rules for +%s\n", b.Rules, b.Profile)
	}
}
