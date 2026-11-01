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

const gabrielUsage = "usage: irie ctl gabriel [on|off|boost [--for 30m]|boost off] [--json]"

// ctlGabriel is Gabriel's report (DESIGN §4.11): whether you are at the
// computer and it is busy, what is in your focus, and what he holds back
// for it. on and off turn him on or off first, in the config file
// (arch.gabriel.on); boost gives your focus the machine first for a while
// whether it is busy or not, and boost off ends that.
func ctlGabriel(args []string) error {
	fs := flag.NewFlagSet("ctl gabriel", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the report as JSON")
	boostFor := fs.String("for", "", "with boost: how long, such as 30m (default arch.gabriel.boost)")
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
	verb := strings.Join(words, " ")
	switch {
	case verb != "" && verb != "on" && verb != "off" && verb != "boost" && verb != "boost off",
		*boostFor != "" && verb != "boost":
		return errors.New(gabrielUsage)
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	var a ipc.GabrielArgs
	switch verb {
	case "on", "off":
		set := ipc.ConfigArgs{Set: []ipc.ConfigSet{{Key: "arch.gabriel.on", Value: json.RawMessage(fmt.Sprint(verb == "on"))}}}
		if err := ipc.Call(sock, 10*time.Second, ipc.OpConfig, set, nil); err != nil {
			return err
		}
	case "boost":
		a = ipc.GabrielArgs{Boost: true, For: *boostFor}
	case "boost off":
		a = ipc.GabrielArgs{Stop: true}
	}
	var r ipc.GabrielResult
	if err := ipc.Call(sock, 20*time.Second, ipc.OpGabriel, a, &r); err != nil {
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
	case !r.Busy && r.BoostUntil != nil:
		fmt.Fprintf(w, "Gabriel: you are at the computer and boosting, so what is out of your focus steps back (%s).\n", machine)
	case !r.Busy:
		fmt.Fprintf(w, "Gabriel: you are at the computer and it has room to spare (%s), so nothing is held back.\n", machine)
	default:
		front := ""
		if r.Front != "" {
			front = ", with " + r.Front + " in front"
		}
		fmt.Fprintf(w, "Gabriel: you are at the computer%s and it is busy (%s), so what is out of your focus steps back.\n", front, machine)
	}
	if r.BoostUntil != nil {
		fmt.Fprintf(w, "Boost: on until %s; your focus gets the machine first whether it is busy or not, and the rest runs on the efficiency cores. `irie ctl gabriel boost off` ends it.\n", r.BoostUntil.Local().Format("15:04"))
	}
	if len(r.Freed) > 0 {
		fmt.Fprintf(w, "Freed the memory of idle local models: %s\n", strings.Join(r.Freed, ", "))
	}
	if r.LowPower {
		fmt.Fprintf(w, "Low Power Mode is on, which slows the whole Mac: turn it off in System Settings > Battery for full speed.\n")
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
