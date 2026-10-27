package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/paths"
	"github.com/irie-hq/irie/internal/ui"
)

const ctlUsage = `usage: irie ctl <command>

  irie ctl list [--json]                 the sessions and their states
  irie ctl spawn [--wait] [--cwd DIR] [--name NAME] [@REPO[!]] [+PROFILE] TASK...
                                         start a task and print its session id,
                                         starting Irie if needed. @REPO runs it in
                                         a new worktree of that repo, @REPO! in the
                                         repo's own checkout; +PROFILE picks the
                                         agent. TASK - reads the task from stdin.
                                         --wait waits until the agent is ready,
                                         and fails if it is not
  irie ctl show [--focus] SESSION        show a session in the slot
  irie ctl watch [--session SESSION]     print events as NDJSON until interrupted
`

// ctl is `irie ctl`: Irie from scripts, through the core's socket.
func ctl(args []string) error {
	if len(args) == 0 {
		return errors.New(ctlUsage)
	}
	switch args[0] {
	case "list":
		return ctlList(args[1:])
	case "spawn":
		return ctlSpawn(args[1:])
	case "show":
		return ctlShow(args[1:])
	case "watch":
		return ctlWatch(args[1:])
	}
	return fmt.Errorf("unknown ctl command %q\n\n%s", args[0], ctlUsage)
}

// socket is the core's socket. A core that does not answer is an error
// that says how to start one.
func socket() (string, error) {
	p, err := paths.FromEnv()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(p.Socket()); err != nil {
		return "", errors.New("Irie is not running; `irie up` starts it")
	}
	return p.Socket(), nil
}

func ctlList(args []string) error {
	fs := flag.NewFlagSet("ctl list", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the list as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	var r ipc.ListResult
	if err := ipc.Call(sock, 5*time.Second, ipc.OpList, nil, &r); err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(r)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "SESSION\tSTATE\tNAME\tDETAIL")
	for _, s := range r.Sessions {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", s.ID, s.State, s.Name, s.Detail)
	}
	return tw.Flush()
}

// spawnTimeout bounds a spawn, which may make a worktree and check out its
// submodules first.
const spawnTimeout = 10 * time.Minute

func ctlSpawn(args []string) error {
	fs := flag.NewFlagSet("ctl spawn", flag.ContinueOnError)
	cwd := fs.String("cwd", "", "run the task in this directory")
	name := fs.String("name", "", "name the session")
	wait := fs.Bool("wait", false, "wait until the agent is ready, and fail if it is not")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// The @ and + tokens of the command bar may lead the task.
	home, _ := os.UserHomeDir()
	var a ipc.SpawnArgs
	words, err := ui.SpawnTokens(&a, fs.Args(), home)
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
	if strings.TrimSpace(task) == "" {
		return errors.New("usage: irie ctl spawn [--wait] [--cwd DIR] [--name NAME] [@REPO[!]] [+PROFILE] TASK...")
	}
	if *cwd != "" && a.Repo != "" {
		return errors.New("--cwd runs a task in place, so it cannot go with @REPO")
	}
	dir := *cwd
	if dir != "" && dir != "~" && !strings.HasPrefix(dir, "~/") {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return err
		}
		dir = abs
	}
	// A script can spawn with no window open.
	if err := up(nil); err != nil {
		return err
	}
	cfg, p, err := load()
	if err != nil {
		return err
	}
	a.Task, a.Cwd, a.Name = task, dir, *name
	var r ipc.SpawnResult
	if err := ipc.Call(p.Socket(), spawnTimeout, ipc.OpSpawn, a, &r); err != nil {
		return err
	}
	fmt.Println(r.Session.ID)
	if !*wait {
		return nil
	}
	// The core marks a spawn stalled at the ready timeout, within a tick.
	return waitReady(p.Socket(), r.Session.ID, cfg.Spawn.ReadyTimeout+5*time.Second)
}

// waitReady waits until a new session leaves starting, and fails unless
// the agent came up.
func waitReady(sock, id string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		var r ipc.ListResult
		if err := ipc.Call(sock, 5*time.Second, ipc.OpList, nil, &r); err != nil {
			return err
		}
		for _, s := range r.Sessions {
			if s.ID != id || s.State == ipc.StateStarting {
				continue
			}
			switch s.State {
			case ipc.StateFailed, ipc.StateStalled, ipc.StateExited:
				msg := fmt.Sprintf("%s %s: %s", s.Name, s.State, s.Detail)
				for _, l := range s.Tail {
					msg += "\n  " + l
				}
				return errors.New(msg)
			}
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("session %s did not start within %v", id, timeout)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func ctlShow(args []string) error {
	fs := flag.NewFlagSet("ctl show", flag.ContinueOnError)
	focus := fs.Bool("focus", false, "also move the focus to the slot")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: irie ctl show [--focus] SESSION")
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	return ipc.Call(sock, 10*time.Second, ipc.OpShow, ipc.ShowArgs{Session: fs.Arg(0), Focus: *focus}, nil)
}

func ctlWatch(args []string) error {
	fs := flag.NewFlagSet("ctl watch", flag.ContinueOnError)
	session := fs.String("session", "", "only this session's events")
	if err := fs.Parse(args); err != nil {
		return err
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	c, err := ipc.Dial(sock, time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	frames, err := c.Subscribe(ipc.SubscribeArgs{Events: true, Session: *session})
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	for f := range frames {
		if f.T == ipc.FrameEvent && f.Event != nil {
			if err := enc.Encode(f.Event); err != nil {
				return err
			}
		}
	}
	return errors.New("the core went away")
}
