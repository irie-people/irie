//go:build !nowriting

package core

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/harness"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/journal"
	"github.com/irie-people/irie/internal/tmux"
	"github.com/irie-people/irie/internal/variorum"
)

// testDoor is Variorum's door as the op's tests need it, from the door's
// protocol: it answers initialize, then one call: list_notes, read_note of
// the note n1 (any other isn't shared), or show_in_app.
const testDoor = `#!/bin/sh
while IFS= read -r line; do
	case $line in
	*'"method":"initialize"'*)
		echo '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","capabilities":{}}}' ;;
	*'"list_notes"'*)
		echo '{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"{\"notes\":[{\"id\":\"n1\",\"title\":\"Chapter 3\",\"edited\":\"2026-11-14T10:00:00Z\"}]}"}]}}' ;;
	*'"read_note"'*)
		case $line in
		*'"note_id":"n1"'*)
			echo '{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"{\"note_id\":\"n1\",\"title\":\"Chapter 3\",\"blocks\":[{\"block_id\":\"b1\",\"shown_take_id\":\"t1\",\"takes\":[{\"take_id\":\"t1\",\"chosen\":true,\"text\":\"It was late.\"}],\"comments\":[]}]}"}]}}' ;;
		*)
			echo '{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"That note is not shared with this client."}],"isError":true}}' ;;
		esac ;;
	*'"show_in_app"'*)
		echo '{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"{\"shown\":true}"}]}}' ;;
	esac
done
`

// testVariorum makes Variorum.app, its door testDoor, in an Applications
// folder of the test's own, and returns the door.
func testVariorum(t *testing.T) string {
	t.Helper()
	folder := t.TempDir()
	old := variorum.Folders
	variorum.Folders = []string{folder}
	t.Cleanup(func() { variorum.Folders = old })
	contents := filepath.Join(folder, "Variorum.app", "Contents")
	if err := os.MkdirAll(filepath.Join(contents, "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict><key>CFBundleIdentifier</key><string>io.lduarte.Variorum</string></dict></plist>
`
	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	door := filepath.Join(contents, "MacOS", variorum.Program)
	if err := os.WriteFile(door, []byte(testDoor), 0o755); err != nil {
		t.Fatal(err)
	}
	return door
}

const (
	testSessionsKey = "vmk2_claude-code_" + "k7k7k7k7k7k7k7k7k7k7k7k7k7k7k7k7k7k7k7k7k7x"
	testTabKey      = "vmk1_" + "t9t9t9t9t9t9t9t9t9t9t9t9t9t9t9t9t9t9t9t9t9z"
)

// TestTheVariorumOp: off with its plugin; setup until both setups are
// kept, then ready; the notes shared with Irie, the note a session's agent
// named last in a call to the door, and Show in Variorum, each through the
// tab's setup and in the door's own words when it refuses; nothing but
// status for a process inside an agent; and forget.
func TestTheVariorumOp(t *testing.T) {
	door := testVariorum(t)
	cfg := config.Default()
	cfg.Plugins.Writing = false
	c := bareCore(t, cfg)
	op := func(a ipc.VariorumArgs) ipc.VariorumResult {
		t.Helper()
		r, err := c.variorumOp(0, a)
		if err != nil {
			t.Fatalf("%+v: %v", a, err)
		}
		for _, key := range []string{testSessionsKey, testTabKey} {
			if b, _ := json.Marshal(r); strings.Contains(string(b), key) {
				t.Fatalf("%+v: the answer holds a key: %s", a, b)
			}
		}
		return r
	}
	if r := op(ipc.VariorumArgs{}); r.State != ipc.VariorumOff || !strings.Contains(r.Text, brand.Slug+" plugins on writing") || !r.Installed {
		t.Fatalf("plugin off: %+v", r)
	}

	// The op is the tab's: off with the tab's switch.
	cfg.Plugins.Writing, cfg.Variorum.Tab = true, false
	c.useConfig(cfg)
	if r := op(ipc.VariorumArgs{Action: ipc.VariorumNotes}); r.State != ipc.VariorumOff || !strings.Contains(r.Text, brand.Slug+" plugins on writing tab") || r.Notes != nil {
		t.Fatalf("tab off: %+v", r)
	}

	cfg.Variorum.Tab = true
	c.useConfig(cfg)
	if r := op(ipc.VariorumArgs{Action: ipc.VariorumStatus}); r.State != ipc.VariorumSetup || r.Sessions || r.Tab || !r.Installed {
		t.Errorf("nothing kept: %+v", r)
	}
	if r := op(ipc.VariorumArgs{Action: ipc.VariorumNotes}); r.Error == "" || !strings.Contains(r.Error, "Any MCP client") {
		t.Errorf("notes without the tab's setup: %+v", r)
	}
	setup := fmt.Sprintf("claude mcp remove -s user variorum >/dev/null 2>&1; claude mcp add -s user variorum -e VARIORUM_MCP_KEY=%s -- %q", testSessionsKey, door)
	if r := op(ipc.VariorumArgs{Action: ipc.VariorumConnect, Setup: setup}); r.Error != "" || !r.Sessions || r.Tab || !strings.HasPrefix(r.Text, "Kept Claude Code's setup, for writing sessions.") {
		t.Errorf("connect Claude Code's: %+v", r)
	}
	stanza := fmt.Sprintf(`{"mcpServers": {"variorum": {"command": %q, "args": [], "env": {"VARIORUM_MCP_KEY": %q}}}}`, door, testTabKey)
	if r := op(ipc.VariorumArgs{Action: ipc.VariorumConnect, Setup: stanza}); r.Error != "" || r.State != ipc.VariorumReady || !r.Sessions || !r.Tab || !strings.HasPrefix(r.Text, "Kept Any MCP client's setup, for the Variorum tab.") {
		t.Errorf("connect Any MCP client's: %+v", r)
	}
	if r := op(ipc.VariorumArgs{Action: ipc.VariorumConnect, Setup: "not a setup " + testTabKey}); r.Error == "" || r.State != ipc.VariorumReady {
		t.Errorf("connect what isn't a setup: %+v", r)
	}
	if b, err := os.ReadFile(variorum.File(c.paths.Data)); err != nil || !strings.Contains(string(b), testTabKey) {
		t.Fatalf("door.json: %s, %v", b, err)
	}

	if r := op(ipc.VariorumArgs{Action: ipc.VariorumNotes}); r.Error != "" || len(r.Notes) != 1 || r.Notes[0] != (ipc.VariorumNoteRef{ID: "n1", Title: "Chapter 3", Edited: "2026-11-14T10:00:00Z"}) {
		t.Errorf("notes: %+v", r)
	}
	if r := op(ipc.VariorumArgs{Action: ipc.VariorumRead}); r.Error != "No note yet: pick one from the notes shared with "+brand.Name+"." || r.Note != nil {
		t.Errorf("read with no note: %+v", r)
	}
	if r := op(ipc.VariorumArgs{Action: ipc.VariorumRead, Session: "s1"}); !strings.HasPrefix(r.Error, "No note yet") {
		t.Errorf("read for a session that named none: %+v", r)
	}
	if r := op(ipc.VariorumArgs{Action: ipc.VariorumRead, Note: "n9"}); r.Error != "That note is not shared with this client." {
		t.Errorf("read a note not shared: %+v", r)
	}

	// The note a session's agent names in a call to the door is the
	// session's. Its hooks go to the journal.
	if err := c.paths.EnsureState(); err != nil {
		t.Fatal(err)
	}
	var err error
	if c.journal, err = journal.Open(c.paths.Journal(), journal.Options{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.journal.Close() })
	x := &session{id: "s1", name: "Chapter 3", agent: "main", out: c.outcome("s1")}
	c.sessions[x.id] = x
	for _, tc := range []struct{ tool, input string }{
		{"mcp__variorum__read_note", `{"note_id": "n1", "with_ids": true}`},
		{"mcp__other__read_note", `{"note_id": "n7"}`},
		{"mcp__variorum__list_notes", `{"limit": 5}`},
		{"mcp__variorum__add_comment", `{"note_id": 7}`},
	} {
		payload, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "session_id": "v1", "tool_name": tc.tool, "tool_input": json.RawMessage(tc.input), "tool_use_id": "u1"})
		if _, err := c.hook(ipc.HookArgs{Harness: "claude-code", Session: x.id, Payload: payload}); err != nil {
			t.Fatalf("hook %s: %v", tc.tool, err)
		}
	}
	r := op(ipc.VariorumArgs{Action: ipc.VariorumRead, Session: x.id})
	if r.Error != "" || r.Note == nil || r.Note.ID != "n1" || r.Note.Title != "Chapter 3" || len(r.Note.Blocks) != 1 || r.Note.Blocks[0].Takes[0].Text != "It was late." {
		t.Errorf("read the session's note: %+v", r)
	}
	c.mu.Lock()
	c.noteVariorum(x, "mcp__variorum-dev__show_in_app", json.RawMessage(`{"target": "note", "note_id": "n2"}`))
	note := c.variorum.notes[x.id]
	c.mu.Unlock()
	if note != "n2" {
		t.Errorf("the debug build's door: the note is %q", note)
	}

	if r := op(ipc.VariorumArgs{Action: ipc.VariorumShow, Note: "n1", Block: "b1", Target: "takes"}); r.Error != "" || r.Shown != "shown" {
		t.Errorf("show: %+v", r)
	}
	if r := op(ipc.VariorumArgs{Action: ipc.VariorumShow, Target: "lens", Note: "n1"}); r.Error == "" {
		t.Errorf("show a lens: %+v", r)
	}
	if _, err := c.variorumOp(0, ipc.VariorumArgs{Action: "write"}); err == nil {
		t.Error("an action that isn't one went through")
	}

	// This test's own parent stands for an agent's host.
	c.snap = tmux.Snapshot{Panes: []tmux.Pane{{ID: "%1", Role: tmux.RoleAgent, PID: os.Getppid()}}}
	for _, action := range []string{ipc.VariorumNotes, ipc.VariorumRead, ipc.VariorumShow, ipc.VariorumConnect, ipc.VariorumForget} {
		r, err := c.variorumOp(os.Getpid(), ipc.VariorumArgs{Action: action, Note: "n1", Setup: stanza})
		if err != nil || !strings.HasPrefix(r.Error, "An agent can't") || r.Notes != nil || r.Note != nil || r.Shown != "" {
			t.Errorf("%s from an agent: %+v, %v", action, r, err)
		}
	}
	if r, err := c.variorumOp(os.Getpid(), ipc.VariorumArgs{}); err != nil || r.Error != "" || r.State != ipc.VariorumReady {
		t.Errorf("status from an agent: %+v, %v", r, err)
	}
	c.snap = tmux.Snapshot{}

	if r := op(ipc.VariorumArgs{Action: ipc.VariorumForget}); r.Error != "" || r.State != ipc.VariorumSetup || r.Sessions || r.Tab || !strings.HasPrefix(r.Text, "Forgot both") {
		t.Errorf("forget: %+v", r)
	}
	if _, err := os.Stat(variorum.File(c.paths.Data)); !os.IsNotExist(err) {
		t.Errorf("door.json after forget: %v", err)
	}
}

// TestOnlyAWritingSessionGetsVariorumsDoor: the writing guide's folder
// gets `irie variorum door`, with no key, while the plugin is on and
// Claude Code's setup is kept; any other folder never does.
func TestOnlyAWritingSessionGetsVariorumsDoor(t *testing.T) {
	cfg := config.Default()
	c := bareCore(t, cfg)
	c.bin = "/home/alice/.local/share/irie/bin/irie"
	writing := c.guideDir(guideWrite)
	if mcp := c.variorumMCP(writing); mcp != nil {
		t.Errorf("no setup kept: %+v", mcp)
	}
	if err := variorum.Save(c.paths.Data, variorum.Sessions, variorum.Setup{Command: "/Applications/Variorum.app/Contents/MacOS/variorum-mcp", Key: testSessionsKey}); err != nil {
		t.Fatal(err)
	}
	mcp := c.variorumMCP(writing)
	if len(mcp) != 1 || mcp["variorum"].Command != c.bin || strings.Join(mcp["variorum"].Args, " ") != "variorum door" {
		t.Fatalf("a writing session: %+v", mcp)
	}
	if b, _ := json.Marshal(mcp); strings.Contains(string(b), testSessionsKey) {
		t.Errorf("the session's server holds the key: %s", b)
	}
	for _, dir := range []string{c.guideDir(guideSwitch), filepath.Join(c.paths.Data, "scratch", "task"), "/home/alice/book"} {
		if mcp := c.variorumMCP(dir); mcp != nil {
			t.Errorf("%s: %+v", dir, mcp)
		}
	}
	// The tab's switch is the tab's: the session keeps its door.
	cfg.Variorum.Tab = false
	c.useConfig(cfg)
	if mcp := c.variorumMCP(writing); len(mcp) != 1 {
		t.Errorf("tab off: %+v", mcp)
	}
	cfg.Plugins.Writing = false
	c.useConfig(cfg)
	if mcp := c.variorumMCP(writing); mcp != nil {
		t.Errorf("plugin off: %+v", mcp)
	}
}

// TestAWriteSessionGetsVariorumsDoor: a session :write starts gets
// Variorum's door as an MCP server for it alone, `irie variorum door`,
// with no key in its files; an ordinary session on the same profile gets
// none.
func TestAWriteSessionGetsVariorumsDoor(t *testing.T) {
	c := prepare(t, agentConfig{})
	c.WriteConfig(t, agentConfig{extra: fmt.Sprintf("[profiles.claude]\ncommand = %q\nenv = { IRIE_FAKE_PLUGINS = \"1\", IRIE_FAKE_MCP = \"1\" }\n", c.fake)}.toml(c.fake))
	// The plugin folders are read-only; the test's folders go when it ends.
	t.Cleanup(func() {
		filepath.WalkDir(c.paths.Skills(), func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				os.Chmod(p, 0o700)
			}
			return nil
		})
	})
	if err := variorum.Save(c.paths.Data, variorum.Sessions, variorum.Setup{Command: "/Applications/Variorum.app/Contents/MacOS/variorum-mcp", Key: testSessionsKey}); err != nil {
		t.Fatal(err)
	}
	c.up()

	servers := func(id string) map[string]harness.MCPServer {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(c.paths.Session(id), harness.MCPFile))
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), testSessionsKey) {
			t.Errorf("the session's mcp.json holds the key:\n%s", b)
		}
		var f struct {
			Servers map[string]harness.MCPServer `json:"mcpServers"`
		}
		if err := json.Unmarshal(b, &f); err != nil {
			t.Fatal(err)
		}
		return f.Servers
	}

	var r ipc.InputResult
	if err := c.call(ipc.OpInput, ipc.InputArgs{Text: ":write notes on Chapter 3"}, &r); err != nil {
		t.Fatal(err)
	}
	if r.Session == nil {
		t.Fatalf("input = %+v", r)
	}
	s := servers(r.Session.ID)
	if door := s["variorum"]; door.Command != c.bin || strings.Join(door.Args, " ") != "variorum door" {
		t.Errorf("the writing session's servers: %+v", s)
	}
	c.waitState(r.Session.ID, ipc.StateWorking)

	var sr ipc.SpawnResult
	if err := c.call(ipc.OpSpawn, ipc.SpawnArgs{Task: "tidy the build", Profile: "claude"}, &sr); err != nil {
		t.Fatal(err)
	}
	if s := servers(sr.Session.ID); s != nil {
		t.Errorf("an ordinary session's servers: %+v", s)
	}
}
