package core

import (
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/testutil"
)

func TestTheSlotShowsTheConversationAndFlipsToTheTerminal(t *testing.T) {
	c := setup(t)
	a := c.spawn("prompt")
	c.waitState(a.ID, ipc.StateWorking)

	// Shown, the session's conversation is in the slot: the placeholder
	// draws it, and the agent's pane stays home.
	c.show(a.ID, true)
	if p := c.slot(); p["@irie_role"] != "placeholder" {
		t.Fatalf("the slot holds %v, want the placeholder", p)
	}
	if r, _ := c.row(a.ID); !r.Shown {
		t.Errorf("the row is not shown: %+v", r)
	}
	option := func(name string) string {
		out, _ := c.TryTmux("show-options", "-gqv", name)
		return strings.TrimSpace(out)
	}
	if option("@irie_selected") != a.ID || option("@irie_raw") != "0" {
		t.Errorf("kept view: selected %q, raw %q", option("@irie_selected"), option("@irie_raw"))
	}

	// The flip key turns the slot to the raw terminal, and back.
	var r ipc.FlipResult
	if err := c.call(ipc.OpFlip, nil, &r); err != nil || !r.Terminal {
		t.Fatalf("flip: %+v, %v", r, err)
	}
	if p := c.slot(); p["pane_id"] != a.Pane {
		t.Fatalf("after the flip the slot holds %v, want %s", p, a.Pane)
	}
	if row, _ := c.row(a.ID); !row.Shown {
		t.Errorf("the row is not shown in the terminal view: %+v", row)
	}

	// A restarted core keeps the view it had.
	c.restartCore()
	time.Sleep(1500 * time.Millisecond) // a few ticks, any of which could undo it
	if p := c.slot(); p["pane_id"] != a.Pane {
		t.Errorf("after a restart the slot holds %v, want %s", p, a.Pane)
	}

	if err := c.call(ipc.OpFlip, nil, &r); err != nil || r.Terminal {
		t.Fatalf("flip back: %+v, %v", r, err)
	}
	testutil.Eventually(t, 5*time.Second, "the agent did not go home", func() bool {
		return c.slot()["@irie_role"] == "placeholder"
	})
	if row, _ := c.row(a.ID); !row.Shown {
		t.Errorf("the row is not shown after the flip back: %+v", row)
	}
}

func TestTheFlipKeyIsBound(t *testing.T) {
	c := setup(t)
	out := c.Tmux(t, "list-keys", "-T", "root")
	if !strings.Contains(out, "ctl flip") || !strings.Contains(out, `C-\\`) {
		t.Errorf("no flip binding in:\n%s", out)
	}
}

func TestTheSlotPaneDrawsTheShownConversation(t *testing.T) {
	c := setup(t)
	s := c.spawn("prompt,tool:Bash,stop")
	c.waitState(s.ID, ipc.StateDone)
	appendFile(t, c.transcriptOf(s.ID), userSays("u1", 0, "add the phrasebook page")+
		assistantSays("a1", 1, map[string]any{"type": "text", "text": "Added a **Phrasebook** page."})+
		assistantSays("a2", 2, toolUse("toolu_1", "Bash", map[string]any{"command": "go test ./..."}))+
		toolResult("r1", 42, "toolu_1", false, "ok", nil))
	c.show(s.ID, true)
	var screen string
	testutil.Eventually(t, 10*time.Second, "the slot never drew the conversation", func() bool {
		screen, _ = c.TryTmux("capture-pane", "-p", "-t", "irie:0.1")
		return strings.Contains(screen, "add the phrasebook page") && strings.Contains(screen, "Claude") &&
			strings.Contains(screen, "Ran the tests")
	})
	if !strings.Contains(screen, "Message "+s.Name) {
		t.Errorf("no message box:\n%s", screen)
	}
}

func TestAFirstCoreWithTheViewPicksTheSessionInTheSlot(t *testing.T) {
	c := setupWith(t, agentConfig{terminal: true})
	a := c.spawn("prompt")
	c.waitState(a.ID, ipc.StateWorking)
	c.show(a.ID, false)
	// An older core kept no view: forget it, and start one that shows
	// conversations.
	c.Tmux(t, "set-option", "-gu", "@irie_selected")
	c.Tmux(t, "set-option", "-gu", "@irie_raw")
	c.WriteConfig(t, agentConfig{}.toml(c.fake))
	c.restartCore()
	testutil.Eventually(t, 5*time.Second, "the agent did not go home for its conversation", func() bool {
		return c.slot()["@irie_role"] == "placeholder"
	})
	if r, _ := c.row(a.ID); !r.Shown {
		t.Errorf("the session in the slot is not the one shown: %+v", r)
	}
}
