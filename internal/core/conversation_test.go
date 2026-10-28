package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
)

// transcriptLine is one line of a Claude Code transcript, shaped as Claude
// Code writes them: a type, a uuid, a timestamp and a message whose content
// is a string or blocks.
func transcriptLine(v map[string]any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b) + "\n"
}

func at(sec int) string {
	return event.Stamp(time.Date(2026, 10, 27, 5, 0, 0, 0, time.UTC).Add(time.Duration(sec) * time.Second))
}

func userSays(uuid string, sec int, content any) string {
	return transcriptLine(map[string]any{"type": "user", "uuid": uuid, "timestamp": at(sec), "message": map[string]any{"role": "user", "content": content}})
}

func assistantSays(uuid string, sec int, blocks ...map[string]any) string {
	return transcriptLine(map[string]any{"type": "assistant", "uuid": uuid, "timestamp": at(sec),
		"message": map[string]any{"id": "msg_" + uuid, "role": "assistant", "content": blocks}})
}

func toolUse(id, name string, input map[string]any) map[string]any {
	return map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}
}

func toolResult(uuid string, sec int, id string, failed bool, content string, structured any) string {
	v := map[string]any{"type": "user", "uuid": uuid, "timestamp": at(sec), "message": map[string]any{"role": "user",
		"content": []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": content, "is_error": failed}}}}
	if structured != nil {
		v["toolUseResult"] = structured
	}
	return transcriptLine(v)
}

func appendFile(t *testing.T, path, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

// aConversation is a transcript with one of each kind of line the view
// shows or leaves out.
func aConversation() string {
	return userSays("u1", 0, "add the phrasebook page") +
		transcriptLine(map[string]any{"type": "user", "uuid": "m2", "isMeta": true, "timestamp": at(0), "message": map[string]any{"role": "user", "content": "Base directory for this skill: /home/alice"}}) +
		assistantSays("a1", 1, map[string]any{"type": "text", "text": "I'll add it."}) +
		assistantSays("a2", 1, map[string]any{"type": "thinking", "thinking": "where does it go?"}) +
		assistantSays("a3", 2, toolUse("toolu_1", "Bash", map[string]any{"command": "go test ./...", "description": "Run the tests"})) +
		assistantSays("a4", 3, toolUse("toolu_2", "Edit", map[string]any{"file_path": "/home/alice/site/phrasebook.md", "old_string": "a\nb\nc", "new_string": "a\nB\nc\nd"})) +
		toolResult("r1", 42, "toolu_1", false, "ok", map[string]any{"stdout": "ok", "stderr": "", "interrupted": false}) +
		toolResult("r2", 43, "toolu_2", false, "The file was updated", map[string]any{"filePath": "/home/alice/site/phrasebook.md",
			"structuredPatch": []any{map[string]any{"oldStart": 1, "oldLines": 3, "newStart": 1, "newLines": 4, "lines": []string{" a", "-b", "+B", " c", "+d"}}}}) +
		assistantSays("a5", 44, toolUse("toolu_3", "Agent", map[string]any{"description": "Find the config loader", "prompt": "Find where the config is loaded.", "subagent_type": "Explore"})) +
		toolResult("r3", 50, "toolu_3", false, "It is in config.go.", map[string]any{"status": "completed", "agentId": "a1b2", "totalDurationMs": 61000, "totalToolUseCount": 7}) +
		transcriptLine(map[string]any{"type": "attachment", "uuid": "q1", "timestamp": at(51), "attachment": map[string]any{"type": "queued_command", "prompt": "also link it from the home page"}}) +
		transcriptLine(map[string]any{"type": "attachment", "uuid": "q2", "timestamp": at(51), "attachment": map[string]any{"type": "queued_command", "prompt": "hello from a peer", "origin": map[string]any{"kind": "peer"}}}) +
		userSays("i1", 52, []any{map[string]any{"type": "text", "text": "[Request interrupted by user for tool use]"}}) +
		userSays("c1", 53, "<command-name>/model</command-name>\n<command-message>model</command-message>\n<command-args>opus</command-args>") +
		userSays("c2", 53, "<local-command-stdout>Set model to opus</local-command-stdout>") +
		userSays("s1", 54, "<system-reminder>Remember the rules.</system-reminder>") +
		userSays("s2", 54, "keep going<system-reminder>Remember the rules.</system-reminder>") +
		transcriptLine(map[string]any{"type": "assistant", "uuid": "x1", "isSidechain": true, "timestamp": at(55), "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "a subagent's words"}}}}) +
		transcriptLine(map[string]any{"type": "system", "uuid": "cb1", "subtype": "compact_boundary", "timestamp": at(56)}) +
		transcriptLine(map[string]any{"type": "file-history-snapshot", "messageId": "m"}) +
		assistantSays("a6", 57, toolUse("toolu_4", "Bash", map[string]any{"command": "make"})) +
		toolResult("r4", 58, "toolu_4", true, "make: *** No rule to make target", nil) +
		assistantSays("a7", 59, toolUse("toolu_5", "Read", map[string]any{"file_path": "/home/alice/site/Store.swift"}))
}

func TestAConversationReadsAsItems(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	appendFile(t, path, aConversation())
	cv := newConversation()
	if !cv.update([]string{path}, true) {
		t.Fatal("no change after reading the transcript")
	}
	type want struct{ id, kind, text, phrase, state string }
	wants := []want{
		{"u:u1", ipc.ItemUser, "add the phrasebook page", "", ""},
		{"a:a1:0", ipc.ItemText, "I'll add it.", "", ""},
		{"t:toolu_1", ipc.ItemTool, "", "Ran the tests", ipc.ItemOK},
		{"t:toolu_2", ipc.ItemTool, "", "Edited phrasebook.md", ipc.ItemOK},
		{"t:toolu_3", ipc.ItemAgent, "Find the config loader", "Asked a helper to find the config loader", ipc.ItemOK},
		{"u:q1", ipc.ItemUser, "also link it from the home page", "", ""},
		{"n:i1", ipc.ItemNote, "Interrupted", "", ""},
		{"u:c1", ipc.ItemUser, "/model opus", "", ""},
		{"u:s2", ipc.ItemUser, "keep going", "", ""},
		{"n:cb1", ipc.ItemNote, "The conversation was compacted", "", ""},
		{"t:toolu_4", ipc.ItemTool, "", "Couldn’t build the project", ipc.ItemError},
		{"t:toolu_5", ipc.ItemTool, "", "Reading Store.swift", ipc.ItemRunning},
	}
	got := cv.all(0).Items
	if len(got) != len(wants) {
		var ids []string
		for _, it := range got {
			ids = append(ids, it.ID)
		}
		t.Fatalf("items %v, want %d", ids, len(wants))
	}
	for i, w := range wants {
		g := got[i]
		if g.ID != w.id || g.Kind != w.kind || g.Text != w.text || g.Phrase != w.phrase || g.State != w.state {
			t.Errorf("item %d = %+v, want %+v", i, g, w)
		}
	}
	bash, edit, agent, failed := got[2], got[3], got[4], got[10]
	if bash.Ms != 40000 || bash.Detail != "go test ./..." || bash.Tool != "Bash" {
		t.Errorf("the test run = %+v", bash)
	}
	if edit.Added != 2 || edit.Removed != 1 || edit.Group != "edit" || edit.Detail != "/home/alice/site/phrasebook.md" {
		t.Errorf("the edit = %+v", edit)
	}
	if agent.AgentType != "Explore" || agent.Ms != 61000 || agent.Tools != 7 || agent.Detail != "Find where the config is loaded." {
		t.Errorf("the subagent = %+v", agent)
	}
	if !got[5].Queued {
		t.Errorf("a message typed while it worked is not marked queued: %+v", got[5])
	}
	if failed.Output != "make: *** No rule to make target" {
		t.Errorf("the failed call = %+v", failed)
	}

	// Once the agent is not at work, a call without a result has stopped.
	seen := cv.ver
	if !cv.update([]string{path}, false) {
		t.Fatal("no change when the agent stopped")
	}
	c, ok := cv.since(seen)
	if !ok || len(c.Items) != 1 || c.Items[0].ID != "t:toolu_5" || c.Items[0].State != ipc.ItemStopped || c.Reset {
		t.Errorf("since the stop: %+v", c)
	}
}

func TestAConversationFollowsItsTranscript(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	cv := newConversation()
	cv.update([]string{path}, true) // learns the path
	if cv.update([]string{path}, true) || len(cv.items) > 0 {
		t.Error("a change before the transcript exists")
	}
	appendFile(t, path, userSays("u1", 0, "hello"))
	cv.update([]string{path}, true)
	seen := cv.ver

	// A line arrives in two writes.
	line := assistantSays("a1", 1, map[string]any{"type": "text", "text": "Hi! What shall we make?"})
	appendFile(t, path, line[:20])
	if cv.update([]string{path}, true) {
		t.Error("half a line changed the conversation")
	}
	appendFile(t, path, line[20:])
	cv.update([]string{path}, true)
	c, ok := cv.since(seen)
	if !ok || c.Reset || len(c.Items) != 1 || c.Items[0].Text != "Hi! What shall we make?" {
		t.Fatalf("since %d: %+v", seen, c)
	}
	seen = cv.ver
	if c, ok := cv.since(seen); ok {
		t.Errorf("a change with nothing new: %+v", c)
	}

	// After /clear, a second transcript follows the first.
	second := filepath.Join(dir, "t.jsonl")
	appendFile(t, second, userSays("u2", 9, "start over"))
	cv.update([]string{path, second}, true)
	c, _ = cv.since(seen)
	if len(c.Items) != 2 || c.Items[0].Kind != ipc.ItemNote || c.Items[1].Text != "start over" || c.Transcript != second {
		t.Errorf("after a second transcript: %+v", c)
	}
	seen = cv.ver

	// A transcript replaced by a shorter one is read again from the start.
	if err := os.WriteFile(path, []byte(userSays("u3", 0, "hi")), 0o600); err != nil {
		t.Fatal(err)
	}
	cv.update([]string{path, second}, true)
	c, _ = cv.since(seen)
	if !c.Reset || len(c.Items) != 3 || c.Items[0].Text != "hi" || c.Items[2].Text != "start over" {
		t.Errorf("after the transcript was replaced: %+v", c)
	}
}

func TestLineDiff(t *testing.T) {
	for _, c := range []struct {
		a, b           string
		added, removed int
	}{
		{"", "one\ntwo\n", 2, 0},
		{"one\ntwo", "", 0, 2},
		{"a\nb\nc", "a\nB\nc\nd", 2, 1},
		{"same", "same", 0, 0},
		{"x\ny\nz", "y\nz\nw", 1, 1},
	} {
		if a, r := lineDiff(c.a, c.b); a != c.added || r != c.removed {
			t.Errorf("lineDiff(%q, %q) = +%d −%d, want +%d −%d", c.a, c.b, a, r, c.added, c.removed)
		}
	}
}

func TestClaudeTranscriptIsFoundWhereClaudeCodeKeepsIt(t *testing.T) {
	config := t.TempDir()
	cwd := "/home/alice/.local/share/irie/worktrees/site/phrasebook"
	want := filepath.Join(config, "projects", "-home-alice--local-share-irie-worktrees-site-phrasebook", "6f1c0d2e-0000-4000-8000-000000000001.jsonl")
	if got := claudeTranscript(config, cwd, "6f1c0d2e-0000-4000-8000-000000000001"); got != "" {
		t.Errorf("found %s before it exists", got)
	}
	appendFile(t, want, userSays("u1", 0, "hi"))
	if got := claudeTranscript(config, cwd, "6f1c0d2e-0000-4000-8000-000000000001"); got != want {
		t.Errorf("claudeTranscript = %q, want %q", got, want)
	}
	if got := claudeTranscript(config, "/"+strings.Repeat("a", 220), "x"); got != "" {
		t.Errorf("a name over 200 characters gave %q", got)
	}
}

// transcriptOf is the transcript the session's SessionStart hook named.
func (c *coreEnv) transcriptOf(id string) string {
	c.t.Helper()
	for _, e := range c.events(id) {
		if e.Kind == event.SessionStarted && e.Str("transcript") != "" {
			return e.Str("transcript")
		}
	}
	c.t.Fatalf("no transcript for %s in %v", id, kinds(c.events(id)))
	return ""
}

func TestTheConversationOpAndItsFrames(t *testing.T) {
	c := setup(t)
	s := c.spawn("prompt,tool:Bash,stop")
	c.waitState(s.ID, ipc.StateDone)
	path := c.transcriptOf(s.ID)
	var none json.RawMessage
	if err := c.call(ipc.OpConversation, ipc.ConversationArgs{Session: s.ID}, &none); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(none), `"items":[]`) || strings.Contains(string(none), `"transcript"`) {
		t.Errorf("before its transcript is written: %s", none)
	}
	appendFile(t, path, userSays("u1", 0, "prompt,tool:Bash,stop")+
		assistantSays("a1", 1, toolUse("toolu_1", "Bash", map[string]any{"command": "go test ./..."}))+
		toolResult("r1", 3, "toolu_1", false, "ok", nil))

	var got ipc.Conversation
	if err := c.call(ipc.OpConversation, ipc.ConversationArgs{Session: s.ID}, &got); err != nil {
		t.Fatal(err)
	}
	if got.Session != s.ID || got.Transcript != path || len(got.Items) != 2 || got.Items[1].Phrase != "Ran the tests" || got.Needs != nil {
		t.Fatalf("conversation = %+v", got)
	}
	if err := c.call(ipc.OpConversation, ipc.ConversationArgs{Session: "s000000000000"}, &got); err == nil {
		t.Error("a conversation of no session")
	}

	cl, err := ipc.Dial(c.paths.Socket(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	frames, err := cl.Subscribe(ipc.SubscribeArgs{Session: s.ID, Conversation: true})
	if err != nil {
		t.Fatal(err)
	}
	next := func(what string) ipc.Conversation {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case f, ok := <-frames:
				if !ok {
					t.Fatalf("%s: the subscription ended", what)
				}
				if f.T == ipc.FrameConversation && f.Conversation != nil {
					return *f.Conversation
				}
			case <-deadline:
				t.Fatalf("no conversation frame: %s", what)
			}
		}
	}
	if first := next("the first frame"); !first.Reset || len(first.Items) != 2 {
		t.Fatalf("first frame = %+v", first)
	}
	appendFile(t, path, assistantSays("a2", 4, map[string]any{"type": "text", "text": "All **40** tests pass."}))
	if d := next("the new reply"); d.Reset || len(d.Items) != 1 || d.Items[0].Text != "All **40** tests pass." {
		t.Fatalf("delta = %+v", d)
	}
}

func TestTheConversationSaysWhatItWaitsFor(t *testing.T) {
	c := setup(t)
	asks := c.spawn("prompt,tool:AskUserQuestion")
	allowed := c.spawn("prompt,permission")
	c.waitState(asks.ID, ipc.StateNeedsYou)
	c.waitState(allowed.ID, ipc.StateNeedsYou)
	appendFile(t, c.transcriptOf(asks.ID), assistantSays("a1", 1, toolUse("toolu_1", "AskUserQuestion", map[string]any{"questions": []any{map[string]any{
		"question": "Which colour should the button be?", "header": "Colour", "multiSelect": false,
		"options": []any{map[string]any{"label": "Green", "description": "Calm"}, map[string]any{"label": "Gold", "description": "Warm"}},
	}}})))

	var got ipc.Conversation
	if err := c.call(ipc.OpConversation, ipc.ConversationArgs{Session: asks.ID}, &got); err != nil {
		t.Fatal(err)
	}
	if n := got.Needs; n == nil || n.Text != "Asks: which colour should the button be?" || n.Reason != "question" || !slices.Equal(n.Options, []string{"Green", "Gold"}) {
		t.Errorf("the question's needs = %+v", got.Needs)
	}
	if err := c.call(ipc.OpConversation, ipc.ConversationArgs{Session: allowed.ID}, &got); err != nil {
		t.Fatal(err)
	}
	if n := got.Needs; n == nil || n.Text != "Asks: may it run rm -rf build?" || n.Reason != "permission" {
		t.Errorf("the permission's needs = %+v", got.Needs)
	}
}
