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

	"github.com/irie-people/irie/internal/ipc"
)

// psUsage is the part of ctlUsage for agents seen as processes
// (proposals/agent-os.md).
const psUsage = `  irie ctl ps [--json] [--watch]         every agent as a process: its state as a
                                         letter (R working, S done or idle, W
                                         waits on you, H paused, T frozen, I
                                         starting, Z exited, X failed), priority,
                                         model, how full its context is, tokens,
                                         cost, memory, age and name. --watch
                                         draws it again each second
`

// proc is one agent as ps shows it.
type proc struct {
	ID       string   `json:"id"`
	State    string   `json:"state"`  // a letter
	Status   string   `json:"status"` // the state in words
	Priority int      `json:"priority"`
	Model    string   `json:"model,omitempty"`
	Context  *float64 `json:"context,omitempty"` // percent
	Tokens   int64    `json:"tokens"`
	Output   int64    `json:"output"`
	CostUSD  *float64 `json:"cost_usd,omitempty"`
	Memory   int64    `json:"memory,omitempty"`
	Age      float64  `json:"age_s"`
	Name     string   `json:"name"`
	Host     string   `json:"host,omitempty"`
}

func ctlPs(args []string) error {
	fs := flag.NewFlagSet("ctl ps", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the agents as JSON")
	watch := fs.Bool("watch", false, "draw the table again each second")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: irie ctl ps [--json] [--watch]")
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	for {
		var r ipc.ListResult
		if err := ipc.Call(sock, 5*time.Second, ipc.OpList, nil, &r); err != nil {
			return err
		}
		procs := procsOf(r.Sessions, time.Now())
		if *asJSON {
			if err := json.NewEncoder(os.Stdout).Encode(procs); err != nil {
				return err
			}
		} else {
			if *watch {
				fmt.Print("\x1b[H\x1b[2J")
			}
			printProcs(os.Stdout, procs)
		}
		if !*watch {
			return nil
		}
		time.Sleep(time.Second)
	}
}

// procsOf is the rows as processes, in the list's order.
func procsOf(rows []ipc.Session, now time.Time) []proc {
	out := make([]proc, 0, len(rows))
	for _, s := range rows {
		p := proc{
			ID: s.ID, State: stateLetter(s), Status: strings.ReplaceAll(s.State, "_", " "), Priority: s.Priority,
			Model: strings.TrimPrefix(or(s.Model, "+"+s.Profile), "claude-"), Context: s.Context, CostUSD: s.CostUSD,
			Memory: s.Memory, Age: now.Sub(s.Created).Seconds(), Name: s.Name, Host: s.Host,
		}
		if s.Frozen != nil {
			p.Status = "frozen"
		}
		if t := s.Tokens; t != nil {
			p.Tokens, p.Output = t.Input+t.Output+t.CacheRead+t.CacheWrite, t.Output
		}
		out = append(out, p)
	}
	return out
}

// stateLetter is a row's state as ps(1) would say it.
func stateLetter(s ipc.Session) string {
	if s.Frozen != nil {
		return "T"
	}
	switch s.State {
	case ipc.StateWorking:
		return "R"
	case ipc.StateDone, ipc.StateIdle:
		return "S"
	case ipc.StateNeedsYou, ipc.StateStalled:
		return "W"
	case ipc.StatePaused, ipc.StatePausing:
		return "H"
	case ipc.StateStarting:
		return "I"
	case ipc.StateExited:
		return "Z"
	case ipc.StateFailed:
		return "X"
	}
	return "?"
}

// printProcs writes the table, with a line that sums it up first.
func printProcs(w io.Writer, procs []proc) {
	var working, waiting int
	var cost float64
	var mem int64
	for _, p := range procs {
		switch p.State {
		case "R":
			working++
		case "W":
			waiting++
		}
		if p.CostUSD != nil {
			cost += *p.CostUSD
		}
		mem += p.Memory
	}
	fmt.Fprintf(w, "%d agents · %d working · %d wait on you · $%.2f · %s\n", len(procs), working, waiting, cost, bytesOf(mem))
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tS\tPRI\tMODEL\tCTX\tTOKENS\tCOST\tMEM\tAGE\tNAME")
	for _, p := range procs {
		ctx, cost := "-", "-"
		if p.Context != nil {
			ctx = fmt.Sprintf("%.0f%%", *p.Context)
		}
		if p.CostUSD != nil {
			cost = fmt.Sprintf("$%.2f", *p.CostUSD)
		}
		pri := "0"
		if p.Priority > 0 {
			pri = fmt.Sprintf("+%d", p.Priority)
		} else if p.Priority < 0 {
			pri = fmt.Sprint(p.Priority)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", p.ID, p.State, pri, oneLine(p.Model, 18), ctx, count(p.Tokens), cost,
			bytesOf(p.Memory), age(time.Duration(p.Age*float64(time.Second))), oneLine(p.Name, 48))
	}
	tw.Flush()
}

// count is n in a few characters: 950, 12k, 3.4M.
func count(n int64) string {
	switch {
	case n == 0:
		return "-"
	case n < 1000:
		return fmt.Sprint(n)
	case n < 1_000_000:
		return fmt.Sprintf("%.0fk", float64(n)/1e3)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1e6)
}

// bytesOf is n bytes in a few characters.
func bytesOf(n int64) string {
	switch {
	case n <= 0:
		return "-"
	case n < 1<<20:
		return fmt.Sprintf("%dK", n>>10)
	case n < 1<<30:
		return fmt.Sprintf("%dM", n>>20)
	}
	return fmt.Sprintf("%.1fG", float64(n)/(1<<30))
}

// age is d in its largest unit.
func age(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
