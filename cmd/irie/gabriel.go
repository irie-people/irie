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

	"github.com/irie-people/irie/internal/ipc"
)

const gabrielUsage = "usage: irie ctl gabriel [on|off] [--json]"

// ctlGabriel is Gabriel's report (DESIGN §4.11): whether you are at the
// computer and it is busy, what is in your focus, and what he holds back
// for it. on and off turn him on or off first, in the config file
// (arch.gabriel.on).
func ctlGabriel(args []string) error {
	fs := flag.NewFlagSet("ctl gabriel", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the report as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	verb := ""
	if fs.NArg() > 0 {
		verb = fs.Arg(0)
		if err := fs.Parse(fs.Args()[1:]); err != nil {
			return err
		}
	}
	if fs.NArg() > 0 || verb != "" && verb != "on" && verb != "off" {
		return errors.New(gabrielUsage)
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	if verb != "" {
		set := ipc.ConfigArgs{Set: []ipc.ConfigSet{{Key: "arch.gabriel.on", Value: json.RawMessage(fmt.Sprint(verb == "on"))}}}
		if err := ipc.Call(sock, 10*time.Second, ipc.OpConfig, set, nil); err != nil {
			return err
		}
	}
	var r ipc.GabrielResult
	if err := ipc.Call(sock, 10*time.Second, ipc.OpGabriel, nil, &r); err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(r)
	}
	return printGabriel(os.Stdout, r)
}

// printGabriel writes Gabriel's report in plain words.
func printGabriel(w io.Writer, r ipc.GabrielResult) error {
	machine := fmt.Sprintf("%.1f of %d CPUs' worth of work waiting", r.RunQueue, r.CPUs)
	switch r.Pressure {
	case 2:
		machine += ", memory under pressure"
	case 4:
		machine += ", memory critically short"
	}
	switch {
	case !r.On:
		fmt.Fprintf(w, "Gabriel: off. `irie ctl gabriel on` turns him on.\n")
		return nil
	case !r.Present:
		fmt.Fprintf(w, "Gabriel: you are away (no keyboard or mouse for %s), so nothing is held back.\n", r.Idle)
	case !r.Busy:
		fmt.Fprintf(w, "Gabriel: you are at the computer and it has room to spare (%s), so nothing is held back.\n", machine)
	default:
		front := ""
		if r.Front != "" {
			front = ", with " + r.Front + " in front"
		}
		fmt.Fprintf(w, "Gabriel: you are at the computer%s and it is busy (%s), so what is out of your focus steps back.\n", front, machine)
	}
	if len(r.Focus) > 0 {
		fmt.Fprintf(w, "In your focus: %s\n", sessionNames(r.Focus))
	}
	if len(r.Held) > 0 {
		fmt.Fprintf(w, "Sessions held back: %s\n", sessionNames(r.Held))
	}
	if len(r.Apps) > 0 {
		var apps []string
		for _, a := range r.Apps {
			apps = append(apps, fmt.Sprintf("%s (pid %d, %.0f%% of a CPU)", a.Name, a.PID, a.CPU))
		}
		fmt.Fprintf(w, "Apps outside Irie slowed: %s\n", strings.Join(apps, ", "))
	} else if r.Holding && !r.Outside {
		fmt.Fprintf(w, "Apps outside Irie: left alone (arch.gabriel.outside = false)\n")
	}
	return nil
}

func sessionNames(ss []ipc.GabrielSession) string {
	var names []string
	for _, s := range ss {
		names = append(names, s.Name)
	}
	return strings.Join(names, ", ")
}
