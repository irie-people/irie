//go:build !novpn

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/plugins"
	"github.com/irie-people/irie/internal/vpn"
)

// The VPN plugin's commands (DESIGN §4.18): irie setup vpn. go build -tags
// novpn leaves this file out.

// vpnSetupUsage is the VPN plugin's line in the usage.
const vpnSetupUsage = `  ` + brand.Slug + ` setup vpn [--yes] [VPN]
                          choose the VPN to follow, of those this copy
                          offers, and show it in every client
`

func init() { pluginSetups["vpn"] = setupVPN }

// setupVPN is `irie setup vpn`: it chooses the VPN that Irie follows from
// those this copy offers, asks for what it connects to when it needs that,
// and turns VPN control on. It installs nothing: a VPN's app is the
// person's, or their company's, to install.
func setupVPN(args []string, stdin io.Reader, stdout io.Writer) error {
	flags := flag.NewFlagSet("setup vpn", flag.ContinueOnError)
	yes := flags.Bool("yes", false, "say yes to each question")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return errors.New(setupUsage)
	}
	cfg, p, err := load()
	if err != nil {
		return err
	}
	a := newAsker(*yes, stdin, stdout)
	offered := config.VPNOffered()
	fmt.Fprintln(stdout, "The VPNs "+brand.Name+" can follow here, ● where their command is installed:")
	for _, name := range offered {
		k, ok := vpn.KindOf(name)
		if !ok {
			continue
		}
		fmt.Fprintf(stdout, "  %s %-14s %s\n", vpnMark(cfg, k), k.Name, vpnLine(k))
	}
	name := flags.Arg(0)
	if name == "" {
		name = pickVPN(cfg, offered)
	}
	k, ok := vpn.KindOf(name)
	if !ok || !slices.Contains(offered, name) {
		return fmt.Errorf("%s doesn't follow %q: it follows %s", brand.Name, name, strings.Join(offered, ", "))
	}
	if !k.Runs() {
		return errors.New(k.NotHere())
	}
	fmt.Fprintln(stdout)
	if !a.ask("Follow " + k.Title + " in every client?") {
		fmt.Fprintln(stdout, "Leaving it as it is.")
		return nil
	}
	edits := []config.Edit{{Key: "vpn.provider", Value: k.Name}}
	if k.Name != cfg.VPN.Provider {
		// The last VPN's command and server stay behind: they go back to
		// their defaults, such as the server the copy's brand names.
		edits = append(edits, config.Edit{Key: "vpn.cli", Value: nil}, config.Edit{Key: "vpn.server", Value: nil})
		cfg.VPN.CLI, cfg.VPN.Server = "", brand.VPNServer
	}
	cfg.VPN.Provider = k.Name
	if k.Server != "" && k.Name != "command" {
		if k.Name == "system" {
			listSystemVPNs(stdout)
		}
		if got := a.line(fmt.Sprintf("vpn.server, %s [%s]:", k.Server, cfg.VPN.Server)); got != "" && got != cfg.VPN.Server {
			edits = append(edits, config.Edit{Key: "vpn.server", Value: got})
			cfg.VPN.Server = got
		}
	}
	edits = append(edits, config.Edit{Key: "plugins.vpn", Value: true}, config.Edit{Key: "vpn.on", Value: true})
	if err := setConfig(p, edits); err != nil {
		return err
	}
	if _, why := plugins.FindVPN(cfg); why != "" {
		fmt.Fprintln(stdout, "VPN control is on, and "+k.Title+" needs setting up: "+why)
		if k.Name == "command" {
			fmt.Fprintln(stdout, "In config.toml's [vpn], set status, connect and disconnect to your VPN's commands, each a list such as [\"/usr/local/bin/myvpn\", \"status\"], and connected to the pattern its status prints while connected.")
		}
		return nil
	}
	fmt.Fprintln(stdout, "VPN control is on: every client shows "+k.Title+", and :vpn on or :vpn off in any task bar changes it.")
	return nil
}

// vpnMark is ● for a VPN whose command is here, and · for one that isn't.
func vpnMark(cfg config.Config, k vpn.Kind) string {
	if k.Name != cfg.VPN.Provider {
		// vpn.cli is the configured VPN's own command, not this one's.
		cfg.VPN.CLI = ""
	}
	cfg.VPN.Provider = k.Name
	if _, err := k.Find(plugins.VPNSettings(cfg)); err == nil {
		return "●"
	}
	return "·"
}

// vpnLine is a VPN's line in the list: its name, and what it needs.
func vpnLine(k vpn.Kind) string {
	switch {
	case !k.Runs():
		return k.Title + ", which isn't for this system"
	case k.Name == "command":
		return k.Title + ", set in config.toml"
	case k.Needs:
		return k.Title + ", given " + k.Server
	}
	return k.Title
}

// pickVPN is the VPN to offer when none is named: the one config.toml
// names if its command is here, else the first that is, else the first.
func pickVPN(cfg config.Config, offered []string) string {
	if k, ok := vpn.KindOf(cfg.VPN.Provider); ok && slices.Contains(offered, k.Name) && vpnMark(cfg, k) == "●" {
		return k.Name
	}
	for _, name := range offered {
		if k, ok := vpn.KindOf(name); ok && k.Runs() && k.Name != "system" && k.Name != "command" && vpnMark(cfg, k) == "●" {
			return k.Name
		}
	}
	return offered[0]
}

// listSystemVPNs prints the names of the system's own VPNs, for vpn.server.
func listSystemVPNs(stdout io.Writer) {
	names, err := vpn.SystemVPNs()
	switch {
	case err != nil:
		fmt.Fprintln(stdout, "The system's VPNs can't be listed: "+err.Error())
	case len(names) == 0:
		fmt.Fprintln(stdout, "The system has no VPN yet: add one in System Settings › VPN (macOS) or NetworkManager (Linux).")
	default:
		fmt.Fprintln(stdout, "The system's VPNs: "+strings.Join(names, ", "))
	}
}

// line asks for a line of text on a terminal, and returns it trimmed; ""
// with --yes or without a terminal, for the value already there.
func (a *asker) line(q string) string {
	if a.yes || !a.tty {
		return ""
	}
	fmt.Fprintf(a.out, "%s ", q)
	s, _ := a.in.ReadString('\n')
	return strings.TrimSpace(s)
}
