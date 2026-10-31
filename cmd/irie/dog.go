package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/repo"
)

// ctlDog is the jot dog's report (DESIGN §4.8): the jots whose turn is over
// but whose work has not landed, open ones first, then those that closed on
// their own in the last day. A jot you closed is done and never listed. The
// core looks at each afresh.
func ctlDog(args []string) error {
	fs := flag.NewFlagSet("ctl dog", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the jots as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New("usage: irie ctl dog [--json]")
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	var r ipc.DogResult
	if err := ipc.Call(sock, 2*time.Minute, ipc.OpDog, nil, &r); err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(r)
	}
	if len(r.Jots) == 0 {
		fmt.Println("every jot is through")
		return nil
	}
	home, _ := os.UserHomeDir()
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "SESSION\tSTATE\tNAME\tLEFT\tWHERE")
	for _, j := range r.Jots {
		for i, w := range j.Loose.Where {
			where := repo.Tilde(w.Path, home)
			if w.Branch != "" {
				where += " (" + w.Branch + ")"
			}
			if i == 0 {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", j.ID, j.State, j.Name, j.Loose.Words(), where)
			} else {
				fmt.Fprintf(tw, "\t\t\t\t%s\n", where)
			}
		}
	}
	return tw.Flush()
}
