package plugins

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// Bundles (docs/proposals/plugin-panes.md): a manifest with a plugins list
// names plugins, or parts of them as PLUGIN.PART, that go on and off
// together. Its switch sets each one's own switch, and holds none: so each
// keeps its own switch, and the bundle reads on only while all of them are.

// status says how the bundle stands, from how the plugins here stand.
func (b Bundle) status(all []ipc.Plugin) ipc.PluginBundle {
	w := ipc.PluginBundle{Name: b.Name, Title: b.Title, Summary: b.Summary, Manifest: b.Manifest, Problem: b.Problem, State: ipc.BundleOff, Plugins: []ipc.BundlePlugin{}}
	if b.Problem != "" {
		return w
	}
	on, n := 0, 0
	for _, ref := range b.Plugins {
		m := member(ref, all)
		w.Plugins = append(w.Plugins, m)
		if m.State == ipc.PartMissing || m.State == ipc.PartHeld {
			continue
		}
		n++
		if m.On {
			on++
		}
	}
	switch {
	case n > 0 && on == n:
		w.State = ipc.BundleOn
	case on > 0:
		w.State = ipc.BundleSome
	}
	return w
}

// member is how one plugin or part that a bundle names stands.
func member(ref string, all []ipc.Plugin) ipc.BundlePlugin {
	plugin, part, _ := strings.Cut(ref, ".")
	m := ipc.BundlePlugin{Name: ref, Plugin: plugin, Part: part, Title: ref, State: ipc.PartMissing}
	i := slices.IndexFunc(all, func(p ipc.Plugin) bool { return p.Name == plugin })
	if i < 0 {
		m.Detail = "No plugin named " + plugin + " here: neither one of this build's nor a manifest " + plugin + ".toml in the plugins folder."
		return m
	}
	p := all[i]
	m.Title = p.Title
	if p.Problem != "" {
		m.State, m.Detail = ipc.PartNeeds, filepath.Base(p.Manifest)+" doesn't read: "+p.Problem
		return m
	}
	if part == "" {
		// As the bundle's switch turns it on: a plugin of one part with a
		// switch, such as the VPN, comes on whole (edits).
		m.On = p.On
		if x, ok := onlySwitch(p); ok {
			m.On = p.On && x.On
		}
		m.State, m.Detail = pluginState(p)
		return m
	}
	j := slices.IndexFunc(p.Parts, func(x ipc.PluginPart) bool { return x.Name == part })
	if j < 0 {
		m.Title = p.Title + " › " + part
		m.Detail = "The " + p.Title + " plugin has no part named " + part + " in this build."
		return m
	}
	x := p.Parts[j]
	m.Title = x.Title
	m.On = p.On && (x.Switch == "" || x.On)
	m.State, m.Detail = x.State, x.Detail
	if x.Switch == "" && x.State != ipc.PartHeld {
		m.Detail = strings.TrimSpace(m.Detail + " It has no switch of its own: it is on while the " + p.Title + " plugin is.")
	}
	return m
}

// pluginState is a whole plugin's state, as a part's would be: off, a part
// that needs something, ready, or on with none of its parts on.
func pluginState(p ipc.Plugin) (string, string) {
	if !p.On {
		return ipc.PartOff, "Off: its own switch, or the bundle's, turns it on."
	}
	for _, x := range p.Parts {
		if x.State == ipc.PartNeeds {
			return ipc.PartNeeds, x.Detail
		}
	}
	for _, x := range p.Parts {
		if x.State == ipc.PartReady {
			if x.Detail == "" {
				return ipc.PartReady, "On."
			}
			return ipc.PartReady, x.Detail
		}
	}
	return ipc.PartOff, "On, but none of its parts is: each has a switch of its own, in Settings › Plugins."
}

// onlySwitch is a plugin's one part with a switch of its own, when it has
// one part and no other.
func onlySwitch(p ipc.Plugin) (ipc.PluginPart, bool) {
	if len(p.Parts) == 1 && p.Parts[0].Switch != "" && p.Parts[0].State != ipc.PartHeld {
		return p.Parts[0], true
	}
	return ipc.PluginPart{}, false
}

// FindBundle is the bundle named name in dir, the plugins folder.
func FindBundle(dir, name string) (Bundle, bool) {
	_, bundles := Mine(dir)
	i := slices.IndexFunc(bundles, func(b Bundle) bool { return b.Name == name })
	if i < 0 {
		return Bundle{}, false
	}
	return bundles[i], true
}

// BundleEdits are the changes to config.toml that turn each plugin or part
// the bundle named name names on or off: a plugin by its switch, a part by
// its own, and a part going on takes its plugin on with it. A part with no
// switch of its own can only go on with its plugin, and stays while the
// plugin does. What isn't here is left out.
func BundleEdits(dir, name string, on bool) ([]config.Edit, error) {
	mine, bundles := Mine(dir)
	i := slices.IndexFunc(bundles, func(b Bundle) bool { return b.Name == name })
	if i < 0 {
		return nil, fmt.Errorf("no bundle %q in the plugins folder", name)
	}
	b := bundles[i]
	if b.Problem != "" {
		return nil, fmt.Errorf("%s doesn't read, so its bundle has no switch yet: %s", filepath.Base(b.Manifest), b.Problem)
	}
	all := append(All(), mine...)
	var out []config.Edit
	set := map[string]bool{}
	add := func(key string, v any) {
		if !set[key] {
			set[key] = true
			out = append(out, config.Edit{Key: key, Value: v})
		}
	}
	for _, ref := range b.Plugins {
		plugin, part, _ := strings.Cut(ref, ".")
		j := slices.IndexFunc(all, func(p Plugin) bool { return p.Name == plugin })
		if j < 0 || all[j].problem != "" {
			continue
		}
		p := all[j]
		if part == "" {
			// The plugin goes on or off as irie plugins on PLUGIN turns it.
			es, err := edits(all, "", p.Name, "", on)
			if err != nil {
				continue
			}
			for _, e := range es {
				add(e.Key, e.Value)
			}
			continue
		}
		x, ok := p.Part(part)
		if !ok || x.Held != "" {
			continue
		}
		if on {
			add(p.key, true)
		}
		if x.Switch != "" {
			v := x.Off
			if on {
				v = x.On
			}
			add(x.Switch, v)
		}
	}
	if len(out) == 0 {
		word := "off"
		if on {
			word = "on"
		}
		return nil, fmt.Errorf("the %s bundle has nothing here to turn %s: none of %s is here", b.Name, word, strings.Join(b.Plugins, ", "))
	}
	return out, nil
}

// BundleText is the bundles in words: a line for each, then one for each
// plugin or part it names.
func BundleText(bundles []ipc.PluginBundle) string {
	var b strings.Builder
	for i, x := range bundles {
		if i > 0 {
			b.WriteString("\n")
		}
		if x.Problem != "" {
			fmt.Fprintf(&b, "%s: its manifest doesn't read. %s\n", filepath.Base(x.Manifest), x.Problem)
			continue
		}
		fmt.Fprintf(&b, "%s bundle: %s.", x.Title, BundleWords(x))
		if x.Summary != "" {
			fmt.Fprintf(&b, " %s", x.Summary)
		}
		b.WriteString("\n")
		for _, m := range x.Plugins {
			detail := m.Detail
			if detail == "" {
				detail = "Off."
			}
			fmt.Fprintf(&b, "  %s %s: %s\n", Mark(m.State), m.Title, detail)
		}
	}
	return b.String()
}

// BundleWords is a bundle's state in words: on, off, or how many of its
// plugins are on.
func BundleWords(x ipc.PluginBundle) string {
	switch x.State {
	case ipc.BundleOn:
		return "on"
	case ipc.BundleSome:
		on, n := 0, 0
		for _, m := range x.Plugins {
			if m.State == ipc.PartMissing || m.State == ipc.PartHeld {
				continue
			}
			n++
			if m.On {
				on++
			}
		}
		return fmt.Sprintf("%d of %d on", on, n)
	}
	return "off"
}

// AllText is the bundles, then the plugins, in words, as irie plugins
// prints them and :plugins answers.
func AllText(all []ipc.Plugin, bundles []ipc.PluginBundle) string {
	if len(bundles) == 0 {
		return Text(all)
	}
	return BundleText(bundles) + "\n" + Text(all)
}
