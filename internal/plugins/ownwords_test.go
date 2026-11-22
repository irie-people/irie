package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
)

// Words of your own: [words.<id>] tables in a manifest (ownwords.go).

// TestAManifestBringsAWord: a word on the plugin's skill, in a folder of
// yours, says how it stands and is typed from the Plugins menu; it keeps
// no plugin off by itself, and while its plugin is off it says why.
func TestAManifestBringsAWord(t *testing.T) {
	e := env(t)
	h := home(t)
	skillFolder(t, h, "grants")
	if err := os.MkdirAll(filepath.Join(h, "Documents", "Grants"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest(t, e, "grants-app.toml", `
[skills.grants]
path = "~/apps/grants/skills/grants"

[words.grants]
title = "Grant work"
skill = "grants"
dir = "~/Documents/Grants"
`)
	ws := Words(e)
	if len(ws) != 1 || ws[0].Off != ":grants is off, as the grants-app plugin is: `"+brand.Slug+" plugins on grants-app` turns it on." {
		t.Fatalf("off, Words = %+v", ws)
	}
	e.Config.Plugins.Manifests["grants-app"] = true
	p := mine(t, e, "grants-app")
	x := part(t, p, "words.grants")
	if x.Title != "Grant work" || x.State != ipc.PartReady || x.Setup != ":grants" ||
		x.Detail != "Type :grants in a task bar to start a session on /"+brand.Slug+":grants, or :grants and a task to hand it the task." {
		t.Errorf("on: %+v", x)
	}
	want := OwnWord{Name: "grants", Plugin: "grants-app", Title: "Grant work", Skill: "grants", Dir: filepath.Join(h, "Documents", "Grants"), Manifest: filepath.Join(e.Paths.Plugins(), "grants-app.toml")}
	if ws := Words(e); len(ws) != 1 || ws[0] != want {
		t.Errorf("on, Words = %+v\nwant %+v", ws, want)
	}
	// Its folder gone, it says so.
	os.Remove(filepath.Join(h, "Documents", "Grants"))
	if x := part(t, mine(t, e, "grants-app"), "words.grants"); x.State != ipc.PartNeeds || !strings.Contains(x.Detail, "Grants, isn't there") {
		t.Errorf("no folder: %+v", x)
	}

	// A word alone keeps its plugin on, and with no skill it takes a task.
	e = env(t)
	manifest(t, e, "invoices.toml", "[words.invoice]\n")
	p = mine(t, e, "invoices")
	if x := part(t, p, "words.invoice"); !p.On || x.State != ipc.PartReady || x.Setup != "" || x.Title != ":invoice" ||
		x.Detail != "Type :invoice and a task in a task bar to start a session for it." {
		t.Errorf("a word alone: %+v, %+v", p, x)
	}
}

// TestAWordThatDoesntReadSaysWhy: a word that can't start a session says
// why, in its part and when typed.
func TestAWordThatDoesntReadSaysWhy(t *testing.T) {
	for _, c := range []struct{ id, table, want string }{
		{"close", ``, brand.Name + " has a :close of its own: give [words.close] another name."},
		{"grants", `skill = "funding"`, `skill = "funding" isn't one of this plugin's skills`},
		{"grants", `dir = "Documents"`, `dir = "Documents" isn't a full path to a folder`},
		{"Grants", ``, "isn't a word"},
	} {
		e := env(t)
		home(t)
		manifest(t, e, "grants-app.toml", "[words."+c.id+"]\n"+c.table+"\n")
		x := part(t, mine(t, e, "grants-app"), "words."+c.id)
		if x.State != ipc.PartNeeds || !strings.Contains(x.Detail, c.want) {
			t.Errorf("%s %s: %+v, want it to need %q", c.id, c.table, x, c.want)
		}
		if ws := Words(e); len(ws) != 1 || !strings.HasPrefix(ws[0].Off, ":"+c.id+" can't start a session: ") {
			t.Errorf("%s %s: Words = %+v", c.id, c.table, ws)
		}
	}
	// Of two plugins with one word, the first keeps it.
	e := env(t)
	manifest(t, e, "a-grants.toml", "[words.grants]\n")
	manifest(t, e, "b-grants.toml", "[words.grants]\n")
	if x := part(t, mine(t, e, "b-grants"), "words.grants"); x.State != ipc.PartNeeds || x.Detail != "Your plugin a-grants has the word :grants first: give [words.grants] another name." {
		t.Errorf("the later word: %+v", x)
	}
	if ws := Words(e); len(ws) != 2 || ws[0].Off != "" || ws[1].Off == "" {
		t.Errorf("Words = %+v", ws)
	}
}
