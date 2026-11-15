package plugins

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// Plugins of your own: manifests in the plugins folder
// (docs/proposals/plugin-panes.md). These tests carry no build tag: a
// manifest needs no plugin of Irie's.

// manifest writes plugins/NAME in e's config folder.
func manifest(t *testing.T, e Env, name, text string) string {
	t.Helper()
	dir := e.Paths.Plugins()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// answering is an address on this computer where something takes
// connections, until the test ends.
func answering(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	return l.Addr().String()
}

// silent is an address on this computer where nothing answers.
func silent(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

// mine is how the plugin of your own named name stands in e.
func mine(t *testing.T, e Env, name string) ipc.Plugin {
	t.Helper()
	for _, p := range Status(e) {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("no plugin %q among %+v", name, Status(e))
	return ipc.Plugin{}
}

func TestNoFolderNoPluginsOfYourOwn(t *testing.T) {
	if ps, bs := Mine(""); ps != nil || bs != nil {
		t.Errorf("Mine(\"\") = %v, %v", ps, bs)
	}
	e := env(t)
	if ps, bs := Mine(e.Paths.Plugins()); ps != nil || bs != nil {
		t.Errorf("no folder: %v, %v", ps, bs)
	}
	if len(Status(e)) != len(All()) {
		t.Errorf("status = %+v, want only %s's own", Status(e), "Irie")
	}
}

// TestAManifestBringsAWebPane: a manifest is a plugin, on by default, whose
// pane shows its page once something answers there.
func TestAManifestBringsAWebPane(t *testing.T) {
	e := env(t)
	addr := answering(t)
	path := manifest(t, e, "invoices.toml", `
title = "Invoices"
summary = "The invoicing app, beside your sessions."

[panes.invoices]
title = "Bills"
address = "http://`+addr+`/inbox"
icon = "doc.text"
start = """cd ~/code/invoices &&
  make serve"""
`)
	p := mine(t, e, "invoices")
	if !p.On || p.Title != "Invoices" || p.Summary != "The invoicing app, beside your sessions." || p.Manifest != path || p.Problem != "" || len(p.Parts) != 1 {
		t.Fatalf("plugin = %+v", p)
	}
	x := p.Parts[0]
	if x.Name != "invoices" || x.Title != "Bills" || x.State != ipc.PartReady || x.Switch != "" || !strings.Contains(x.Detail, "http://"+addr+"/inbox") {
		t.Errorf("part = %+v", x)
	}
	want := ipc.PluginPane{ID: "invoices.invoices", Title: "Bills", Icon: "doc.text", Kind: ipc.PaneWeb, Address: "http://" + addr + "/inbox",
		Origin: "http://" + addr, Start: "cd ~/code/invoices && make serve", Store: "invoices"}
	if x.Pane == nil || *x.Pane != want {
		t.Errorf("pane = %+v, want %+v", x.Pane, want)
	}
	if text := Text([]ipc.Plugin{p}); !strings.HasPrefix(text, "Invoices plugin: on. The invoicing app") || !strings.Contains(text, "● Bills: Ready") {
		t.Errorf("text = %q", text)
	}
}

// TestAPaneSaysWhatStartsItsApp: with nothing answering, the pane needs its
// app, and says what starts it, or that something must.
func TestAPaneSaysWhatStartsItsApp(t *testing.T) {
	e := env(t)
	addr := silent(t)
	manifest(t, e, "ledger.toml", "[panes.ledger]\naddress = \"http://"+addr+"\"\nstart = \"ledger serve\"\n")
	manifest(t, e, "shop.toml", "[panes.shop]\naddress = \"http://"+addr+"\"\n")
	x := mine(t, e, "ledger").Parts[0]
	if x.State != ipc.PartNeeds || x.Detail != "Nothing answers at "+addr+" yet. Start it: ledger serve" || x.Pane == nil || x.Pane.Address != "http://"+addr+"/" {
		t.Errorf("ledger = %+v", x)
	}
	if x := mine(t, e, "shop").Parts[0]; x.State != ipc.PartNeeds || !strings.Contains(x.Detail, "start the app that serves it") {
		t.Errorf("shop = %+v", x)
	}
	// A plugin with no title takes its manifest's name.
	if p := mine(t, e, "shop"); p.Title != "shop" || p.Parts[0].Title != "shop" || !strings.Contains(p.Summary, "shop.toml") {
		t.Errorf("shop = %+v", p)
	}
}

// TestAPaneStaysOnThisComputer: an address elsewhere needs remote = true
// and https, and is never knocked on.
func TestAPaneStaysOnThisComputer(t *testing.T) {
	e := env(t)
	manifest(t, e, "away.toml", "[panes.away]\naddress = \"https://example.com/app\"\n")
	manifest(t, e, "plain.toml", "[panes.plain]\naddress = \"http://example.com/\"\nremote = true\n")
	manifest(t, e, "far.toml", "[panes.far]\naddress = \"https://example.com/app\"\nremote = true\n")
	knocked := false
	was := knock
	knock = func(string) bool { knocked = true; return false }
	t.Cleanup(func() { knock = was })
	if x := mine(t, e, "away").Parts[0]; x.State != ipc.PartNeeds || x.Pane != nil || !strings.Contains(x.Detail, "example.com isn't this computer") {
		t.Errorf("away = %+v", x)
	}
	if x := mine(t, e, "plain").Parts[0]; x.State != ipc.PartNeeds || x.Pane != nil || !strings.Contains(x.Detail, "must use https") {
		t.Errorf("plain = %+v", x)
	}
	x := mine(t, e, "far").Parts[0]
	if x.State != ipc.PartReady || x.Pane == nil || !x.Pane.Remote || x.Pane.Origin != "https://example.com" || !strings.Contains(x.Detail, "off this computer") {
		t.Errorf("far = %+v", x)
	}
	if knocked {
		t.Error("knocked on an address off this computer")
	}
}

func TestWebAddresses(t *testing.T) {
	for _, c := range []struct {
		raw          string
		remote       bool
		page, origin string
		dial         string
		bad          string
	}{
		{raw: "http://LOCALHOST:8080", page: "http://localhost:8080/", origin: "http://localhost:8080", dial: "localhost:8080"},
		{raw: "http://127.0.0.1:80/a?b=c#d", page: "http://127.0.0.1/a?b=c#d", origin: "http://127.0.0.1", dial: "127.0.0.1:80"},
		{raw: "https://127.0.0.1/", page: "https://127.0.0.1/", origin: "https://127.0.0.1", dial: "127.0.0.1:443"},
		{raw: "http://[::1]:9000/", page: "http://[::1]:9000/", origin: "http://[::1]:9000", dial: "[::1]:9000"},
		{raw: "http://127.8.0.1:5000", page: "http://127.8.0.1:5000/", origin: "http://127.8.0.1:5000", dial: "127.8.0.1:5000"},
		{raw: "HTTPS://Example.com:8443/x", remote: true, page: "https://example.com:8443/x", origin: "https://example.com:8443"},
		{raw: "", bad: "has no address"},
		{raw: "127.0.0.1:8080", bad: "isn't a web page's address"},
		{raw: "ftp://127.0.0.1/", bad: "isn't a web page's address"},
		{raw: "http://alice:secret@127.0.0.1/", bad: "a name or a password"},
		{raw: "http://192.168.1.20:8080/", bad: "isn't this computer"},
		{raw: "http://example.com/", remote: true, bad: "must use https"},
	} {
		w, why := webAddress(c.raw, c.remote)
		if c.bad != "" {
			if !strings.Contains(why, c.bad) {
				t.Errorf("%q: %q, want it to say %q", c.raw, why, c.bad)
			}
			continue
		}
		if why != "" || w.page != c.page || w.origin != c.origin || w.dial != c.dial || w.remote != (c.dial == "") {
			t.Errorf("%q = %+v (%q), want %s, %s, %s", c.raw, w, why, c.page, c.origin, c.dial)
		}
	}
}

// TestYourPluginHasASwitch: plugins.manifests.NAME turns it off, and on;
// its pane has no switch of its own.
func TestYourPluginHasASwitch(t *testing.T) {
	e := env(t)
	manifest(t, e, "invoices.toml", "[panes.invoices]\naddress = \"http://"+silent(t)+"\"\n")
	dir := e.Paths.Plugins()
	edits, err := EditsHere(dir, "invoices", "", false)
	if err != nil || len(edits) != 1 || edits[0] != (config.Edit{Key: "plugins.manifests.invoices", Value: false}) {
		t.Fatalf("edits = %v, %v", edits, err)
	}
	e.Config.Plugins.Manifests["invoices"] = false
	p := mine(t, e, "invoices")
	if p.On || p.Parts[0].State != ipc.PartPluginOff {
		t.Errorf("off = %+v", p)
	}
	if _, err := EditsHere(dir, "invoices", "invoices", true); err == nil || !strings.Contains(err.Error(), "no switch of its own") {
		t.Errorf("the pane's switch: %v", err)
	}
	if _, err := EditsHere(dir, "nothing", "", true); err == nil || !strings.Contains(err.Error(), "invoices") {
		t.Errorf("no such plugin: %v", err)
	}
	// Irie's own plugins switch here too.
	if len(All()) > 0 {
		if edits, err := EditsHere(dir, All()[0].Name, "", false); err != nil || len(edits) != 1 || edits[0].Key != All()[0].Key() {
			t.Errorf("Irie's own: %v, %v", edits, err)
		}
	}
}

// TestAManifestThatDoesntReadSaysWhy: it shows, with no switch and no
// parts, saying what is wrong.
func TestAManifestThatDoesntReadSaysWhy(t *testing.T) {
	e := env(t)
	manifest(t, e, "broken.toml", "title = \"Broken\n")
	manifest(t, e, "colour.toml", "title = \"Colour\"\ncolour = \"blue\"\n[panes.colour]\naddress = \"http://127.0.0.1:1/\"\nshade = 3\n")
	manifest(t, e, "My Plugin.toml", "title = \"Mine\"\n")
	manifest(t, e, order[0]+".toml", "title = \"Not Irie's\"\n")
	manifest(t, e, "big.toml", "title = \"Big\"\n# "+strings.Repeat("x", maxManifestSize)+"\n")
	for name, says := range map[string]string{
		"broken":           "doesn't read as TOML",
		"colour":           "keys Irie doesn't know: colour, panes.colour.shade",
		"My Plugin.toml":   "isn't a manifest's name",
		order[0] + ".toml": "name of one of Irie's own plugins",
		"big":              "too big to be a manifest",
	} {
		p := mine(t, e, name)
		if p.On || len(p.Parts) != 0 || !strings.Contains(p.Problem, says) {
			t.Errorf("%s = %+v, want it to say %q", name, p, says)
		}
		if _, err := EditsHere(e.Paths.Plugins(), name, "", true); err == nil || !strings.Contains(err.Error(), "doesn't read") {
			t.Errorf("%s's switch: %v", name, err)
		}
	}
	if text := Text([]ipc.Plugin{mine(t, e, "broken")}); !strings.HasPrefix(text, "broken.toml: its manifest doesn't read.") {
		t.Errorf("text = %q", text)
	}
}

// TestSchedulesWaitForALaterBuild: [schedules.<id>] tables are kept, each a
// part that is held, whatever keys they have.
func TestSchedulesWaitForALaterBuild(t *testing.T) {
	e := env(t)
	manifest(t, e, "mornings.toml", `
title = "Mornings"
[schedules.follow]
title = "Morning follows"
at = "08:00"
[schedules.digest]
every = "1d"
`)
	p := mine(t, e, "mornings")
	if p.Problem != "" || len(p.Parts) != 2 {
		t.Fatalf("plugin = %+v", p)
	}
	if x := part(t, p, "schedules.follow"); x.State != ipc.PartHeld || x.Title != "Morning follows" || x.Pane != nil || !strings.Contains(x.Detail, "[schedules.follow]") {
		t.Errorf("follow = %+v", x)
	}
	if x := part(t, p, "schedules.digest"); x.State != ipc.PartHeld || x.Title != "digest" {
		t.Errorf("digest = %+v", x)
	}
}

// TestManifestEdges: a manifest with nothing in it says how to add a pane,
// a name that isn't a pane's or an icon that isn't a symbol's is caught, and
// other files in the folder are passed over.
func TestManifestEdges(t *testing.T) {
	e := env(t)
	addr := answering(t)
	manifest(t, e, "empty.toml", "title = \"Empty\"\n")
	manifest(t, e, "odd.toml", "[panes.Odd]\naddress = \"http://"+addr+"\"\n[panes.fine]\naddress = \"http://"+addr+"\"\nicon = \"not a symbol\"\n")
	manifest(t, e, ".hidden.toml", "title = \"Hidden\"\n")
	manifest(t, e, "notes.txt", "title = \"Notes\"\n")
	if err := os.MkdirAll(filepath.Join(e.Paths.Plugins(), "folder.toml"), 0o700); err != nil {
		t.Fatal(err)
	}
	if x := mine(t, e, "empty").Parts; len(x) != 1 || x[0].State != ipc.PartNeeds || !strings.Contains(x[0].Detail, "[panes.empty]") {
		t.Errorf("empty = %+v", x)
	}
	p := mine(t, e, "odd")
	if x := part(t, p, "Odd"); x.State != ipc.PartNeeds || x.Pane != nil || !strings.Contains(x.Detail, "isn't a pane's name") {
		t.Errorf("Odd = %+v", x)
	}
	if x := part(t, p, "fine"); x.State != ipc.PartReady || x.Pane == nil || x.Pane.Icon != "" {
		t.Errorf("fine = %+v", x)
	}
	mine, _ := Mine(e.Paths.Plugins())
	if len(mine) != 2 {
		t.Errorf("plugins = %+v, want empty and odd only", mine)
	}
}

func TestPluginsOfYourOwnAreOnUntilTurnedOff(t *testing.T) {
	var p config.Plugins
	if !p.ManifestOn("invoices") {
		t.Error("an unset plugin of your own is off")
	}
	p.Manifests = map[string]bool{"invoices": false, "ledger": true}
	if p.ManifestOn("invoices") || !p.ManifestOn("ledger") {
		t.Errorf("switches = %v", p.Manifests)
	}
}
