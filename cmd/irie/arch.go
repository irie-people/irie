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

const archUsage = "usage: irie ctl arch [--json]"

// ctlArch lists Irie's arch agents (DESIGN §4.11): each one's icon, name
// and work, what he does now, and what you can do with him.
func ctlArch(args []string) error {
	fs := flag.NewFlagSet("ctl arch", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the list as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New(archUsage)
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	var r ipc.ArchResult
	if err := ipc.Call(sock, 10*time.Second, ipc.OpArch, nil, &r); err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(r)
	}
	return printArch(os.Stdout, r)
}

// printArch writes the arch agents in plain words.
func printArch(w io.Writer, r ipc.ArchResult) error {
	for i, a := range r.Agents {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "%s\n", archName(a))
		if a.Status != "" {
			fmt.Fprintf(w, "  %s\n", a.Status)
		}
		if len(a.Actions) > 0 {
			var labels []string
			for _, x := range a.Actions {
				labels = append(labels, x.Label)
			}
			fmt.Fprintf(w, "  You can: %s\n", strings.Join(labels, ", "))
		}
	}
	return nil
}

// archName is how an arch agent is introduced: his icon, his name (and
// nickname) and his work.
func archName(a ipc.ArchAgent) string {
	name := strings.ToUpper(a.Name[:1]) + a.Name[1:]
	if a.Nickname != "" {
		name = a.Nickname + " (" + name + ")"
	}
	if a.Icon != "" {
		name = a.Icon + " " + name
	}
	if a.Title != "" {
		name += ": " + a.Title
	}
	if !a.On {
		name += " (off)"
	}
	return name
}
