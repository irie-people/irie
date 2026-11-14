package plugins

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/paths"
)

// The helpers of every plugin's tests. They carry no build tag, so the
// tests of each plugin a build has compile whichever others it leaves out
// (DESIGN §4.17).

// env is a computer with nothing of the privacy plugin's on it: every
// program it looks for is configured to a path that isn't there, so the
// machine the tests run on doesn't matter.
func env(t *testing.T) Env {
	t.Helper()
	dir := t.TempDir()
	p := paths.Paths{Config: filepath.Join(dir, "config"), State: filepath.Join(dir, "state"), Data: filepath.Join(dir, "data"), Runtime: filepath.Join(dir, "run")}
	cfg := config.Default()
	cfg.VPN.Mullvad = filepath.Join(dir, "none", "mullvad")
	cfg.Anon.Tor = filepath.Join(dir, "none", "tor")
	cfg.Browser.Path = filepath.Join(dir, "none", "mullvad-browser")
	return Env{Config: cfg, Paths: p}
}

// part is the part of p named name.
func part(t *testing.T, p ipc.Plugin, name string) ipc.PluginPart {
	t.Helper()
	for _, x := range p.Parts {
		if x.Name == name {
			return x
		}
	}
	t.Fatalf("no part %q", name)
	return ipc.PluginPart{}
}

// program makes an executable file in the test's folder and returns it.
func program(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}
