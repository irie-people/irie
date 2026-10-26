// Package paths locates Irie's files. Irie uses the XDG base directories on
// every platform, macOS included, so paths match Linux (DESIGN §5.4).
package paths

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// MaxSocketPath is the longest unix socket path, in bytes, that Irie uses.
// macOS caps sun_path at 104 bytes including the terminating NUL.
const MaxSocketPath = 104

// Paths holds Irie's own directory inside each XDG base directory.
type Paths struct {
	Config  string // $XDG_CONFIG_HOME/irie
	State   string // $XDG_STATE_HOME/irie
	Data    string // $XDG_DATA_HOME/irie
	Cache   string // $XDG_CACHE_HOME/irie
	Runtime string // $XDG_RUNTIME_DIR/irie, or $TMPDIR/irie-<uid>
}

// FromEnv resolves the paths from the process environment.
func FromEnv() (Paths, error) { return New(os.Getenv, os.Getuid()) }

// New resolves the paths from getenv for the user uid.
func New(getenv func(string) string, uid int) (Paths, error) {
	home := getenv("HOME")
	if !filepath.IsAbs(home) {
		return Paths{}, errors.New("paths: HOME is not set to an absolute path")
	}
	// The XDG spec says relative values are invalid and must be ignored.
	base := func(key, fallback string) string {
		if v := getenv(key); filepath.IsAbs(v) {
			return filepath.Join(v, "irie")
		}
		return filepath.Join(home, fallback, "irie")
	}
	p := Paths{
		Config: base("XDG_CONFIG_HOME", ".config"),
		State:  base("XDG_STATE_HOME", ".local/state"),
		Data:   base("XDG_DATA_HOME", ".local/share"),
		Cache:  base("XDG_CACHE_HOME", ".cache"),
	}
	short := filepath.Join("/tmp", "irie-"+strconv.Itoa(uid))
	switch run, tmp := getenv("XDG_RUNTIME_DIR"), getenv("TMPDIR"); {
	case filepath.IsAbs(run):
		p.Runtime = filepath.Join(run, "irie")
	case filepath.IsAbs(tmp):
		p.Runtime = filepath.Join(tmp, "irie-"+strconv.Itoa(uid))
	default:
		p.Runtime = short
	}
	if len(p.Socket()) >= MaxSocketPath {
		p.Runtime = short
	}
	return p, nil
}

func (p Paths) ConfigFile() string            { return filepath.Join(p.Config, "config.toml") }
func (p Paths) TmuxConf() string              { return filepath.Join(p.State, "tmux.conf") }
func (p Paths) Sessions() string              { return filepath.Join(p.State, "sessions") }
func (p Paths) Session(id string) string      { return filepath.Join(p.Sessions(), id) }
func (p Paths) CoreLog() string               { return filepath.Join(p.State, "core.log") }
func (p Paths) UILog() string                 { return filepath.Join(p.State, "ui.log") }
func (p Paths) Scratch() string               { return filepath.Join(p.Data, "scratch") }
func (p Paths) Socket() string                { return filepath.Join(p.Runtime, "core.sock") }
func (p Paths) CoreLock() string              { return filepath.Join(p.Runtime, "core.lock") }
func (p Paths) UpLock() string                { return filepath.Join(p.Runtime, "up.lock") }
func (p Paths) SpecFile(id string) string     { return filepath.Join(p.Session(id), "spec.json") }
func (p Paths) TaskFile(id string) string     { return filepath.Join(p.Session(id), "task.md") }
func (p Paths) ExitFile(id string) string     { return filepath.Join(p.Session(id), "exit.json") }
func (p Paths) ScratchDir(slug string) string { return filepath.Join(p.Scratch(), slug) }

// EnsureState creates the state and data directories with mode 0700.
func (p Paths) EnsureState() error {
	for _, d := range []string{p.State, p.Sessions(), p.Data} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	return nil
}

// EnsureRuntime creates the runtime directory and checks that it is a real
// directory owned by this user with mode 0700. It may live in a shared /tmp,
// so a directory someone else made is refused.
func (p Paths) EnsureRuntime() error {
	if err := os.MkdirAll(p.Runtime, 0o700); err != nil {
		return err
	}
	fi, err := os.Lstat(p.Runtime)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("paths: %s is not a directory", p.Runtime)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Getuid() {
		return fmt.Errorf("paths: %s belongs to another user", p.Runtime)
	}
	if fi.Mode().Perm() != 0o700 {
		return os.Chmod(p.Runtime, 0o700)
	}
	return nil
}
