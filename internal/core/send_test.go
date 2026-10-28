package core

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/testutil"
)

func TestASentMessageReachesTheAgentAsTyped(t *testing.T) {
	c := setup(t)
	s := c.spawn("prompt,stop")
	c.waitState(s.ID, ipc.StateDone)

	if err := c.call(ipc.OpSend, ipc.SendArgs{Session: s.ID, Text: "add the phrasebook page\n"}, nil); err != nil {
		t.Fatal(err)
	}
	// The fake agent submits each line it reads as a prompt.
	testutil.Eventually(t, 5*time.Second, "the agent never got the message", func() bool {
		for _, e := range c.events(s.ID) {
			if n, _ := e.Int("chars"); e.Kind == event.PromptSubmitted && n == len("add the phrasebook page") {
				return true
			}
		}
		return false
	})
	// The transcript does not have it yet, so it is pending.
	var cv ipc.Conversation
	if err := c.call(ipc.OpConversation, ipc.ConversationArgs{Session: s.ID}, &cv); err != nil {
		t.Fatal(err)
	}
	if len(cv.Pending) != 1 || cv.Pending[0].Text != "add the phrasebook page" || !cv.Pending[0].Pending {
		t.Fatalf("pending = %+v", cv.Pending)
	}
	// Once the transcript has it, it is no longer pending.
	appendFile(t, c.transcriptOf(s.ID), transcriptLine(map[string]any{
		"type": "user", "uuid": "u9", "timestamp": event.Stamp(time.Now()), "message": map[string]any{"role": "user", "content": "add the phrasebook page"},
	}))
	if err := c.call(ipc.OpConversation, ipc.ConversationArgs{Session: s.ID}, &cv); err != nil {
		t.Fatal(err)
	}
	if len(cv.Pending) != 0 || len(cv.Items) != 1 || cv.Items[0].Text != "add the phrasebook page" {
		t.Errorf("after the transcript has it: items %+v, pending %+v", cv.Items, cv.Pending)
	}
}

func TestSendRefusesWhatWouldGoWrong(t *testing.T) {
	c := setup(t)
	asks := c.spawn("prompt,tool:AskUserQuestion")
	idle := c.spawn("prompt,stop")
	c.waitState(asks.ID, ipc.StateNeedsYou)
	c.waitState(idle.ID, ipc.StateDone)

	busy := func(args ipc.SendArgs, want string) {
		t.Helper()
		err := c.call(ipc.OpSend, args, nil)
		var e *ipc.Error
		if !errors.As(err, &e) || e.Code != ipc.CodeBusy || !strings.Contains(e.Msg, want) {
			t.Errorf("send %+v: %v, want busy: …%s…", args, err, want)
		}
	}
	// Enter would answer the question in its terminal.
	busy(ipc.SendArgs{Session: asks.ID, Text: "Green"}, "waiting for you in its terminal")
	// The fake agent never asks for bracketed paste, so each line would go
	// as a message of its own.
	busy(ipc.SendArgs{Session: idle.ID, Text: "one\ntwo"}, "several lines")
	if err := c.call(ipc.OpSend, ipc.SendArgs{Session: idle.ID, Text: "  \n "}, nil); err == nil {
		t.Error("sent nothing")
	}
	if err := c.call(ipc.OpSend, ipc.SendArgs{Session: "s000000000000", Text: "hi"}, nil); err == nil {
		t.Error("sent to no session")
	}
	// Nothing reached the agent that asks.
	for _, e := range c.events(asks.ID) {
		if e.Kind == event.PromptSubmitted && e.Source == event.SourceHook {
			if n, _ := e.Int("chars"); n == len("Green") {
				t.Errorf("the question got the message: %+v", e)
			}
		}
	}
	if b, _ := filepath.Glob(filepath.Join(c.paths.Session(idle.ID), "message.txt")); len(b) > 0 {
		t.Errorf("a message file was left behind: %v", b)
	}
}

func TestPendingMessagesGoOnceTaken(t *testing.T) {
	cv := newConversation()
	sent := time.Date(2026, 10, 27, 5, 0, 10, 0, time.UTC)
	cv.sent("first", sent, true)
	cv.sent("second", sent.Add(time.Second), true)
	if c := cv.all(0); len(c.Pending) != 2 || !c.Pending[0].Queued {
		t.Fatalf("pending = %+v", c.Pending)
	}
	cv.said("u1", sent.Add(-time.Minute), "an older message", false)
	if len(cv.pending) != 2 {
		t.Errorf("an older message took a pending one: %+v", cv.pending)
	}
	cv.said("u2", sent.Add(5*time.Second), "first", true)
	if len(cv.pending) != 1 || cv.pending[0].Text != "second" {
		t.Errorf("after the first was taken: %+v", cv.pending)
	}
	cv.expire(sent.Add(time.Hour))
	if len(cv.pending) != 0 {
		t.Errorf("an hour on: %+v", cv.pending)
	}
}
