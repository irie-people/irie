//go:build !noprivacy

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/plugins"
	"github.com/irie-people/irie/internal/vpn"
)

// The privacy plugin's commands (DESIGN §4.17): irie dms, irie anon, irie
// browser and irie setup anon, browser and privacy. go build -tags
// noprivacy leaves this file out, with anon.go, browser.go and dms.go.
// The privacy plugin's lines in the usage.
const (
	privacyUsage = `  ` + brand.Slug + ` dms <command>      the dead man's switch guide's helpers: its checks,
                          check-in and calendar (` + brand.Slug + ` dms for help)
  ` + brand.Slug + ` setup anon [--yes] get anonymous research ready: install Tor and
                          SearXNG, and turn +anon and +tor tasks on
  ` + brand.Slug + ` anon [<command>]   anonymous research's compartments and exits: check,
                          run a program through one, forget (` + brand.Slug + ` anon help)
`
	privacySetupUsage = `  ` + brand.Slug + ` setup privacy [--yes]
                          set up each part of the privacy plugin in turn:
                          Mullvad VPN, anonymous research, Mullvad Browser
                          and the dead man's switch
`
	privacyServerUsage = `  ` + brand.Slug + ` browser [--path EXE] [--profile DIR] [--template DIR] [--headless=BOOL]
               [--idle DUR] [--proxy socks5://HOST:PORT] [--search URL] [--check URL]
                            the browser connector: an MCP server that drives
                            Mullvad Browser for an agent; --check opens a page
  ` + brand.Slug + ` anon mcp --dir DIR --compartment NAME [--search URL] [--evidence DIR]
                            an anonymous research session's tools: search,
                            fetch and the exit's check through its compartment
`
	privacyRuntimeUsage = `, and ` + brand.Name + `'s own Tor and SearXNG`
)

func init() {
	pluginCmds["dms"] = pluginCmd{run: dmsCmd}
	pluginCmds["anon"] = pluginCmd{run: anonCmd, code: func(args []string) (int, bool) {
		if len(args) > 0 && args[0] == "mcp" {
			return anonServe(args[1:]), true
		}
		return 0, false
	}}
	pluginCmds["browser"] = pluginCmd{code: func(args []string) (int, bool) { return browserCmd(args), true }}
	pluginSetups["anon"] = setupAnon
	pluginSetups["browser"] = setupBrowser
	pluginSetups["privacy"] = setupPrivacy
}

// setupPrivacy is `irie setup privacy`: the privacy plugin on, then each
// part that isn't ready, in turn, each after a yes (DESIGN §4.17). It
// installs nothing and changes nothing without one.
func setupPrivacy(args []string, stdin io.Reader, stdout io.Writer) error {
	flags := flag.NewFlagSet("setup privacy", flag.ContinueOnError)
	yes := flags.Bool("yes", false, "say yes to each part")
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
	a := newAsker(*yes, stdin, stdout)
	pl, _ := plugins.Find("privacy")
	if !pl.IsOn(cfg) {
		if !a.ask("The privacy plugin is off. Turn it on?") {
			fmt.Fprintln(stdout, "Leaving it off.")
			return nil
		}
		if err := setConfig(p, []config.Edit{{Key: pl.Key(), Value: true}}); err != nil {
			return err
		}
		if cfg, _, err = load(); err != nil {
			return err
		}
	}
	var parts []ipc.PluginPart
	status := pluginStatus(cfg, p)
	for _, x := range status {
		if x.Name == pl.Name {
			fmt.Fprint(stdout, plugins.Text([]ipc.Plugin{x}))
			parts = x.Parts
		}
	}
	for _, x := range parts {
		if x.State == ipc.PartReady || x.State == ipc.PartHeld {
			continue
		}
		fmt.Fprintf(stdout, "\n%s: %s\n", x.Title, x.Summary)
		if !a.ask("Set it up now?") {
			fmt.Fprintln(stdout, "Leaving it as it is.")
			continue
		}
		var err error
		switch x.Name {
		case plugins.VPN:
			err = setupVPN(cfg, p, stdout)
		case plugins.Anon:
			err = setupAnon(yesArgs(*yes), stdin, stdout)
		case plugins.Browser:
			err = setupBrowser(yesArgs(*yes), stdin, stdout)
		case plugins.Switch:
			err = startSwitchGuide(p, stdout)
		}
		if err != nil {
			fmt.Fprintf(stdout, "%s isn't set up: %v\n", x.Title, err)
		}
	}
	if cfg, _, err = load(); err != nil {
		return err
	}
	fmt.Fprintln(stdout)
	for _, x := range pluginStatus(cfg, p) {
		if x.Name == pl.Name {
			fmt.Fprintln(stdout, plugins.Short(x)+".")
		}
	}
	return nil
}

func yesArgs(yes bool) []string {
	if yes {
		return []string{"--yes"}
	}
	return nil
}

// setupVPN turns Mullvad VPN control on once the app is installed. The app
// itself is the person's to install: it runs a VPN service of its own, and
// signs in with their account.
func setupVPN(cfg config.Config, p paths.Paths, stdout io.Writer) error {
	if _, err := vpn.Find(expand(cfg.VPN.Mullvad)); err != nil {
		fmt.Fprintln(stdout, "Install the Mullvad VPN app from https://mullvad.net/download and sign in to it, then run `"+brand.Slug+" setup privacy` again.")
		return nil
	}
	if err := setConfig(p, []config.Edit{{Key: "vpn.on", Value: true}}); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Mullvad VPN control is on: every client shows the VPN, and :vpn on, :vpn off or :vpn PLACE in a task bar changes it.")
	return nil
}

// startSwitchGuide starts the dead man's switch guide, a session of its
// own, through the core.
func startSwitchGuide(p paths.Paths, stdout io.Writer) error {
	var r ipc.Session
	if err := ipc.Call(p.Socket(), 30*time.Second, ipc.OpTool, ipc.ToolArgs{Name: ipc.ToolSwitch}, &r); err != nil {
		return fmt.Errorf(brand.Name+"'s core: %w (is "+brand.Name+" running? `"+brand.Slug+" up` starts it)", err)
	}
	fmt.Fprintf(stdout, "Started %s, a session that takes you through it a step at a time. The first time, Claude Code asks whether to trust its folder, which is "+brand.Name+"'s own: choose Yes, I trust this folder.\n", r.Name)
	return nil
}
