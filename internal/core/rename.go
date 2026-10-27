package core

// Renaming a session (DESIGN §2.4, M4). A session's name lives in its agent
// pane's @irie_name, which travels with the pane through swaps and from which
// a restarted core rebuilds its registry (adopt), so a new name survives core
// restarts and installs, and close keeps it for reopen. A name given by
// rename is the user's: @irie_named pins it, so Claude Code's titles leave it
// be (title.go). The window keeps the name it was born with: nothing reads
// it, and while the session is shown its pane sits in irie:0, whose name is
// Irie's own.

import (
	"github.com/irie-hq/irie/internal/event"
	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/tmux"
)

// maxName caps a name, in bytes, as move caps the name it carries in.
const maxName = 200

// rename gives a session the name a asks for, with its whitespace and
// control characters made single spaces.
func (c *Core) rename(a ipc.RenameArgs) (ipc.Session, error) {
	name := cleanText(a.Name, maxName)
	if name == "" {
		return ipc.Session{}, ipc.Errorf(ipc.CodeBadArgs, "a session's name cannot be empty")
	}
	c.mu.Lock()
	s, err := c.setName(a.Session, name)
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	return s, err
}

// setName gives session id the name, numbered when another row has it, and
// pins it. It records ui.renamed with the name before and after, and
// whether the name before was the user's, for undo (§4.5). The caller holds
// c.mu.
func (c *Core) setName(id, name string) (ipc.Session, error) {
	x := c.sessions[id]
	if x == nil {
		return ipc.Session{}, ipc.Errorf(ipc.CodeNotFound, "no session %q", id)
	}
	before, named := x.name, x.named
	if name = unique(name, c.names(x)); name == before && named {
		return x.wire(), nil
	}
	if _, err := c.tmux.RunList([][]string{
		{"set-option", "-p", "-t", x.pane, tmux.OptName, name},
		{"set-option", "-p", "-t", x.pane, tmux.OptNamed, flag(true)},
	}); err != nil {
		return ipc.Session{}, err
	}
	x.name, x.named = name, true
	// Its own name renames nothing, but makes the name the user's.
	if name == before {
		c.log.Info("pinned", "session", id, "name", name)
		return x.wire(), nil
	}
	c.record(x, event.Event{Source: event.SourceUser, Kind: event.UIRenamed, Data: map[string]any{"before": before, "after": name, "before_named": named}})
	c.log.Info("renamed", "session", id, "before", before, "after", name)
	return x.wire(), nil
}
