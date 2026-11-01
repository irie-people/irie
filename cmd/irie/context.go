package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/irie-people/irie/internal/ctxgit"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/paths"
)

// contextUsage is the part of ctlUsage for contexts in git (DESIGN §4.13).
const contextUsage = `  irie ctl ctx log [--json] [-n N] SESSION[@AT]
                                         a session's context in git, newest
                                         turn first, a commit each: the prompt
                                         that started it, from which session.
                                         AT names a point: a turn (3, or -1 for
                                         the one before the newest), a tag, a
                                         commit, or an entry's uuid
  irie ctl ctx show [--raw] SESSION[@AT] one turn as it reads, or --raw its
                                         transcript lines
  irie ctl ctx diff A[@AT] B[@AT]        where two contexts parted, the turns
                                         each took since, and how they differ
  irie ctl ctx tag SESSION[@AT] NAME     name a point of a context; the name
                                         then serves as AT, and as a fork's
                                         start
  irie ctl ctx sync SESSION              store a session's newest turns now
  irie ctl ctx git ARGS...               run git on the context store, such as
                                         log --graph --all
  irie ctl fork [--at AT] [--here] [--name NAME] [--model MODEL] [--wait] [+PROFILE] SESSION [TASK...]
                                         carry a session's context on from a
                                         point of it in a new session, in a
                                         worktree of its own at that turn's
                                         code (--here: in the session's own
                                         folder, sharing its files). TASK is
                                         its next prompt, - reads stdin; with
                                         none it waits for you. Prints its id
`

// ctlContext is `irie ctl ctx`.
func ctlContext(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: irie ctl ctx log|show|diff|tag|sync|git ...")
	}
	switch args[0] {
	case "log":
		return ctxLog(args[1:])
	case "show":
		return ctxShow(args[1:])
	case "diff":
		return ctxDiff(args[1:])
	case "tag":
		return ctxTag(args[1:])
	case "sync":
		return ctxSync(args[1:])
	case "git":
		return ctxGit(args[1:])
	case "path":
		p, err := paths.FromEnv()
		if err != nil {
			return err
		}
		fmt.Println(p.ContextStore())
		return nil
	}
	return fmt.Errorf("irie ctl ctx: no command %q: log, show, diff, tag, sync or git", args[0])
}

// splitAt splits SESSION@AT. An @ at the start, or with nothing or a space
// after it, is part of the session's name.
func splitAt(s string) (session, at string) {
	i := strings.LastIndex(s, "@")
	if i <= 0 || i == len(s)-1 || strings.ContainsAny(s[i+1:], " \t") {
		return s, ""
	}
	return s[:i], s[i+1:]
}

// contextCall runs the context op.
func contextCall(a ipc.ContextArgs) (ipc.ContextResult, error) {
	var r ipc.ContextResult
	sock, err := socket()
	if err != nil {
		return r, err
	}
	err = ipc.Call(sock, time.Minute, ipc.OpContext, a, &r)
	return r, err
}

// point is the newest turn a SESSION[@AT] names.
func point(arg string) (ipc.ContextResult, ipc.ContextTurn, error) {
	session, at := splitAt(arg)
	r, err := contextCall(ipc.ContextArgs{Action: ipc.ContextLog, Session: session, At: at, Limit: 1})
	if err != nil {
		return r, ipc.ContextTurn{}, err
	}
	if len(r.Turns) == 0 {
		return r, ipc.ContextTurn{}, fmt.Errorf("%s has no turns stored", arg)
	}
	return r, r.Turns[0], nil
}

func ctxLog(args []string) error {
	fs := flag.NewFlagSet("ctl ctx log", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the turns as JSON")
	n := fs.Int("n", 0, "at most this many turns")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: irie ctl ctx log [--json] [-n N] SESSION[@AT]")
	}
	session, at := splitAt(fs.Arg(0))
	r, err := contextCall(ipc.ContextArgs{Action: ipc.ContextLog, Session: session, At: at, Limit: *n})
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	for _, t := range r.Turns {
		marks := ""
		switch {
		case t.Partial:
			marks = " (still going)"
		case t.Cut != "":
			marks = " (cut)"
		}
		from := ""
		if r.Session != "" && t.Session != r.Session {
			from = "from " + or(t.Name, t.Session)
		}
		fmt.Fprintf(tw, "%s\tturn %d\t%s\t%s\t%s%s\n", t.Commit[:12], t.Turn, ago(t.Time), from, oneLine(t.Prompt, 80), marks)
	}
	return tw.Flush()
}

func ctxShow(args []string) error {
	fs := flag.NewFlagSet("ctl ctx show", flag.ContinueOnError)
	raw := fs.Bool("raw", false, "the turn's transcript lines, as Claude Code wrote them")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: irie ctl ctx show [--raw] SESSION[@AT]")
	}
	r, t, err := point(fs.Arg(0))
	if err != nil {
		return err
	}
	ext := "md"
	if *raw {
		ext = "jsonl"
	}
	if !*raw {
		fmt.Printf("# Turn %d of %s · %s · %s\n\n", t.Turn, or(t.Name, t.Session), t.Commit[:12], t.Time.Local().Format("2006-01-02 15:04"))
	}
	return storeGit(r.Repo, os.Stdout, "show", fmt.Sprintf("%s:turns/%04d.%s", t.Commit, t.Turn, ext))
}

func ctxDiff(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: irie ctl ctx diff A[@AT] B[@AT]")
	}
	ra, ta, err := point(args[0])
	if err != nil {
		return err
	}
	_, tb, err := point(args[1])
	if err != nil {
		return err
	}
	repo := ra.Repo
	base, err := storeOut(repo, "merge-base", ta.Commit, tb.Commit)
	if err != nil || base == "" {
		fmt.Println("These contexts share no turn.")
	} else {
		subject, _ := storeOut(repo, "log", "-1", "--format=%(trailers:key=Irie-Turn,valueonly,separator=) %s", base)
		turn, prompt, _ := strings.Cut(subject, " ")
		fmt.Printf("Parted after turn %s, %s: %s\n", turn, base[:12], oneLine(prompt, 80))
		for _, side := range []struct {
			arg string
			t   ipc.ContextTurn
		}{{args[0], ta}, {args[1], tb}} {
			out, _ := storeOut(repo, "log", "--first-parent", "--reverse", "--format=  turn %(trailers:key=Irie-Turn,valueonly,separator=)  %s", base+".."+side.t.Commit)
			fmt.Printf("\n%s since:\n", side.arg)
			if out == "" {
				fmt.Println("  nothing")
			} else {
				fmt.Println(out)
			}
		}
		fmt.Println()
	}
	return storeGit(repo, os.Stdout, "diff", ta.Commit, tb.Commit, "--", "turns/*.md")
}

func ctxTag(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: irie ctl ctx tag SESSION[@AT] NAME")
	}
	session, at := splitAt(args[0])
	r, err := contextCall(ipc.ContextArgs{Action: ipc.ContextTag, Session: session, At: at, Name: args[1]})
	if err != nil {
		return err
	}
	fmt.Println(r.Commit)
	return nil
}

func ctxSync(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: irie ctl ctx sync SESSION")
	}
	r, err := contextCall(ipc.ContextArgs{Action: ipc.ContextSync, Session: args[0]})
	if err != nil {
		return err
	}
	fmt.Println(r.Commit)
	return nil
}

// ctxGit runs git on the store, which needs no running core.
func ctxGit(args []string) error {
	p, err := paths.FromEnv()
	if err != nil {
		return err
	}
	if _, err := os.Stat(p.ContextStore()); err != nil {
		return errors.New("no context is stored yet: a session's first turn stores one")
	}
	return storeGit(p.ContextStore(), os.Stdout, args...)
}

// storeGit runs git on the store at repo, its output to out.
func storeGit(repo string, out io.Writer, args ...string) error {
	cmd := exec.Command("git", append([]string{"--git-dir=" + repo}, args...)...)
	cmd.Env = append(ctxgit.Env(), "GIT_PAGER=", "PAGER=")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, out, os.Stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			os.Exit(exit.ExitCode())
		}
		return err
	}
	return nil
}

// storeOut runs git on the store and returns what it printed, trimmed.
func storeOut(repo string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"--git-dir=" + repo}, args...)...)
	cmd.Env = ctxgit.Env()
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// ctlFork is `irie ctl fork`.
func ctlFork(args []string) error {
	fs := flag.NewFlagSet("ctl fork", flag.ContinueOnError)
	at := fs.String("at", "", "the point to fork from: a turn, -N, a tag, a commit or an entry's uuid; the newest turn by default")
	here := fs.Bool("here", false, "work in the session's own folder, sharing its files")
	name := fs.String("name", "", "name the fork")
	model := fs.String("model", "", "the model for this fork alone")
	wait := fs.Bool("wait", false, "wait until the fork's agent is ready, and fail if it is not")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var a ipc.ForkArgs
	rest := fs.Args()
	for len(rest) > 0 && strings.HasPrefix(rest[0], "+") && len(rest[0]) > 1 {
		a.Profile = rest[0][1:]
		rest = rest[1:]
	}
	if len(rest) == 0 {
		return errors.New("usage: irie ctl fork [--at AT] [--here] [--name NAME] [--model MODEL] [--wait] [+PROFILE] SESSION [TASK...]")
	}
	a.Session, a.At = splitAt(rest[0])
	if *at != "" {
		a.At = *at
	}
	task := strings.Join(rest[1:], " ")
	if task == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		task = string(b)
	}
	a.Task, a.Name, a.Model, a.Here = task, *name, *model, *here
	cfg, p, err := load()
	if err != nil {
		return err
	}
	var r ipc.ForkResult
	if err := ipc.Call(p.Socket(), spawnTimeout, ipc.OpFork, a, &r); err != nil {
		return err
	}
	fmt.Println(r.Session.ID)
	fmt.Fprintf(os.Stderr, "forked %s from turn %d (%s) into %s\n", a.Session, r.Turn, r.From[:12], r.Session.Cwd)
	for _, w := range r.Session.Warnings {
		fmt.Fprintln(os.Stderr, "  "+w)
	}
	if !*wait {
		return nil
	}
	return waitReady(p.Socket(), r.Session.ID, cfg.Spawn.ReadyTimeout+5*time.Second)
}

// ago says how long ago t was, in its largest unit.
func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case t.IsZero():
		return ""
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

// oneLine is s on one line, cut to n runes.
func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
