// Package testutil holds helpers shared by Irie's tests: once-per-package
// builds of the irie binary and the fake agent, short directories for unix
// sockets, isolated XDG environments, private tmux servers and temp git
// repos kept away from the user's own git config. Nothing a test starts
// outlives its run, even a run that is killed or times out (reap.go).
package testutil

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/brand"
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
	// buildEnv is the environment the test binary started in, taken before
	// any test sets HOME with t.Setenv. A build runs in it, so that it uses
	// the person's own module and build caches whichever test builds first:
	// under a test's temp HOME, go fetched every module afresh, slowly, and
	// left them read-only where the test's cleanup could not remove them.
	buildEnv = os.Environ()
)

// Main runs a package's tests, then removes the binaries BuildIrie and
// BuildFakeAgent made. Packages that call them, or NewEnv, use it as their
// TestMain. It first cleans what earlier runs that died left (reap.go),
// and it lets the binary run as its own reaper, so that a run killed now
// leaves nothing behind either.
func Main(m *testing.M) {
	if code, ok := asReaper(); ok {
		os.Exit(code)
	}
	mainRan = true
	// No test keeps a key in the user's Keychain (internal/keyring), even
	// one that builds a core without a test Irie around it.
	os.Setenv(brand.Env("KEYCHAIN"), "off")
	sweep(filepath.Join("/tmp", shortPrefix+"*"), filepath.Join(os.TempDir(), buildPrefix+"*"))
	code := m.Run()
	if buildDir != "" {
		os.RemoveAll(buildDir)
		release(buildDir)
	}
	os.Exit(code)
}

// The names of the dirs ShortDir and the builds make, before their random
// part.
const (
	shortPrefix = "irie-t-"
	buildPrefix = "irie-bin-"
)

// BinsEnv names the folder of binaries the test runner built before the
// tests (tools/test.sh, DESIGN §2.6): one build of each helper serves every
// test process of a run, which otherwise builds its own.
const BinsEnv = "IRIE_TEST_BINS"

// buildOnce builds the Go package pkg once per test binary, into a temp dir
// shared by every build, and returns the binary's path. When the test
// runner built pkg already, the binary is a link to that build.
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
		buildMu.Lock()
		if buildDir == "" {
			buildDir, b.err = os.MkdirTemp("", buildPrefix)
			if b.err == nil {
				own(buildDir)
			}
		}
		dir := buildDir
		buildMu.Unlock()
		if b.err != nil {
			return
		}
		b.bin = filepath.Join(dir, name)
		if prebuilt(pkg, b.bin) {
			return
		}
		goBin, err := exec.LookPath("go")
		if err != nil {
			b.err = err
			return
		}
		cmd := exec.Command(goBin, "build", "-o", b.bin, pkg)
		cmd.Env = buildEnv
		if out, err := cmd.CombinedOutput(); err != nil {
			b.err = fmt.Errorf("go build %s: %v\n%s", pkg, err, out)
		}
	})
	if b.err != nil {
		t.Fatal(b.err)
	}
	return b.bin
}

// prebuilt links bin to the test runner's build of pkg, which go build
// named after pkg's last element, and reports whether there was one. A
// link of the process's own keeps the name the tests look for, such as
// llama-server, and leaves the build itself to the other processes.
func prebuilt(pkg, bin string) bool {
	dir := os.Getenv(BinsEnv)
	if dir == "" {
		return false
	}
	src := filepath.Join(dir, path.Base(pkg))
	if fi, err := os.Stat(src); err != nil || !fi.Mode().IsRegular() {
		return false
	}
	return os.Link(src, bin) == nil || os.Symlink(src, bin) == nil
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

// FreePorts returns the first of n consecutive TCP ports on 127.0.0.1
// that nothing listens on now, for a test whose code takes a port and the
// ones after it, as the core's tunnels do (llama_port + 1, + 2, ...). With
// tests running side by side (tools/test.sh), the port after a free one is
// often another test's.
func FreePorts(t testing.TB, n int) int {
	t.Helper()
	for range 100 {
		first := FreePort(t)
		var held []net.Listener
		for i := 1; i < n; i++ {
			l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", first+i))
			if err != nil {
				break
			}
			held = append(held, l)
		}
		ok := len(held) == n-1
		for _, l := range held {
			l.Close()
		}
		if ok {
			return first
		}
	}
	t.Fatalf("no %d free ports in a row on 127.0.0.1", n)
	return 0
}

// ShortDir returns a new directory under /tmp. Its path stays short enough for
// a unix socket inside it, which macOS caps at about 104 bytes. At cleanup
// any tmux server in it ends, with everything it runs, and it is removed;
// when the run dies first, the reaper does that (reap.go).
func ShortDir(t testing.TB) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", shortPrefix)
	if err != nil {
		t.Fatal(err)
	}
	own(d)
	t.Cleanup(func() {
		cleanDir(d)
		release(d)
	})
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
// and Socket names a private tmux server. At cleanup every tmux server in
// the socket dir ends, with what it runs, even when the run is killed
// (reap.go).
type Env struct {
	ConfigHome, StateHome, DataHome, CacheHome string
	RuntimeDir                                 string // XDG_RUNTIME_DIR, under ShortDir
	TmuxTmpDir                                 string // TMUX_TMPDIR, under ShortDir
	Socket                                     string // tmux -L name: <slug>-test-<random>
}

// NewEnv sets up an isolated environment for t with t.Setenv. The
// package's TestMain must be Main, whose reaper ends the env's tmux servers
// when the run is killed before its cleanups.
func NewEnv(t *testing.T) *Env {
	t.Helper()
	if !mainRan {
		t.Fatal("testutil.NewEnv: make testutil.Main the package's TestMain, so that a killed run cannot leave its tmux servers behind")
	}
	root := t.TempDir()
	short := ShortDir(t)
	e := &Env{
		ConfigHome: filepath.Join(root, "config"),
		StateHome:  filepath.Join(root, "state"),
		DataHome:   filepath.Join(root, "data"),
		CacheHome:  filepath.Join(root, "cache"),
		RuntimeDir: filepath.Join(short, "run"),
		TmuxTmpDir: filepath.Join(short, "tmux"),
		// The core's sweep of leftovers knows a test's server by this name.
		Socket: brand.Slug + "-test-" + Random(4),
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
	// A test core never slows the machine's other processes for the user's
	// focus (Gabriel, DESIGN §4.11): it would take the user's own agents for
	// apps outside it.
	// The switches are the product's own variables (brand.Env), so that a
	// renamed copy's test cores are switched off too: theirs read PESTO_LID
	// in a copy called Pesto, not IRIE_LID.
	t.Setenv(brand.Env("GABRIEL"), "off")
	// Nor does it look over the machine, or restart itself, for Raphael
	// (DESIGN §4.11): the user's own core does that once.
	t.Setenv(brand.Env("RAPHAEL"), "off")
	// Nor does it hold Raguel's council by itself (DESIGN §4.11): that
	// starts a real Claude Code session.
	t.Setenv(brand.Env("RAGUEL"), "off")
	// Nor does it turn the Mac's sleep off and on for a closed lid (DESIGN
	// §4.12): that would be the user's own Mac, and its lid.
	t.Setenv(brand.Env("LID"), "off")
	// Nor does it stop the Iries of other test runs and agents (DESIGN §2.9):
	// the tests that run beside it are using theirs.
	t.Setenv("IRIE_LEFTOVERS", "off")
	// Nor does it keep mail passwords in the user's Keychain: they go to
	// files in its own state folder (docs/proposals/mail.md).
	t.Setenv(brand.Env("KEYCHAIN"), "off")
	// Nor does it read the machine's memory: a local model's spawn would
	// pass or fail with what the user's other programs take. It sees a
	// machine with room for the fake models (IRIE_FAKE_MEMORY, in GiB).
	t.Setenv("IRIE_FAKE_MEMORY", "64/48")
	for _, k := range []string{"TMUX", "TMUX_PANE", brand.Env("BIN")} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	// Registered after the Setenv calls, so it runs before they are undone.
	t.Cleanup(e.killServers)
	return e
}

// killServers ends every tmux server of the env, the team server (DESIGN
// §2.3) and any other a test started besides the private one, and waits
// for what they run to exit, so none of it writes into a temp dir, such as
// an agent host's exit.json, while the temp dirs are being removed. What
// has not exited after stopGrace is killed: a team server lives on without
// sessions, and an agent can hang on a hook it runs in a session of its
// own, which the end of its pane does not reach.
func (e *Env) killServers() { stopServers(tmuxSockets(e.TmuxTmpDir)) }

// WriteConfig writes config.toml with the test's tmux socket and extra TOML.
func (e *Env) WriteConfig(t testing.TB, extra string) {
	t.Helper()
	// The product's own folder, which a renamed copy's core reads.
	dir := filepath.Join(e.ConfigHome, brand.Slug)
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
