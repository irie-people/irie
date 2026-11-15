package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/testutil"
)

// TestTheCoreKeepsYourHelpersRunning: a helper of yours starts in its own
// window once its plugin is on, says who it is, has its status passed on
// with what can't be shown left out, starts again when asked, and stops
// once the plugin is off (DESIGN §4.17).
func TestTheCoreKeepsYourHelpersRunning(t *testing.T) {
	c := setup(t)
	dir := t.TempDir()
	who, status := filepath.Join(dir, "who"), filepath.Join(dir, "status.json")
	if err := os.MkdirAll(c.paths.Plugins(), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf("title = \"Inbox\"\n\n[helpers.tick]\ntitle = \"Tick\"\nrun = [\"sh\", \"-c\", \"echo $%s > %s; exec sleep 300\"]\ndir = %q\nstatus = %q\nopen = %q\n",
		brand.Env("HELPER"), who, dir, status, dir)
	if err := os.WriteFile(filepath.Join(c.paths.Plugins(), "inbox.toml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	helpers := func() int {
		n := 0
		for _, p := range c.panes() {
			if p["@irie_role"] == "helper" {
				n++
			}
		}
		return n
	}
	get := func() ipc.HelperInfo {
		t.Helper()
		var r ipc.HelpersResult
		if err := c.call(ipc.OpHelpers, ipc.HelpersArgs{}, &r); err != nil {
			t.Fatal(err)
		}
		if len(r.Helpers) != 1 {
			t.Fatalf("helpers = %+v", r.Helpers)
		}
		return r.Helpers[0]
	}

	// A manifest that runs a program is off until you turn it on.
	time.Sleep(2 * ownHelperLook)
	if n := helpers(); n != 0 {
		t.Fatalf("%d helpers before the plugin is on", n)
	}
	if h := get(); h.On || h.Running || !h.Runs || h.Title != "Tick" || h.Open != dir {
		t.Fatalf("off: %+v", h)
	}

	on := true
	if err := c.call(ipc.OpPlugins, ipc.PluginsArgs{Plugin: "inbox", On: &on}, nil); err != nil {
		t.Fatal(err)
	}
	testutil.Eventually(t, 30*time.Second, "the helper never started", func() bool {
		b, _ := os.ReadFile(who)
		return strings.TrimSpace(string(b)) == "inbox.tick"
	})
	if n := helpers(); n != 1 {
		t.Errorf("%d helper windows, want 1", n)
	}

	if err := os.WriteFile(status, []byte(`{"state": "working", "line": "Watching   your texts", "items": [
		{"title": "One", "state": "working", "session": "s38abcdefghij"},
		{"title": "", "detail": "no title: left out"},
		{"title": "Two", "state": "sleepy", "session": "not a session", "open": "relative/path"}
	]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h := get()
	if !h.On || !h.Running || h.Since == "" || h.Problem != "" || h.State != "working" || h.Line != "Watching your texts" || h.Updated == "" {
		t.Errorf("running: %+v", h)
	}
	want := []ipc.HelperItem{{Title: "One", State: "working", Session: "s38abcdefghij"}, {Title: "Two"}}
	if fmt.Sprint(h.Items) != fmt.Sprint(want) {
		t.Errorf("items = %+v, want %+v", h.Items, want)
	}

	if err := os.Remove(who); err != nil {
		t.Fatal(err)
	}
	if err := c.call(ipc.OpHelpers, ipc.HelpersArgs{ID: "inbox.tick", Restart: true}, nil); err != nil {
		t.Fatal(err)
	}
	testutil.Eventually(t, 30*time.Second, "the helper never started again", func() bool {
		_, err := os.Stat(who)
		return err == nil
	})
	if err := c.call(ipc.OpHelpers, ipc.HelpersArgs{ID: "nothing.here", Restart: true}, nil); err == nil {
		t.Error("restarting a helper that isn't there: no error")
	}

	off := false
	if err := c.call(ipc.OpPlugins, ipc.PluginsArgs{Plugin: "inbox", On: &off}, nil); err != nil {
		t.Fatal(err)
	}
	testutil.Eventually(t, 30*time.Second, "the helper never stopped", func() bool { return helpers() == 0 })
	if h := get(); h.On || h.Running {
		t.Errorf("off again: %+v", h)
	}
}
