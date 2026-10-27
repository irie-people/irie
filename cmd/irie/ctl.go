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
  irie ctl spawn [--wait] [--force] [--cwd DIR] [--name NAME] [@REPO[!]] [+PROFILE] TASK...
                                         start a task and print its session id,
                                         starting Irie if needed. @REPO runs it in
                                         a new worktree of that repo, @REPO! in the
                                         repo's own checkout; +PROFILE picks the
                                         agent, +ollama:MODEL a local model. TASK -
                                         reads the task from stdin. --wait waits
                                         until the agent is ready, and fails if it
                                         is not; --force spawns a local profile even
                                         when its context or memory falls short
  irie ctl history [--json] [QUERY...]   the sessions that closed, newest first, whose
                                         name or id has every QUERY word. With
                                         spawn.close_when_done a finished session
                                         closes itself and the sidebar has no Done
                                         group: look here instead
  irie ctl reopen [SESSION]              bring a closed session back, by id or name
                                         (the one closed last with none), carrying
                                         on its conversation; prints its new id
  irie ctl show [--focus] SESSION        show a session in the slot
  irie ctl rename SESSION NAME...        rename a session, by its id or name,
                                         and print the name it got: numbered
                                         when another session has it
  irie ctl flip                          flip the slot between the shown session's
                                         conversation and its raw terminal
  irie ctl send SESSION TEXT...          type a message to a session's agent, as
                                         a person would; TEXT - reads stdin
  irie ctl conversation [--json] [--follow] SESSION
                                         print a session's conversation, read
                                         from its transcript; --follow keeps
                                         printing what changes
  irie ctl watch [--session SESSION]     print events as NDJSON until interrupted
  irie ctl move [--wait] [+PROFILE] SESSION
                                         carry an idle Claude Code session that
                                         runs outside Irie on in Irie, and print
                                         its new session id. SESSION is its id or
                                         name, as ctl list shows under OUTSIDE IRIE
  irie ctl local [--json]                the local Ollama server, the machine's
                                         memory, and each model weighed against it
  irie ctl preflight [--model MODEL] [--force] [PROFILE]
                                         check a local profile's model (default
                                         +local) as a spawn would, loading it
  irie ctl route [--json] TASK...        where +auto would run a task, and why:
                                         each profile's accuracy for the task's
                                         class, its cost and its cost per success
  irie ctl delegate [--to PROFILE] [--mode MODE] [--timeout D] TASK...
                                         from an agent: run a subtask in this
                                         directory on the cheapest profile accurate
                                         enough for it, wait, and print its result.
                                         Exits 3 when it needs a stronger model
                                         than the caller's cheaper ones, 4 when it
                                         is still running at the timeout
  irie ctl rate SESSION good|bad         rate a session's work; the router learns
                                         each profile's accuracy from it
` + controlUsage

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
	case "history":
		return ctlHistory(args[1:])
	case "reopen":
		return ctlReopen(args[1:])
	case "show":
		return ctlShow(args[1:])
	case "rename":
		return ctlRename(args[1:])
	case "conversation":
		return ctlConversation(args[1:])
	case "send":
		return ctlSend(args[1:])
	case "flip":
		return ctlFlip(args[1:])
	case "watch":
		return ctlWatch(args[1:])
	case "move":
		return ctlMove(args[1:])
	case "local":
		return ctlLocal(args[1:])
	case "preflight":
		return ctlPreflight(args[1:])
	case "route":
		return ctlRoute(args[1:])
	case "delegate":
		return ctlDelegate(args[1:])
	case "rate":
		return ctlRate(args[1:])
	case "freeze", "thaw":
		return ctlFreeze(args[1:], args[0] == "freeze")
	case "promote", "unpromote":
		return ctlPromote(args[1:], map[string]int{"promote": 1}[args[0]])
	case "reorder":
		return ctlReorder(args[1:])
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
	// Claude Code sessions that run outside Irie, under a heading of their
	// own; ctl move takes them by id or name.
	if len(r.Outside) > 0 || r.OutsideProblem != "" {
		fmt.Fprintln(tw, "\nOUTSIDE IRIE")
		if r.OutsideProblem != "" {
			fmt.Fprintln(tw, r.OutsideProblem)
		}
	}
	if len(r.Outside) > 0 {
		fmt.Fprintln(tw, "SESSION\tSTATUS\tKIND\tNAME\tCWD")
		for _, o := range r.Outside {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", or(o.VendorID, "-"), or(o.Status, "-"), o.Kind, or(o.Name, "-"), o.Cwd)
		}
	}
	return tw.Flush()
}

// ctlRename renames one of Irie's sessions, which it finds by id or name.
func ctlRename(args []string) error {
	if len(args) < 2 {
		return errors.New("usage: irie ctl rename SESSION NAME...")
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	var l ipc.ListResult
	if err := ipc.Call(sock, 5*time.Second, ipc.OpList, nil, &l); err != nil {
		return err
	}
	id := args[0]
	for _, s := range l.Sessions {
		if s.Name == args[0] {
			id = s.ID
		}
	}
	var r ipc.RenameResult
	if err := ipc.Call(sock, 5*time.Second, ipc.OpRename, ipc.RenameArgs{Session: id, Name: strings.Join(args[1:], " ")}, &r); err != nil {
		return err
	}
	fmt.Println(r.Session.Name)
	return nil
}

func or(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// ctlMove moves a session that runs outside Irie in. It finds the session
// by its full id, its short id or its name in the core's list.
func ctlMove(args []string) error {
	const usage = "usage: irie ctl move [--wait] [+PROFILE] SESSION"
	fs := flag.NewFlagSet("ctl move", flag.ContinueOnError)
	wait := fs.Bool("wait", false, "wait until the moved session is ready, and fail if it is not")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest, profile := fs.Args(), ""
	if len(rest) > 0 && strings.HasPrefix(rest[0], "+") {
		profile, rest = rest[0][1:], rest[1:]
	}
	if len(rest) != 1 || rest[0] == "" {
		return errors.New(usage)
	}
	if err := up(nil); err != nil {
		return err
	}
	cfg, p, err := load()
	if err != nil {
		return err
	}
	var r ipc.ListResult
	if err := ipc.Call(p.Socket(), 15*time.Second, ipc.OpList, nil, &r); err != nil {
		return err
	}
	id, err := pickOutside(r, rest[0])
	if err != nil {
		return err
	}
	var res ipc.MoveResult
	if err := ipc.Call(p.Socket(), spawnTimeout, ipc.OpMove, ipc.MoveArgs{VendorID: id, Profile: profile}, &res); err != nil {
		return err
	}
	fmt.Println(res.Session.ID)
	if !*wait {
		return nil
	}
	return waitReady(p.Socket(), res.Session.ID, cfg.Spawn.ReadyTimeout+5*time.Second)
}

// pickOutside returns the vendor id of the outside session that what names:
// its full id, its short id, or its name when only one has it. A full id
// that is not listed goes to the core as it is, which says why it cannot
// move.
func pickOutside(r ipc.ListResult, what string) (string, error) {
	var named []ipc.OutsideSession
	for _, o := range r.Outside {
		switch {
		case o.VendorID == what || o.ID == what && o.VendorID != "":
			return o.VendorID, nil
		case o.Name == what:
			named = append(named, o)
		}
	}
	switch len(named) {
	case 1:
		if named[0].VendorID == "" {
			return "", fmt.Errorf("%s has no session id yet, so it cannot move", what)
		}
		return named[0].VendorID, nil
	case 0:
		if len(what) == 36 && strings.Count(what, "-") == 4 {
			return what, nil
		}
		msg := fmt.Sprintf("no Claude Code session %q runs outside Irie; `irie ctl list` shows them", what)
		if r.OutsideProblem != "" {
			msg += " (" + r.OutsideProblem + ")"
		}
		return "", errors.New(msg)
	}
	var ids []string
	for _, o := range named {
		ids = append(ids, or(o.VendorID, "-")+" in "+o.Cwd)
	}
	return "", fmt.Errorf("%d sessions are named %q; give one's id: %s", len(named), what, strings.Join(ids, ", "))
}

// spawnTimeout bounds a spawn, which may make a worktree and check out its
// submodules first.
const spawnTimeout = 10 * time.Minute

func ctlSpawn(args []string) error {
	fs := flag.NewFlagSet("ctl spawn", flag.ContinueOnError)
	cwd := fs.String("cwd", "", "run the task in this directory")
	name := fs.String("name", "", "name the session")
	wait := fs.Bool("wait", false, "wait until the agent is ready, and fail if it is not")
	force := fs.Bool("force", false, "spawn a local profile even when its context or memory falls short")
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
		return errors.New("usage: irie ctl spawn [--wait] [--force] [--cwd DIR] [--name NAME] [@REPO[!]] [+PROFILE] TASK...")
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
	a.Task, a.Cwd, a.Name, a.Force = task, dir, *name, *force
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

func ctlConversation(args []string) error {
	fs := flag.NewFlagSet("ctl conversation", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print each conversation or frame as JSON")
	follow := fs.Bool("follow", false, "keep printing what changes")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: irie ctl conversation [--json] [--follow] SESSION")
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	print := func(cv ipc.Conversation) error {
		if *asJSON {
			return json.NewEncoder(os.Stdout).Encode(cv)
		}
		if cv.Reset && len(cv.Items) == 0 && cv.Transcript == "" {
			fmt.Println("(no transcript: this session started before Irie followed transcripts; its terminal has everything)")
		}
		for _, it := range cv.Items {
			fmt.Println(conversationLine(it))
		}
		if cv.Needs != nil {
			fmt.Println(conversationLine(*cv.Needs))
		}
		return nil
	}
	if !*follow {
		var cv ipc.Conversation
		if err := ipc.Call(sock, 10*time.Second, ipc.OpConversation, ipc.ConversationArgs{Session: fs.Arg(0)}, &cv); err != nil {
			return err
		}
		return print(cv)
	}
	c, err := ipc.Dial(sock, time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	frames, err := c.Subscribe(ipc.SubscribeArgs{Session: fs.Arg(0), Conversation: true})
	if err != nil {
		return err
	}
	for f := range frames {
		if f.T == ipc.FrameConversation && f.Conversation != nil {
			if err := print(*f.Conversation); err != nil {
				return err
			}
		}
	}
	return errors.New("the core went away")
}

// conversationLine is one item of a conversation as a line of text.
func conversationLine(it ipc.Item) string {
	first := func(s string) string {
		line, rest, _ := strings.Cut(strings.TrimSpace(s), "\n")
		if strings.TrimSpace(rest) != "" {
			line += " …"
		}
		return line
	}
	switch it.Kind {
	case ipc.ItemUser:
		return "You: " + first(it.Text)
	case ipc.ItemText:
		return "Claude: " + first(it.Text)
	case ipc.ItemNote:
		return "— " + it.Text
	case ipc.ItemNeeds:
		return "● " + it.Text
	}
	mark := map[string]string{ipc.ItemRunning: "…", ipc.ItemOK: "✓", ipc.ItemError: "✕", ipc.ItemStopped: "·"}[it.State]
	line := "  ▸ " + it.Phrase + "  " + mark
	if it.Ms > 0 {
		line += " " + (time.Duration(it.Ms) * time.Millisecond).Round(time.Second/10).String()
	}
	if it.Added > 0 || it.Removed > 0 {
		line += fmt.Sprintf(" +%d −%d", it.Added, it.Removed)
	}
	return line
}

func ctlSend(args []string) error {
	if len(args) < 2 {
		return errors.New("usage: irie ctl send SESSION TEXT...")
	}
	text := strings.Join(args[1:], " ")
	if text == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		text = string(b)
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	return ipc.Call(sock, 10*time.Second, ipc.OpSend, ipc.SendArgs{Session: args[0], Text: text}, nil)
}

// matchClosed are the closed sessions whose name or id holds every word of
// query, case folded, in the order given (newest first).
func matchClosed(closed []ipc.ClosedSession, query []string) []ipc.ClosedSession {
	var out []ipc.ClosedSession
	for _, c := range closed {
		hay := strings.ToLower(c.Name + " " + c.ID)
		ok := true
		for _, w := range query {
			ok = ok && strings.Contains(hay, strings.ToLower(w))
		}
		if ok {
			out = append(out, c)
		}
	}
	return out
}

// ctlHistory lists the sessions that closed, which reopen can bring back.
func ctlHistory(args []string) error {
	fs := flag.NewFlagSet("ctl history", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the closed sessions as JSON")
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
	got := matchClosed(r.Closed, fs.Args())
	if *asJSON {
		if got == nil {
			got = []ipc.ClosedSession{}
		}
		return json.NewEncoder(os.Stdout).Encode(got)
	}
	if len(got) == 0 {
		fmt.Println("no closed sessions match")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "SESSION\tCLOSED\tNAME")
	for _, c := range got {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", c.ID, c.Closed.Local().Format("Jan 2 15:04"), c.Name)
	}
	return tw.Flush()
}

// ctlReopen brings a closed session back, found by id or name; with none
// named, the one closed last.
func ctlReopen(args []string) error {
	if len(args) > 1 {
		return errors.New("usage: irie ctl reopen [SESSION]")
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	id := ""
	if len(args) == 1 {
		var l ipc.ListResult
		if err := ipc.Call(sock, 5*time.Second, ipc.OpList, nil, &l); err != nil {
			return err
		}
		id = args[0]
		for _, c := range l.Closed {
			if c.ID == args[0] || strings.EqualFold(c.Name, args[0]) {
				id = c.ID
				break
			}
		}
	}
	var r ipc.ReopenResult
	if err := ipc.Call(sock, 10*time.Minute, ipc.OpReopen, ipc.ReopenArgs{Session: id}, &r); err != nil {
		return err
	}
	fmt.Println(r.Session.ID)
	return nil
}

func ctlFlip(args []string) error {
	if len(args) > 0 {
		return errors.New("usage: irie ctl flip")
	}
	sock, err := socket()
	if err != nil {
		return err
	}
	var r ipc.FlipResult
	return ipc.Call(sock, 10*time.Second, ipc.OpFlip, nil, &r)
}
