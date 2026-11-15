//go:build !nowriting

package core

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/harness"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/plugins"
	"github.com/irie-people/irie/internal/variorum"
)

// variorumState is what the core keeps of Variorum's door
// (docs/proposals/writers.md): the note each session's agent named last in
// a call to the door, which the Variorum tab shows. It keeps it in memory
// alone; the setups and their keys are internal/variorum's.
type variorumState struct {
	notes map[string]string // by session id; the caller holds c.mu
	calls sync.Mutex        // one call to the door at a time, outside c.mu
}

// variorumServer is the name a writing session knows Variorum's door by,
// so that its tools are Variorum's own: mcp__variorum__read_note and the
// rest.
const variorumServer = "variorum"

// maxNoteID is the longest note id the core keeps from a session's call.
const maxNoteID = 200

// variorumMCP is the MCP server, by name, that a session started in dir
// gets for Variorum: `irie variorum door` for a writing session, while the
// writing plugin is on and Irie keeps Claude Code's setup; else none. The
// server names no key: the door reads it from Irie's own file as it
// starts.
func (c *Core) variorumMCP(dir string) map[string]harness.MCPServer {
	if !c.writing(dir) || plugins.Why(*c.cfg(), plugins.VariorumTab) != "" {
		return nil
	}
	if st, err := variorum.Load(c.paths.Data); err != nil || st.Sessions == nil {
		return nil
	}
	return map[string]harness.MCPServer{variorumServer: {Command: c.bin, Args: []string{"variorum", "door"}}}
}

// noteVariorum remembers the note a session's agent names in a call to
// Variorum's door, as Claude Code names its tools: the server variorum,
// or variorum-dev for Variorum's debug build. The caller holds c.mu.
func (c *Core) noteVariorum(x *session, tool string, input json.RawMessage) {
	if !strings.HasPrefix(tool, "mcp__variorum__") && !strings.HasPrefix(tool, "mcp__variorum-dev__") {
		return
	}
	var in map[string]json.RawMessage
	var note string
	if json.Unmarshal(input, &in) != nil || json.Unmarshal(in["note_id"], &note) != nil || note == "" || len(note) > maxNoteID {
		return
	}
	if c.variorum.notes == nil {
		c.variorum.notes = map[string]string{}
	}
	c.variorum.notes[x.id] = note
}

// variorumOp answers the variorum op (docs/proposals/writers.md): where
// Irie's part of Variorum stands, Variorum's setups kept or forgotten, and
// the notes shared with Irie read or shown through the door with the
// tab's setup. A call to the door runs outside c.mu, one at a time, and
// only when asked: Variorum keeps a line of its log for each. Only status
// is open to a process inside an agent: the tab's door reads what the
// writer shares with Irie, for the writer.
func (c *Core) variorumOp(peer int, a ipc.VariorumArgs) (ipc.VariorumResult, error) {
	data := c.paths.Data
	if why := plugins.Why(*c.cfg(), plugins.VariorumTab); why != "" {
		r := variorum.Status(data)
		r.State, r.Text = ipc.VariorumOff, why
		return r, nil
	}
	action := a.Action
	if action == "" {
		action = ipc.VariorumStatus
	}
	switch action {
	case ipc.VariorumStatus:
		return variorum.Status(data), nil
	case ipc.VariorumConnect, ipc.VariorumForget, ipc.VariorumNotes, ipc.VariorumRead, ipc.VariorumShow:
	default:
		return ipc.VariorumResult{}, ipc.Errorf(ipc.CodeBadArgs, "no Variorum action %q: the actions are status, connect, forget, notes, read and show", a.Action)
	}
	if c.fromAgent(peer) {
		r := variorum.Status(data)
		r.Error = "An agent can't use the Variorum tab's door: it reads what you share with " + brand.Slug + ", for you. A writing session has Variorum's door of its own."
		return r, nil
	}
	switch action {
	case ipc.VariorumConnect:
		r, kind := variorum.Connect(data, a.Setup)
		if kind != "" {
			c.log.Info("variorum", "kept", kind)
		}
		return r, nil
	case ipc.VariorumForget:
		r := variorum.ForgetAll(data)
		if r.Error == "" {
			c.log.Info("variorum", "forgot", "both")
		}
		return r, nil
	}

	r := variorum.Status(data)
	st, err := variorum.Load(data)
	switch {
	case err != nil:
		r.Error = err.Error()
		return r, nil
	case st.Tab == nil:
		r.Error = brand.Name + " doesn't have Variorum's Any MCP client setup yet, which the Variorum tab reads with: in Variorum's Settings ▸ MCP ▸ Connect a client, choose Any MCP client, copy its setup and press Connect."
		return r, nil
	}
	note := a.Note
	if action == ipc.VariorumRead && note == "" && a.Session != "" {
		c.mu.Lock()
		note = c.variorum.notes[a.Session]
		c.mu.Unlock()
	}
	if action == ipc.VariorumRead && note == "" {
		r.Error = "No note yet: pick one from the notes shared with " + brand.Name + "."
		return r, nil
	}

	c.variorum.calls.Lock()
	defer c.variorum.calls.Unlock()
	ctx, start := context.Background(), time.Now()
	switch action {
	case ipc.VariorumNotes:
		r.Notes, err = st.Tab.Notes(ctx)
	case ipc.VariorumRead:
		r.Note, err = st.Tab.Read(ctx, note)
	case ipc.VariorumShow:
		r.Shown, err = st.Tab.Show(ctx, a.Target, note, a.Block)
	}
	// One line a call, without its arguments.
	if err != nil {
		r.Error = err.Error()
		c.log.Info("variorum", "call", action, "took", time.Since(start).Round(time.Millisecond), "err", r.Error)
	} else {
		c.log.Info("variorum", "call", action, "took", time.Since(start).Round(time.Millisecond))
	}
	return r, nil
}
