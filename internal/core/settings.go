package core

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/irie-hq/irie/internal/config"
	"github.com/irie-hq/irie/internal/ipc"
)

// settings answers the config op (DESIGN §5.5): it makes the changes asked
// for to the config file, in place, runs on the result from then on, and
// returns the config in effect.
func (c *Core) settings(a ipc.ConfigArgs) (ipc.ConfigResult, error) {
	path := c.paths.ConfigFile()
	if len(a.Set) > 0 {
		if err := c.configure(path, a.Set); err != nil {
			return ipc.ConfigResult{}, err
		}
	}
	r := ipc.ConfigResult{Path: path, Config: config.Tree(*c.cfg())}
	if _, err := config.Load(path); err != nil {
		r.Problem = err.Error()
	}
	return r, nil
}

// configure edits the config file and swaps in the config it then holds,
// hand edits made since the core read it included. A change the file
// cannot take, or a config that would not load, leaves both as they were.
// What the core reads only as it starts, such as tmux.socket, still waits
// for its next start, and Irie's own Ollama server keeps its settings until
// it starts again.
func (c *Core) configure(path string, sets []ipc.ConfigSet) error {
	edits := make([]config.Edit, len(sets))
	keys := make([]string, len(sets))
	for i, s := range sets {
		v, err := config.ParseValue(s.Key, s.Value)
		if err != nil {
			return ipc.Errorf(ipc.CodeBadArgs, "%v", err)
		}
		edits[i], keys[i] = config.Edit{Key: s.Key, Value: v}, s.Key
	}
	c.confMu.Lock()
	defer c.confMu.Unlock()
	// A config file linked from elsewhere, as dotfiles often are, is
	// written where it lives, so the link stays.
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	out, err := config.Apply(data, edits)
	if err != nil {
		return ipc.Errorf(ipc.CodeBadArgs, "config %s: %v", path, err)
	}
	cfg, err := config.Parse(out, path)
	if err != nil {
		return ipc.Errorf(ipc.CodeBadArgs, "%v", err)
	}
	if err := writeConfig(path, out); err != nil {
		return err
	}
	c.useConfig(cfg)
	c.log.Info("settings", "keys", keys)
	// Every sidebar hears red_on_done and close_when_done at once.
	c.publishHeader()
	return nil
}

// writeConfig replaces the config file whole, so no reader sees half of
// it. The file keeps its mode; a new one is the user's alone.
func writeConfig(path string, data []byte) error {
	mode := fs.FileMode(0o600)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".config-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	if err == nil {
		err = f.Chmod(mode)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
