package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	irie "github.com/irie-people/irie/core"
	"github.com/irie-people/irie/internal/ipc"
)

// raceUsage is the part of ctlUsage for races (proposals/agent-os.md).
const raceUsage = `  irie ctl race [--from SESSION[@AT]] [--check CMD] [--timeout D] [--json] +PROFILE... [@REPO] TASK...
                                         race one task on several profiles, a
                                         session and worktree each: new in @REPO,
                                         or forked from a point of a session's
                                         context. When a racer's turn ends, CMD
                                         runs in its folder (exit 0 passes). It
                                         prints each racer's time, tokens, cost
                                         and result, best first, and keeps them
                                         in the state dir's races folder
`

// Race is one race and how each racer did.
type Race struct {
	ID      string    `json:"id"`
	Task    string    `json:"task"`
	From    string    `json:"from,omitempty"`
	Repo    string    `json:"repo,omitempty"`
	Check   string    `json:"check,omitempty"`
	Started time.Time `json:"started"`
	Racers  []*Racer  `json:"racers"`
}

// Racer is one profile's run.
type Racer struct {
	Profile string       `json:"profile"`
	Session string       `json:"session,omitempty"`
	Name    string       `json:"name,omitempty"`
	Cwd     string       `json:"cwd,omitempty"`
	Model   string       `json:"model,omitempty"`
	Started time.Time    `json:"started"`
	Ended   time.Time    `json:"ended,omitzero"`
	Seconds float64      `json:"seconds"`
	State   string       `json:"state"` // how its turn ended: done, needs_you, exited, failed, closed, timeout, or not started
	Tokens  int64        `json:"tokens"`
	Output  int64        `json:"output_tokens"`
	CostUSD *float64     `json:"cost_usd,omitempty"`
	Check   *CheckResult `json:"check,omitempty"`
	Error   string       `json:"error,omitempty"`
	last    ipc.Session
	done    bool
	worked  bool // seen at work: a resumed fork is idle for a moment before its prompt
}

// CheckResult is how a racer's folder did under the race's check.
type CheckResult struct {
	Pass    bool    `json:"pass"`
	Exit    int     `json:"exit"`
	Seconds float64 `json:"seconds"`
	Tail    string  `json:"tail,omitempty"`
}

// checkTimeout bounds one racer's check.
const checkTimeout = 15 * time.Minute

func ctlRace(args []string) error {
	fs := flag.NewFlagSet("ctl race", flag.ContinueOnError)
	from := fs.String("from", "", "fork every racer from this point of a session's context, SESSION[@AT]")
	check := fs.String("check", "", "a shell command run in each racer's folder once its turn ends; exit 0 passes")
	timeout := fs.Duration("timeout", time.Hour, "stop waiting for racers after this long")
	asJSON := fs.Bool("json", false, "print the race as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var profiles []string
	rest := fs.Args()
	for len(rest) > 0 && strings.HasPrefix(rest[0], "+") && len(rest[0]) > 1 {
		profiles = append(profiles, rest[0][1:])
		rest = rest[1:]
	}
	home, _ := os.UserHomeDir()
	var spawn ipc.SpawnArgs
	words, err := irie.SpawnTokens(&spawn, rest, home)
	if err != nil {
		return err
	}
	task := strings.Join(words, " ")
	if task == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		task = string(b)
	}
	if len(profiles) == 0 || strings.TrimSpace(task) == "" {
		return errors.New("usage: irie ctl race [--from SESSION[@AT]] [--check CMD] [--timeout D] [--json] +PROFILE... [@REPO] TASK...")
	}
	if *from != "" && spawn.Repo != "" {
		return errors.New("a race starts from a session's context or in @REPO, not both")
	}
	_, p, err := load()
	if err != nil {
		return err
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	now := time.Now()
	race := &Race{ID: now.UTC().Format("20060102-150405"), Task: task, From: *from, Repo: spawn.Repo, Check: *check, Started: now}
	for _, profile := range profiles {
		r := &Racer{Profile: profile, Started: time.Now(), State: "not started"}
		race.Racers = append(race.Racers, r)
		name := fmt.Sprintf("race %s +%s", race.ID, profile)
		var s ipc.Session
		if *from != "" {
			session, at := splitAt(*from)
			var f ipc.ForkResult
			err = ipc.Call(sock, spawnTimeout, ipc.OpFork, ipc.ForkArgs{Session: session, At: at, Profile: profile, Task: task, Name: name}, &f)
			s = f.Session
		} else {
			a := spawn
			a.Task, a.Profile, a.Name = task, profile, name
			var sr ipc.SpawnResult
			err = ipc.Call(sock, spawnTimeout, ipc.OpSpawn, a, &sr)
			s = sr.Session
		}
		if err != nil {
			r.Error, r.done = err.Error(), true
			fmt.Fprintf(os.Stderr, "+%s did not start: %v\n", profile, err)
			continue
		}
		r.Session, r.Name, r.Cwd, r.Model, r.State, r.last = s.ID, s.Name, s.Cwd, s.Model, s.State, s
		fmt.Fprintf(os.Stderr, "+%s is %s, in %s\n", profile, s.ID, s.Cwd)
	}
	wait(sock, race, *timeout)
	for _, r := range race.Racers {
		if *check != "" && r.Cwd != "" && r.State != "not started" {
			r.Check = runCheck(*check, r.Cwd)
		}
	}
	rank(race)
	dir := filepath.Join(p.State, "races")
	if err := saveRace(dir, race); err != nil {
		fmt.Fprintf(os.Stderr, "the race was not kept: %v\n", err)
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(race)
	}
	printRace(os.Stdout, race)
	fmt.Fprintf(os.Stderr, "kept in %s\n", filepath.Join(dir, race.ID+".md"))
	return nil
}

// over says whether a racer's turn is over: it waits on you, or its agent
// has ended, or, once it has been seen at work, it is done.
func over(state string, worked bool) bool {
	switch state {
	case ipc.StateNeedsYou, ipc.StateStalled, ipc.StateExited, ipc.StateFailed:
		return true
	case ipc.StateDone, ipc.StateIdle:
		return worked
	}
	return false
}

// wait follows the racers until each one's turn is over, it has gone from
// the list (a session that closes itself when done), or the timeout.
func wait(sock string, race *Race, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for {
		left := 0
		var l ipc.ListResult
		err := ipc.Call(sock, 5*time.Second, ipc.OpList, nil, &l)
		now := time.Now()
		for _, r := range race.Racers {
			if r.done {
				continue
			}
			if err != nil {
				left++
				continue
			}
			i := slices.IndexFunc(l.Sessions, func(s ipc.Session) bool { return s.ID == r.Session })
			if i < 0 {
				finish(r, "closed", now)
				continue
			}
			row := l.Sessions[i]
			r.last = row
			r.worked = r.worked || row.State == ipc.StateWorking
			if over(row.State, r.worked) {
				finish(r, row.State, now)
			} else {
				left++
			}
		}
		if left == 0 {
			return
		}
		if now.After(deadline) {
			for _, r := range race.Racers {
				if !r.done {
					finish(r, "timeout", now)
				}
			}
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// finish records how a racer ended, from the last row seen of it.
func finish(r *Racer, state string, at time.Time) {
	r.done, r.State, r.Ended = true, state, at
	r.Seconds = at.Sub(r.Started).Seconds()
	if t := r.last.Tokens; t != nil {
		r.Tokens, r.Output = t.Input+t.Output+t.CacheRead+t.CacheWrite, t.Output
	}
	r.CostUSD = r.last.CostUSD
	if r.last.Model != "" {
		r.Model = r.last.Model
	}
}

// runCheck runs the check in dir, in a process group of its own that a
// timeout ends whole.
func runCheck(command, dir string) *CheckResult {
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	start := time.Now()
	err := cmd.Run()
	r := &CheckResult{Pass: err == nil, Seconds: time.Since(start).Seconds()}
	if cmd.ProcessState != nil {
		r.Exit = cmd.ProcessState.ExitCode()
	}
	if b := bytes.TrimSpace(out.Bytes()); len(b) > 0 {
		if len(b) > 2000 {
			b = b[len(b)-2000:]
		}
		r.Tail = string(b)
	}
	return r
}

// rank puts the racers best first: those whose turn ended done before the
// rest, then those that passed the check, then the fastest, then the
// cheapest.
func rank(race *Race) {
	score := func(r *Racer) int {
		s := 0
		if r.State == ipc.StateDone || r.State == ipc.StateIdle || r.State == "closed" {
			s += 2
		}
		if r.Check != nil && r.Check.Pass {
			s += 4
		}
		return s
	}
	cost := func(r *Racer) float64 {
		if r.CostUSD == nil {
			return 0
		}
		return *r.CostUSD
	}
	slices.SortStableFunc(race.Racers, func(a, b *Racer) int {
		switch {
		case score(a) != score(b):
			return score(b) - score(a)
		case a.Seconds != b.Seconds:
			if a.Seconds < b.Seconds {
				return -1
			}
			return 1
		case cost(a) < cost(b):
			return -1
		case cost(a) > cost(b):
			return 1
		}
		return 0
	})
}

// printRace writes the race's table, best first.
func printRace(w io.Writer, race *Race) {
	fmt.Fprintf(w, "Race %s: %s\n", race.ID, oneLine(race.Task, 80))
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "#\tPROFILE\tMODEL\tENDED\tTIME\tTOKENS\tOUT\tCOST\tCHECK\tSESSION")
	for i, r := range race.Racers {
		cost, check := "-", "-"
		if r.CostUSD != nil {
			cost = fmt.Sprintf("$%.2f", *r.CostUSD)
		}
		if r.Check != nil {
			check = "fail"
			if r.Check.Pass {
				check = "pass"
			}
		}
		fmt.Fprintf(tw, "%d\t+%s\t%s\t%s\t%.0fs\t%s\t%s\t%s\t%s\t%s\n", i+1, r.Profile, strings.TrimPrefix(r.Model, "claude-"),
			strings.ReplaceAll(r.State, "_", " "), r.Seconds, count(r.Tokens), count(r.Output), cost, check, r.Session)
	}
	tw.Flush()
}

// saveRace keeps the race as JSON and as Markdown, which reads as a page.
func saveRace(dir string, race *Race) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(race, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, race.ID+".json"), append(b, '\n'), 0o600); err != nil {
		return err
	}
	var md bytes.Buffer
	fmt.Fprintf(&md, "# Race %s\n\n%s\n\n", race.ID, strings.TrimSpace(race.Task))
	if race.Check != "" {
		fmt.Fprintf(&md, "Check: `%s`\n\n", race.Check)
	}
	md.WriteString("| # | Profile | Model | Ended | Time | Tokens | Output | Cost | Check |\n|---|---|---|---|---|---|---|---|---|\n")
	for i, r := range race.Racers {
		cost, check := "", ""
		if r.CostUSD != nil {
			cost = fmt.Sprintf("$%.2f", *r.CostUSD)
		}
		if r.Check != nil {
			check = map[bool]string{true: "pass", false: "fail"}[r.Check.Pass]
		}
		fmt.Fprintf(&md, "| %d | +%s | %s | %s | %.0f s | %s | %s | %s | %s |\n", i+1, r.Profile, r.Model,
			strings.ReplaceAll(r.State, "_", " "), r.Seconds, count(r.Tokens), count(r.Output), cost, check)
	}
	return os.WriteFile(filepath.Join(dir, race.ID+".md"), md.Bytes(), 0o600)
}
