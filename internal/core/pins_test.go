package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/plugins"
)

// helperManifest writes a manifest whose helper runs a program, in c's
// plugins folder.
func helperManifest(t *testing.T, c *Core, name string) {
	t.Helper()
	dir := c.paths.Plugins()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".toml"), []byte("[helpers.listen]\nrun = [\"/bin/echo\", \"hi\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestYourWordPinsWhatYourPluginRuns: turning a plugin of yours on pins
// what it runs; the core keeps the pins file as it holds them, writing it
// again when it goes or says otherwise.
func TestYourWordPinsWhatYourPluginRuns(t *testing.T) {
	c := bareCore(t, config.Default())
	helperManifest(t, c, "inbox")
	helperManifest(t, c, "later")
	c.mu.Lock()
	c.keepPins()
	c.mu.Unlock()
	if pins, ok := plugins.ReadPins(c.paths); !ok || len(pins) != 0 {
		t.Fatalf("at the first look, with both off: %v, %v", pins, ok)
	}
	on := true
	if _, err := c.pluginsOp(ipc.PluginsArgs{Plugin: "inbox", On: &on}); err != nil {
		t.Fatal(err)
	}
	pins, _ := plugins.ReadPins(c.paths)
	if _, ok := pins["inbox"]; !ok || len(pins) != 1 {
		t.Fatalf("after your word: %v", pins)
	}
	if err := os.Remove(filepath.Join(c.paths.State, "plugin-pins.json")); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	c.keepPins()
	c.mu.Unlock()
	if got, ok := plugins.ReadPins(c.paths); !ok || !plugins.SamePins(got, pins) {
		t.Errorf("after the file went: %v", got)
	}
	if err := plugins.WritePins(c.paths, plugins.Pins{"inbox": {Files: map[string]string{"/x": "y"}}, "later": {}}); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	c.keepPins()
	c.mu.Unlock()
	if got, _ := plugins.ReadPins(c.paths); !plugins.SamePins(got, pins) {
		t.Errorf("after the file was edited: %v", got)
	}
}

// TestPluginsOnBeforePinsArePinnedAsTheyStand: the first look, with no
// pins file, pins each plugin of yours that runs something and is on, so
// what ran before goes on running.
func TestPluginsOnBeforePinsArePinnedAsTheyStand(t *testing.T) {
	cfg := config.Default()
	cfg.Plugins.Manifests = map[string]bool{"inbox": true}
	c := bareCore(t, cfg)
	helperManifest(t, c, "inbox")
	helperManifest(t, c, "later")
	c.mu.Lock()
	c.keepPins()
	c.mu.Unlock()
	pins, ok := plugins.ReadPins(c.paths)
	if _, in := pins["inbox"]; !ok || !in || len(pins) != 1 {
		t.Errorf("pins = %v, %v", pins, ok)
	}
}

// TestAnAgentHearsWhatAPinnedEditHolds: an agent about to write a file a
// plugin of yours runs is told the edit holds it until you say, so it can
// tell you.
func TestAnAgentHearsWhatAPinnedEditHolds(t *testing.T) {
	c := bareCore(t, config.Default())
	c.ownHelpers.pins = plugins.Pins{"inbox": {Files: map[string]string{"/home/alice/inbox/listen.sh": "abc"}}}
	started := []event.Event{{Kind: event.ToolStarted}}
	note := c.pinnedNote("Edit", json.RawMessage(`{"file_path":"/home/alice/inbox/listen.sh"}`), started)
	if !strings.Contains(note, "plugin inbox runs") || !strings.Contains(note, "plugins on inbox") {
		t.Errorf("note = %q", note)
	}
	if n := c.pinnedNote("Edit", json.RawMessage(`{"file_path":"/home/alice/inbox/notes.md"}`), started); n != "" {
		t.Errorf("another file: %q", n)
	}
	if n := c.pinnedNote("Read", json.RawMessage(`{"file_path":"/home/alice/inbox/listen.sh"}`), started); n != "" {
		t.Errorf("a read: %q", n)
	}
}
