package plugins

import (
	"fmt"
	"io"
	"maps"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// Plugins of your own (docs/proposals/plugin-panes.md). A manifest in the
// plugins folder of Irie's config folder, plugins/NAME.toml, is a plugin
// named NAME made of parts that need no code of Irie's, for now panes that
// show a web page in the Mac app's right sidebar. With a plugins list it is
// a bundle instead: plugins, or parts of them, that go on and off together.
// A manifest is data: Irie reads it and runs nothing from it.

// The most a folder may hold, and a manifest's file, so a stray file can't
// slow the plugins op.
const (
	maxManifests    = 64
	maxManifestSize = 64 << 10
)

// manifestName is a manifest's name, its file's without .toml, and a pane's
// or a part's: the words config keys and skills take.
var manifestName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// symbolName is an SF Symbols name, such as doc.text.
var symbolName = regexp.MustCompile(`^[a-z0-9]+(\.[a-z0-9]+)*$`)

// manifestFile is a manifest as its TOML reads.
type manifestFile struct {
	Title   string              `toml:"title"`
	Summary string              `toml:"summary"`
	Panes   map[string]paneFile `toml:"panes"`
	// Helpers are programs of yours that Irie keeps running (helpers.go).
	Helpers map[string]helperFile `toml:"helpers"`
	// Skills and Tools are what agents' sessions get from the plugin: a
	// skill of yours, and an MCP server of yours (ownskills.go,
	// owntools.go).
	Skills map[string]skillFile `toml:"skills"`
	Tools  map[string]toolsFile `toml:"tools"`
	// Schedules are kept for a later build, which reads them: until then
	// each shows as a part that is held.
	Schedules map[string]toml.Primitive `toml:"schedules"`
	// Plugins makes the manifest a bundle's: what it turns on and off, a
	// plugin's name or PLUGIN.PART a line.
	Plugins []string `toml:"plugins"`
}

// paneFile is one of a manifest's [panes.<id>] tables.
type paneFile struct {
	Title   string `toml:"title"`
	Address string `toml:"address"`
	Icon    string `toml:"icon"`
	Start   string `toml:"start"`
	Remote  bool   `toml:"remote"`
}

// Bundle is one of your bundles: plugins, or parts of them, that go on and
// off together under one switch.
type Bundle struct {
	Name, Title, Summary string
	Manifest             string   // its file
	Plugins              []string // what it names, a plugin's name or PLUGIN.PART each
	Problem              string   // why its manifest doesn't read; "" = it does
}

// Mine are your own plugins and bundles: those of the manifests in dir, the
// plugins folder, each in the order of its file's name. A manifest that
// doesn't read is a plugin, or a bundle, that says why. No folder, none.
func Mine(dir string) ([]Plugin, []Bundle) {
	if dir == "" {
		return nil, nil
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	var ps []Plugin
	var bs []Bundle
	n := 0
	for _, ent := range ents {
		file := ent.Name()
		if ent.IsDir() || strings.HasPrefix(file, ".") || filepath.Ext(file) != ".toml" {
			continue
		}
		if n++; n > maxManifests {
			break
		}
		p, b := readManifest(filepath.Join(dir, file))
		if b != nil {
			bs = append(bs, *b)
		} else {
			ps = append(ps, p)
		}
	}
	firstNames(ps)
	return ps, bs
}

// readManifest reads one manifest: a plugin, or a bundle when it has a
// plugins list.
func readManifest(path string) (Plugin, *Bundle) {
	file := filepath.Base(path)
	name := strings.TrimSuffix(file, ".toml")
	broken := func(why string) (Plugin, *Bundle) {
		return Plugin{Name: name, Title: name, Manifest: path, problem: why, on: func(config.Config) bool { return false }}, nil
	}
	switch {
	case !manifestName.MatchString(name) || len(name) > 64:
		// Its name is no plugin's, so the file's stands in for it.
		name = file
		return broken(file + " isn't a manifest's name: lowercase letters, digits and single hyphens, at most 64, then .toml, such as invoices.toml. The name is the plugin's.")
	case slices.Contains(order, name):
		name = file
		return broken(file + " has the name of one of " + brand.Name + "'s own plugins: give the file another.")
	}
	data, why := readSmall(path)
	if why != "" {
		return broken(why)
	}
	var mf manifestFile
	md, err := toml.Decode(string(data), &mf)
	if err != nil {
		return broken("It doesn't read as TOML: " + err.Error())
	}
	if keys := undecoded(md); len(keys) > 0 {
		return broken("It has keys " + brand.Name + " doesn't know: " + strings.Join(keys, ", ") + ". A plugin has title, summary, and [panes.NAME], [helpers.NAME], [skills.NAME] and [tools.NAME] tables; a bundle has title, summary and plugins.")
	}
	title, summary := line(mf.Title, 60), line(mf.Summary, 300)
	if title == "" {
		title = name
	}
	if md.IsDefined("plugins") {
		b := &Bundle{Name: name, Title: title, Summary: summary, Manifest: path}
		if len(mf.Panes) > 0 || len(mf.Helpers) > 0 || len(mf.Skills) > 0 || len(mf.Tools) > 0 || len(mf.Schedules) > 0 {
			b.Problem = "A bundle names plugins and has no parts of its own: put its panes, helpers, skills, tools or schedules in a manifest of their own, and name that in the bundle's plugins."
			return Plugin{}, b
		}
		for _, ref := range mf.Plugins {
			ref = strings.TrimSpace(ref)
			plugin, part, dotted := strings.Cut(ref, ".")
			if !manifestName.MatchString(plugin) || dotted && !manifestName.MatchString(part) {
				b.Problem = "plugins: " + strconv.Quote(ref) + " isn't a plugin's name, or a part's as PLUGIN.PART, such as writing.variorum."
				return Plugin{}, b
			}
			if !slices.Contains(b.Plugins, ref) {
				b.Plugins = append(b.Plugins, ref)
			}
		}
		return Plugin{}, b
	}
	p := Plugin{
		Name: name, Title: title, Summary: summary, Manifest: path,
		key: "plugins.manifests." + name,
		on:  func(c config.Config) bool { return c.Plugins.ManifestOn(name) },
	}
	// gated is the switch of a plugin that shows a page off this computer,
	// runs a program of yours or speaks to agents: it is on only once the
	// owner has said so, so a manifest dropped in the folder, by an agent
	// say, can do none of it by itself.
	gated := func(c config.Config) bool { on, set := c.Plugins.Manifests[name]; return set && on }
	if p.Summary == "" {
		p.Summary = "A plugin of your own, from " + file + " in the plugins folder."
	}
	for _, id := range slices.Sorted(maps.Keys(mf.Panes)) {
		x := panePart(p, id, mf.Panes[id])
		if x.Pane != nil && x.Pane.Remote {
			// A page off this computer shows only once the owner has said
			// so, with the plugin's switch.
			p.on, p.remote = gated, true
		}
		p.Parts = append(p.Parts, x)
	}
	for _, id := range slices.Sorted(maps.Keys(mf.Helpers)) {
		x := helperPart(p, id, mf.Helpers[id])
		if len(x.Helper.Run) > 0 {
			// A program of yours runs only once the owner has said so,
			// with the plugin's switch, as a page off this computer.
			p.on, p.runs = gated, true
		}
		p.Parts = append(p.Parts, x)
	}
	// A skill's words and a server's tools go to every agent's session, so
	// they too wait for the owner's switch.
	for _, id := range slices.Sorted(maps.Keys(mf.Skills)) {
		p.on = gated
		p.Parts = append(p.Parts, skillPart(p, id, mf.Skills[id]))
	}
	for _, id := range slices.Sorted(maps.Keys(mf.Tools)) {
		p.on = gated
		p.Parts = append(p.Parts, toolsPart(p, id, mf.Tools[id]))
	}
	for _, id := range slices.Sorted(maps.Keys(mf.Schedules)) {
		var s struct {
			Title string `toml:"title"`
		}
		_ = md.PrimitiveDecode(mf.Schedules[id], &s)
		t := line(s.Title, 60)
		if t == "" {
			t = id
		}
		p.Parts = append(p.Parts, Part{
			Name: "schedules." + id, Title: t,
			Summary: "A schedule of this plugin's: [schedules." + id + "] in its manifest.",
			Held:    "A later build runs a manifest's schedules: until then " + file + " keeps [schedules." + id + "], and nothing of it runs.",
		})
	}
	if len(p.Parts) == 0 {
		p.Parts = []Part{{
			Name: "pane", Title: title,
			Summary: "A tab in the right sidebar, once the manifest has a pane.",
			own:     always,
			check: func(Env, bool) (bool, string) {
				return false, file + " has no pane yet: a [panes." + name + "] table with an address adds one."
			},
		}}
	}
	return p, nil
}

// readSmall reads a manifest's file: a regular file, as a link may name,
// and no more of it than a manifest may hold, so a pipe or a device in the
// folder can neither hang the plugins op nor fill its memory. What is wrong
// is in a sentence, or "".
func readSmall(path string) ([]byte, string) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, "It doesn't read: " + err.Error()
	}
	if !st.Mode().IsRegular() {
		return nil, "It isn't a file " + brand.Name + " reads: a manifest is a plain text file."
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, "It doesn't read: " + err.Error()
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxManifestSize+1))
	if err != nil {
		return nil, "It doesn't read: " + err.Error()
	}
	if len(data) > maxManifestSize {
		return nil, fmt.Sprintf("It is too big to be a manifest, which holds at most %d KB.", maxManifestSize>>10)
	}
	return data, ""
}

// undecoded are the manifest's keys that no field took, but for those in
// [schedules.<id>] tables, which a later build reads.
func undecoded(md toml.MetaData) []string {
	var out []string
	for _, k := range md.Undecoded() {
		if len(k) > 0 && k[0] == "schedules" {
			continue
		}
		out = append(out, k.String())
	}
	return out
}

// panePart is the part a [panes.<id>] table makes: a tab in the right
// sidebar that shows a web page.
func panePart(p Plugin, id string, f paneFile) Part {
	title := line(f.Title, 40)
	if title == "" {
		title = p.Title
	}
	x := Part{Name: id, Title: title, Brings: []string{"a tab in the Mac app's right sidebar"}, own: always}
	fails := func(why string) Part {
		x.Summary = "A tab in the Mac app's right sidebar, once its manifest names a page it may show."
		x.check = func(Env, bool) (bool, string) { return false, why }
		return x
	}
	if !manifestName.MatchString(id) {
		return fails("[panes." + id + "] isn't a pane's name: lowercase letters, digits and single hyphens, such as [panes." + p.Name + "].")
	}
	w, why := webAddress(f.Address, f.Remote)
	if why != "" {
		return fails(why)
	}
	icon := strings.TrimSpace(f.Icon)
	if !symbolName.MatchString(icon) {
		icon = ""
	}
	start := line(f.Start, 500)
	x.Summary = "A tab in the Mac app's right sidebar that shows " + w.page + ", with logins of its own that last."
	x.Pane = &ipc.PluginPane{Title: title, Icon: icon, Kind: ipc.PaneWeb, Address: w.page, Origin: w.origin, Remote: w.remote, Start: start}
	x.check = func(_ Env, on bool) (bool, string) { return checkWebPane(w, start, on) }
	return x
}

// web is a web pane's address: the page, the one origin it may load from,
// and, on this computer, where to knock to see that something answers.
type web struct {
	page, origin string
	shown        string // the host and port, for sentences
	dial         string // host:port on this computer; "" = off it
	remote       bool
}

// webAddress reads a pane's address: http or https, on this computer
// unless remote, and then only https. What is wrong with it is in a
// sentence, or "".
func webAddress(raw string, remote bool) (web, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return web{}, "The pane has no address: address = \"http://127.0.0.1:8080/\" names the page it shows."
	}
	u, err := url.Parse(raw)
	if err == nil {
		u.Scheme = strings.ToLower(u.Scheme)
	}
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return web{}, "address = " + strconv.Quote(raw) + " isn't a web page's address: it takes one such as http://127.0.0.1:8080/."
	}
	if u.User != nil {
		return web{}, "address = " + strconv.Quote(raw) + " has a name or a password in it: leave them out and sign in in the pane, which keeps the login."
	}
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, "%") {
		return web{}, "address = " + strconv.Quote(raw) + " names a network interface (%): leave it out, as 127.0.0.1 or ::1 needs none."
	}
	def := map[string]string{"http": "80", "https": "443"}[u.Scheme]
	port := u.Port()
	if port == "" {
		port = def
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.To4() != nil {
		// An IPv4 address, even one written as IPv6 (::ffff:127.0.0.1),
		// in the form a browser keeps it in.
		host = ip.To4().String()
	}
	local := host == "localhost" || ip != nil && ip.IsLoopback()
	switch {
	case !local && !remote:
		return web{}, host + " isn't this computer: a pane loads only from 127.0.0.1 or localhost unless its manifest also says remote = true."
	case !local && u.Scheme != "https":
		return web{}, "A pane off this computer must use https: " + raw + " would send what you type in it over the network unencrypted."
	}
	hostport := host
	if strings.Contains(host, ":") {
		hostport = "[" + host + "]"
	}
	if port != def {
		hostport += ":" + port
	}
	u.Host = hostport
	if u.Path == "" {
		u.Path = "/"
	}
	w := web{page: u.String(), origin: u.Scheme + "://" + hostport, shown: hostport, remote: !local}
	if local {
		w.dial = net.JoinHostPort(host, port)
	}
	return w, ""
}

// checkWebPane says whether a web pane's page can show: one off this
// computer, as far as Irie looks, and one on it once something answers
// there.
func checkWebPane(w web, start string, on bool) (bool, string) {
	switch {
	case !on:
		return true, ""
	case w.remote:
		return true, "Ready: its tab in the right sidebar shows " + w.page + ", off this computer, as remote = true in its manifest and your switch allow."
	case knock(w.dial):
		return true, "Ready: its tab in the right sidebar shows " + w.page + "."
	case start != "":
		return false, "Nothing answers at " + w.shown + " yet. Start it: " + start
	}
	return false, "Nothing answers at " + w.shown + " yet: start the app that serves it, and its tab shows it."
}

// knock says whether something takes a TCP connection at addr, on this
// computer, within a moment. Nothing is sent.
var knock = func(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// always is a part's own switch when it has none: it is on while its
// plugin is.
func always(config.Config) bool { return true }

// line is s on one line, cut to at most n characters.
func line(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		s = strings.TrimSpace(string(r[:n-1])) + "…"
	}
	return s
}
