// Package testutil holds helpers shared by Irie's tests: once-per-package
// builds of the irie binary and the fake agent, short directories for unix
// sockets, isolated XDG environments, private tmux servers and temp git
// repos kept away from the user's own git config.
package testutil

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type build struct {
	once sync.Once
	bin  string
	err  error
}

var (
	buildMu  sync.Mutex
	buildDir string
	builds   = map[string]*build{}
)

// Main runs a package's tests, then removes the binaries BuildIrie and
// BuildFakeAgent made. Packages that call them use it as their TestMain.
func Main(m *testing.M) {
	code := m.Run()
	if buildDir != "" {
		os.RemoveAll(buildDir)
	}
	os.Exit(code)
}

// buildOnce builds the Go package pkg once per test binary, into a temp dir
// shared by every build, and returns the binary's path.
func buildOnce(t testing.TB, pkg, name string) string {
	t.Helper()
	buildMu.Lock()
	b := builds[pkg]
	if b == nil {
		b = &build{}
		builds[pkg] = b
	}
	buildMu.Unlock()
	b.once.Do(func() {
		goBin, err := exec.LookPath("go")
		if err != nil {
			b.err = err
			return
		}
		buildMu.Lock()
		if buildDir == "" {
			buildDir, b.err = os.MkdirTemp("", "irie-bin-")
		}
		dir := buildDir
		buildMu.Unlock()
		if b.err != nil {
			return
		}
		b.bin = filepath.Join(dir, name)
		if out, err := exec.Command(goBin, "build", "-o", b.bin, pkg).CombinedOutput(); err != nil {
			b.err = fmt.Errorf("go build %s: %v\n%s", pkg, err, out)
		}
	})
	if b.err != nil {
		t.Fatal(b.err)
	}
	return b.bin
}

// BuildIrie builds ./cmd/irie once per test binary and returns its path.
func BuildIrie(t testing.TB) string {
	t.Helper()
	return buildOnce(t, "github.com/irie-people/irie/cmd/irie", "irie")
}

// BuildFakeAgent builds the fake agent, Claude Code's stand-in, once per test
// binary and returns its path.
func BuildFakeAgent(t testing.TB) string {
	t.Helper()
	return buildOnce(t, "github.com/irie-people/irie/internal/testutil/fakeagent", "fakeagent")
}

// BuildFakeOllama builds the fake Ollama server, `ollama serve`'s stand-in,
// once per test binary and returns its path.
func BuildFakeOllama(t testing.TB) string {
	t.Helper()
	return buildOnce(t, "github.com/irie-people/irie/internal/testutil/fakeollama", "fakeollama")
}

// BuildFakeLlama builds the stand-in for llama.cpp's llama-server once per
// test binary and returns its path.
func BuildFakeLlama(t testing.TB) string {
	t.Helper()
	return buildOnce(t, "github.com/irie-people/irie/internal/testutil/fakellama", "llama-server")
}

// BuildOldHost builds the stand-in for `irie host` as it was before freeze
// once per test binary and returns its path.
func BuildOldHost(t testing.TB) string {
	t.Helper()
	return buildOnce(t, "github.com/irie-people/irie/internal/testutil/oldhost", "oldhost")
}

// FreePort returns a TCP port on 127.0.0.1 that nothing listens on now.
func FreePort(t testing.TB) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// ShortDir returns a new directory under /tmp. Its path stays short enough for
// a unix socket inside it, which macOS caps at about 104 bytes.
func ShortDir(t testing.TB) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "irie-t-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

// NeedTmux skips the test when tmux is not installed and returns its path.
func NeedTmux(t testing.TB) string {
	t.Helper()
	p, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux not installed")
	}
	return p
}

// Random returns n random bytes as lowercase hex.
func Random(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Env keeps a test away from the user's own Irie and tmux. Every XDG dir, the
// runtime dir and the tmux socket dir point at fresh temp dirs, TMUX is unset,
// and Socket names a private tmux server that is killed at cleanup.
type Env struct {
	ConfigHome, StateHome, DataHome, CacheHome string
	RuntimeDir                                 string // XDG_RUNTIME_DIR, under ShortDir
	TmuxTmpDir                                 string // TMUX_TMPDIR, under ShortDir
	Socket                                     string // tmux -L name: irie-test-<random>
}

// NewEnv sets up an isolated environment for t with t.Setenv.
func NewEnv(t *testing.T) *Env {
	t.Helper()
	root := t.TempDir()
	short := ShortDir(t)
	e := &Env{
		ConfigHome: filepath.Join(root, "config"),
		StateHome:  filepath.Join(root, "state"),
		DataHome:   filepath.Join(root, "data"),
		CacheHome:  filepath.Join(root, "cache"),
		RuntimeDir: filepath.Join(short, "run"),
		TmuxTmpDir: filepath.Join(short, "tmux"),
		Socket:     "irie-test-" + Random(4),
	}
	for _, d := range []string{e.ConfigHome, e.StateHome, e.DataHome, e.CacheHome, e.RuntimeDir, e.TmuxTmpDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("XDG_CONFIG_HOME", e.ConfigHome)
	t.Setenv("XDG_STATE_HOME", e.StateHome)
	t.Setenv("XDG_DATA_HOME", e.DataHome)
	t.Setenv("XDG_CACHE_HOME", e.CacheHome)
	t.Setenv("XDG_RUNTIME_DIR", e.RuntimeDir)
	t.Setenv("TMUX_TMPDIR", e.TmuxTmpDir)
	for _, k := range []string{"TMUX", "TMUX_PANE", "IRIE_BIN"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	// Registered after the Setenv calls, so it runs before they are undone.
	t.Cleanup(e.killServer)
	return e
}

// killServer kills the private tmux server and waits for its panes' first
// processes to exit, so none of them writes into a temp dir, such as an
// agent host's exit.json, while the temp dirs are being removed.
func (e *Env) killServer() {
	out, _ := e.tmuxCmd("list-panes", "-a", "-F", "#{pane_pid}").Output()
	e.tmuxCmd("kill-server").Run()
	deadline := time.Now().Add(5 * time.Second)
	for _, f := range strings.Fields(string(out)) {
		pid, err := strconv.Atoi(f)
		if err != nil {
			continue
		}
		for syscall.Kill(pid, 0) == nil && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// WriteConfig writes config.toml with the test's tmux socket and extra TOML.
func (e *Env) WriteConfig(t testing.TB, extra string) {
	t.Helper()
	dir := filepath.Join(e.ConfigHome, "irie")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("[tmux]\nsocket = %q\n\n%s", e.Socket, extra)
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// tmuxCmd runs tmux on the private server. -u, as the core's own runner has
// it: a name is UTF-8 whatever the test's locale.
func (e *Env) tmuxCmd(args ...string) *exec.Cmd {
	cmd := exec.Command("tmux", append([]string{"-u", "-L", e.Socket}, args...)...)
	cmd.Env = append(os.Environ(), "TMUX_TMPDIR="+e.TmuxTmpDir)
	return cmd
}

// Tmux runs a command against the test's private server and returns its
// trimmed output. It fails the test when tmux fails.
func (e *Env) Tmux(t testing.TB, args ...string) string {
	t.Helper()
	out, err := e.tmuxCmd(args...).CombinedOutput()
	if err != nil {
		t.Fatalf("tmux %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// TryTmux is Tmux without failing the test.
func (e *Env) TryTmux(args ...string) (string, error) {
	out, err := e.tmuxCmd(args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// NeedGit skips the test when git is not installed and returns its path.
func NeedGit(t testing.TB) string {
	t.Helper()
	p, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	return p
}

// gitEnv are the variables that point git at a repo, or feed it config,
// from outside: a test never inherits them.
var gitEnv = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_IMPLICIT_WORK_TREE", "GIT_GRAFT_FILE", "GIT_NO_REPLACE_OBJECTS",
	"GIT_REPLACE_REF_BASE", "GIT_PREFIX", "GIT_SHALLOW_FILE", "GIT_CONFIG", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT",
}

// IsolateGit keeps git, in the test and in every process it starts, away
// from the user's own git config and from any repo the caller's environment
// points at. Git never looks for a repo above the temp dirs, and commits get
// a fixed identity.
func IsolateGit(t *testing.T) {
	t.Helper()
	for _, k := range gitEnv {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	for k, v := range map[string]string{
		"GIT_CONFIG_GLOBAL":       "/dev/null",
		"GIT_CONFIG_NOSYSTEM":     "1",
		"GIT_CEILING_DIRECTORIES": os.TempDir() + ":/tmp",
		"GIT_AUTHOR_NAME":         "alice",
		"GIT_AUTHOR_EMAIL":        "alice@example.com",
		"GIT_COMMITTER_NAME":      "alice",
		"GIT_COMMITTER_EMAIL":     "alice@example.com",
	} {
		t.Setenv(k, v)
	}
}

// Git runs git in dir and returns its trimmed output, failing the test when
// git fails. dir must be absolute, so a test never reaches the repo it runs
// in by accident.
func Git(t testing.TB, dir string, args ...string) string {
	t.Helper()
	if !filepath.IsAbs(dir) {
		t.Fatalf("git in %q: not an absolute path", dir)
	}
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git -C %s %s: %v\n%s", dir, strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// WriteFiles writes files under dir, path → contents, making directories as
// needed.
func WriteFiles(t testing.TB, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// NewRepo makes a git repo at dir, on branch main, whose one commit holds
// files. Call IsolateGit first.
func NewRepo(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	NeedGit(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	Git(t, dir, "init", "-q", "-b", "main")
	WriteFiles(t, dir, files)
	Git(t, dir, "add", "-A")
	Git(t, dir, "commit", "-q", "-m", "First commit")
	return dir
}

// Eventually polls cond every 20 ms until it returns true, and fails the test
// with msg when timeout passes first.
func Eventually(t testing.TB, timeout time.Duration, msg string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("after %v: %s", timeout, msg)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
