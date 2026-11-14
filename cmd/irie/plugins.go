package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/plugins"
)

// irie plugins is Irie's plugins (DESIGN §4.17): sets of features that go on
// and off together, such as privacy.
const pluginsUsage = `usage: ` + brand.Slug + ` plugins [on|off PLUGIN [PART]]

` + brand.Name + `'s plugins: sets of features that go on and off together. Each part
also has a switch of its own, and the plugin's switch turns them all off.

  ` + brand.Slug + ` plugins                  each plugin, and how far each part is set up here
  ` + brand.Slug + ` plugins on PLUGIN        turn a plugin on, such as privacy
  ` + brand.Slug + ` plugins off PLUGIN       turn it off: all its parts go off with it
  ` + brand.Slug + ` plugins on PLUGIN PART   turn one part on, such as ` + brand.Slug + ` plugins on privacy vpn
  ` + brand.Slug + ` plugins off PLUGIN PART  turn one part off

  ` + brand.Slug + ` setup privacy [--yes]    set up each part of the privacy plugin in turn
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
				fmt.Fprintf(stdout, "`"+brand.Slug+" setup %s`, or %s in a task bar, sets up each part in turn.\n", pl.Name, pl.Setup)
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
