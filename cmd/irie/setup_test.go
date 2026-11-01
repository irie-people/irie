package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/tmux"
)

// bare is a computer with nothing Irie needs: an empty PATH, and no
// install folders searched.
func bare(t *testing.T) (home, bin string) {
	t.Helper()
	root := t.TempDir()
	home, bin = filepath.Join(root, "home"), filepath.Join(root, "bin")
	for _, d := range []string{home, bin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin)
	t.Setenv("SHELL", "/bin/zsh")
	for _, k := range []string{"XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(k, "")
	}
	old, oldRun := tmux.Dirs, runFix
	tmux.Dirs = []string{}
	// No test runs an installer: a fix that would is refused.
	runFix = func(argv []string, _ io.Writer) error {
		t.Errorf("a test ran a fix: %q", argv)
		return errors.New("not in a test")
	}
	t.Cleanup(func() { tmux.Dirs, runFix = old, oldRun })
	return home, bin
}

func script(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestFirstRunWithoutTmuxSaysWhatIsMissingAndStops(t *testing.T) {
	bare(t)
	var out bytes.Buffer
	err := firstRun(config.Default(), strings.NewReader(""), &out)
	if err == nil || !strings.Contains(err.Error(), "tmux") {
		t.Errorf("firstRun = %v", err)
	}
	for _, want := range []string{"Irie needs a few things", "✕ tmux", "not installed", "✕ Claude Code", "`irie setup`"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("no %q in:\n%s", want, out.String())
		}
	}
}

func TestFirstRunWithEverythingSaysNothing(t *testing.T) {
	_, bin := bare(t)
	script(t, bin, "tmux", `echo "tmux 3.7b"`)
	script(t, bin, "claude", `case "$1" in --version) echo "2.1.316 (Claude Code)";; auth) echo '{"loggedIn": true}';; esac`)
	var out bytes.Buffer
	if err := firstRun(config.Default(), strings.NewReader(""), &out); err != nil || out.Len() > 0 {
		t.Errorf("firstRun = %v:\n%s", err, out.String())
	}
}

func TestFirstRunSignedOutStartsAnyway(t *testing.T) {
	_, bin := bare(t)
	script(t, bin, "tmux", `echo "tmux 3.7b"`)
	script(t, bin, "claude", `case "$1" in --version) echo "2.1.316 (Claude Code)";; auth) echo '{"loggedIn": false}'; exit 1;; esac`)
	var out bytes.Buffer
	if err := firstRun(config.Default(), strings.NewReader(""), &out); err != nil {
		t.Errorf("firstRun = %v", err)
	}
	if s := out.String(); !strings.Contains(s, "✕ Signed in") || !strings.Contains(s, "Starting Irie anyway") {
		t.Errorf("printed:\n%s", s)
	}
}

func TestSetupJSON(t *testing.T) {
	_, bin := bare(t)
	script(t, bin, "tmux", `echo "tmux 3.7b"`)
	var out bytes.Buffer
	err := setupIrie([]string{"--json"}, strings.NewReader(""), &out)
	if err == nil {
		t.Error("ready without Claude Code")
	}
	if s := out.String(); !strings.Contains(s, `"id": "tmux"`) || !strings.Contains(s, `"status": "missing"`) {
		t.Errorf("printed:\n%s", s)
	}
}

func TestSetupAddsTheLineToThePATHWithAYes(t *testing.T) {
	home, bin := bare(t)
	script(t, bin, "tmux", `echo "tmux 3.7b"`)
	script(t, bin, "git", `echo "git version 2.47.1"`)
	lb := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(lb, 0o755); err != nil {
		t.Fatal(err)
	}
	script(t, lb, "claude", `case "$1" in --version) echo "2.1.316 (Claude Code)";; auth) echo '{"loggedIn": true}';; esac`)
	script(t, lb, "irie", "")
	t.Setenv("IRIE_BIN", filepath.Join(lb, "irie"))
	var out bytes.Buffer
	if err := setupIrie([]string{"--yes"}, strings.NewReader(""), &out); err != nil {
		t.Fatalf("setup = %v:\n%s", err, out.String())
	}
	b, _ := os.ReadFile(filepath.Join(home, ".zshrc"))
	if !strings.Contains(string(b), `export PATH="$HOME/.local/bin:$PATH"`) {
		t.Errorf(".zshrc =\n%s\nprinted:\n%s", b, out.String())
	}
	if !strings.Contains(out.String(), "Everything irie.") {
		t.Errorf("printed:\n%s", out.String())
	}
}

func TestSetupRunsAFixAndChecksAgain(t *testing.T) {
	_, bin := bare(t)
	script(t, bin, "tmux", `echo "tmux 3.7b"`)
	script(t, bin, "git", `echo "git version 2.47.1"`)
	script(t, bin, "claude", `case "$1" in --version) echo "2.1.316 (Claude Code)";; auth) [ -e "$0.in" ] && echo '{"loggedIn": true}' || { echo '{"loggedIn": false}'; exit 1; };; esac`)
	var ran [][]string
	runFix = func(argv []string, _ io.Writer) error {
		ran = append(ran, argv)
		return os.WriteFile(filepath.Join(bin, "claude.in"), nil, 0o644)
	}
	var out bytes.Buffer
	if err := setupIrie([]string{"--yes"}, strings.NewReader(""), &out); err != nil {
		t.Fatalf("setup = %v:\n%s", err, out.String())
	}
	if len(ran) != 1 || ran[0][1] != "auth" || ran[0][2] != "login" {
		t.Errorf("ran %q", ran)
	}
	if s := out.String(); !strings.Contains(s, "✓ Signed in") || !strings.Contains(s, "Everything irie.") {
		t.Errorf("printed:\n%s", s)
	}
}

func TestSetupWithoutAYesChangesNothing(t *testing.T) {
	home, bin := bare(t)
	script(t, bin, "tmux", `echo "tmux 3.7b"`)
	// No terminal to ask in: every fix is left as it is.
	var out bytes.Buffer
	if err := setupIrie(nil, strings.NewReader(""), &out); err == nil {
		t.Error("ready without Claude Code")
	}
	if _, err := os.Stat(filepath.Join(home, ".zshrc")); err == nil {
		t.Error(".zshrc written without a yes")
	}
	if !strings.Contains(out.String(), "Left as it is.") {
		t.Errorf("printed:\n%s", out.String())
	}
}
