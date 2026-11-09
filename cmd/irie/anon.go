package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/irie-people/irie/internal/anon"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/repo"
	"github.com/irie-people/irie/internal/version"
	"github.com/irie-people/irie/internal/vpn"
)

const anonUsage = `usage: irie anon [--show]          how anonymous research stands, and its compartments
       irie anon up                start Irie's Tor and SearXNG as the compartments need them
       irie anon check [NAME]      check where each compartment, or NAME, leaves from
       irie anon run [--tor] NAME -- COMMAND [ARGS...]
                                   run a program of yours through compartment NAME's exit,
                                   made if need be: its proxy variables set, checked first
       irie anon proxy NAME        print compartment NAME's proxy address, for your own tools
       irie anon forget NAME       drop compartment NAME, which lets its exit go
       irie setup anon [--yes]     install Tor and SearXNG and turn anonymous research on

A task typed with +anon runs in a compartment of its own, +anon:NAME in the
named one, which tasks share; +tor and +tor:NAME leave through Tor (DESIGN §4.16).
Exits are named only with --show, and only to you: never paste one into an agent.`

// anonCmd is `irie anon`: anonymous research's state and compartments
// (DESIGN §4.16), and the MCP server that sessions run.
func anonCmd(args []string) error {
	cfg, p, err := load()
	if err != nil {
		return err
	}
	d := anon.Dirs{Root: p.Anon()}
	sub := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "":
		fs := flag.NewFlagSet("anon", flag.ContinueOnError)
		show := fs.Bool("show", false, "name each compartment's relay")
		if err := fs.Parse(args); err != nil {
			return err
		}
		return anonStatus(os.Stdout, cfg, p, d, *show)
	case "up":
		var r ipc.AnonResult
		if err := ipc.Call(p.Socket(), 3*time.Minute, ipc.OpAnon, ipc.AnonArgs{Up: true}, &r); err != nil {
			return err
		}
		return anonStatus(os.Stdout, cfg, p, d, false)
	case "check":
		return anonCheck(os.Stdout, d, args)
	case "run":
		return anonRun(cfg, p, d, args)
	case "proxy":
		if len(args) != 1 {
			return errors.New(anonUsage)
		}
		c, err := compartment(d, args[0])
		if err != nil {
			return err
		}
		fmt.Println(c.Proxy())
		return nil
	case "forget":
		if len(args) != 1 {
			return errors.New(anonUsage)
		}
		found, err := anon.Forget(anon.Store{Path: d.Compartments()}, args[0])
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("no compartment %q", args[0])
		}
		fmt.Printf("Forgot %s: its exit is free again. A task named for it later starts afresh, from another exit.\n", args[0])
		return nil
	}
	return errors.New(anonUsage)
}

func compartment(d anon.Dirs, name string) (anon.Compartment, error) {
	all, err := anon.Store{Path: d.Compartments()}.Load()
	if err != nil {
		return anon.Compartment{}, err
	}
	c, ok := all[name]
	if !ok {
		return anon.Compartment{}, fmt.Errorf("no compartment %q: `irie anon` lists them", name)
	}
	return c, nil
}

// anonServe is `irie anon mcp`: one session's anonymous research tools.
func anonServe(args []string) int {
	o, err := anon.ParseArgs(args, os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "irie anon mcp:", err)
		return 2
	}
	o.Version = version.String()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	signal.Ignore(syscall.SIGPIPE)
	if err := anon.Serve(ctx, os.Stdin, os.Stdout, o); err != nil {
		fmt.Fprintln(os.Stderr, "irie anon mcp:", err)
		return 1
	}
	return 0
}

func anonStatus(w io.Writer, cfg config.Config, p paths.Paths, d anon.Dirs, show bool) error {
	on := "off: `irie setup anon` sets it up"
	if cfg.Anon.On {
		on = "on: +anon and +tor tasks run in compartments"
	}
	fmt.Fprintf(w, "Anonymous research is %s.\n", on)
	fmt.Fprintf(w, "Mullvad VPN: %s\n", vpnWords(cfg))
	var r ipc.AnonResult
	coreErr := ipc.Call(p.Socket(), 10*time.Second, ipc.OpAnon, ipc.AnonArgs{}, &r)
	tor := "not installed (irie setup anon installs it)"
	if _, err := anon.FindTor(expand(cfg.Anon.Tor)); err == nil {
		tor = "installed; it starts with the first +tor task"
	}
	if r.Tor {
		tor = "running"
	}
	fmt.Fprintf(w, "Tor: %s\n", tor)
	search := "not installed (irie setup anon installs it)"
	switch {
	case r.Search != "":
		search = "running at " + r.Search + ", asking the engines from its own exit"
	case anon.SearxInstalled(d) && !cfg.Anon.Search:
		search = "installed, and off (anon.search)"
	case anon.SearxInstalled(d):
		search = "installed; it starts with the first anonymous task, or `irie anon up`"
	}
	fmt.Fprintf(w, "SearXNG: %s\n", search)
	if coreErr != nil && cfg.Anon.On {
		fmt.Fprintf(w, "(Irie's core did not answer: %v)\n", coreErr)
	}
	all, err := anon.Store{Path: d.Compartments()}.Load()
	if err != nil {
		return err
	}
	if len(all) == 0 {
		fmt.Fprintln(w, "\nNo compartments yet.")
		return nil
	}
	fmt.Fprintln(w, "\nCompartments:")
	for _, c := range anon.Sorted(all) {
		where := c.Where()
		if show {
			where += " · " + c.Proxy()
		}
		note := ""
		if c.Name == anon.SearchName {
			note = " (SearXNG's)"
		}
		fmt.Fprintf(w, "  %-20s %-8s %s, since %s%s\n", c.Name, c.Route, where, c.Created.Local().Format("2006-01-02"), note)
	}
	return nil
}

func vpnWords(cfg config.Config) string {
	bin, err := vpn.Find(cfg.VPN.Mullvad)
	if err != nil {
		return "not installed: anonymous research needs it"
	}
	st, err := vpn.CLI{Bin: bin}.Status()
	switch {
	case err != nil:
		return "can't tell: " + err.Error()
	case st.State == vpn.Connected:
		return "connected"
	}
	return st.State + ": anonymous tasks start only while it is connected"
}

func expand(path string) string {
	if path == "" {
		return ""
	}
	h, _ := os.UserHomeDir()
	return repo.Expand(path, h)
}

func anonCheck(w io.Writer, d anon.Dirs, args []string) error {
	all, err := anon.Store{Path: d.Compartments()}.Load()
	if err != nil {
		return err
	}
	list := anon.Sorted(all)
	if len(args) == 1 {
		c, ok := all[args[0]]
		if !ok {
			return fmt.Errorf("no compartment %q", args[0])
		}
		list = []anon.Compartment{c}
	} else if len(args) > 1 {
		return errors.New(anonUsage)
	}
	if len(list) == 0 {
		fmt.Fprintln(w, "No compartments yet.")
		return nil
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	bad := 0
	for _, c := range list {
		cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		words, err := anon.Verify(cctx, c)
		cancel()
		if err != nil {
			bad++
			fmt.Fprintf(w, "✗ %s: %v\n", c.Name, err)
			continue
		}
		fmt.Fprintf(w, "✓ %s: %s\n", c.Name, words)
	}
	if bad > 0 {
		return exitCode{1, fmt.Errorf("%d of %d compartments failed their check", bad, len(list))}
	}
	return nil
}

// anonRun is `irie anon run`: one of the user's own programs, such as a
// crawler or a research agent, through a compartment's exit. The exit is
// checked first, and the program is not run when the check fails.
func anonRun(cfg config.Config, p paths.Paths, d anon.Dirs, args []string) error {
	fs := flag.NewFlagSet("anon run", flag.ContinueOnError)
	tor := fs.Bool("tor", false, "make the compartment on Tor, when it is new")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 3 || rest[1] != "--" {
		return errors.New(anonUsage)
	}
	name, argv := rest[0], rest[2:]
	if name == anon.SearchName {
		return errors.New("search is SearXNG's compartment alone: name another")
	}
	route := ""
	if *tor {
		route = anon.Tor
	}
	mac, err := macExit(cfg)
	if err != nil {
		return err
	}
	relays, _ := anon.ReadRelays("")
	o := anon.Options{Route: cfg.Anon.Route, Countries: cfg.Anon.Countries, Owned: cfg.Anon.Owned, TorPort: cfg.Anon.TorPort}
	t, err := anon.Take(anon.Store{Path: d.Compartments()}, name, route, o, relays, mac, time.Now())
	if err != nil {
		return err
	}
	if t.Made {
		fmt.Fprintf(os.Stderr, "irie anon: new compartment %s, leaving from %s\n", t.Name, t.Where())
	}
	if t.Route == anon.Tor {
		var r ipc.AnonResult
		if err := ipc.Call(p.Socket(), 3*time.Minute, ipc.OpAnon, ipc.AnonArgs{Up: true}, &r); err != nil {
			return fmt.Errorf("starting Irie's Tor: %w", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	words, err := anon.Verify(ctx, t.Compartment)
	cancel()
	if err != nil {
		return fmt.Errorf("compartment %s: %w; not running %s", t.Name, err, argv[0])
	}
	fmt.Fprintf(os.Stderr, "irie anon: %s %s\n", t.Name, words)
	bin, err := exec.LookPath(argv[0])
	if err != nil {
		return err
	}
	env := append(os.Environ(), anon.ProxyEnv(t.Proxy())...)
	env = append(env, "IRIE_ANON="+t.Name)
	return syscall.Exec(bin, argv, env)
}

// macExit is the relay of the Mac's own Mullvad tunnel, which must be up.
func macExit(cfg config.Config) (string, error) {
	bin, err := vpn.Find(cfg.VPN.Mullvad)
	if err != nil {
		return "", errors.New("anonymous research needs Mullvad VPN, which is not installed")
	}
	st, err := vpn.CLI{Bin: bin}.Status()
	if err != nil {
		return "", fmt.Errorf("can't read Mullvad VPN's state: %v", err)
	}
	if st.State != vpn.Connected {
		return "", errors.New("anonymous research needs Mullvad VPN connected, so that nothing leaves from this Mac's own address")
	}
	return st.Host, nil
}

// setupAnon is `irie setup anon`: it gets anonymous research ready (DESIGN
// §4.16). It checks Mullvad VPN, turns anon.on, installs Tor with Homebrew
// and SearXNG from its own repository at a pinned commit, each after a
// yes, starts SearXNG through an exit of its own, and searches once as an
// agent would.
func setupAnon(args []string, stdin io.Reader, stdout io.Writer) error {
	flags := flag.NewFlagSet("setup anon", flag.ContinueOnError)
	yes := flags.Bool("yes", false, "install Tor and SearXNG without asking")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return errors.New(setupUsage)
	}
	cfg, p, err := load()
	if err != nil {
		return err
	}
	d := anon.Dirs{Root: p.Anon()}
	if _, err := macExit(cfg); err != nil {
		return err
	}
	if _, err := anon.ReadRelays(""); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Mullvad VPN is connected, and its relay list is here.")
	// +anon tasks need neither Tor nor SearXNG: they fetch and browse
	// through their exits. So it goes on now, whatever is installed next.
	if err := anonOn(p); err != nil {
		return fmt.Errorf("anon.on could not be turned on: %w", err)
	}
	fmt.Fprintln(stdout, "Anonymous research is on: +anon tasks run in compartments of their own.")
	in := bufio.NewReader(stdin)
	ask := func(q string) bool {
		if *yes {
			return true
		}
		if !isTerminal(stdin) {
			return false
		}
		fmt.Fprintf(stdout, "%s [y/N] ", q)
		line, _ := in.ReadString('\n')
		a := strings.ToLower(strings.TrimSpace(line))
		return a == "y" || a == "yes"
	}
	run := func(argv []string) error {
		fmt.Fprintf(stdout, "$ %s\n", strings.Join(argv, " "))
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Stdout, cmd.Stderr = stdout, os.Stderr
		return cmd.Run()
	}

	if _, err := anon.FindTor(expand(cfg.Anon.Tor)); errors.Is(err, anon.ErrNoTor) {
		brew, berr := exec.LookPath("brew")
		switch {
		case berr != nil:
			fmt.Fprintln(stdout, "Tor is not installed, and there is no Homebrew here: install tor with your system's packages, then run `irie setup anon` again. +anon tasks work without it; +tor tasks need it.")
		case ask("Tor is not installed. Install it with `brew install tor`?"):
			if err := run([]string{brew, "install", "tor"}); err != nil {
				return fmt.Errorf("installing Tor: %w", err)
			}
		default:
			fmt.Fprintln(stdout, "Leaving Tor out: +tor tasks need it, +anon tasks don't.")
		}
	} else if err == nil {
		fmt.Fprintln(stdout, "Tor is installed.")
	}

	if !anon.SearxInstalled(d) {
		git, err := exec.LookPath("git")
		if err != nil {
			return errors.New("installing SearXNG needs git")
		}
		uv, _ := exec.LookPath("uv")
		py, _ := exec.LookPath("python3")
		if uv == "" && py == "" {
			return errors.New("installing SearXNG needs uv or python3 (3.10 or later)")
		}
		steps := anon.SearxSteps(d, git, uv, py)
		fmt.Fprintf(stdout, "SearXNG is not installed. Irie installs it into %s from %s at commit %.12s:\n", d.Searx(), anon.SearxRepo, anon.SearxCommit)
		for _, s := range steps {
			fmt.Fprintf(stdout, "  %s\n", strings.Join(s, " "))
		}
		if !ask("Install SearXNG?") {
			fmt.Fprintln(stdout, "Leaving SearXNG out: +anon tasks fetch and browse without it, and have no search tool. `irie setup anon` again installs it.")
			return nil
		}
		os.RemoveAll(d.SearxSrc())
		for _, s := range steps {
			if err := run(s); err != nil {
				return fmt.Errorf("installing SearXNG: %w", err)
			}
		}
	} else {
		fmt.Fprintln(stdout, "SearXNG is installed.")
	}

	fmt.Fprintln(stdout, "Starting SearXNG through an exit of its own…")
	var r ipc.AnonResult
	if err := ipc.Call(p.Socket(), 3*time.Minute, ipc.OpAnon, ipc.AnonArgs{Up: true}, &r); err != nil {
		return fmt.Errorf("Irie's core: %w (is Irie running? `irie up` starts it)", err)
	}
	for _, pr := range r.Problems {
		fmt.Fprintln(stdout, "  "+pr)
	}
	if r.Search == "" {
		return errors.New("SearXNG did not start: `irie anon` says why, and its window irie-searxng in irie-agents shows its output")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	res, err := anon.Search(ctx, r.Search, anon.SearchQuery{Q: "open source metasearch engine"})
	if err != nil {
		return err
	}
	lines := strings.Split(res.Words("open source metasearch engine"), "\n")
	fmt.Fprintln(stdout, "\n"+strings.Join(lines[:min(len(lines), 8)], "\n")+"\n")
	fmt.Fprintf(stdout, "Ready. Type +anon before a task to research anonymously, +anon:NAME to share a compartment, +tor for Tor. Your own tools can search at %s, and `irie anon run NAME -- COMMAND` sends one through a compartment's exit.\n", r.Search)
	return nil
}

// anonOn sets anon.on = true: through the core when it runs, which keeps
// the file's comments and checks the result, else in the file.
func anonOn(p paths.Paths) error {
	set := ipc.ConfigArgs{Set: []ipc.ConfigSet{{Key: "anon.on", Value: json.RawMessage(`true`)}}}
	if err := ipc.Call(p.Socket(), 10*time.Second, ipc.OpConfig, set, nil); err == nil {
		return nil
	}
	data, err := os.ReadFile(p.ConfigFile())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	out, err := config.Apply(data, []config.Edit{{Key: "anon.on", Value: true}})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p.ConfigFile()), 0o700); err != nil {
		return err
	}
	return filetx.WriteFile(p.ConfigFile(), out, 0o600)
}
