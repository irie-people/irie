package ui

import (
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/irie-hq/irie/internal/ipc"
)

type sent struct{ session, text string }

type fakeSlotBackend struct {
	mu   sync.Mutex
	sent []sent
	err  error
}

func (b *fakeSlotBackend) Send(session, text string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sent = append(b.sent, sent{session, text})
	return b.err
}

// slotSend applies msg and runs the commands it returns, as a program would.
func slotSend(m *slotModel, msg tea.Msg) *slotModel {
	next, cmd := m.Update(msg)
	m = next.(*slotModel)
	for cmd != nil {
		out := cmd()
		cmd = nil
		if out != nil {
			if _, batch := out.(tea.BatchMsg); batch {
				break
			}
			next, cmd = m.Update(out)
			m = next.(*slotModel)
		}
	}
	return m
}

func slotScreen(m *slotModel) string { return ansiRE.ReplaceAllString(m.View().Content, "") }

func at(min int) time.Time { return time.Date(2026, 10, 27, 5, min, 0, 0, time.UTC) }

// aSlot is the slot with docs-guide shown, and a conversation in it.
func aSlot(t *testing.T, state string) (*slotModel, *fakeSlotBackend) {
	t.Helper()
	b := &fakeSlotBackend{}
	follow := make(chan string, 4)
	m := newSlot(b, SlotOptions{FocusKey: "C-]", FlipKey: `C-\`})
	m.follow = follow
	m = slotSend(m, tea.WindowSizeMsg{Width: 90, Height: 40})
	row := ipc.Session{ID: "s02docsguide00", Name: "docs-guide", State: state, Shown: true, Detail: "Running the tests"}
	if state == ipc.StateNeedsYou {
		row.Detail = "Asks: may it run rm -rf build?"
	}
	m = slotSend(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameSnapshot, Sessions: []ipc.Session{{ID: "s01other00000", Name: "other"}, row}}})
	select {
	case id := <-follow:
		if id != row.ID {
			t.Fatalf("follows %q, want %q", id, row.ID)
		}
	default:
		t.Fatal("the shown session's conversation is not followed")
	}
	cv := ipc.Conversation{Session: row.ID, Reset: true, Transcript: "/home/alice/t.jsonl", Items: []ipc.Item{
		{ID: "u:1", Kind: ipc.ItemUser, Time: at(0), Text: "add the phrasebook page"},
		{ID: "a:1", Kind: ipc.ItemText, Time: at(1), Text: "I'll add a **Phrasebook** page under `docs/`.\n\n- linked from the guide"},
		{ID: "t:1", Kind: ipc.ItemTool, Time: at(1), Tool: "Read", Phrase: "Read guide.md", Detail: "/home/alice/docs/guide.md", State: ipc.ItemOK, Group: "read", Ms: 120},
		{ID: "t:2", Kind: ipc.ItemTool, Time: at(1), Tool: "Read", Phrase: "Read DESIGN.md", Detail: "/home/alice/docs/DESIGN.md", State: ipc.ItemOK, Group: "read", Ms: 90},
		{ID: "t:3", Kind: ipc.ItemTool, Time: at(2), Tool: "Bash", Phrase: "Ran the tests", Detail: "go test ./...", State: ipc.ItemOK, Ms: 40200},
		{ID: "t:4", Kind: ipc.ItemAgent, Time: at(2), Tool: "Agent", Text: "Find the glossary", AgentType: "Explore", State: ipc.ItemOK, Ms: 61000},
		{ID: "t:5", Kind: ipc.ItemTool, Time: at(3), Tool: "Edit", Phrase: "Edited guide.md", Detail: "/home/alice/docs/guide.md", State: ipc.ItemOK, Group: "edit", Added: 8, Removed: 5},
		{ID: "t:6", Kind: ipc.ItemTool, Time: at(3), Tool: "Write", Phrase: "Wrote phrasebook.md", Detail: "/home/alice/docs/phrasebook.md", State: ipc.ItemOK, Group: "edit", Added: 34, Removed: 2},
		{ID: "t:7", Kind: ipc.ItemTool, Time: at(4), Tool: "Bash", Phrase: "Couldn’t build the site", State: ipc.ItemError, Output: "make: *** No rule to make target 'site'."},
		{ID: "n:1", Kind: ipc.ItemNote, Time: at(5), Text: "Interrupted"},
	}, Pending: []ipc.Item{{ID: "s:1", Kind: ipc.ItemUser, Time: at(6), Text: "and add a table of contents", Pending: true, Queued: true}}}
	if state == ipc.StateNeedsYou {
		cv.Needs = &ipc.Item{ID: "needs", Kind: ipc.ItemNeeds, Text: "Asks: may it run rm -rf build?", Reason: "permission"}
	}
	m = slotSend(m, talkMsg{session: row.ID, update: cv})
	return m, b
}

func TestTheSlotDrawsTheShownConversation(t *testing.T) {
	m, _ := aSlot(t, ipc.StateWorking)
	screen := slotScreen(m)
	for _, want := range []string{
		"docs-guide · Running the tests",
		"You", "add the phrasebook page",
		"✦ Claude", "Phrasebook", "• linked from the guide",
		"✓ Read 2 files",
		"✓ Ran the tests", "40s",
		"↳ Explore Find the glossary ✓", "1m",
		"✓ Edited 2 files", "+42 −7",
		"✕ Couldn’t build the site", "No rule to make target",
		"Interrupted",
		"it reads this at its next step", "and add a table of contents",
		"Message docs-guide…", "Ctrl+\\",
	} {
		if !strings.Contains(screen, want) {
			t.Errorf("the slot lacks %q:\n%s", want, screen)
		}
	}
	// Cards are drawn round your messages and the message box.
	if strings.Count(screen, "╭") < 3 {
		t.Errorf("want cards round the messages and the box:\n%s", screen)
	}
	if lines := strings.Split(screen, "\n"); len(lines) != 40 {
		t.Errorf("%d lines, want the pane's 40", len(lines))
	}
}

func TestTheSlotSendsWhatYouType(t *testing.T) {
	m, b := aSlot(t, ipc.StateWorking)
	for _, r := range "also link it from home" {
		m = slotSend(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if !strings.Contains(slotScreen(m), "also link it from home") {
		t.Fatalf("the box does not show what was typed:\n%s", slotScreen(m))
	}
	m = slotSend(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(b.sent) != 1 || b.sent[0] != (sent{"s02docsguide00", "also link it from home"}) {
		t.Fatalf("sent %+v", b.sent)
	}
	if m.input.Value() != "" {
		t.Errorf("the box keeps %q after sending", m.input.Value())
	}
}

func TestTheSlotOffersTheTerminalWhileTheAgentWaits(t *testing.T) {
	m, b := aSlot(t, ipc.StateNeedsYou)
	screen := slotScreen(m)
	for _, want := range []string{"Asks: may it run rm -rf build?", "docs-guide is waiting for you in its terminal", "answer it there"} {
		if !strings.Contains(screen, want) {
			t.Errorf("the slot lacks %q:\n%s", want, screen)
		}
	}
	if strings.Contains(screen, "Message docs-guide") {
		t.Errorf("the message box shows while the agent waits:\n%s", screen)
	}
	for _, r := range "yes" {
		m = slotSend(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	m = slotSend(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(b.sent) != 0 {
		t.Errorf("sent %+v while the agent waits", b.sent)
	}
}

func TestTheSlotShowsTheKeysWithNothingShown(t *testing.T) {
	m := newSlot(&fakeSlotBackend{}, SlotOptions{FocusKey: "C-]", FlipKey: `C-\`})
	m = slotSend(m, tea.WindowSizeMsg{Width: 80, Height: 30})
	m = slotSend(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameSnapshot, Sessions: []ipc.Session{{ID: "s01other00000", Name: "other"}}}})
	screen := slotScreen(m)
	for _, want := range []string{"Ctrl+]", "Ctrl+\\", "flip between its conversation and its terminal", "In the sidebar:"} {
		if !strings.Contains(screen, want) {
			t.Errorf("the keys lack %q:\n%s", want, screen)
		}
	}
}

func TestTheSlotScrollsBack(t *testing.T) {
	m, _ := aSlot(t, ipc.StateWorking)
	m = slotSend(m, tea.WindowSizeMsg{Width: 90, Height: 16})
	if strings.Contains(slotScreen(m), "add the phrasebook page") {
		t.Fatalf("a short pane shows the start:\n%s", slotScreen(m))
	}
	m = slotSend(m, tea.KeyPressMsg{Code: tea.KeyPgUp})
	m = slotSend(m, tea.KeyPressMsg{Code: tea.KeyPgUp})
	m = slotSend(m, tea.KeyPressMsg{Code: tea.KeyPgUp})
	if !strings.Contains(slotScreen(m), "add the phrasebook page") {
		t.Errorf("PgUp does not reach the start:\n%s", slotScreen(m))
	}
}
