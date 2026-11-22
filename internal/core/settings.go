package core

import (
	"maps"
	"slices"
	"strings"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/ipc"
)

// settings answers the config op (DESIGN §5.5): it makes the changes asked
// for to the config file, in place, runs on the result from then on, and
// returns the config in effect.
func (c *Core) settings(a ipc.ConfigArgs) (ipc.ConfigResult, error) {
	return c.settingsWith(a, nil)
}

// settingsFrom is the config op from the process peer. Settings are inside
// ops (DESIGN §4.5), but the arch agents' guards are the person's alone to
// lower: from inside an agent, a change that would lower one is refused
// whole (keepGuards).
func (c *Core) settingsFrom(peer int, a ipc.ConfigArgs) (ipc.ConfigResult, error) {
	var guard func(before, after config.Config) error
	if len(a.Set) > 0 && c.fromAgent(peer) {
		guard = keepGuards
	}
	return c.settingsWith(a, guard)
}

func (c *Core) settingsWith(a ipc.ConfigArgs, guard func(before, after config.Config) error) (ipc.ConfigResult, error) {
	path := c.paths.ConfigFile()
	if len(a.Set) > 0 {
		if err := c.configureWith(path, a.Set, guard); err != nil {
			return ipc.ConfigResult{}, err
		}
	}
	r := ipc.ConfigResult{Path: path, Config: config.Tree(*c.cfg())}
	if _, err := config.Load(path); err != nil {
		r.Problem = err.Error()
	}
	return r, nil
}

// keepGuards refuses a change that would lower one of the arch agents'
// guards (DESIGN §4.11): Mikael, his door, or the good he hears in the
// person's own words; and Gabriel's guard against windows, with Gabriel
// himself while it stands, since it works only while he is on. Raising a
// guard, and every other setting, stay an agent's to change.
func keepGuards(before, after config.Config) error {
	m0, m1 := before.Arch.Mikael, after.Arch.Mikael
	g0, g1 := before.Arch.Gabriel, after.Arch.Gabriel
	var lowered []string
	if m0.On && !m1.On {
		lowered = append(lowered, "arch.mikael.on")
	}
	if m0.Door && !m1.Door {
		lowered = append(lowered, "arch.mikael.door")
	}
	if !slices.Equal(m0.Good, m1.Good) {
		lowered = append(lowered, "arch.mikael.good")
	}
	if g0.Guard && !g1.Guard {
		lowered = append(lowered, "arch.gabriel.guard")
	}
	if g0.On && g0.Guard && !g1.On {
		lowered = append(lowered, "arch.gabriel.on")
	}
	if len(lowered) > 0 {
		return ipc.Errorf(ipc.CodeBadArgs, "An agent can't turn off or reword an arch agent's guard (%s): only you can, in %s's Settings › Arch Agents or from a terminal of your own.", strings.Join(lowered, ", "), brand.Name)
	}
	// A plugin of yours goes on only at your word, as the plugins op
	// says: one can run programs of yours and give agents its tools.
	var turned []string
	for _, name := range slices.Sorted(maps.Keys(after.Plugins.Manifests)) {
		if after.Plugins.Manifests[name] && !before.Plugins.Manifests[name] {
			turned = append(turned, "plugins.manifests."+name)
		}
	}
	if len(turned) > 0 {
		return ipc.Errorf(ipc.CodeBadArgs, "An agent can't turn a plugin on (%s): a plugin can run programs of yours and give every agent its tools, so that is yours to do, in %s's Settings › Plugins or from a terminal of your own.", strings.Join(turned, ", "), brand.Name)
	}
	return nil
}

// configure edits the config file and swaps in the config it then holds,
// hand edits made since the core read it included. A change the file
// cannot take, or a config that would not load, leaves both as they were.
// What the core reads only as it starts, such as tmux.socket, still waits
// for its next start, and Irie's own Ollama server keeps its settings until
// it starts again.
func (c *Core) configure(path string, sets []ipc.ConfigSet) error {
	return c.configureWith(path, sets, nil)
}

// configureWith is configure that first asks guard, when there is one,
// about the change: what the file held against what it would hold. A
// refusal leaves the file and the config as they were.
func (c *Core) configureWith(path string, sets []ipc.ConfigSet, guard func(before, after config.Config) error) error {
	edits := make([]config.Edit, len(sets))
	keys := make([]string, len(sets))
	for i, s := range sets {
		v, err := config.ParseValue(s.Key, s.Value)
		if err != nil {
			return ipc.Errorf(ipc.CodeBadArgs, "%v", err)
		}
		edits[i], keys[i] = config.Edit{Key: s.Key, Value: v}, s.Key
	}
	c.confMu.Lock()
	defer c.confMu.Unlock()
	// The edit is one transaction on the file (internal/filetx): a writer
	// elsewhere waits its turn, so neither loses the other's change, and no
	// reader sees half of the file. A config file linked from elsewhere, as
	// dotfiles often are, is written where it lives, so the link stays. The
	// file keeps its mode; a new one is the user's alone.
	var cfg config.Config
	err := filetx.Update(path, 0o600, func(data []byte) ([]byte, error) {
		out, err := config.Apply(data, edits)
		if err != nil {
			return nil, ipc.Errorf(ipc.CodeBadArgs, "config %s: %v", path, err)
		}
		if cfg, err = config.Parse(out, path); err != nil {
			return nil, ipc.Errorf(ipc.CodeBadArgs, "%v", err)
		}
		if guard != nil {
			// Weighed against what the file holds now, hand edits
			// included; a file that doesn't load is weighed against the
			// config the core runs on.
			before, err := config.Parse(data, path)
			if err != nil {
				before = *c.cfg()
			}
			if err := guard(before, cfg); err != nil {
				return nil, err
			}
		}
		return out, nil
	})
	if err != nil {
		return err
	}
	was := c.cfg().Alerts
	c.useConfig(cfg)
	c.log.Info("settings", "keys", keys)
	if cfg.Alerts != was {
		go c.alertsKeep()
	}
	// Every sidebar hears red_on_done and close_when_done at once.
	c.publishHeader()
	return nil
}
