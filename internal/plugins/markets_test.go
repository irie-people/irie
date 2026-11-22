//go:build !nomarkets

package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
)

func marketsStatus(t *testing.T, e Env) ipc.Plugin {
	t.Helper()
	for _, p := range Status(e) {
		if p.Name == "markets" {
			return p
		}
	}
	t.Fatal("no markets plugin")
	return ipc.Plugin{}
}

// TestMarketsIsOffByDefault: a new Irie shows nothing of the markets plugin
// until asked, and its words say which switch turns it on.
func TestMarketsIsOffByDefault(t *testing.T) {
	e := env(t)
	p := marketsStatus(t, e)
	if p.On || p.Title != "Markets" || len(p.Parts) != 1 {
		t.Fatalf("markets = %+v", p)
	}
	if x := part(t, p, Map); x.State != ipc.PartPluginOff || x.Switch != "" || x.Setup != "" {
		t.Errorf("map = %+v", x)
	}
	if why := Why(e.Config, Map); !strings.Contains(why, brand.Slug+" plugins on markets") {
		t.Errorf("Why = %q", why)
	}
	if ed, err := Edits("markets", "", true); err != nil || len(ed) != 1 || ed[0].Key != "plugins.markets" || ed[0].Value != true {
		t.Errorf("Edits = %+v, %v", ed, err)
	}
	if _, err := Edits("markets", Map, true); err == nil || !strings.Contains(err.Error(), "no switch of its own") {
		t.Errorf("Edits of the part = %v", err)
	}
}

// TestTheMarketMapNeedsAFolderThenItsPage: on, the map says what it lacks,
// a folder chosen and then the page in it, and is ready once the page is
// there; a folder without its page can still take a session.
func TestTheMarketMapNeedsAFolderThenItsPage(t *testing.T) {
	e := env(t)
	e.Config.Plugins.Markets = true
	state := func() ipc.PluginPart { return part(t, marketsStatus(t, e), Map) }

	if x := state(); x.State != ipc.PartNeeds || !strings.Contains(x.Detail, "markets.folder") {
		t.Errorf("no folder: %+v", x)
	}
	if why := Why(e.Config, Map); why != "" {
		t.Errorf("Why with the plugin on = %q", why)
	}

	dir := filepath.Join(t.TempDir(), "maps")
	e.Config.Markets.Folder = dir
	if x := state(); x.State != ipc.PartNeeds || !strings.Contains(x.Detail, "isn't a folder here") {
		t.Errorf("no such folder: %+v", x)
	}
	if w, _ := FindMap(e.Config); w.HasFolder() {
		t.Errorf("%s is no folder, yet HasFolder", dir)
	}

	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if x := state(); x.State != ipc.PartNeeds || !strings.Contains(x.Detail, "No map in") || !strings.Contains(x.Detail, ":markets TASK") {
		t.Errorf("no page: %+v", x)
	}
	if w, _ := FindMap(e.Config); !w.HasFolder() {
		t.Errorf("%s is a folder, yet not HasFolder", dir)
	}

	page := filepath.Join(dir, MapPage)
	if err := os.WriteFile(page, []byte("<title>Markets</title>"), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 11, 9, 15, 48, 0, 0, time.Local)
	if err := os.Chtimes(page, when, when); err != nil {
		t.Fatal(err)
	}
	if x := state(); x.State != ipc.PartReady || !strings.Contains(x.Detail, page) || !strings.Contains(x.Detail, "9 Nov 15:48") {
		t.Errorf("with its page: %+v", x)
	}
	w, why := FindMap(e.Config)
	if why != "" || w.Folder != dir || w.Page != page || !w.Changed.Equal(when) {
		t.Errorf("FindMap = %+v, %q", w, why)
	}

	// Off, the plugin keeps its folder for when it is on again.
	e.Config.Plugins.Markets = false
	if x := state(); x.State != ipc.PartPluginOff || e.Config.Markets.Folder != dir {
		t.Errorf("plugin off: %+v", x)
	}
}

// TestTheMarketMapsFolderTakesATilde: markets.folder may start at the home
// folder, as config.toml's other paths do.
func TestTheMarketMapsFolderTakesATilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "maps"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Plugins.Markets, cfg.Markets.Folder = true, "~/maps"
	w, why := FindMap(cfg)
	if w.Folder != filepath.Join(home, "maps") || w.Page != filepath.Join(home, "maps", MapPage) || !strings.Contains(why, "~/maps") {
		t.Errorf("FindMap = %+v, %q", w, why)
	}
}
