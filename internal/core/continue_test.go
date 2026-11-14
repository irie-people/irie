package core

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/testutil"
)

func TestAgentPromptReadsClaudeCodesPrompt(t *testing.T) {
	rule := strings.Repeat("─", 60)
	titled := strings.Repeat("─", 48) + " clip-bot ─"
	for _, c := range []struct {
		name   string
		screen []string
		typed  string
		shows  bool
	}{
		{"empty", []string{"⏺ Done.", "", titled, "❯ ", rule, "  ✓ to verify · ctx ▰▱▱▱▱▱▱▱ 13%", "  ⏵⏵ auto mode on (shift+tab to cycle)"}, "", true},
		{"agents listed under it", []string{titled, "❯", rule, "  ● needs you · $4.96", "  ⏺ main", "  ◯ door-check  Check the door code"}, "", true},
		{"typed", []string{rule, "❯ fix the flaky test", rule, "  ? for shortcuts"}, "fix the flaky test", true},
		{"several lines", []string{rule, "❯ first line", "  second line", rule}, "first line\nsecond line", true},
		{"a box, before 2.0", []string{"╭" + rule + "╮", "│ >                                │", "╰" + rule + "╯"}, "", true},
		{"a box with text", []string{"╭" + rule + "╮", "│ > hello                          │", "╰" + rule + "╯"}, "hello", true},
		{"what you said before is not it", []string{"❯ add the phrasebook page", "", "⏺ Added it.", rule, "❯ ", rule}, "", true},
		{"a permission's menu", []string{rule, " Bash command", "", "   rm -rf build", "", " Do you want to proceed?", " ❯ 1. Yes", "   2. No"}, "", false},
		{"a menu right under a rule", []string{rule, "❯ 1. Yes", "  2. No", rule}, "1. Yes\n2. No", true},
		{"bash mode", []string{rule, "! ", rule}, "", false},
		{"no rule under it", []string{rule, "❯ "}, "", false},
		{"no rule over it", []string{"❯ ", rule}, "", false},
		{"a short dash is no rule", []string{"──", "❯ ", "──"}, "", false},
		{"nothing", nil, "", false},
	} {
		if typed, shows := agentPrompt(c.screen); typed != c.typed || shows != c.shows {
			t.Errorf("%s: agentPrompt = %q, %v; want %q, %v", c.name, typed, shows, c.typed, c.shows)
		}
	}
}

// promptShows waits until session s's agent shows its prompt holding
// typed, as the fake agent's box step draws it.
func (c *coreEnv) promptShows(s ipc.Session, typed string) {
	c.t.Helper()
	testutil.Eventually(c.t, 10*time.Second, "the agent's prompt never held “"+typed+"”", func() bool {
		out, err := c.TryTmux("capture-pane", "-p", "-t", s.Pane)
		got, shows := agentPrompt(strings.Split(out, "\n"))
		return err == nil && shows && got == typed
	})
}

func TestContinueSendsTheAgentOnFromItsEmptyPrompt(t *testing.T) {
	c := setup(t)
	s := c.spawn("prompt,stop,box")
	c.waitState(s.ID, ipc.StateDone)
	c.promptShows(s, "")

	if err := c.call(ipc.OpContinue, ipc.ContinueArgs{Session: s.ID}, nil); err != nil {
		t.Fatal(err)
	}
	c.prompted(s.ID, continueWord)
	// Pending in the conversation, as a message sent is.
	if got := c.typedTo(s.ID); len(got) != 1 || got[0] != continueWord {
		t.Errorf("pending = %q", got)
	}
}

func TestContinuePressesNoEnterUnlessThePromptShowsTheWord(t *testing.T) {
	c := setup(t)
	// What is typed lands outside the prompt, as when a menu came up
	// between the look and the paste: Enter would answer the menu.
	s := c.spawn("prompt,stop,box:away")
	c.waitState(s.ID, ipc.StateDone)
	c.promptShows(s, "")

	err := c.call(ipc.OpContinue, ipc.ContinueArgs{Session: s.ID}, nil)
	var e *ipc.Error
	if !errors.As(err, &e) || e.Code != ipc.CodeBusy || !strings.Contains(e.Msg, "changed before Enter") {
		t.Fatalf("continue: %v, want busy: …changed before Enter…", err)
	}
	time.Sleep(300 * time.Millisecond)
	for _, e := range c.events(s.ID) {
		if n, _ := e.Int("chars"); e.Kind == event.PromptSubmitted && e.Source == event.SourceHook && n == len(continueWord) {
			t.Error("Enter went")
		}
	}
	if got := c.typedTo(s.ID); len(got) != 0 {
		t.Errorf("still pending: %q", got)
	}
}

func TestContinueGoesOnAfterATurnFailed(t *testing.T) {
	c := setup(t)
	s := c.spawn("prompt,stopfail:overloaded,box")
	c.waitState(s.ID, ipc.StateNeedsYou)
	c.promptShows(s, "")

	// Its agent waits at its own prompt, which shows, so continue goes, as
	// a message sent would (TestSendReachesASessionWhoseTurnFailed).
	if err := c.call(ipc.OpContinue, ipc.ContinueArgs{Session: s.ID}, nil); err != nil {
		t.Fatal(err)
	}
	c.prompted(s.ID, continueWord)
}

func TestContinueRefusesWithoutAnEmptyPrompt(t *testing.T) {
	c := setup(t)
	plain := c.spawn("prompt,stop")
	drafted := c.spawn("prompt,stop,box")
	scrolled := c.spawn("prompt,stop,box")
	asks := c.spawn("prompt,tool:AskUserQuestion")
	for _, s := range []ipc.Session{plain, drafted, scrolled} {
		c.waitState(s.ID, ipc.StateDone)
	}
	c.waitState(asks.ID, ipc.StateNeedsYou)
	c.promptShows(drafted, "")
	c.promptShows(scrolled, "")
	c.keys(drafted, "-l", "half a thought")
	c.promptShows(drafted, "half a thought")
	c.Tmux(t, "copy-mode", "-t", scrolled.Pane)

	busy := func(s ipc.Session, want string) {
		t.Helper()
		err := c.call(ipc.OpContinue, ipc.ContinueArgs{Session: s.ID}, nil)
		var e *ipc.Error
		if !errors.As(err, &e) || e.Code != ipc.CodeBusy || !strings.Contains(e.Msg, want) {
			t.Errorf("continue %s: %v, want busy: …%s…", s.Name, err, want)
		}
	}
	busy(plain, "shows no prompt")                // no prompt drawn
	busy(drafted, "has something typed")          // the word would join the draft
	busy(scrolled, "scrolled back")               // Enter would go to tmux's copy mode
	busy(asks, "waiting for you in its terminal") // Enter would answer the question
	var e *ipc.Error
	if err := c.call(ipc.OpContinue, ipc.ContinueArgs{Session: "s000000000000"}, nil); !errors.As(err, &e) || e.Code != ipc.CodeNotFound {
		t.Errorf("continue no session: %v", err)
	}
	c.Tmux(t, "copy-mode", "-q", "-t", scrolled.Pane)

	// None of them was typed to.
	time.Sleep(300 * time.Millisecond)
	for _, s := range []ipc.Session{plain, drafted, scrolled, asks} {
		for _, e := range c.events(s.ID) {
			if n, _ := e.Int("chars"); e.Kind == event.PromptSubmitted && e.Source == event.SourceHook && n == len(continueWord) {
				t.Errorf("%s got “continue”", s.Name)
			}
		}
		if got := c.typedTo(s.ID); len(got) != 0 {
			t.Errorf("%s: pending = %q", s.Name, got)
		}
	}
	if got, _ := c.TryTmux("capture-pane", "-p", "-t", drafted.Pane); !strings.Contains(got, "❯ half a thought") {
		t.Errorf("the draft went:\n%s", got)
	}
}
