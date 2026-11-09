package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/plugins"
	"github.com/irie-people/irie/internal/vpn"
)

// irie plugins is Irie's plugins (DESIGN §4.17): sets of features that go on
// and off together, such as privacy.
const pluginsUsage = `usage: irie plugins [on|off PLUGIN [PART]]

Irie's plugins: sets of features that go on and off together. Each part
also has a switch of its own, and the plugin's switch turns them all off.

  irie plugins                  each plugin, and how far each part is set up here
  irie plugins on PLUGIN        turn a plugin on, such as privacy
  irie plugins off PLUGIN       turn it off: all its parts go off with it
  irie plugins on PLUGIN PART   turn one part on, such as irie plugins on privacy vpn
  irie plugins off PLUGIN PART  turn one part off

  irie setup privacy [--yes]    set up each part of the privacy plugin in turn
`

func pluginsCmd(args []string, stdout io.Writer) error {
	if len(args) > 0 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		fmt.Fprint(stdout, pluginsUsage)
		return nil
	}
	cfg, p, err := load()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		fmt.Fprint(stdout, plugins.Text(pluginStatus(cfg, p)))
		return nil
	}
	if (args[0] != "on" && args[0] != "off") || len(args) < 2 || len(args) > 3 {
		return errors.New(pluginsUsage)
	}
	part := ""
	if len(args) == 3 {
		part = args[2]
	}
	edits, err := plugins.Edits(args[1], part, args[0] == "on")
	if err != nil {
		return err
	}
	if err := setConfig(p, edits); err != nil {
		return err
	}
	if cfg, _, err = load(); err != nil {
		return err
	}
	for _, pl := range pluginStatus(cfg, p) {
		if pl.Name != args[1] {
			continue
		}
		if part == "" {
			fmt.Fprint(stdout, plugins.Text([]ipc.Plugin{pl}))
			if pl.On && pl.Setup != "" {
				fmt.Fprintf(stdout, "`irie setup %s`, or %s in a task bar, sets up each part in turn.\n", pl.Name, pl.Setup)
			}
			return nil
		}
		for _, x := range pl.Parts {
			if x.Name == part || strings.EqualFold(x.Title, part) {
				fmt.Fprintf(stdout, "%s %s: %s\n", plugins.Mark(x.State), x.Title, x.Detail)
			}
		}
	}
	return nil
}

// pluginStatus is how the plugins stand: the core's answer, which knows the
// VPN now, else what the files say.
func pluginStatus(cfg config.Config, p paths.Paths) []ipc.Plugin {
	var r ipc.PluginsResult
	if err := ipc.Call(p.Socket(), 5*time.Second, ipc.OpPlugins, ipc.PluginsArgs{}, &r); err == nil && len(r.Plugins) > 0 {
		return r.Plugins
	}
	return plugins.Status(plugins.Env{Config: cfg, Paths: p})
}

// setConfig makes edits to config.toml: through the core when it runs,
// which keeps the file's comments, checks the result and runs on it at
// once, else in the file.
func setConfig(p paths.Paths, edits []config.Edit) error {
	set := ipc.ConfigArgs{}
	for _, e := range edits {
		v, err := json.Marshal(e.Value)
		if err != nil {
			return err
		}
		set.Set = append(set.Set, ipc.ConfigSet{Key: e.Key, Value: v})
	}
	err := ipc.Call(p.Socket(), 10*time.Second, ipc.OpConfig, set, nil)
	var refused *ipc.Error
	switch {
	case err == nil:
		return nil
	case errors.As(err, &refused):
		// The core read the change and refused it.
		return err
	}
	data, err := os.ReadFile(p.ConfigFile())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	out, err := config.Apply(data, edits)
	if err != nil {
		return err
	}
	if _, err := config.Parse(out, p.ConfigFile()); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p.ConfigFile()), 0o700); err != nil {
		return err
	}
	return filetx.WriteFile(p.ConfigFile(), out, 0o600)
}

// asker asks yes-or-no questions on a terminal: yes to all with --yes, and
// no to all when stdin is no terminal.
type asker struct {
	yes bool
	in  *bufio.Reader
	out io.Writer
	tty bool
}

func newAsker(yes bool, stdin io.Reader, stdout io.Writer) *asker {
	return &asker{yes: yes, in: bufio.NewReader(stdin), out: stdout, tty: isTerminal(stdin)}
}

func (a *asker) ask(q string) bool {
	if a.yes {
		return true
	}
	if !a.tty {
		return false
	}
	fmt.Fprintf(a.out, "%s [y/N] ", q)
	line, _ := a.in.ReadString('\n')
	s := strings.ToLower(strings.TrimSpace(line))
	return s == "y" || s == "yes"
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
		fmt.Fprintln(stdout, "Install the Mullvad VPN app from https://mullvad.net/download and sign in to it, then run `irie setup privacy` again.")
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
		return fmt.Errorf("Irie's core: %w (is Irie running? `irie up` starts it)", err)
	}
	fmt.Fprintf(stdout, "Started %s, a session that takes you through it a step at a time. The first time, Claude Code asks whether to trust its folder, which is Irie's own: choose Yes, I trust this folder.\n", r.Name)
	return nil
}
