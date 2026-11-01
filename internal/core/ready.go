package core

import (
	"os"

	"github.com/irie-people/irie/internal/ready"
)

// ready checks that the computer has what Irie needs, as its agents see
// it: the core runs in the server, with the environment they get (DESIGN
// §2.7). It runs commands, so it holds no lock.
func (c *Core) ready() ready.Report {
	cfg := c.conf.Load()
	return ready.Check(ready.Options{
		Env:    os.Environ(),
		Tmux:   c.tmux.Bin,
		Claude: cfg.Profiles["claude"].Command,
		SignIn: true,
	})
}
