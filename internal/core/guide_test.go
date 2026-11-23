package core

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/tmux"
)

// bareCore is a core with its own folders and no tmux, for what happens
// before a spawn reaches tmux.
func bareCore(t *testing.T, cfg config.Config) *Core {
	t.Helper()
	// It sees a machine with room for the fake models, as a test core
	// started by testutil does, never the user's own memory and disk.
	t.Setenv(FakeMemoryEnv, "64/48")
	t.Setenv(FakeRoomEnv, "64/500/0")
	home := t.TempDir()
	p, err := paths.New(func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}, os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	return newCore(Options{Config: cfg, Paths: p, Version: "test", Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, &tmux.Runner{Bin: filepath.Join(home, "no-tmux")})
}

// noPlugins is a claude whose --help lists no --plugin-dir.
func noPlugins(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\ncase \"$1\" in --version) echo '2.1.316 (Claude Code)';; --help) echo '  -n, --name <name>  Set a display name';; esac\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}
