package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/paths"
)

// TestPluginsTurnsABundleOnAndOff: irie plugins lists your own plugins and
// bundles from the plugins folder, and on and off take a bundle's name
// (docs/proposals/plugin-panes.md).
func TestPluginsTurnsABundleOnAndOff(t *testing.T) {
	bare(t)
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	p, err := paths.FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(p.Plugins(), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		"invoices.toml": "title = \"Invoices\"\n[panes.invoices]\naddress = \"http://127.0.0.1:1/\"\n",
		"shop.toml":     "title = \"Shop\"\nplugins = [\"invoices\"]\n",
	} {
		if err := os.WriteFile(filepath.Join(p.Plugins(), name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if err := pluginsCmd(nil, &out); err != nil {
		t.Fatal(err)
	}
	if s := out.String(); !strings.HasPrefix(s, "Shop bundle: on.") || !strings.Contains(s, "Invoices plugin: on.") {
		t.Errorf("irie plugins:\n%s", s)
	}
	out.Reset()
	if err := pluginsCmd([]string{"off", "shop"}, &out); err != nil {
		t.Fatal(err)
	}
	if s := out.String(); !strings.HasPrefix(s, "Shop bundle: off.") {
		t.Errorf("irie plugins off shop:\n%s", s)
	}
	b, err := os.ReadFile(p.ConfigFile())
	if err != nil || !strings.Contains(string(b), "invoices = false") {
		t.Errorf("config.toml: %v\n%s", err, b)
	}
	out.Reset()
	if err := pluginsCmd([]string{"on", "invoices"}, &out); err != nil || !strings.HasPrefix(out.String(), "Invoices plugin: on.") {
		t.Errorf("irie plugins on invoices: %v\n%s", err, out.String())
	}
}
