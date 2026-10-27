package core

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/testutil"
)

// sidechain marks transcript lines as a helper's, as Claude Code writes
// every line of a subagent's own transcript.
func sidechain(lines string) string {
	var out strings.Builder
	for _, l := range strings.Split(strings.TrimSpace(lines), "\n") {
		var v map[string]any
		if err := json.Unmarshal([]byte(l), &v); err != nil {
			panic(err)
		}
		v["isSidechain"], v["agentId"] = true, "h1"
		out.WriteString(transcriptLine(v))
	}
	return out.String()
}

// aHelper is a helper's own transcript: its task, a reply, a call that
// ran, one that runs, and a later message from its session.
func aHelper() string {
	return sidechain(userSays("t1", 0, "Find why the login test fails one run in ten.") +
		assistantSays("h1", 1, map[string]any{"type": "text", "text": "I'll run it in a loop."}) +
		assistantSays("h2", 2, toolUse("toolu_h1", "Grep", map[string]any{"pattern": "sessionCache"})) +
		toolResult("hr1", 3, "toolu_h1", false, "auth/session.go", nil) +
		assistantSays("h3", 4, toolUse("toolu_h2", "Bash", map[string]any{"command": "go test -race ./auth", "description": "Run the auth tests"})) +
		userSays("t2", 5, "Look at the token refresh too."))
}

func TestAHelpersConversationIsReadFromItsOwnTranscript(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-h1.jsonl")
	appendFile(t, path, aHelper())

	cv := newHelperConversation()
	if !cv.update([]string{path}, true) {
		t.Fatal("no change after reading the helper's transcript")
	}
	type want struct{ id, kind, from, text, state string }
	wants := []want{
		{"u:t1", ipc.ItemUser, "parent", "Find why the login test fails one run in ten.", ""},
		{"a:h1:0", ipc.ItemText, "", "I'll run it in a loop.", ""},
		{"t:toolu_h1", ipc.ItemTool, "", "", ipc.ItemOK},
		{"t:toolu_h2", ipc.ItemTool, "", "", ipc.ItemRunning},
		{"u:t2", ipc.ItemUser, "", "Look at the token refresh too.", ""},
	}
	if len(cv.items) != len(wants) {
		t.Fatalf("items = %+v", cv.items)
	}
	for i, w := range wants {
		it := cv.items[i]
		if it.ID != w.id || it.Kind != w.kind || it.From != w.from || w.text != "" && it.Text != w.text || it.State != w.state {
			t.Errorf("item %d = %+v, want %+v", i, it, w)
		}
	}

	// Once the helper has stopped, the call it left has stopped too.
	if !cv.update([]string{path}, false) || cv.items[3].State != ipc.ItemStopped {
		t.Errorf("after the helper stopped: %+v", cv.items[3])
	}

	// The session's own reader leaves a helper's lines out.
	main := newConversation()
	main.update([]string{path}, true)
	if len(main.items) != 0 {
		t.Errorf("the session's reader took a helper's lines: %+v", main.items)
	}
}

func TestASubagentItemNamesItsHelper(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	appendFile(t, path, aConversation()+
		assistantSays("b1", 60, toolUse("toolu_bg", "Agent", map[string]any{"description": "Map the auth flow", "prompt": "Map it.", "subagent_type": "Explore", "run_in_background": true}))+
		toolResult("br1", 61, "toolu_bg", false, "Async agent launched", map[string]any{"status": "async_launched", "agentId": "bg7"})+
		assistantSays("b2", 62, toolUse("toolu_odd", "Agent", map[string]any{"description": "Odd", "prompt": "x", "subagent_type": "Explore"}))+
		toolResult("br2", 63, "toolu_odd", false, "done", map[string]any{"status": "completed", "agentId": "../../etc"}))
	cv := newConversation()
	cv.update([]string{path}, true)
	byID := map[string]ipc.Item{}
	for _, it := range cv.items {
		byID[it.ID] = it
	}
	if got := byID["t:toolu_3"].Agent; got != "sub:a1b2" {
		t.Errorf("a finished subagent names helper %q, want sub:a1b2", got)
	}
	if it := byID["t:toolu_bg"]; it.Agent != "sub:bg7" || !strings.Contains(it.Phrase, "(in the background)") {
		t.Errorf("a subagent in the background = %+v", it)
	}
	if got := byID["t:toolu_odd"].Agent; got != "" {
		t.Errorf("an id that is no helper's was taken: %q", got)
	}
}

func TestARunningSubagentIsTiedToARunningHelperOfItsKind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	appendFile(t, path, assistantSays("a1", 1,
		toolUse("toolu_e", "Agent", map[string]any{"description": "Map auth", "prompt": "x", "subagent_type": "Explore"}),
		toolUse("toolu_g", "Agent", map[string]any{"description": "Find the race", "prompt": "y"}),
		toolUse("toolu_e2", "Agent", map[string]any{"description": "Map billing", "prompt": "z", "subagent_type": "Explore"})))
	cv := newConversation()
	cv.update([]string{path}, true)
	t0 := time.Date(2026, 10, 27, 5, 0, 0, 0, time.UTC)
	running := []ipc.Subagent{
		{Agent: "sub:e1", Type: "Explore", Started: t0},
		{Agent: "sub:g1", Type: "general-purpose", Started: t0.Add(time.Second)},
	}
	if !cv.linkAgents(running) {
		t.Fatal("nothing was tied")
	}
	got := map[string]string{}
	for _, it := range cv.items {
		got[it.ID] = it.Agent
	}
	if got["t:toolu_e"] != "sub:e1" || got["t:toolu_g"] != "sub:g1" || got["t:toolu_e2"] != "" {
		t.Errorf("tied = %v", got)
	}
	if cv.linkAgents(running) {
		t.Error("tying again changed something")
	}
}

func TestTheConversationOpAndSubscriptionFollowAHelper(t *testing.T) {
	c := setup(t)
	s := c.spawn("prompt,sub:h1:tool:Grep+sleep:4s,stop")
	testutil.Eventually(t, 5*time.Second, "the helper did not start", func() bool {
		r, _ := c.row(s.ID)
		return len(r.Subagents) == 1
	})
	helper := filepath.Join(strings.TrimSuffix(c.transcriptOf(s.ID), ".jsonl"), "subagents", "agent-h1.jsonl")
	appendFile(t, helper, sidechain(userSays("t1", 0, "Find the race.")+
		assistantSays("h1", 1, map[string]any{"type": "text", "text": "Looking."})))

	var got ipc.Conversation
	if err := c.call(ipc.OpConversation, ipc.ConversationArgs{Session: s.ID, Agent: "sub:h1"}, &got); err != nil {
		t.Fatal(err)
	}
	if got.Session != s.ID || got.Agent != "sub:h1" || got.Transcript != helper || len(got.Items) != 2 || got.Items[0].From != "parent" {
		t.Fatalf("the helper's conversation = %+v", got)
	}
	for _, bad := range []string{"sub:../../x", "sub:a/b", "sub:"} {
		if err := c.call(ipc.OpConversation, ipc.ConversationArgs{Session: s.ID, Agent: bad}, &got); err == nil {
			t.Errorf("agent %q was taken", bad)
		}
	}

	cl, err := ipc.Dial(c.paths.Socket(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	frames, err := cl.Subscribe(ipc.SubscribeArgs{Session: s.ID, Agent: "sub:h1", Conversation: true})
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
	if first := next("the first frame"); !first.Reset || first.Agent != "sub:h1" || len(first.Items) != 2 {
		t.Fatalf("first frame = %+v", first)
	}
	appendFile(t, helper, sidechain(assistantSays("h2", 2, map[string]any{"type": "text", "text": "Found it."})))
	if d := next("the helper's new reply"); d.Reset || d.Agent != "sub:h1" || len(d.Items) != 1 || d.Items[0].Text != "Found it." {
		t.Fatalf("delta = %+v", d)
	}
}
