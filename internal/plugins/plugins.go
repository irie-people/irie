// Package plugins is Irie's plugins (DESIGN §4.17): sets of Irie's features
// that go on and off together under one name. A plugin lists its parts, and
// each part says what it brings to Irie, which switch turns it on, what sets
// it up and how far it is set up on this computer.
//
// A plugin's code is Irie's own, built into the irie binary and tested with
// the rest, so nothing is downloaded to run one. [plugins] in config.toml
// turns each one on or off. A plugin that is off turns all its parts off,
// whatever their own switches say: config.Config's VPNOn and the like are
// where the rest of Irie asks.
package plugins

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/repo"
	"github.com/irie-people/irie/internal/skills"
)

// Plugin is one of Irie's plugins.
type Plugin struct {
	Name    string // its key under [plugins], such as privacy
	Title   string // Privacy
	Summary string // what it is for, in a sentence
	Setup   string // the words that set up each of its parts in turn; "" = none
	Parts   []Part

	on func(config.Config) bool // its switch
	// key is its switch's config key, such as plugins.privacy.
	key string
}

// Part is one feature of a plugin.
type Part struct {
	Name    string // vpn: how irie plugins and the words name it
	Title   string // Mullvad VPN
	Summary string // what it lets you do, in a sentence
	// Switch is the config key that turns it on, such as vpn.on, and On
	// and Off the values it takes; "" = it has no switch of its own.
	Switch  string
	On, Off any
	Setup   string   // the words that set it up, such as :setup anon; "" = none
	Brings  []string // what it adds to Irie: commands, words, skills, agents' tools
	// Refused is what its words and commands say while its own switch is
	// off.
	Refused string
	// Skills are the skills of Irie's that it brings: they go to sessions
	// only while its plugin is on.
	Skills []string
	// Held says why the part is not in this build yet, or "" when it is:
	// it shows, so you know it is coming, and nothing of it runs.
	Held string

	own   func(config.Config) bool       // its own switch, as the file has it
	check func(Env, bool) (bool, string) // whether it has what it needs, and its state in a sentence, given whether it is on
}

// Env is what a part's check reads: the config, Irie's folders and what only
// the running core knows. Live is zero outside the core.
type Env struct {
	Config config.Config
	Paths  paths.Paths
	Live   Live
}

// Live is what the core knows that no file says.
type Live struct {
	VPN *ipc.VPN // the VPN's mark, while vpn.on
}

// All are Irie's plugins, in the order they show.
func All() []Plugin { return []Plugin{privacy(), writing()} }

// Find is the plugin named name.
func Find(name string) (Plugin, bool) {
	for _, p := range All() {
		if p.Name == name {
			return p, true
		}
	}
	return Plugin{}, false
}

// Part is the part named name, which may also be its title.
func (p Plugin) Part(name string) (Part, bool) {
	for _, x := range p.Parts {
		if strings.EqualFold(x.Name, name) || strings.EqualFold(x.Title, name) {
			return x, true
		}
	}
	return Part{}, false
}

// Key is the config key that turns the plugin on.
func (p Plugin) Key() string { return p.key }

// IsOn says whether the plugin is on in cfg.
func (p Plugin) IsOn(cfg config.Config) bool { return p.on(cfg) }

// Names are the plugins' names, for messages.
func Names() []string {
	var out []string
	for _, p := range All() {
		out = append(out, p.Name)
	}
	return out
}

// Status says how each plugin and each of its parts stands here.
func Status(e Env) []ipc.Plugin {
	var out []ipc.Plugin
	for _, p := range All() {
		w := ipc.Plugin{Name: p.Name, Title: p.Title, Summary: p.Summary, On: p.on(e.Config), Setup: p.Setup}
		for _, x := range p.Parts {
			w.Parts = append(w.Parts, x.status(e, w.On))
		}
		out = append(out, w)
	}
	return out
}

func (x Part) status(e Env, pluginOn bool) ipc.PluginPart {
	if x.Held != "" {
		return ipc.PluginPart{Name: x.Name, Title: x.Title, Summary: x.Summary, State: ipc.PartHeld, Detail: "Not in Irie yet. " + x.Held}
	}
	own := x.own(e.Config)
	w := ipc.PluginPart{Name: x.Name, Title: x.Title, Summary: x.Summary, Switch: x.Switch, On: own, Setup: x.Setup, Brings: x.Brings}
	ok, detail := x.check(e, own && pluginOn)
	switch {
	case !pluginOn:
		w.State, w.Detail = ipc.PartPluginOff, "Off, as its plugin is."
	case !own:
		w.State, w.Detail = ipc.PartOff, detail
	case !ok:
		w.State, w.Detail = ipc.PartNeeds, detail
	default:
		w.State, w.Detail = ipc.PartReady, detail
	}
	return w
}

// Why is what a word or command of the part named part says when it can't
// run because the part is off, or "" when it is on.
func Why(cfg config.Config, part string) string {
	for _, p := range All() {
		x, ok := p.Part(part)
		if !ok {
			continue
		}
		switch {
		case x.Held != "":
			return x.Title + " isn't in Irie yet. " + x.Held
		case !p.on(cfg):
			return fmt.Sprintf("%s is off, as the %s plugin is: `irie plugins on %s` turns the plugin on.", x.Title, p.Name, p.Name)
		case !x.own(cfg):
			return x.Refused
		}
		return ""
	}
	return ""
}

// SkillsLeft are the skills of Irie's that sessions go without because
// their plugin is off, each with why.
func SkillsLeft(cfg config.Config) map[string]string {
	out := map[string]string{}
	for _, p := range All() {
		if p.on(cfg) {
			continue
		}
		for _, x := range p.Parts {
			for _, s := range x.Skills {
				out[s] = fmt.Sprintf("the %s plugin is off (%s)", p.Name, p.key)
			}
		}
	}
	return out
}

// SkillSettings decide which of Irie's skills a session gets: skills.enabled
// and skills.disabled, whether its profile takes skills, and which plugins
// are off.
func SkillSettings(cfg config.Config, profile bool) skills.Settings {
	return skills.Settings{Enabled: cfg.Skills.Enabled, Disabled: cfg.Skills.Disabled, Profile: profile, Left: SkillsLeft(cfg)}
}

// Edits are the changes to config.toml that turn a plugin on or off, or one
// part of it when part is not "".
func Edits(plugin, part string, on bool) ([]config.Edit, error) {
	p, ok := Find(plugin)
	if !ok {
		return nil, fmt.Errorf("no plugin %q: Irie's plugins are %s", plugin, strings.Join(Names(), ", "))
	}
	if part == "" {
		return []config.Edit{{Key: p.key, Value: on}}, nil
	}
	x, ok := p.Part(part)
	if !ok {
		return nil, fmt.Errorf("the %s plugin has no part %q: its parts are %s", p.Name, part, strings.Join(partNames(p), ", "))
	}
	if x.Held != "" {
		return nil, fmt.Errorf("%s isn't in Irie yet. %s", x.Title, x.Held)
	}
	if x.Switch == "" {
		return nil, fmt.Errorf("%s has no switch of its own: it is on while the %s plugin is", x.Title, p.Name)
	}
	v := x.Off
	if on {
		v = x.On
	}
	return []config.Edit{{Key: x.Switch, Value: v}}, nil
}

func partNames(p Plugin) []string {
	var out []string
	for _, x := range p.Parts {
		out = append(out, x.Name)
	}
	return out
}

// Text is the plugins' status in words: a line for each plugin, then one for
// each part, as irie plugins prints it and the :plugins words answer.
func Text(all []ipc.Plugin) string {
	var b strings.Builder
	for i, p := range all {
		if i > 0 {
			b.WriteString("\n")
		}
		on := "off"
		if p.On {
			on = "on"
		}
		fmt.Fprintf(&b, "%s plugin: %s. %s\n", p.Title, on, p.Summary)
		for _, x := range p.Parts {
			fmt.Fprintf(&b, "  %s %s: %s\n", Mark(x.State), x.Title, x.Detail)
		}
	}
	return b.String()
}

// Mark is a part's state as one glyph, so a list reads at a glance. Each
// state has its own, so colour never carries it alone.
func Mark(state string) string {
	switch state {
	case ipc.PartReady:
		return "●"
	case ipc.PartNeeds:
		return "!"
	case ipc.PartHeld:
		return "○"
	}
	return "·"
}

// Short is a plugin's state in a line: which parts are ready, and which
// lack something.
func Short(p ipc.Plugin) string {
	if !p.On {
		return p.Title + " plugin: off"
	}
	var ready, needs []string
	for _, x := range p.Parts {
		if x.State == ipc.PartHeld {
			continue
		}
		switch x.State {
		case ipc.PartReady:
			ready = append(ready, x.Title)
		case ipc.PartNeeds:
			needs = append(needs, x.Title)
		}
	}
	s := p.Title + " plugin: on"
	if len(ready) > 0 {
		s += "; ready: " + strings.Join(ready, ", ")
	}
	if len(needs) > 0 {
		s += "; needs setting up: " + strings.Join(needs, ", ")
	}
	if len(ready) == 0 && len(needs) == 0 {
		s += "; no part of it is on yet"
	}
	return s
}

// expand is a configured path with ~ for the home folder.
func expand(path string) string {
	if path == "" {
		return ""
	}
	h, _ := os.UserHomeDir()
	return repo.Expand(path, h)
}

// has says whether list holds s.
func has(list []string, s string) bool { return slices.Contains(list, s) }
