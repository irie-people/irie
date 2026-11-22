package paths

import (
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/brand"
)

func lookup(env map[string]string) func(string) string {
	return func(k string) string { return env[k] }
}

func TestXDGOverridesAreHonoured(t *testing.T) {
	p, err := New(lookup(map[string]string{
		"HOME":            "/home/alice",
		"XDG_CONFIG_HOME": "/home/alice/xdg/config",
		"XDG_STATE_HOME":  "/home/alice/xdg/state",
		"XDG_DATA_HOME":   "/home/alice/xdg/data",
		"XDG_CACHE_HOME":  "/home/alice/xdg/cache",
		"XDG_RUNTIME_DIR": "/run/user/1000",
		"TMPDIR":          "/tmp/elsewhere",
	}), 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, got, want string }{
		{"config", p.Config, "/home/alice/xdg/config/" + brand.Slug},
		{"state", p.State, "/home/alice/xdg/state/" + brand.Slug},
		{"data", p.Data, "/home/alice/xdg/data/" + brand.Slug},
		{"cache", p.Cache, "/home/alice/xdg/cache/" + brand.Slug},
		{"runtime", p.Runtime, "/run/user/1000/" + brand.Slug},
		{"config file", p.ConfigFile(), "/home/alice/xdg/config/" + brand.Slug + "/config.toml"},
		{"tmux.conf", p.TmuxConf(), "/home/alice/xdg/state/" + brand.Slug + "/tmux.conf"},
		{"session", p.Session("s0k3j9x2m4a7q"), "/home/alice/xdg/state/" + brand.Slug + "/sessions/s0k3j9x2m4a7q"},
		{"scratch", p.Scratch(), "/home/alice/xdg/data/" + brand.Slug + "/scratch"},
		{"worktrees", p.Worktrees(), "/home/alice/xdg/data/" + brand.Slug + "/worktrees"},
		{"journal", p.Journal(), "/home/alice/xdg/state/" + brand.Slug + "/journal"},
		{"socket", p.Socket(), "/run/user/1000/" + brand.Slug + "/core.sock"},
		{"lock", p.CoreLock(), "/run/user/1000/" + brand.Slug + "/core.lock"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestDefaultsFollowXDGOnEveryPlatform(t *testing.T) {
	p, err := New(lookup(map[string]string{
		"HOME":   "/home/alice",
		"TMPDIR": "/var/folders/ab/cd/T/",
	}), 501)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, got, want string }{
		{"config", p.Config, "/home/alice/.config/" + brand.Slug},
		{"state", p.State, "/home/alice/.local/state/" + brand.Slug},
		{"data", p.Data, "/home/alice/.local/share/" + brand.Slug},
		{"cache", p.Cache, "/home/alice/.cache/" + brand.Slug},
		{"runtime", p.Runtime, "/var/folders/ab/cd/T/" + brand.Slug + "-501"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestRuntimeFallsBackToTmp(t *testing.T) {
	p, err := New(lookup(map[string]string{"HOME": "/home/alice"}), 501)
	if err != nil {
		t.Fatal(err)
	}
	if p.Runtime != "/tmp/"+brand.Slug+"-501" {
		t.Errorf("runtime = %q, want /tmp/"+brand.Slug+"-501", p.Runtime)
	}
}

func TestRelativeXDGValuesAreIgnored(t *testing.T) {
	p, err := New(lookup(map[string]string{
		"HOME":            "/home/alice",
		"XDG_CONFIG_HOME": "relative/config",
		"XDG_RUNTIME_DIR": "relative/run",
		"TMPDIR":          "/tmp",
	}), 501)
	if err != nil {
		t.Fatal(err)
	}
	if p.Config != "/home/alice/.config/"+brand.Slug {
		t.Errorf("config = %q", p.Config)
	}
	if p.Runtime != "/tmp/"+brand.Slug+"-501" {
		t.Errorf("runtime = %q", p.Runtime)
	}
}

func TestSocketPathStaysShort(t *testing.T) {
	long := "/var/folders/" + strings.Repeat("x", 90-len("/var/folders/"))
	if len(long) != 90 {
		t.Fatalf("test setup: TMPDIR is %d bytes", len(long))
	}
	for _, env := range []map[string]string{
		{"HOME": "/home/alice", "TMPDIR": long},
		{"HOME": "/home/alice", "TMPDIR": long + "/"},
		{"HOME": "/home/alice", "XDG_RUNTIME_DIR": long},
	} {
		p, err := New(lookup(env), 4294967294)
		if err != nil {
			t.Fatal(err)
		}
		if n := len(p.Socket()); n >= MaxSocketPath {
			t.Errorf("env %v: socket path %q is %d bytes, want under %d", env, p.Socket(), n, MaxSocketPath)
		}
	}
}

func TestHomeIsRequired(t *testing.T) {
	if _, err := New(lookup(map[string]string{}), 501); err == nil {
		t.Fatal("no error without HOME")
	}
}

func TestEnsureRuntimeDir(t *testing.T) {
	dir := t.TempDir() + "/run/irie"
	p := Paths{Runtime: dir}
	if err := p.EnsureRuntime(); err != nil {
		t.Fatal(err)
	}
	// A second call accepts the directory it made.
	if err := p.EnsureRuntime(); err != nil {
		t.Fatal(err)
	}
}
