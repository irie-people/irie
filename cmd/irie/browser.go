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
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/irie-people/irie/internal/browser"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/repo"
	"github.com/irie-people/irie/internal/version"
)

// browserCmd is `irie browser`: the browser connector, an MCP server on
// stdin and stdout that Irie gives each Claude Code session (DESIGN §4.9).
// With --check it opens one page, prints what an agent would read there
// and exits.
func browserCmd(args []string) int {
	o, check, err := browser.ParseArgs(args, os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "irie browser:", err)
		return 2
	}
	o.Version = version.String()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	// A reply to a client that has gone fails instead of killing the
	// server before it closes its browser.
	signal.Ignore(syscall.SIGPIPE)
	if check != "" {
		text, err := browser.Check(ctx, o, check)
		if err != nil {
			fmt.Fprintln(os.Stderr, "irie browser:", err)
			return 1
		}
		fmt.Println(text)
		return 0
	}
	if err := browser.Serve(ctx, os.Stdin, os.Stdout, o); err != nil {
		fmt.Fprintln(os.Stderr, "irie browser:", err)
		return 1
	}
	return 0
}

// setupBrowser is `irie setup browser`: it gets Irie's browser ready
// (DESIGN §4.15). It finds Mullvad Browser, or installs it by its route
// here after a yes; makes the clean profile each start copies; opens a page
// as an agent would; and turns browser.connector on, so Claude Code
// sessions started from then on get the browser.
func setupBrowser(args []string, stdin io.Reader, stdout io.Writer) error {
	flags := flag.NewFlagSet("setup browser", flag.ContinueOnError)
	yes := flags.Bool("yes", false, "install Mullvad Browser without asking")
	page := flags.String("page", "https://example.com/", "the page to open as a check")
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
	path := cfg.Browser.Path
	if path != "" {
		h, _ := os.UserHomeDir()
		path = repo.Expand(path, h)
	}
	exe, err := browser.Find(path)
	if errors.Is(err, browser.ErrNotFound) {
		inst := browser.HowToInstall(runtime.GOOS, exec.LookPath)
		fmt.Fprintf(stdout, "Mullvad Browser is not installed. Its route here: %s\n", inst.Hint)
		if inst.Argv == nil {
			return errors.New("install Mullvad Browser, then run `irie setup browser` again")
		}
		ok := *yes
		if !ok && isTerminal(stdin) {
			fmt.Fprintf(stdout, "Run `%s` now? [y/N] ", strings.Join(inst.Argv, " "))
			line, _ := bufio.NewReader(stdin).ReadString('\n')
			a := strings.ToLower(strings.TrimSpace(line))
			ok = a == "y" || a == "yes"
		}
		if !ok {
			return errors.New("Mullvad Browser is not installed; run `irie setup browser --yes` to install it")
		}
		cmd := exec.Command(inst.Argv[0], inst.Argv[1:]...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("installing Mullvad Browser: %w", err)
		}
		exe, err = browser.Find(path)
	}
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	o := browser.Options{
		Path: path, Template: p.BrowserTemplate(), Headless: cfg.Browser.Headless,
		Proxy: cfg.Browser.Proxy, Search: cfg.Browser.Search, Version: version.String(),
	}
	fmt.Fprintf(stdout, "Making the clean profile each start copies, in %s…\n", o.Template)
	if err := browser.MakeTemplate(ctx, o, exe); err != nil {
		return fmt.Errorf("making the template profile: %w", err)
	}
	fmt.Fprintf(stdout, "Opening %s as an agent would…\n\n", *page)
	text, err := browser.Check(ctx, o, *page)
	if err != nil {
		return err
	}
	lines := strings.Split(text, "\n")
	fmt.Fprintln(stdout, strings.Join(lines[:min(len(lines), 8)], "\n"))
	fmt.Fprintln(stdout)
	if err := connectorOn(p); err != nil {
		return fmt.Errorf("the browser works, but browser.connector could not be turned on: %w", err)
	}
	fmt.Fprintln(stdout, "Ready: Claude Code sessions started from now on get Irie's browser as irie-browser. `irie config print` shows its settings under [browser].")
	return nil
}

// connectorOn sets browser.connector = "on": through the core when it runs,
// which keeps the file's comments and checks the result, else in the file.
func connectorOn(p paths.Paths) error {
	set := ipc.ConfigArgs{Set: []ipc.ConfigSet{{Key: "browser.connector", Value: json.RawMessage(`"on"`)}}}
	if err := ipc.Call(p.Socket(), 10*time.Second, ipc.OpConfig, set, nil); err == nil {
		return nil
	}
	data, err := os.ReadFile(p.ConfigFile())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	out, err := config.Apply(data, []config.Edit{{Key: "browser.connector", Value: "on"}})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p.ConfigFile()), 0o700); err != nil {
		return err
	}
	return filetx.WriteFile(p.ConfigFile(), out, 0o600)
}
