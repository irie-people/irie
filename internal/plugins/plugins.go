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
	"path/filepath"
	"slices"
	"strings"

	"github.com/irie-people/irie/internal/brand"
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
	// Manifest is the file a plugin of your own comes from, in the plugins
	// folder (manifest.go); "" = one of Irie's.
	Manifest string

	on func(config.Config) bool // its switch
	// key is its switch's config key, such as plugins.privacy.
	key string
	// problem says why its manifest doesn't read: it has no switch, and no
	// parts, until it does.
	problem string
	// remote says a pane of it shows a page off this computer, so it is
	// off until its switch is set on.
	remote bool
	// runs says a helper of it runs a program of yours, so it is off until
	// its switch is set on.
	runs bool
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
	// Pane is the tab it brings to the Mac app's right sidebar, which shows
	// while the part is on (docs/proposals/plugin-panes.md). Its ID, and a
	// web pane's Store, are its plugin's when left empty.
	Pane *ipc.PluginPane
	// Helper is the helper of yours it is, from a manifest's
	// [helpers.<id>] table (helpers.go); nil = it is none.
	Helper *Helper

	own   func(config.Config) bool       // its own switch, as the file has it
	check func(Env, bool) (bool, string) // whether it has what it needs, and its state in a sentence, given whether it is on
	// titled is its title as the config makes it, such as the VPN that
	// vpn.provider names; nil = Title.
	titled func(config.Config) string
}

// title is the part's title in cfg.
func (x Part) title(cfg config.Config) string {
	if x.titled != nil {
		return x.titled(cfg)
	}
	return x.Title
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
	VPN  *ipc.VPN          // the VPN's mark, while vpn.on
	Mail []ipc.MailAccount // the mail accounts, by the core's last look at each
}

// order is the order plugins show in; a plugin this build leaves out is
// skipped.
var order = []string{"vpn", "privacy", "writing", "markets", "mail", "bitwarden", "terms", "extension", "yoink", "obsidian"}

// built are the plugins in this build, by name. Each plugin's file adds
// itself, and a build tag leaves a plugin's file out with its parts' code:
// go build -tags novpn,noprivacy,nowriting,nomarkets,nomail,nobitwarden,noterms,noextension,noyoink,noobsidian
// builds Irie without any of them (DESIGN §4.17).
var built = map[string]func() Plugin{}

// All are Irie's plugins in this build, in the order they show.
func All() []Plugin {
	var out []Plugin
	for _, name := range order {
		if f, ok := built[name]; ok {
			out = append(out, f())
		}
	}
	return out
}

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

// Status says how each plugin and each of its parts stands here: Irie's
// own, then your own, from the plugins folder.
func Status(e Env) []ipc.Plugin {
	all, _ := Here(e)
	return all
}

// Here says how each plugin and each of its parts stands here, Irie's own
// then yours, and how each of your bundles stands.
func Here(e Env) ([]ipc.Plugin, []ipc.PluginBundle) {
	mine, bundles := Mine(e.Paths.Plugins())
	var out []ipc.Plugin
	for _, p := range append(All(), mine...) {
		out = append(out, p.status(e))
	}
	var bs []ipc.PluginBundle
	for _, b := range bundles {
		bs = append(bs, b.status(out))
	}
	return out, bs
}

func (p Plugin) status(e Env) ipc.Plugin {
	// Parts is a list, even an empty one: clients decode it as one.
	w := ipc.Plugin{Name: p.Name, Title: p.Title, Summary: p.Summary, Setup: p.Setup, Manifest: p.Manifest, Problem: p.problem, Parts: []ipc.PluginPart{}}
	if p.problem != "" {
		return w
	}
	w.On = p.on(e.Config)
	if p.remote && !w.On {
		w.Summary += " It shows a page off this computer, so it is off until you turn it on."
	}
	if p.runs && !w.On {
		w.Summary += " It runs a program of yours, so it is off until you turn it on."
	}
	for _, x := range p.Parts {
		s := x.status(e, w.On)
		if x.Pane != nil && x.Held == "" {
			pane := *x.Pane
			if pane.ID == "" {
				pane.ID = p.Name + "." + x.Name
			}
			if pane.Kind == ipc.PaneWeb && pane.Store == "" {
				pane.Store = p.Name
			}
			s.Pane = &pane
		}
		w.Parts = append(w.Parts, s)
	}
	return w
}

func (x Part) status(e Env, pluginOn bool) ipc.PluginPart {
	if x.Held != "" {
		return ipc.PluginPart{Name: x.Name, Title: x.Title, Summary: x.Summary, State: ipc.PartHeld, Detail: "Not in " + brand.Name + " yet. " + x.Held}
	}
	own := x.own(e.Config)
	w := ipc.PluginPart{Name: x.Name, Title: x.title(e.Config), Summary: x.Summary, Switch: x.Switch, On: own, Setup: x.Setup, Brings: x.Brings}
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
			return x.Title + " isn't in " + brand.Name + " yet. " + x.Held
		case !p.on(cfg):
			return fmt.Sprintf("%s is off, as the %s plugin is: `"+brand.Slug+" plugins on %s` turns the plugin on.", x.title(cfg), p.Name, p.Name)
		case !x.own(cfg):
			return x.Refused
		}
		return ""
	}
	// A part of a plugin this build leaves out.
	return part + " isn't in this build of " + brand.Name + "."
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

// Edits are the changes to config.toml that turn one of Irie's plugins on
// or off, or one part of it when part is not "".
func Edits(plugin, part string, on bool) ([]config.Edit, error) {
	return edits(All(), brand.Name+"'s plugins are", plugin, part, on)
}

// EditsHere are the changes to config.toml that turn a plugin on or off,
// Irie's own or one of yours in dir, the plugins folder, or one part of it
// when part is not "".
func EditsHere(dir, plugin, part string, on bool) ([]config.Edit, error) {
	mine, _ := Mine(dir)
	return edits(append(All(), mine...), "the plugins here are", plugin, part, on)
}

// edits finds plugin among all, which a refusal names after are.
func edits(all []Plugin, are, plugin, part string, on bool) ([]config.Edit, error) {
	i := slices.IndexFunc(all, func(p Plugin) bool { return p.Name == plugin })
	if i < 0 {
		var names []string
		for _, p := range all {
			if p.problem == "" {
				names = append(names, p.Name)
			}
		}
		return nil, fmt.Errorf("no plugin %q: %s %s", plugin, are, strings.Join(names, ", "))
	}
	p := all[i]
	if p.problem != "" {
		return nil, fmt.Errorf("%s doesn't read, so its plugin has no switch yet: %s", filepath.Base(p.Manifest), p.problem)
	}
	if part == "" {
		edits := []config.Edit{{Key: p.key, Value: on}}
		// A plugin of one part with a switch, such as the VPN, comes on
		// whole: there is nothing else to want of it.
		if on && len(p.Parts) == 1 && p.Parts[0].Switch != "" && p.Parts[0].Held == "" {
			edits = append(edits, config.Edit{Key: p.Parts[0].Switch, Value: p.Parts[0].On})
		}
		return edits, nil
	}
	x, ok := p.Part(part)
	if !ok {
		return nil, fmt.Errorf("the %s plugin has no part %q: its parts are %s", p.Name, part, strings.Join(partNames(p), ", "))
	}
	if x.Held != "" {
		return nil, fmt.Errorf("%s isn't in "+brand.Name+" yet. %s", x.Title, x.Held)
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
		if p.Problem != "" {
			fmt.Fprintf(&b, "%s: its manifest doesn't read. %s\n", filepath.Base(p.Manifest), p.Problem)
			continue
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
	case ipc.PartMissing:
		return "?"
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
