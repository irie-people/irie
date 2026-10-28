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

func ctlRoute(args []string) error {
	fs := flag.NewFlagSet("ctl route", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the choice as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	task, err := taskArg(fs.Args())
	if err != nil {
		return err
	}
	if strings.TrimSpace(task) == "" {
		return errors.New("usage: irie ctl route [--json] TASK...")
	}
	if err := up(nil); err != nil {
		return err
	}
	_, p, err := load()
	if err != nil {
		return err
	}
	var ch route.Choice
	if err := ipc.Call(p.Socket(), spawnTimeout, ipc.OpRoute, ipc.RouteArgs{Task: task}, &ch); err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(ch)
	}
	printChoice(os.Stdout, ch)
	return nil
}

// printChoice prints the router's choice and every profile it weighed,
// the cheapest per success first.
func printChoice(w io.Writer, ch route.Choice) {
	fmt.Fprintf(w, "class    %s (%s)\n", ch.Class, ch.Why)
	fmt.Fprintf(w, "choice   %s\n\n", ch.Reason)
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "PROFILE\tACCURACY\tOUTCOMES\tCOST\tPER SUCCESS\tNOTE")
	for _, c := range ch.Candidates {
		note := c.Note
		if !c.Usable && note == "" {
			note = "cannot run it now"
		}
		mark := " "
		if c.Profile == ch.Profile {
			mark = "→"
		}
		fmt.Fprintf(tw, "%s %s\t%s\t%d\t%s\t%s\t%s\n", mark, c.Profile, route.Percent(c.Accuracy), c.Outcomes, route.Relative(c.Cost), route.Relative(c.PerSuccess), note)
	}
	tw.Flush()
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
		return errors.New("usage: irie ctl delegate [--to PROFILE] [--mode MODE] [--timeout D] TASK...")
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
	a := ipc.SpawnArgs{Task: task, Cwd: cwd, Profile: profile, Delegate: true, Parent: os.Getenv("IRIE_SESSION"), PermissionMode: *mode}
	var r ipc.SpawnResult
	if err := ipc.Call(p.Socket(), spawnTimeout, ipc.OpSpawn, a, &r); err != nil {
		var ie *ipc.Error
		if errors.As(err, &ie) && ie.Code == ipc.CodeNeedsStronger {
			return exitCode{exitNeedsStronger, fmt.Errorf("%s; do it yourself", err)}
		}
		return err
	}
	s := r.Session
	fmt.Fprintf(os.Stderr, "irie: delegated to +%s as %s (%s)\n", s.Profile, s.Name, s.ID)
	return waitDone(os.Stdout, p.Socket(), s.ID, *timeout)
}

// waitDone waits until a session's turn ends and prints what its agent said
// last. A session that fails or exits is an error; one still at work at the
// timeout goes on in Irie.
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
			switch s.State {
			case ipc.StateDone, ipc.StateIdle:
				var res ipc.ResultResult
				if err := ipc.Call(sock, 5*time.Second, ipc.OpResult, ipc.ResultArgs{Session: id}, &res); err != nil {
					return err
				}
				fmt.Fprintln(w, strings.TrimRight(res.Text, "\n"))
				return nil
			case ipc.StateFailed, ipc.StateExited:
				msg := fmt.Sprintf("%s %s: %s", s.Name, s.State, s.Detail)
				for _, l := range s.Tail {
					msg += "\n  " + l
				}
				return errors.New(msg)
			case ipc.StateNeedsYou, ipc.StateStalled:
				if told != s.Detail {
					fmt.Fprintf(os.Stderr, "irie: %s needs the user in Irie: %s\n", s.Name, s.Detail)
					told = s.Detail
				}
			}
		}
		if time.Now().After(deadline) {
			return exitCode{exitStillRunning, fmt.Errorf("session %s is still at work after %v; it goes on in Irie", id, timeout)}
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func ctlRate(args []string) error {
	if len(args) != 2 || (args[1] != ipc.Good && args[1] != ipc.Bad) {
		return errors.New("usage: irie ctl rate SESSION good|bad")
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	return ipc.Call(sock, 5*time.Second, ipc.OpRate, ipc.RateArgs{Session: args[0], Verdict: args[1]}, nil)
}
