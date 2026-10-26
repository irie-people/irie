// Package testutil holds helpers shared by Irie's tests: a once-per-package
// build of the irie binary, short directories for unix sockets, isolated XDG
// environments and private tmux servers.
package testutil

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	buildOnce sync.Once
	buildDir  string
	buildBin  string
	buildErr  error
)

// Main runs a package's tests, then removes the binary BuildIrie made.
// Packages that call BuildIrie use it as their TestMain.
func Main(m *testing.M) {
	code := m.Run()
	if buildDir != "" {
		os.RemoveAll(buildDir)
	}
	os.Exit(code)
}

// BuildIrie builds ./cmd/irie once per test binary and returns its path.
func BuildIrie(t testing.TB) string {
	t.Helper()
	buildOnce.Do(func() {
		goBin, err := exec.LookPath("go")
		if err != nil {
			buildErr = err
			return
		}
		buildDir, buildErr = os.MkdirTemp("", "irie-bin-")
		if buildErr != nil {
			return
		}
		buildBin = filepath.Join(buildDir, "irie")
		out, err := exec.Command(goBin, "build", "-o", buildBin, "github.com/irie-hq/irie/cmd/irie").CombinedOutput()
		if err != nil {
			buildErr = fmt.Errorf("go build ./cmd/irie: %v\n%s", err, out)
		}
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return buildBin
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
	t.Cleanup(func() { e.tmuxCmd("kill-server").Run() })
	return e
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

func (e *Env) tmuxCmd(args ...string) *exec.Cmd {
	cmd := exec.Command("tmux", append([]string{"-L", e.Socket}, args...)...)
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
