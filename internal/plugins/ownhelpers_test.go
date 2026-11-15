package plugins

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/ipc"
)

// Helpers of your own: [helpers.<id>] tables in a manifest (ownhelpers.go).

// home makes ~ a folder of the test's own.
func home(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	was := homeDir
	homeDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { homeDir = was })
	return dir
}

// TestAManifestBringsAHelper: a helper that runs a program keeps its
// plugin off until you turn it on; then it is ready, with its paths read
// from ~.
func TestAManifestBringsAHelper(t *testing.T) {
	e := env(t)
	h := home(t)
	path := manifest(t, e, "inbox.toml", `
title = "Inbox"

[helpers.listen]
title = "Texts to tasks"
icon = "tray"
run = ["sh", "~/inbox/listen.sh", "--every", "20"]
dir = "~/inbox"
status = "~/.local/state/inbox/status.json"
open = "~/Documents/Inbox"
`)
	p := mine(t, e, "inbox")
	if p.On || !strings.Contains(p.Summary, "runs a program of yours, so it is off until you turn it on") {
		t.Fatalf("a manifest that runs a program is on before you say so: %+v", p)
	}
	if x := part(t, p, "helpers.listen"); x.Title != "Texts to tasks" || x.State != ipc.PartPluginOff {
		t.Errorf("off: %+v", x)
	}
	if hs := Helpers(e); len(hs) != 1 || hs[0].On {
		t.Errorf("off, Helpers = %+v", hs)
	}

	e.Config.Plugins.Manifests["inbox"] = true
	p = mine(t, e, "inbox")
	if x := part(t, p, "helpers.listen"); !p.On || x.State != ipc.PartReady || !strings.Contains(x.Detail, "keeps sh running") {
		t.Errorf("on: %+v", x)
	}
	want := Helper{ID: "inbox.listen", Plugin: "inbox", Title: "Texts to tasks", Icon: "tray",
		Run: []string{"sh", filepath.Join(h, "inbox", "listen.sh"), "--every", "20"}, Dir: filepath.Join(h, "inbox"),
		Status: filepath.Join(h, ".local", "state", "inbox", "status.json"), Open: filepath.Join(h, "Documents", "Inbox"),
		Manifest: path, On: true}
	if hs := Helpers(e); len(hs) != 1 || !reflect.DeepEqual(hs[0], want) {
		t.Errorf("Helpers = %+v\nwant %+v", hs, want)
	}
}

// TestAHelperThatDoesntReadSaysWhy: a table that can't run says what is
// wrong with it, in its part, and runs nothing.
func TestAHelperThatDoesntReadSaysWhy(t *testing.T) {
	for _, c := range []struct{ table, want string }{
		{`run = ["bin/listen"]`, "isn't a name on your PATH or a full path"},
		{`run = [" "]`, "starts with an empty word"},
		{"run = [\"sh\"]\ndir = \"inbox\"", "dir = \"inbox\" isn't a full path"},
		{`status = "status.json"`, "isn't a full path to a file"},
		{`title = "Nothing"`, "has nothing to do"},
		{`run = ["no-such-program-for-irie-tests"]`, "isn't a program on this computer"},
	} {
		e := env(t)
		home(t)
		manifest(t, e, "inbox.toml", "[helpers.listen]\n"+c.table+"\n")
		e.Config.Plugins.Manifests["inbox"] = true
		x := part(t, mine(t, e, "inbox"), "helpers.listen")
		if x.State != ipc.PartNeeds || !strings.Contains(x.Detail, c.want) {
			t.Errorf("%s: part = %+v, want it to need %q", c.table, x, c.want)
		}
	}
	e := env(t)
	manifest(t, e, "inbox.toml", "[helpers.Listen]\nrun = [\"sh\"]\n")
	if x := part(t, mine(t, e, "inbox"), "helpers.Listen"); x.State == ipc.PartReady {
		t.Errorf("a helper's name with a capital: %+v", x)
	}
}

// TestAHelperThatOnlyShowsAStatusIsOn: a helper with no program runs
// nothing, so its plugin is on as any other of yours.
func TestAHelperThatOnlyShowsAStatusIsOn(t *testing.T) {
	e := env(t)
	manifest(t, e, "watch.toml", "[helpers.watch]\nstatus = \"/var/tmp/irie-watch.json\"\n")
	p := mine(t, e, "watch")
	if x := part(t, p, "helpers.watch"); !p.On || x.State != ipc.PartReady {
		t.Errorf("plugin = %+v, part = %+v", p, x)
	}
	if hs := Helpers(e); len(hs) != 1 || len(hs[0].Run) != 0 || hs[0].Status != "/var/tmp/irie-watch.json" || !hs[0].On {
		t.Errorf("Helpers = %+v", hs)
	}
}
