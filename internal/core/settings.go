package core

import (
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/ipc"
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
	// The edit is one transaction on the file (internal/filetx): a writer
	// elsewhere waits its turn, so neither loses the other's change, and no
	// reader sees half of the file. A config file linked from elsewhere, as
	// dotfiles often are, is written where it lives, so the link stays. The
	// file keeps its mode; a new one is the user's alone.
	var cfg config.Config
	err := filetx.Update(path, 0o600, func(data []byte) ([]byte, error) {
		out, err := config.Apply(data, edits)
		if err != nil {
			return nil, ipc.Errorf(ipc.CodeBadArgs, "config %s: %v", path, err)
		}
		if cfg, err = config.Parse(out, path); err != nil {
			return nil, ipc.Errorf(ipc.CodeBadArgs, "%v", err)
		}
		return out, nil
	})
	if err != nil {
		return err
	}
	c.useConfig(cfg)
	c.log.Info("settings", "keys", keys)
	// Every sidebar hears red_on_done and close_when_done at once.
	c.publishHeader()
	return nil
}
