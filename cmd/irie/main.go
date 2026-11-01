// Command irie runs coding agents side by side in one calm view: a sidebar
// of sessions beside the selected agent's live terminal, all in a private
// tmux server.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/core"
	"github.com/irie-people/irie/internal/hook"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/localmodel"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/statusline"
	"github.com/irie-people/irie/internal/team"
	"github.com/irie-people/irie/internal/ui"
	"github.com/irie-people/irie/internal/version"
)

const usage = `usage: irie [command]

  irie                    start Irie if needed and attach to it (same as attach);
                          when no Irie is running, it first checks that this
                          computer has what Irie needs
  irie setup [--check] [--json] [--yes]
                          check that this computer has what Irie needs (tmux,
                          Claude Code, signed in, git, PATH) and offer to fix
                          each part it lacks
  irie up [--login-env]   start the server, core and sidebar if needed;
                          --login-env starts a new server with the login
                          shell's environment, as the Mac app does
  irie attach [--nested]  attach this terminal to Irie
  irie ctl <command>      script Irie: list, spawn, show, watch, move, local,
                          preflight (irie ctl for help)
  irie setup local [--model MODEL] [--yes]
                          get +local ready: install Ollama if needed, pick a
                          model that fits this machine's memory, pull and check it
  irie remote <command>   the phone app: pair a phone, list or forget phones,
                          share the certificate authority (irie remote for help)
  irie dms <command>      the dead man's switch guide's helpers: its checks,
                          check-in and calendar (irie dms for help)
  irie skills             Irie's own skills, whether each is on, and how to
                          start it
  irie version            print the version
  irie config path|print  show the config file's path, or the config in effect

Used by Irie itself:
  irie core [--supervise]   the core
  irie ui [--supervise]     the sidebar
  irie host --spec <file>   the first process of an agent pane
  irie hook                 the shim an agent runs for each hook event
  irie statusline [--then COMMAND]
                            the status line of Claude Code sessions, which runs
                            the user's own
  irie runtime -- COMMAND   Irie's own Ollama server, whose model runners stop
                            with it
  irie placeholder          what the slot shows when no session is shown
  irie team-tmux --tmux BIN --socket PATH --session ID --env-file FILE -- ARGS
                            the tmux an agent with teams runs, which takes
                            Claude Code's teammates to Irie's team server
`

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	name := ""
	if len(args) > 0 {
		name, args = args[0], args[1:]
	}
	var err error
	switch name {
	case "", "attach":
		err = attach(args)
	case "up":
		err = up(args)
	case "core":
		return supervised("core", args, func(o core.Options) error { return core.Run(o) })
	case "ui":
		return supervised("ui", args, func(o core.Options) error {
			return ui.Run(o.Paths.Socket(), ui.Options{RedOnDone: o.Config.UI.RedOnDone, Theme: o.Config.UI.Theme})
		})
	case "host":
		fs := flag.NewFlagSet("host", flag.ContinueOnError)
		spec := fs.String("spec", "", "spec file")
		if fs.Parse(args) != nil || *spec == "" {
			fmt.Fprintln(os.Stderr, "usage: irie host --spec <file>")
			return 2
		}
		return host.Run(*spec)
	case "placeholder":
		// The slot's own pane: the shown session's conversation, or the keys.
		cfg, p, _ := load()
		err = ui.Slot(p.Socket(), ui.SlotOptions{FocusKey: cfg.UI.FocusKey, FlipKey: cfg.UI.FlipKey, Theme: cfg.UI.Theme})
	case "hook":
		return hook.Run(os.Stdin, os.Stdout, os.Getenv)
	case "team-tmux":
		return teamTmux(args)
	case "statusline":
		return statusline.Run(args, os.Stdin, os.Stdout, os.Stderr, os.Getenv)
	case "runtime":
		if len(args) > 0 && args[0] == "--" {
			args = args[1:]
		}
		return localmodel.Serve(args)
	case "ctl":
		err = ctl(args)
	case "remote":
		err = remoteCmd(args)
	case "dms":
		err = dmsCmd(args)
	case "skills":
		err = skillsCmd(args)
	case "setup":
		err = setup(args)
	case "version":
		fmt.Println("irie", version.String())
	case "config":
		err = configCmd(args)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "irie: unknown command %q\n\n%s", name, usage)
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "irie:", err)
		var ec exitCode
		if errors.As(err, &ec) {
			return ec.code
		}
		return 1
	}
	return 0
}

// exitCode is an error with an exit code of its own, for scripts and agents
// that act on it.
type exitCode struct {
	code int
	err  error
}

func (e exitCode) Error() string { return e.err.Error() }
func (e exitCode) Unwrap() error { return e.err }

// load reads the config and paths. On a config error it still returns the
// defaults, for commands that can run without the file.
func load() (config.Config, paths.Paths, error) {
	p, err := paths.FromEnv()
	if err != nil {
		return config.Default(), p, err
	}
	cfg, err := config.Load(p.ConfigFile())
	if err != nil {
		return config.Default(), p, err
	}
	return cfg, p, nil
}

// self is the irie binary that Irie's panes run. IRIE_BIN overrides it.
func self() (string, error) {
	if b := os.Getenv("IRIE_BIN"); b != "" {
		return b, nil
	}
	return os.Executable()
}

func up(args []string) error {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	loginEnv := fs.Bool("login-env", false, "start a new server with the login shell's environment")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New("usage: irie up [--login-env]")
	}
	cfg, p, err := load()
	if err != nil {
		return err
	}
	bin, err := self()
	if err != nil {
		return err
	}
	return core.Up(cfg, p, bin, version.String(), core.UpOptions{LoginEnv: *loginEnv})
}

func attach(args []string) error {
	fs := flag.NewFlagSet("attach", flag.ContinueOnError)
	nested := fs.Bool("nested", false, "attach even inside another tmux")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, p, err := load()
	if err != nil {
		return err
	}
	// A first start, or the first since the computer started: check that
	// Irie has what it needs, and offer to fix what it lacks.
	if os.Getenv("TMUX") == "" && !coreAnswers(p) {
		if err := firstRun(cfg, os.Stdin, os.Stdout); err != nil {
			return err
		}
	}
	if err := up(nil); err != nil {
		return err
	}
	return core.Attach(cfg, *nested)
}

// supervised runs `irie <name>`, or with --supervise keeps it running.
func supervised(name string, args []string, body func(core.Options) error) int {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	supervise := fs.Bool("supervise", false, "restart it whenever it exits")
	if fs.Parse(args) != nil {
		return 2
	}
	cfg, p, err := load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "irie:", err)
		return 1
	}
	logPath := p.CoreLog()
	if name == "ui" {
		logPath = p.UILog()
	}
	log := openLog(p, logPath).With("proc", name)
	if *supervise {
		// The supervisor restarts its own binary: IRIE_BIN is inherited
		// through tmux and may name an older one.
		exe, err := os.Executable()
		if err != nil {
			log.Error("no binary", "err", err)
			return 1
		}
		return core.Supervise(exe, []string{name}, log)
	}
	bin, err := self()
	if err != nil {
		log.Error("no binary", "err", err)
		return 1
	}
	err = body(core.Options{Config: cfg, Paths: p, Bin: bin, Version: version.String(), Log: log})
	if err != nil {
		log.Error("exit", "err", err)
		fmt.Fprintln(os.Stderr, "irie:", err)
		return 1
	}
	return 0
}

func openLog(p paths.Paths, path string) *slog.Logger {
	var w io.Writer = io.Discard
	if p.EnsureState() == nil {
		if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			w = f
		}
	}
	return slog.New(slog.NewTextHandler(w, nil))
}

func configCmd(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: irie config path|print")
	}
	switch args[0] {
	case "path":
		p, err := paths.FromEnv()
		if err != nil {
			return err
		}
		fmt.Println(p.ConfigFile())
		return nil
	case "print":
		cfg, _, err := load()
		if err != nil {
			return err
		}
		return cfg.Write(os.Stdout)
	}
	return errors.New("usage: irie config path|print")
}

// teamTmux is `irie team-tmux`, the tmux shim of a session with teams
// (internal/team). Its own flags come before --, tmux's after.
func teamTmux(args []string) int {
	fs := flag.NewFlagSet("team-tmux", flag.ContinueOnError)
	var s team.Shim
	fs.StringVar(&s.Tmux, "tmux", "", "the real tmux")
	fs.StringVar(&s.Socket, "socket", "", "the team server's socket path")
	fs.StringVar(&s.Session, "session", "", "the Irie session")
	fs.StringVar(&s.EnvFile, "env-file", "", "the environment of its teammates")
	if fs.Parse(args) != nil || s.Tmux == "" || s.Socket == "" || s.Session == "" {
		fmt.Fprintln(os.Stderr, "usage: irie team-tmux --tmux BIN --socket PATH --session ID --env-file FILE -- ARGS")
		return 2
	}
	return s.Run(fs.Args())
}
