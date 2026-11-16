//go:build !noextension

package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/extension"
	"github.com/irie-people/irie/internal/ipc"
)

func extensionStatus(t *testing.T, e Env) ipc.Plugin {
	t.Helper()
	for _, p := range Status(e) {
		if p.Name == "extension" {
			return p
		}
	}
	t.Fatal("no extension plugin")
	return ipc.Plugin{}
}

// TestTheBrowserExtensionStepByStep: on by default, the extension needs
// setting up, then loading in a browser, and is ready once a browser has
// talked to Irie; Terms in short is ready while the plugin is on; off,
// both are, and Terms in short's words say how to turn it on.
func TestTheBrowserExtensionStepByStep(t *testing.T) {
	e := env(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	p := extensionStatus(t, e)
	if !p.On || p.Title != "Browser extension" || p.Setup != ":setup extension" || len(p.Parts) != 2 {
		t.Fatalf("extension = %+v", p)
	}
	if x := part(t, p, Extension); x.State != ipc.PartNeeds || !strings.Contains(x.Detail, "setup extension") {
		t.Errorf("not set up: %+v", x)
	}
	if x := part(t, p, Terms); x.State != ipc.PartReady || !strings.Contains(x.Detail, "terms example.com") {
		t.Errorf("terms: %+v", x)
	}

	var chrome extension.Browser
	for _, b := range extension.Browsers(home) {
		if b.Name == "Chrome" {
			chrome = b
		}
	}
	os.MkdirAll(chrome.Home, 0o755)
	if _, err := extension.Setup(e.Paths, home, "/opt/irie/irie"); err != nil {
		t.Fatal(err)
	}
	if x := part(t, extensionStatus(t, e), Extension); x.State != ipc.PartNeeds || !strings.Contains(x.Detail, "Load unpacked") || !strings.Contains(x.Detail, "Chrome") {
		t.Errorf("set up, not loaded: %+v", x)
	}
	seen, _ := json.Marshal(map[string]time.Time{"Chrome": time.Now().Add(-5 * time.Minute)})
	os.MkdirAll(e.Paths.State, 0o755)
	os.WriteFile(filepath.Join(e.Paths.State, "extension-seen.json"), seen, 0o600)
	if x := part(t, extensionStatus(t, e), Extension); x.State != ipc.PartReady || !strings.Contains(x.Detail, "Chrome 5 minutes ago") {
		t.Errorf("loaded: %+v", x)
	}

	e.Config.Plugins.Extension = false
	p = extensionStatus(t, e)
	if p.On || part(t, p, Terms).State != ipc.PartPluginOff || part(t, p, Extension).State != ipc.PartPluginOff {
		t.Errorf("off: %+v", p)
	}
	if why := Why(e.Config, Terms); !strings.Contains(why, "plugins on extension") {
		t.Errorf("Why = %q", why)
	}
	if ed, err := Edits("extension", "", true); err != nil || len(ed) != 1 || ed[0].Key != "plugins.extension" {
		t.Errorf("Edits = %+v, %v", ed, err)
	}
}
