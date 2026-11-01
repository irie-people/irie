package loginenv

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParseSkipsWhatStartupFilesPrint(t *testing.T) {
	out := []byte("Last login: today\nwelcome " + Marker + " not yet\n" + Marker +
		"PATH=/opt/homebrew/bin:/usr/bin\x00HOME=/home/alice\x00PWD=/home/alice\x00SHLVL=2\x00NOTE=two\nlines\x00TMUX=/tmp/x,1,0\x00")
	env, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"PATH=/opt/homebrew/bin:/usr/bin", "HOME=/home/alice", "NOTE=two\nlines"}
	if !slices.Equal(env, want) {
		t.Errorf("Parse = %q, want %q", env, want)
	}
}

func TestParseWithoutTheMarkerOrPATH(t *testing.T) {
	if _, err := Parse([]byte("PATH=/usr/bin\x00")); err == nil {
		t.Error("no marker parsed")
	}
	if _, err := Parse([]byte(Marker + "HOME=/home/alice\x00")); err == nil {
		t.Error("no PATH parsed")
	}
}

// shell writes a fake login shell that sources an rc file, as zsh -l -i
// would, then runs its -c command.
func shell(t *testing.T, rc string) (string, []string) {
	t.Helper()
	dir := t.TempDir()
	sh := filepath.Join(dir, "fakesh")
	script := "#!/bin/sh\n" + rc + "\nwhile [ $# -gt 1 ]; do shift; done\neval \"$1\"\n"
	if err := os.WriteFile(sh, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return sh, []string{"HOME=" + dir, "PATH=/usr/bin:/bin", "SHELL=" + sh}
}

func TestCaptureRunsTheStartupFiles(t *testing.T) {
	sh, env := shell(t, `echo "a banner"; export PATH="/home/alice/.local/bin:$PATH"; export EDITOR=vi`)
	got, err := Capture(sh, env, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if value(got, "PATH") != "/home/alice/.local/bin:/usr/bin:/bin" || value(got, "EDITOR") != "vi" {
		t.Errorf("Capture = %q", got)
	}
}

func TestCaptureGivesUpOnAShellThatHangs(t *testing.T) {
	sh, env := shell(t, `sleep 30`)
	start := time.Now()
	if _, err := Capture(sh, env, 300*time.Millisecond); err == nil || !strings.Contains(err.Error(), "longer") {
		t.Errorf("Capture = %v", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("gave up after %v", d)
	}
}

func TestForServerKeepsTheEnvironmentWhenTheShellFails(t *testing.T) {
	sh, env := shell(t, `exit 3`)
	env = append(env, "SHELL="+sh)
	got, err := ForServer(env, "linux")
	if err == nil || !slices.Equal(got, env) {
		t.Errorf("ForServer = %q, %v", got, err)
	}
}

func TestForServerSetsAMacLocale(t *testing.T) {
	sh, env := shell(t, ``)
	env = append(env, "SHELL="+sh)
	got, err := ForServer(env, "darwin")
	if err != nil || value(got, "LC_CTYPE") != "UTF-8" {
		t.Errorf("ForServer = %q, %v", got, err)
	}
	sh, env = shell(t, `export LANG=fr_FR.UTF-8`)
	env = append(env, "SHELL="+sh)
	if got, _ := ForServer(env, "darwin"); value(got, "LC_CTYPE") != "" {
		t.Errorf("a locale was added to LANG: %q", got)
	}
}
