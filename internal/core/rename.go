package core

// Renaming a session (DESIGN §2.4, M4). A session's name lives in its agent
// pane's @irie_name, which travels with the pane through swaps and from which
// a restarted core rebuilds its registry (adopt), so a new name survives core
// restarts and installs, and close keeps it for reopen. A name given by
// rename is the user's: @irie_named pins it, so Claude Code's titles leave it
// be (title.go). The window keeps the name it was born with: nothing reads
// it, and while the session is shown its pane sits in irie:0, whose name is
// Irie's own.
//
// A rename that changed the name is a step of the history (§4.5). Undoing it
// brings the name before back as it was: the user's, or one Claude Code's
// titles change again.

import (
	"fmt"

	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/tmux"
)

// maxName caps a name, in bytes, as move caps the name it carries in.
const maxName = 200

// rename gives a session the name a asks for, with its whitespace and
// control characters made single spaces. When the name changed, step is
// what undoing that needs; giving a session the name it has only pins it,
// which is no step.
func (c *Core) rename(a ipc.RenameArgs) (s ipc.Session, step map[string]any, err error) {
	name := cleanText(a.Name, maxName)
	if name == "" {
		return ipc.Session{}, nil, ipc.Errorf(ipc.CodeBadArgs, "a session's name cannot be empty")
	}
	c.mu.Lock()
	s, step, err = c.nameAs(a.Session, name, true)
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	return s, step, err
}

// nameAs gives session id the name, numbered when another row has it, and
// pins it (named) or leaves it to Claude Code's titles, as undoing the
// rename of a name the user never chose does. It records ui.renamed with
// the name before and after, and whether the name before was the user's.
// When the name changed, step is the name before, whether it was the
// user's, and the name after. The caller holds c.mu.
func (c *Core) nameAs(id, name string, named bool) (ipc.Session, map[string]any, error) {
	x := c.sessions[id]
	if x == nil {
		return ipc.Session{}, nil, ipc.Errorf(ipc.CodeNotFound, "no session %q", id)
	}
	before, wasNamed := x.name, x.named
	if name = unique(name, c.names(x)); name == before && named == wasNamed {
		return x.wire(), nil, nil
	}
	if _, err := c.tmux.RunList([][]string{
		{"set-option", "-p", "-t", x.pane, tmux.OptName, name},
		{"set-option", "-p", "-t", x.pane, tmux.OptNamed, flag(named)},
	}); err != nil {
		return ipc.Session{}, nil, err
	}
	x.name, x.named = name, named
	// Its own name renames nothing, but makes the name the user's, or not.
	if name == before {
		c.log.Info("pinned", "session", id, "name", name, "named", named)
		return x.wire(), nil, nil
	}
	data := map[string]any{"before": before, "after": name, "before_named": wasNamed}
	if !named {
		data["named"] = false
	}
	c.record(x, event.Event{Source: event.SourceUser, Kind: event.UIRenamed, Data: data})
	c.log.Info("renamed", "session", id, "before", before, "after", name)
	return x.wire(), map[string]any{"from": before, "from_named": wasNamed, "to": name}, nil
}

// Action kind of rename (undo.go).
const actRename = "rename"

func init() {
	registerAction(actRename, actionKind{verb: "rename", label: renameLabel,
		undo: effect{check: checkRename(false), do: doRename(false)},
		redo: effect{check: checkRename(true), do: doRename(true)}})
}

// renameLabel names both names, which undo and redo trade.
func renameLabel(a action) string {
	return fmt.Sprintf("rename %q to %q", a.str("from"), a.str("to"))
}

// renameTo is the name that undoing a (forward false) or redoing it gives,
// and whether that name is the user's.
func renameTo(a action, forward bool) (string, bool) {
	if forward {
		return a.str("to"), true
	}
	return a.str("from"), a.flag("from_named")
}

// checkRename is what makes renaming a's session back (forward false) or
// again uncertain: that it is closed or gone, that it has the name already,
// that it was renamed since, or that another session has the name now.
func checkRename(forward bool) func(c *Core, a action) []problem {
	return func(c *Core, a action) []problem {
		x := c.sessions[a.Session]
		if x == nil {
			return notOpen(c, a, "rename")
		}
		want, _ := renameTo(a, forward)
		if x.name == want {
			return []problem{alreadyDone(fmt.Sprintf("%q has that name already.", want))}
		}
		var ps []problem
		if x.name != a.Name {
			ps = append(ps, problem{code: "renamed", question: fmt.Sprintf("%q is called %q now.", a.Name, x.name),
				options: []ipc.ChoiceOption{{Key: ipc.ChoiceApply, Label: fmt.Sprintf("Rename it %q anyway", want),
					Detail: fmt.Sprintf("Its name %q goes, and %q comes back.", x.name, want)}},
				recommend: ipc.ChoiceApply})
		}
		if n := unique(want, c.names(x)); n != want {
			ps = append(ps, problem{code: "name-taken", question: fmt.Sprintf("Another session is called %q now.", want),
				options: []ipc.ChoiceOption{{Key: ipc.ChoiceApply, Label: fmt.Sprintf("Rename it %q", n),
					Detail: "Numbers the name, since two rows never share one."}},
				recommend: ipc.ChoiceApply})
		}
		return ps
	}
}

// doRename renames a's session back (forward false) or again the way the
// op does, with no step of its own.
func doRename(forward bool) func(c *Core, a action) (action, error) {
	return func(c *Core, a action) (action, error) {
		name, named := renameTo(a, forward)
		c.mu.Lock()
		s, _, err := c.nameAs(a.Session, name, named)
		frames := c.changes()
		c.mu.Unlock()
		c.publish(frames)
		if err != nil {
			return a, err
		}
		a.Name = s.Name
		return a, nil
	}
}
