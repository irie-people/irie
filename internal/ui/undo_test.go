package ui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/irie-hq/irie/internal/ipc"
)

func (f *fakeBackend) undo(dir string, a ipc.UndoArgs) (ipc.UndoResult, error) {
	f.undos = append(f.undos, strings.TrimSpace(dir+" "+a.ChoiceID+" "+a.Choice))
	if f.undoReply != nil {
		return f.undoReply(dir, a)
	}
	did := map[string]string{ipc.OpUndo: ipc.DidUndo, ipc.OpRedo: ipc.DidRedo}[dir]
	return ipc.UndoResult{Label: `start "task-002"`, Did: did}, nil
}

func (f *fakeBackend) Undo(a ipc.UndoArgs) (ipc.UndoResult, error) { return f.undo(ipc.OpUndo, a) }
func (f *fakeBackend) Redo(a ipc.UndoArgs) (ipc.UndoResult, error) { return f.undo(ipc.OpRedo, a) }

func historyFrame(undo, redo string) FrameMsg {
	return FrameMsg{Frame: ipc.Frame{T: ipc.FrameHistory, Undo: undo, Redo: redo}}
}

func TestUAndShiftUUndoAndRedo(t *testing.T) {
	m, b := newModel(t, 34, 24, 3)
	m = send(m, keyMsg("esc"))
	m = send(m, keyMsg("u"))
	if !reflect.DeepEqual(b.undos, []string{"undo"}) || m.status != `undid start "task-002"` || m.statusErr {
		t.Errorf("u: calls %v, status %q", b.undos, m.status)
	}
	m = send(m, keyMsg("U"))
	if !reflect.DeepEqual(b.undos, []string{"undo", "redo"}) || m.status != `redid start "task-002"` {
		t.Errorf("U: calls %v, status %q", b.undos, m.status)
	}
	// In the command bar u is a letter of the task.
	m = send(m, keyMsg("tab"))
	m = send(m, keyMsg("u"))
	if len(b.undos) != 2 || m.bar.Value() != "u" {
		t.Errorf("u in the bar: calls %v, bar %q", b.undos, m.bar.Value())
	}
}

func TestColonUndoAndRedo(t *testing.T) {
	m, b := newModel(t, 60, 24, 2)
	for _, line := range []string{":undo", ":redo"} {
		m.setFocus(focusBar)
		m.bar.Reset()
		m = typeText(m, line)
		m = send(m, keyMsg("enter"))
		if m.bar.Value() != "" {
			t.Errorf("%s left %q in the bar", line, m.bar.Value())
		}
	}
	if !reflect.DeepEqual(b.undos, []string{"undo", "redo"}) || len(b.spawns) != 0 {
		t.Errorf("calls %v, spawns %v", b.undos, b.spawns)
	}
	m.bar.Reset()
	m.setFocus(focusBar)
	m = typeText(m, ":bogus")
	m = send(m, keyMsg("enter"))
	if !strings.Contains(m.status, ":undo, :redo") {
		t.Errorf("the commands listed: %q", m.status)
	}
}

func TestUndoErrorsAreShown(t *testing.T) {
	m, b := newModel(t, 34, 24, 2)
	m = send(m, keyMsg("esc"))
	b.undoReply = func(string, ipc.UndoArgs) (ipc.UndoResult, error) {
		return ipc.UndoResult{}, ipc.Errorf(ipc.CodeNotFound, "nothing to undo")
	}
	m = send(m, keyMsg("u"))
	if m.status != "nothing to undo" || m.statusErr {
		t.Errorf("status %q, error %v", m.status, m.statusErr)
	}
	b.undoReply = func(string, ipc.UndoArgs) (ipc.UndoResult, error) {
		return ipc.UndoResult{}, errors.New("cannot reopen api-fix: its task is gone")
	}
	m = send(m, keyMsg("U"))
	if m.status != "cannot reopen api-fix: its task is gone" || !m.statusErr {
		t.Errorf("status %q, error %v", m.status, m.statusErr)
	}
	if s := screen(t, m); !strings.Contains(s[len(s)-2], "cannot reopen") {
		t.Errorf("status strip %q", s[len(s)-2])
	}
}

func TestHeaderShowsWhatUndoAndRedoWouldDo(t *testing.T) {
	m, _ := newModel(t, 34, 24, 3)
	if got := screen(t, m)[1]; got != " ↶ –  ↷ –" {
		t.Errorf("history line before any action %q", got)
	}
	m = send(m, historyFrame(`close "api-fix"`, ""))
	if got := screen(t, m)[1]; got != ` ↶ close "api-fix"  ↷ –` {
		t.Errorf("history line %q", got)
	}
	// The rows start below the header, its line and the rule.
	if s := screen(t, m); m.listTop() != 3 || !strings.Contains(s[3], "task-000") || !strings.Contains(s[2], "───") {
		t.Errorf("top of the screen:\n%s", strings.Join(s[:4], "\n"))
	}
	// Long labels are cut, each to its share, and say so.
	m = send(m, historyFrame(`rename "a-very-long-session-name"`, `close "another-long-session-name"`))
	got := screen(t, m)[1]
	if w := lipgloss.Width(got); w > 34 || !strings.Contains(got, "↶ rename") || !strings.Contains(got, "↷ close") || strings.Count(got, "…") != 2 {
		t.Errorf("narrow history line %q (%d columns)", got, lipgloss.Width(got))
	}
	m = send(m, historyFrame(`start "web"`, `close "a-very-long-session-name-indeed"`))
	if got := screen(t, m)[1]; !strings.Contains(got, `↶ start "web"  ↷ close`) || lipgloss.Width(got) > 34 {
		t.Errorf("a short undo leaves the rest to redo: %q", got)
	}
	m = send(m, tea.WindowSizeMsg{Width: 90, Height: 24})
	if got := screen(t, m)[1]; got != ` ↶ start "web"  ↷ close "a-very-long-session-name-indeed"` {
		t.Errorf("wide history line %q", got)
	}
	// A short pane has no room for it.
	m = send(m, tea.WindowSizeMsg{Width: 34, Height: 9})
	if s := screen(t, m); strings.Contains(strings.Join(s, "\n"), "↶") || !strings.Contains(s[1], "task-000") {
		t.Errorf("short pane:\n%s", strings.Join(s, "\n"))
	}
}

func busyChoice() *ipc.Choice {
	return &ipc.Choice{ID: "a1:undo:busy", Question: `"task-002" is still at work.`, Options: []ipc.ChoiceOption{
		{Key: ipc.ChoiceCancel, Label: "Cancel (change nothing)", Detail: "Leaves everything as it is, the history too.", Recommended: true},
		{Key: ipc.ChoiceApply, Label: "Close it now", Detail: "Stops its agent mid-task."},
		{Key: ipc.ChoiceSkip, Label: "Skip this one (drop it from history)", Detail: "Takes it out of the history."},
	}}
}

// asking is a model whose core asks before the first undo.
func asking(t *testing.T) (Model, *fakeBackend) {
	m, b := newModel(t, 50, 24, 3)
	m = send(m, keyMsg("esc"))
	b.undoReply = func(dir string, a ipc.UndoArgs) (ipc.UndoResult, error) {
		switch a.Choice {
		case "":
			return ipc.UndoResult{Label: `start "task-002"`, Did: ipc.DidAsk, Choice: busyChoice()}, nil
		case ipc.ChoiceSkip:
			return ipc.UndoResult{Label: `start "task-002"`, Did: ipc.DidSkip}, nil
		}
		return ipc.UndoResult{Label: `start "task-002"`, Did: ipc.DidUndo}, nil
	}
	m = send(m, keyMsg("u"))
	if m.menu == nil {
		t.Fatalf("no menu; status %q", m.status)
	}
	return m, b
}

func TestAQuestionShowsAsAMenu(t *testing.T) {
	m, b := asking(t)
	s := screen(t, m)
	text := strings.Join(s, "\n")
	for _, want := range []string{`"task-002" is still at work.`, "1 Cancel (change nothing) (recommended)", "2 Close it now", "3 Skip this one (drop it from", "Enter picks · Esc cancels"} {
		if !strings.Contains(text, want) {
			t.Errorf("the menu has no %q:\n%s", want, text)
		}
	}
	// The highlighted option has an edge, not only a colour, and the status
	// strip says what it will do.
	if !strings.Contains(text, "▌1 Cancel") || !strings.Contains(s[len(s)-2], "Leaves everything as it is") {
		t.Errorf("highlight:\n%s", text)
	}
	for i, l := range s {
		if lipgloss.Width(l) > 50 {
			t.Errorf("line %d is %d columns: %q", i, lipgloss.Width(l), l)
		}
	}
	if len(s) != 24 || !strings.HasPrefix(s[23], barPrompt) {
		t.Errorf("the menu moved the bar:\n%s", text)
	}
	m = send(m, keyMsg("down"))
	if s := screen(t, m); !strings.Contains(strings.Join(s, "\n"), "▌2 Close it now") || !strings.Contains(s[len(s)-2], "Stops its agent mid-task.") {
		t.Errorf("after down:\n%s", strings.Join(s, "\n"))
	}
	// Keys go to the menu: x does not start a close.
	m = send(m, keyMsg("x"))
	if len(b.closes) != 0 || m.menu == nil {
		t.Errorf("x while the menu is open: closes %v", b.closes)
	}
	// A click on a row does nothing while it asks.
	m = send(m, tea.MouseClickMsg{Button: tea.MouseLeft, X: 3, Y: m.listTop()})
	if len(b.shows) != 0 || m.menu == nil {
		t.Errorf("a click while the menu is open: shows %v", b.shows)
	}
	// Enter picks the highlighted one, with the question's id.
	m = send(m, keyMsg("enter"))
	if !reflect.DeepEqual(b.undos, []string{"undo", "undo a1:undo:busy apply"}) || m.menu != nil || m.status != `undid start "task-002"` {
		t.Errorf("calls %v, menu %v, status %q", b.undos, m.menu, m.status)
	}
	if strings.Contains(strings.Join(screen(t, m), "\n"), "Close it now") {
		t.Error("the menu is still drawn")
	}
}

func TestNumbersPickAndEscCancels(t *testing.T) {
	m, b := asking(t)
	m = send(m, keyMsg("3"))
	if !reflect.DeepEqual(b.undos, []string{"undo", "undo a1:undo:busy skip"}) || !strings.Contains(m.status, "dropped start \"task-002\" from the history") {
		t.Errorf("3: calls %v, status %q", b.undos, m.status)
	}
	m, b = asking(t)
	m = send(m, keyMsg("9"))
	if m.menu == nil || len(b.undos) != 1 {
		t.Errorf("a number past the options: menu %v, calls %v", m.menu, b.undos)
	}
	m = send(m, keyMsg("esc"))
	if m.menu != nil || len(b.undos) != 1 || m.status != "cancelled; nothing changed" {
		t.Errorf("esc: menu %v, calls %v, status %q", m.menu, b.undos, m.status)
	}
	// Cancel, picked, changes nothing and needs no word with the core.
	m, b = asking(t)
	m = send(m, keyMsg("1"))
	if m.menu != nil || len(b.undos) != 1 || m.status != "cancelled; nothing changed" {
		t.Errorf("1: menu %v, calls %v, status %q", m.menu, b.undos, m.status)
	}
	// The list has the keys again.
	m = send(m, keyMsg("down"))
	if m.sel != 1 {
		t.Errorf("selection %d after the menu closed", m.sel)
	}
}

func TestAMenuFitsAShortPane(t *testing.T) {
	m, _ := asking(t)
	for _, h := range []int{6, 8, 12} {
		m = send(m, tea.WindowSizeMsg{Width: 34, Height: h})
		s := screen(t, m)
		if len(s) != h || !strings.Contains(s[0], "Irie") || !strings.HasPrefix(s[h-1], barPrompt) {
			t.Errorf("height %d:\n%s", h, strings.Join(s, "\n"))
		}
		if text := strings.Join(s, "\n"); !strings.Contains(text, "Close it now") && m.listHeight() >= 3 {
			t.Errorf("height %d lost the options:\n%s", h, text)
		}
	}
}
