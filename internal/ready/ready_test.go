package ready

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fake writes an executable script named name into dir that prints out and
// exits with code.
func fake(t *testing.T, dir, name, script string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// machine is a home folder and a bin folder on PATH, with nothing in it.
func machine(t *testing.T) (home, bin string, o Options) {
	t.Helper()
	root := t.TempDir()
	home, bin = filepath.Join(root, "home", "alice"), filepath.Join(root, "bin")
	for _, d := range []string{home, bin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// /bin for the scripts' own sh and cat; nothing Irie looks for is there.
	return home, bin, Options{
		GOOS:   "linux",
		Env:    []string{"HOME=" + home, "PATH=" + bin, "SHELL=/bin/zsh"},
		Dirs:   []string{},
		SignIn: true,
	}
}

const signedIn = `case "$1" in --version) echo "2.1.316 (Claude Code)";; auth) echo '{"loggedIn": true, "authMethod": "claude.ai", "email": "alice@example.com"}';; esac`

func TestReadyWhenEverythingIsThere(t *testing.T) {
	_, bin, o := machine(t)
	fake(t, bin, "tmux", `echo "tmux 3.7b"`)
	fake(t, bin, "claude", signedIn)
	fake(t, bin, "git", `echo "git version 2.47.1"`)
	r := Check(o)
	if !r.Ready() {
		t.Fatalf("not ready: %+v", r.Blocking())
	}
	want := map[string]string{TmuxID: "3.7b", ClaudeID: "2.1.316", SignInID: "", GitID: "2.47.1"}
	for id, v := range want {
		it, ok := r.Item(id)
		if !ok || it.Status != OK || it.Version != v {
			t.Errorf("%s = %+v, want ok %s", id, it, v)
		}
	}
	if it, _ := r.Item(SignInID); it.For != "Claude Code works on your Claude account" {
		t.Errorf("signed in for %q", it.For)
	}
	if _, ok := r.Item(PathID); ok {
		t.Error("the PATH check ran with no Self")
	}
}

func TestMissingTmuxBlocksAndSaysHowToInstall(t *testing.T) {
	_, bin, o := machine(t)
	fake(t, bin, "claude", signedIn)
	fake(t, bin, "apt-get", `exit 0`)
	r := Check(o)
	if r.Ready() {
		t.Fatal("ready without tmux")
	}
	it, _ := r.Item(TmuxID)
	if it.Status != Missing || !it.Blocks || it.Fix == nil {
		t.Fatalf("tmux = %+v", it)
	}
	if !strings.Contains(it.Fix.Show, "apt-get install -y tmux") || it.Fix.Argv == nil {
		t.Errorf("fix = %+v, want apt-get", it.Fix)
	}
}

func TestOldTmuxIsTooOld(t *testing.T) {
	_, bin, o := machine(t)
	fake(t, bin, "tmux", `echo "tmux 3.1c"`)
	if it, _ := Check(o).Item(TmuxID); it.Status != Missing || !strings.Contains(it.Problem, "too old") {
		t.Errorf("tmux 3.1c = %+v", it)
	}
	for v, ok := range map[string]bool{"3.2": true, "3.2a": true, "3.10": true, "next-3.8": true, "2.9a": false, "3.1": false} {
		if got := versionAtLeast(v, MinTmux); got != ok {
			t.Errorf("versionAtLeast(%q) = %v", v, got)
		}
	}
	fake(t, bin, "tmux", `echo "tmux master"`)
	if it, _ := Check(o).Item(TmuxID); it.Status != OK {
		t.Errorf("tmux master = %+v, want ok", it)
	}
}

func TestMissingClaudeOffersTheOfficialInstaller(t *testing.T) {
	_, bin, o := machine(t)
	fake(t, bin, "tmux", `echo "tmux 3.7b"`)
	r := Check(o)
	it, _ := r.Item(ClaudeID)
	if it.Status != Missing || !it.Blocks || it.Fix == nil || it.Fix.Show != ClaudeInstaller {
		t.Fatalf("claude = %+v", it)
	}
	if _, ok := r.Item(SignInID); ok {
		t.Error("sign-in checked with no Claude Code")
	}
}

func TestClaudeInLocalBinIsFoundForAgents(t *testing.T) {
	home, bin, o := machine(t)
	fake(t, bin, "tmux", `echo "tmux 3.7b"`)
	fake(t, filepath.Join(home, LocalBin), "claude", signedIn)
	// ~/.local/bin is not on PATH, but agents get it (PathEnv).
	if it, _ := Check(o).Item(ClaudeID); it.Status != OK {
		t.Errorf("claude in ~/.local/bin = %+v, want ok", it)
	}
}

func TestClaudeWhereAgentsDontLook(t *testing.T) {
	home, bin, o := machine(t)
	fake(t, bin, "tmux", `echo "tmux 3.7b"`)
	fake(t, filepath.Join(home, ".claude", "local"), "claude", signedIn)
	it, _ := Check(o).Item(ClaudeID)
	if it.Status != Missing || it.Fix == nil || it.Fix.Line == "" {
		t.Fatalf("claude in ~/.claude/local = %+v", it)
	}
	if want := filepath.Join(home, ".zshrc"); it.Fix.File != want || it.Fix.Line != `export PATH="$HOME/.claude/local:$PATH"` {
		t.Errorf("fix = %+v", it.Fix)
	}
}

func TestOldClaudeWarns(t *testing.T) {
	_, bin, o := machine(t)
	fake(t, bin, "tmux", `echo "tmux 3.7b"`)
	fake(t, bin, "claude", `case "$1" in --version) echo "2.1.200 (Claude Code)";; auth) echo '{"loggedIn": true}';; esac`)
	r := Check(o)
	if it, _ := r.Item(ClaudeID); it.Status != Warn || it.Fix == nil {
		t.Errorf("claude 2.1.200 = %+v, want warn", it)
	}
	if !r.Ready() {
		t.Error("an old Claude Code blocks")
	}
}

func TestSignedOutBlocksAndOffersLogin(t *testing.T) {
	_, bin, o := machine(t)
	fake(t, bin, "tmux", `echo "tmux 3.7b"`)
	p := fake(t, bin, "claude", `case "$1" in --version) echo "2.1.316 (Claude Code)";; auth) echo 'Warning: noise'; echo '{"loggedIn": false, "authMethod": "none"}'; exit 1;; esac`)
	r := Check(o)
	it, _ := r.Item(SignInID)
	if it.Status != Missing || !it.Blocks || r.Ready() {
		t.Fatalf("signed out = %+v", it)
	}
	if !slices.Equal(it.Fix.Argv, []string{p, "auth", "login"}) {
		t.Errorf("fix = %+v", it.Fix)
	}
}

func TestSignInUnknownOnAnOldClaudeCode(t *testing.T) {
	_, bin, o := machine(t)
	fake(t, bin, "tmux", `echo "tmux 3.7b"`)
	fake(t, bin, "claude", `case "$1" in --version) echo "2.1.316 (Claude Code)";; *) echo "error: unknown command 'auth'" >&2; exit 1;; esac`)
	r := Check(o)
	if it, _ := r.Item(SignInID); it.Status != Unknown || !r.Ready() {
		t.Errorf("no auth status = %+v, ready %v", it, r.Ready())
	}
}

func TestGitIsNeededOnlyForProjects(t *testing.T) {
	_, bin, o := machine(t)
	fake(t, bin, "tmux", `echo "tmux 3.7b"`)
	fake(t, bin, "claude", signedIn)
	r := Check(o)
	it, _ := r.Item(GitID)
	if it.Status != Missing || it.Blocks || !r.Ready() {
		t.Errorf("no git = %+v, ready %v", it, r.Ready())
	}
}

func TestPathCheck(t *testing.T) {
	home, bin, o := machine(t)
	fake(t, bin, "tmux", `echo "tmux 3.7b"`)
	self := fake(t, filepath.Join(home, ".local", "share", "irie", "bin"), "irie", "")
	o.Self = self
	it, _ := Check(o).Item(PathID)
	if it.Status != Warn || it.Fix == nil || it.Fix.Line != `export PATH="$HOME/.local/share/irie/bin:$PATH"` {
		t.Fatalf("irie off PATH = %+v", it)
	}

	// ~/.local/bin/irie, a link to it, but ~/.local/bin is not on the
	// terminal's PATH: agents get it, a terminal does not.
	lb := filepath.Join(home, LocalBin)
	if err := os.MkdirAll(lb, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(self, filepath.Join(lb, "irie")); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{filepath.Join(lb, "irie"), self} {
		o.Self = s
		if it, _ := Check(o).Item(PathID); it.Status != Warn || it.Fix == nil || it.Fix.Line != `export PATH="$HOME/.local/bin:$PATH"` {
			t.Errorf("irie as %s, linked in ~/.local/bin off PATH = %+v", s, it)
		}
	}
	o.Env = []string{"HOME=" + home, "PATH=" + bin + ":" + lb}
	if it, _ := Check(o).Item(PathID); it.Status != OK {
		t.Errorf("irie on PATH = %+v", it)
	}

	other := fake(t, bin, "irie", "")
	if it, _ := Check(o).Item(PathID); it.Status != Warn || it.Path != other {
		t.Errorf("another irie first = %+v", it)
	}
}

func TestPathEnv(t *testing.T) {
	home := t.TempDir()
	lb := filepath.Join(home, LocalBin)
	env := []string{"HOME=" + home, "PATH=/usr/bin:/bin"}
	if got := PathEnv(env, home); !slices.Equal(got, env) {
		t.Errorf("no ~/.local/bin: %v", got)
	}
	if err := os.MkdirAll(lb, 0o755); err != nil {
		t.Fatal(err)
	}
	got := PathEnv(env, home)
	if got[1] != "PATH=/usr/bin:/bin:"+lb {
		t.Errorf("PathEnv = %v", got)
	}
	if env[1] != "PATH=/usr/bin:/bin" {
		t.Error("PathEnv changed its input")
	}
	if again := PathEnv(got, home); !slices.Equal(again, got) {
		t.Errorf("twice = %v", again)
	}
}

func TestStartupLine(t *testing.T) {
	home := "/home/alice"
	for _, c := range []struct{ shell, zdotdir, goos, file, line string }{
		{"/bin/zsh", "", "darwin", "/home/alice/.zshrc", `export PATH="$HOME/.local/bin:$PATH"`},
		{"/bin/zsh", "/home/alice/.config/zsh", "linux", "/home/alice/.config/zsh/.zshrc", `export PATH="$HOME/.local/bin:$PATH"`},
		{"/bin/bash", "", "darwin", "/home/alice/.bash_profile", `export PATH="$HOME/.local/bin:$PATH"`},
		{"/usr/bin/bash", "", "linux", "/home/alice/.bashrc", `export PATH="$HOME/.local/bin:$PATH"`},
		{"/usr/bin/fish", "", "linux", "/home/alice/.config/fish/config.fish", "fish_add_path $HOME/.local/bin"},
		{"", "", "linux", "/home/alice/.profile", `export PATH="$HOME/.local/bin:$PATH"`},
	} {
		file, line := StartupLine(c.shell, home, c.zdotdir, c.goos, "/home/alice/.local/bin")
		if file != c.file || line != c.line {
			t.Errorf("%s on %s = %s, %s", c.shell, c.goos, file, line)
		}
	}
}

func TestAddLineOnce(t *testing.T) {
	f := filepath.Join(t.TempDir(), "fish", "config.fish")
	for range 2 {
		if err := AddLine(f, "fish_add_path $HOME/.local/bin"); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(f)
	if n := strings.Count(string(b), "fish_add_path"); n != 1 {
		t.Errorf("the line is there %d times:\n%s", n, b)
	}
	g := filepath.Join(t.TempDir(), ".zshrc")
	os.WriteFile(g, []byte("alias ll='ls -l'"), 0o644)
	AddLine(g, "x")
	if b, _ := os.ReadFile(g); !strings.HasPrefix(string(b), "alias ll='ls -l'\n\n# Added by irie setup") || !strings.HasSuffix(string(b), "\nx\n") {
		t.Errorf("appended:\n%q", b)
	}
}

func TestMacWithoutHomebrewOffersIt(t *testing.T) {
	_, _, o := machine(t)
	o.GOOS = "darwin"
	it, _ := Check(o).Item(TmuxID)
	if it.Fix == nil || !strings.Contains(it.Fix.Say, "Homebrew") || it.Fix.Argv[0] != "/bin/bash" {
		t.Errorf("tmux on a Mac without Homebrew = %+v", it.Fix)
	}
}

func TestMacWithHomebrew(t *testing.T) {
	_, bin, o := machine(t)
	o.GOOS = "darwin"
	brew := fake(t, bin, "brew", "")
	it, _ := Check(o).Item(TmuxID)
	if it.Fix == nil || !slices.Equal(it.Fix.Argv, []string{brew, "install", "tmux"}) || it.Fix.Show != "brew install tmux" {
		t.Errorf("tmux with Homebrew = %+v", it.Fix)
	}
}
