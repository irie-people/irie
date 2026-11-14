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
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
)

const gabrielUsage = "usage: " + brand.Slug + " ctl gabriel [on|off|boost [--for 30m]|boost always|boost off] [--json]"

// ctlGabriel is Gabriel's report (DESIGN §4.11): whether you are at the
// computer and it is busy, the tab you are on, what else is in your focus,
// and what he holds back for it. on and off turn him on or off first, in the config file
// (arch.gabriel.on); boost gives your focus the machine first for a while
// whether it is busy or not, boost always does so whenever you are at the
// computer (arch.gabriel.always, in the config file too), and boost off
// ends either.
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
	case verb != "" && verb != "on" && verb != "off" && verb != "boost" && verb != "boost always" && verb != "boost off",
		*boostFor != "" && verb != "boost":
		return errors.New(gabrielUsage)
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	// set writes a key of Gabriel's in the config file, which the core runs
	// on at once; a nil value puts it back to its default.
	set := func(key string, v json.RawMessage) error {
		a := ipc.ConfigArgs{Set: []ipc.ConfigSet{{Key: key, Value: v}}}
		return ipc.Call(sock, 10*time.Second, ipc.OpConfig, a, nil)
	}
	var a ipc.GabrielArgs
	switch verb {
	case "on", "off":
		if err := set("arch.gabriel.on", json.RawMessage(fmt.Sprint(verb == "on"))); err != nil {
			return err
		}
	case "boost":
		a = ipc.GabrielArgs{Boost: true, For: *boostFor}
	case "boost always":
		if err := set("arch.gabriel.always", json.RawMessage("true")); err != nil {
			return err
		}
	case "boost off":
		a = ipc.GabrielArgs{Stop: true}
	}
	var r ipc.GabrielResult
	if err := ipc.Call(sock, 20*time.Second, ipc.OpGabriel, a, &r); err != nil {
		return err
	}
	if verb == "boost off" && r.Always {
		// Boosting always ends too, back to the default.
		if err := set("arch.gabriel.always", nil); err != nil {
			return err
		}
		if err := ipc.Call(sock, 20*time.Second, ipc.OpGabriel, ipc.GabrielArgs{}, &r); err != nil {
			return err
		}
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
		fmt.Fprintf(w, "Gabriel: off. `"+brand.Slug+" ctl gabriel on` turns him on.\n")
		return nil
	case !r.Present:
		fmt.Fprintf(w, "Gabriel: you are away (no keyboard or mouse for %s), so nothing is held back.\n", r.Idle)
	case !r.Busy && (r.BoostUntil != nil || r.Always):
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
	if r.Always {
		fmt.Fprintf(w, "Boost: always, while you are at the computer (arch.gabriel.always); your focus gets the machine first whether it is busy or not, and the rest runs on the efficiency cores, slower even when the machine has room to spare. `"+brand.Slug+" ctl gabriel boost off` ends it.\n")
	} else if r.BoostUntil != nil {
		fmt.Fprintf(w, "Boost: on until %s; your focus gets the machine first whether it is busy or not, and the rest runs on the efficiency cores. `"+brand.Slug+" ctl gabriel boost off` ends it.\n", r.BoostUntil.Local().Format("15:04"))
	}
	if len(r.Freed) > 0 {
		fmt.Fprintf(w, "Freed the memory of idle local models: %s\n", strings.Join(r.Freed, ", "))
	}
	if r.LowPower {
		fmt.Fprintf(w, "Low Power Mode is on, which slows the whole Mac: turn it off in System Settings > Battery for full speed.\n")
	}
	if len(r.Top) > 0 {
		what := "the tab you are on"
		if len(r.Top) > 1 {
			what += ", with the subtasks it delegated"
		}
		switch {
		case r.TopBusy && r.Pressure >= 4:
			what += "; memory is critically short, so every other session but an install steps back for it, the rest of your focus too"
		case r.TopBusy:
			what += fmt.Sprintf("; it is at work (%.0f%% of a CPU now), so every other session but an install steps back for it, the rest of your focus too", r.TopCPU)
		case r.Holding:
			what += "; it never steps back, and while it runs light the rest of your focus keeps its full speed"
		default:
			// Gabriel holds nothing back: the top is there for a promotion.
			what += "; it never steps back, not even for a promotion"
		}
		fmt.Fprintf(w, "On top: %s, %s.\n", sessionNames(r.Top), what)
	}
	if rest := withoutSessions(r.Focus, r.Top); len(rest) > 0 {
		fmt.Fprintf(w, "In your focus: %s\n", sessionNames(rest))
	}
	if len(r.Back) > 0 {
		fmt.Fprintf(w, "Stepping back for the tab you are on: %s\n", sessionNames(r.Back))
	}
	if len(r.Held) > 0 {
		fmt.Fprintf(w, "Sessions held back: %s\n", sessionNames(r.Held))
	}
	if len(r.Apps) > 0 {
		var apps []string
		for _, a := range r.Apps {
			apps = append(apps, fmt.Sprintf("%s (pid %d, %.0f%% of a CPU)", a.Name, a.PID, a.CPU))
		}
		fmt.Fprintf(w, "Apps outside "+brand.Name+" slowed: %s\n", strings.Join(apps, ", "))
	} else if r.Holding && !r.Outside {
		fmt.Fprintf(w, "Apps outside "+brand.Name+": left alone (arch.gabriel.outside = false)\n")
	}
	return nil
}

// withoutSessions is ss without those in drop.
func withoutSessions(ss, drop []ipc.GabrielSession) []ipc.GabrielSession {
	var out []ipc.GabrielSession
	for _, s := range ss {
		if !slices.ContainsFunc(drop, func(d ipc.GabrielSession) bool { return d.ID == s.ID }) {
			out = append(out, s)
		}
	}
	return out
}

func sessionNames(ss []ipc.GabrielSession) string {
	var names []string
	for _, s := range ss {
		names = append(names, s.Name)
	}
	return strings.Join(names, ", ")
}
