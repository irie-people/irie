package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/paths"
)

// TestPluginsTurnsAPartAndThePluginOnAndOff: with no core to ask, irie
// plugins writes config.toml itself, and reads how each part stands from
// the files (DESIGN §4.17).
func TestPluginsTurnsAPartAndThePluginOnAndOff(t *testing.T) {
	bare(t)
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	var out bytes.Buffer
	if err := pluginsCmd(nil, &out); err != nil {
		t.Fatal(err)
	}
	if s := out.String(); !strings.HasPrefix(s, "Privacy plugin: on.") || !strings.Contains(s, "○ Bitwarden for agents: Not in Irie yet.") {
		t.Errorf("irie plugins:\n%s", s)
	}
	out.Reset()
	if err := pluginsCmd([]string{"on", "privacy", "browser"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Mullvad Browser for agents:") {
		t.Errorf("irie plugins on privacy browser:\n%s", out.String())
	}
	if err := pluginsCmd([]string{"off", "privacy"}, &out); err != nil {
		t.Fatal(err)
	}
	p, err := paths.FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[browser]\nconnector = \"on\"", "[plugins]\nprivacy = false"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("config.toml lacks %q:\n%s", want, b)
		}
	}
	if fi, err := os.Stat(p.ConfigFile()); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("config.toml's mode: %v, %v", fi.Mode(), err)
	}
	for _, args := range [][]string{{"on", "privacy", "vault"}, {"on", "nothing"}, {"on"}, {"toggle", "privacy"}} {
		if err := pluginsCmd(args, &out); err == nil {
			t.Errorf("irie plugins %s went through", strings.Join(args, " "))
		}
	}
	if _, err := os.Stat(filepath.Dir(p.ConfigFile())); err != nil {
		t.Fatal(err)
	}
}
