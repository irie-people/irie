package host

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestASpecAndAnExitReadBackAsWritten: the files the core and the host hand
// each other read back as they were written, and one that is missing, cut
// short or without a command says so, naming the file.
func TestASpecAndAnExitReadBackAsWritten(t *testing.T) {
	dir := t.TempDir()
	spec := Spec{
		Session:  "s1",
		Argv:     []string{"claude", "--resume", "x"},
		Env:      map[string]string{"A": "1"},
		Path:     []string{"/home/alice/bin"},
		Cwd:      "/home/alice/src",
		Shell:    []string{"/bin/zsh", "-l"},
		ShellEnv: map[string]string{"TERM": "xterm-256color"},
	}
	p := filepath.Join(dir, "spec.json")
	if err := WriteSpec(p, spec); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("the spec's mode: %v %v", fi, err)
	}
	if got, err := ReadSpec(p); err != nil || !reflect.DeepEqual(got, spec) {
		t.Errorf("ReadSpec = %+v, %v; want %+v", got, err, spec)
	}

	x := Exit{Code: 130, Signal: 2, At: time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC), Host: 4242}
	xp := filepath.Join(dir, "exit.json")
	if err := writeJSON(xp, x); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadExit(xp); err != nil || got.Code != x.Code || got.Signal != x.Signal || got.Host != x.Host || !got.At.Equal(x.At) {
		t.Errorf("ReadExit = %+v, %v; want %+v", got, err, x)
	}

	gone := filepath.Join(dir, "gone.json")
	if _, err := ReadSpec(gone); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a missing spec: %v", err)
	}
	if _, err := ReadExit(gone); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a missing exit: %v", err)
	}
	cut := filepath.Join(dir, "cut.json")
	if err := os.WriteFile(cut, []byte(`{"argv": [`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSpec(cut); err == nil || !strings.Contains(err.Error(), cut) {
		t.Errorf("a spec cut short: %v", err)
	}
	if _, err := ReadExit(cut); err == nil || !strings.Contains(err.Error(), cut) {
		t.Errorf("an exit cut short: %v", err)
	}
	none := filepath.Join(dir, "none.json")
	if err := os.WriteFile(none, []byte(`{"session": "s1", "cwd": "/home/alice"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSpec(none); err == nil || !strings.Contains(err.Error(), none+": no argv") {
		t.Errorf("a spec with no command: %v", err)
	}
}

// TestLookPathSearchesTheAgentsPATH: the host finds the agent's program on
// the PATH the agent will run with, in absolute folders only, so a relative
// entry never runs a program from the folder the agent starts in, and only
// a file that may run counts. A name with a slash is that file.
func TestLookPathSearchesTheAgentsPATH(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	bin := filepath.Join(dir, "bin")
	for _, d := range []string{bin, filepath.Join(bin, "folder"), filepath.Join(dir, "rel")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	tool := filepath.Join(bin, "tool")
	for f, mode := range map[string]os.FileMode{tool: 0o755, filepath.Join(bin, "plain"): 0o644, filepath.Join(dir, "rel", "tool"): 0o755} {
		if err := os.WriteFile(f, []byte("#!/bin/sh\n"), mode); err != nil {
			t.Fatal(err)
		}
	}
	env := []string{"HOME=/home/alice", "PATH=rel" + string(os.PathListSeparator) + bin}

	if p, err := LookPath("tool", env); err != nil || p != tool {
		t.Errorf("LookPath(tool) = %q, %v; want %q", p, err, tool)
	}
	for _, name := range []string{"plain", "folder", "missing", filepath.Join(bin, "plain")} {
		if p, err := LookPath(name, env); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("LookPath(%s) = %q, %v; want not found", name, p, err)
		}
	}
	if p, err := LookPath(tool, nil); err != nil || p != tool {
		t.Errorf("LookPath(%s) = %q, %v", tool, p, err)
	}
	if p, err := LookPath("tool", []string{"HOME=/home/alice"}); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("with no PATH, LookPath(tool) = %q, %v", p, err)
	}
}

// TestPrependPathPutsTheSessionsFoldersFirst: a session's folders go before
// the PATH its agent would have, make one when there is none, and leave the
// environment as it was when there are none.
func TestPrependPathPutsTheSessionsFoldersFirst(t *testing.T) {
	sep := string(os.PathListSeparator)
	for _, c := range []struct{ env, dirs, want []string }{
		{[]string{"A=1", "PATH=/usr/bin"}, []string{"/x", "/y"}, []string{"A=1", "PATH=/x" + sep + "/y" + sep + "/usr/bin"}},
		{[]string{"PATH="}, []string{"/x"}, []string{"PATH=/x"}},
		{[]string{"A=1"}, []string{"/x"}, []string{"A=1", "PATH=/x"}},
		{[]string{"A=1", "PATH=/usr/bin"}, nil, []string{"A=1", "PATH=/usr/bin"}},
	} {
		if got := prependPath(slices.Clone(c.env), c.dirs); !slices.Equal(got, c.want) {
			t.Errorf("prependPath(%q, %q) = %q, want %q", c.env, c.dirs, got, c.want)
		}
	}
}
