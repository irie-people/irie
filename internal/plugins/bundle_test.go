package plugins

import (
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

// Bundles: a manifest with a plugins list (docs/proposals/plugin-panes.md).

// bundle is how the bundle named name stands in e.
func bundle(t *testing.T, e Env, name string) ipc.PluginBundle {
	t.Helper()
	_, bs := Here(e)
	for _, b := range bs {
		if b.Name == name {
			return b
		}
	}
	t.Fatalf("no bundle %q among %+v", name, bs)
	return ipc.PluginBundle{}
}

// apply makes edits to e's config as config.toml would take them.
func apply(t *testing.T, e *Env, edits []config.Edit) {
	t.Helper()
	data, err := config.Apply(nil, edits)
	if err != nil {
		t.Fatal(err)
	}
	// The edits go over what e has, as they would over its file.
	cfg, err := config.Parse(data, "config.toml")
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range cfg.Plugins.Manifests {
		e.Config.Plugins.Manifests[k] = v
	}
}

// TestABundleTurnsItsPluginsOnAndOff: one switch sets each plugin's own,
// and the bundle reads on, off or some from theirs.
func TestABundleTurnsItsPluginsOnAndOff(t *testing.T) {
	e := env(t)
	manifest(t, e, "invoices.toml", "[panes.invoices]\naddress = \"http://"+answering(t)+"\"\n")
	manifest(t, e, "ledger.toml", "[panes.ledger]\naddress = \"http://"+silent(t)+"\"\nstart = \"ledger serve\"\n")
	path := manifest(t, e, "shop.toml", `
title = "Shop"
summary = "The apps that run the shop."
plugins = [
  "invoices",
  "ledger",
  "invoices",
]
`)
	b := bundle(t, e, "shop")
	if b.Title != "Shop" || b.Summary != "The apps that run the shop." || b.Manifest != path || b.State != ipc.BundleOn || b.Problem != "" || len(b.Plugins) != 2 {
		t.Fatalf("bundle = %+v", b)
	}
	if m := b.Plugins[0]; m.Name != "invoices" || m.Plugin != "invoices" || m.Part != "" || m.Title != "invoices" || !m.On || m.State != ipc.PartReady {
		t.Errorf("invoices = %+v", m)
	}
	if m := b.Plugins[1]; !m.On || m.State != ipc.PartNeeds || !strings.Contains(m.Detail, "Start it: ledger serve") {
		t.Errorf("ledger = %+v", m)
	}
	// A bundle is no plugin: it isn't among them.
	for _, p := range Status(e) {
		if p.Name == "shop" {
			t.Errorf("the bundle is listed as a plugin: %+v", p)
		}
	}
	dir := e.Paths.Plugins()
	off, err := BundleEdits(dir, "shop", false)
	if err != nil || len(off) != 2 || off[0] != (config.Edit{Key: "plugins.manifests.invoices", Value: false}) || off[1] != (config.Edit{Key: "plugins.manifests.ledger", Value: false}) {
		t.Fatalf("off = %v, %v", off, err)
	}
	apply(t, &e, off)
	if b := bundle(t, e, "shop"); b.State != ipc.BundleOff || b.Plugins[0].On || b.Plugins[0].State != ipc.PartOff {
		t.Errorf("after off = %+v", b)
	}
	// One of them on again on its own: the bundle reads some.
	e.Config.Plugins.Manifests["ledger"] = true
	b = bundle(t, e, "shop")
	if b.State != ipc.BundleSome || BundleWords(b) != "1 of 2 on" {
		t.Errorf("one on = %+v (%s)", b, BundleWords(b))
	}
	on, err := BundleEdits(dir, "shop", true)
	if err != nil || len(on) != 2 || on[0].Value != true || on[1].Value != true {
		t.Fatalf("on = %v, %v", on, err)
	}
	apply(t, &e, on)
	b = bundle(t, e, "shop")
	if b.State != ipc.BundleOn {
		t.Errorf("after on = %+v", b)
	}
	all, bs := Here(e)
	text := AllText(all, bs)
	if !strings.HasPrefix(text, "Shop bundle: on. The apps that run the shop.\n  ● invoices: Ready") || !strings.Contains(text, "  ! ledger: Nothing answers") || !strings.Contains(text, "\n\n") {
		t.Errorf("text = %q", text)
	}
	if !strings.Contains(text, "invoices plugin: on.") {
		t.Errorf("text has no plugins: %q", text)
	}
}

// TestABundleNamesWhatIsntHere: a plugin or part that isn't here shows as
// missing, and counts for nothing.
func TestABundleNamesWhatIsntHere(t *testing.T) {
	e := env(t)
	manifest(t, e, "invoices.toml", "[panes.invoices]\naddress = \"http://"+answering(t)+"\"\n")
	manifest(t, e, "broken.toml", "title = \"Broken\n")
	manifest(t, e, "shop.toml", "plugins = [\"invoices\", \"ghost\", \"invoices.nothing\", \"broken\"]\n")
	b := bundle(t, e, "shop")
	if b.Title != "shop" || len(b.Plugins) != 4 {
		t.Fatalf("bundle = %+v", b)
	}
	if m := b.Plugins[1]; m.State != ipc.PartMissing || m.On || !strings.Contains(m.Detail, "No plugin named ghost here") {
		t.Errorf("ghost = %+v", m)
	}
	if m := b.Plugins[2]; m.State != ipc.PartMissing || !strings.Contains(m.Detail, "no part named nothing") {
		t.Errorf("invoices.nothing = %+v", m)
	}
	if m := b.Plugins[3]; m.State != ipc.PartNeeds || !strings.Contains(m.Detail, "broken.toml doesn't read") {
		t.Errorf("broken = %+v", m)
	}
	// invoices is on and broken is not, so it reads some.
	if b.State != ipc.BundleSome {
		t.Errorf("state = %q", b.State)
	}
	if text := BundleText([]ipc.PluginBundle{b}); !strings.Contains(text, "? ghost: No plugin named ghost") {
		t.Errorf("text = %q", text)
	}
	edits, err := BundleEdits(e.Paths.Plugins(), "shop", false)
	if err != nil || len(edits) != 1 || edits[0].Key != "plugins.manifests.invoices" {
		t.Errorf("edits = %v, %v", edits, err)
	}
	manifest(t, e, "nothing.toml", "plugins = [\"ghost\"]\n")
	if _, err := BundleEdits(e.Paths.Plugins(), "nothing", true); err == nil || !strings.Contains(err.Error(), "nothing here to turn on") {
		t.Errorf("a bundle of nothing here: %v", err)
	}
	if _, err := BundleEdits(e.Paths.Plugins(), "ghost", true); err == nil || !strings.Contains(err.Error(), `no bundle "ghost"`) {
		t.Errorf("no bundle: %v", err)
	}
	if b, ok := FindBundle(e.Paths.Plugins(), "shop"); !ok || b.Name != "shop" {
		t.Errorf("FindBundle = %+v, %v", b, ok)
	}
}

// TestABundleThatDoesntReadSaysWhy: a bundle holds no parts of its own, and
// names plugins in their own words.
func TestABundleThatDoesntReadSaysWhy(t *testing.T) {
	e := env(t)
	manifest(t, e, "both.toml", "plugins = [\"invoices\"]\n[panes.both]\naddress = \"http://127.0.0.1:1/\"\n")
	manifest(t, e, "odd.toml", "plugins = [\"Invoices Plugin\"]\n")
	for name, says := range map[string]string{"both": "has no parts of its own", "odd": "isn't a plugin's name"} {
		b := bundle(t, e, name)
		if b.State != ipc.BundleOff || len(b.Plugins) != 0 || !strings.Contains(b.Problem, says) {
			t.Errorf("%s = %+v, want it to say %q", name, b, says)
		}
		if _, err := BundleEdits(e.Paths.Plugins(), name, true); err == nil || !strings.Contains(err.Error(), "doesn't read") {
			t.Errorf("%s's switch: %v", name, err)
		}
		if text := BundleText([]ipc.PluginBundle{b}); !strings.HasPrefix(text, name+".toml: its manifest doesn't read.") {
			t.Errorf("text = %q", text)
		}
	}
}
