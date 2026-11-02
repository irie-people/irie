package tmux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeExec writes an executable file.
func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

const shimScript = "#!/bin/sh\n" + ShimMark + "\n# through this tmux, and every other call goes to tmux as it would.\nexec /home/alice/.local/share/irie/bin/irie team-tmux -- \"$@\"\n"

func TestFindPassesOverIriesShims(t *testing.T) {
	root := t.TempDir()
	shims := []string{filepath.Join(root, "s1", "bin"), filepath.Join(root, "s2", "bin")}
	real := filepath.Join(root, "real")
	for _, d := range shims {
		writeExec(t, filepath.Join(d, "tmux"), shimScript)
	}
	writeExec(t, filepath.Join(real, "tmux"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", strings.Join(append(shims, real), string(filepath.ListSeparator)))
	got, err := Find("")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(real, "tmux"); got != want {
		t.Fatalf("Find = %s, want the real tmux %s, not a shim", got, want)
	}
}

func TestCleanPathDropsOnlyShimDirs(t *testing.T) {
	root := t.TempDir()
	shim := filepath.Join(root, "s1", "bin")
	writeExec(t, filepath.Join(shim, "tmux"), shimScript)
	other := filepath.Join(root, "tools")
	writeExec(t, filepath.Join(other, "tmux"), "#!/bin/sh\nexit 0\n")
	sep := string(filepath.ListSeparator)
	in := strings.Join([]string{shim, other, "", "/usr/bin", shim}, sep)
	want := strings.Join([]string{other, "", "/usr/bin"}, sep)
	if got := CleanPath(in); got != want {
		t.Fatalf("CleanPath(%q) = %q, want %q", in, got, want)
	}
	env := WithoutShims([]string{"HOME=/home/alice", "PATH=" + in})
	if env[0] != "HOME=/home/alice" || env[1] != "PATH="+want {
		t.Fatalf("WithoutShims = %q", env)
	}
	if IsShim(filepath.Join(other, "tmux")) || IsShim(filepath.Join(root, "missing")) {
		t.Fatal("IsShim took a plain file, or a missing one, for a shim")
	}
}
