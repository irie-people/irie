package ui

import (
	"errors"
	"fmt"
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
	if got := statusText(t, m); !strings.Contains(got, "cannot reopen") {
		t.Errorf("status strip %q", got)
	}
}

func TestHeaderShowsWhatUndoAndRedoWouldDo(t *testing.T) {
	m, _ := newModel(t, 34, 24, 3)
	if got := strings.TrimRight(screen(t, m)[1], " "); got != " ↶ –  ↷ –" {
		t.Errorf("history line before any action %q", got)
	}
	m = send(m, historyFrame(`close "api-fix"`, ""))
	if got := strings.TrimRight(screen(t, m)[1], " "); got != ` ↶ close "api-fix"  ↷ –` {
		t.Errorf("history line %q", got)
	}
	// It is the title bar's second line, on its ground, the glyphs in the
	// accent's ink and nothing to do faint.
	c := cells(strings.Split(m.View().Content, "\n")[1])
	for _, x := range c {
		if x.bg != rgb(m.pal.ground) {
			t.Errorf("history line: %q is on %s, not the title bar's ground", x.r, x.bg)
			break
		}
	}
	if c[1].fg != rgb(m.pal.accentInk) || c[3].fg != rgb(m.pal.ink) || c[22].r != "–" || c[22].fg != rgb(m.pal.muted) {
		t.Errorf("history line colours: ↶ %s, label %s, %q %s", c[1].fg, c[3].fg, c[22].r, c[22].fg)
	}
	painted(t, "history line", m)
	// The rows start below the header, its line and the tabs.
	if s := screen(t, m); m.listTop() != 3 || !strings.Contains(s[2], "List │ Repo") || !strings.Contains(s[3], "Not in a repo") {
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
	if got := strings.TrimRight(screen(t, m)[1], " "); got != ` ↶ start "web"  ↷ close "a-very-long-session-name-indeed"` {
		t.Errorf("wide history line %q", got)
	}
	// A short pane has no room for it, nor for the tabs.
	m = send(m, tea.WindowSizeMsg{Width: 34, Height: 9})
	if s := screen(t, m); strings.Contains(strings.Join(s, "\n"), "↶") || m.listTop() != 1 || !strings.Contains(s[1], "Not in a repo") {
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
	// strip, here the details card, says what it will do.
	if !strings.Contains(text, "▌1 Cancel") || !strings.Contains(statusText(t, m), "Leaves everything as it is") {
		t.Errorf("highlight:\n%s", text)
	}
	// It is a box at the bottom of the list, edged in the accent as what has
	// the keyboard is, the highlighted option on the selection fill.
	raw := strings.Split(m.View().Content, "\n")
	bottom := m.listTop() + m.listHeight() - 1
	if !strings.HasPrefix(s[bottom], " ╰") || cells(raw[bottom])[1].fg != rgb(m.pal.accent) {
		t.Errorf("the list's last line %q is not the menu's bottom edge in the accent", s[bottom])
	}
	if c := cells(raw[lineOf(t, m, "▌1 Cancel")]); c[2].fg != rgb(m.pal.accent) || c[4].bg != rgb(m.pal.selection) || !c[5].bold {
		t.Errorf("highlight: edge %s, fill %s, bold %v", c[2].fg, c[4].bg, c[5].bold)
	}
	painted(t, "the menu", m)
	if bar := barText(t, m); len(s) != 24 || !strings.Contains(bar[0], barPrompt) {
		t.Errorf("the menu moved the bar:\n%s", text)
	}
	m = send(m, keyMsg("down"))
	if s := screen(t, m); !strings.Contains(strings.Join(s, "\n"), "▌2 Close it now") || !strings.Contains(statusText(t, m), "Stops its agent mid-task.") {
		t.Errorf("after down:\n%s", strings.Join(s, "\n"))
	}
	// Keys go to the menu: x does not start a close.
	m = send(m, keyMsg("x"))
	if len(b.closes) != 0 || m.menu == nil {
		t.Errorf("x while the menu is open: closes %v", b.closes)
	}
	// A click on a card does nothing while it asks: nor does the button
	// coming up, which opens a card when the menu is not there.
	y := lineOf(t, m, "task-000")
	m = send(m, tea.MouseClickMsg{Button: tea.MouseLeft, X: 3, Y: y})
	m = send(m, tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 3, Y: y})
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
	sel := m.sel
	m = send(m, keyMsg("down"))
	if m.sel != sel+1 {
		t.Errorf("selection %d after the menu closed, want %d", m.sel, sel+1)
	}
}

func TestAMenuFitsAShortPane(t *testing.T) {
	m, _ := asking(t)
	for _, h := range []int{6, 8, 12, 16, 30} {
		m = send(m, tea.WindowSizeMsg{Width: 34, Height: h})
		s := screen(t, m)
		if len(s) != h || !strings.Contains(s[0], "Irie") || !strings.Contains(barText(t, m)[0], barPrompt) {
			t.Errorf("height %d:\n%s", h, strings.Join(s, "\n"))
		}
		painted(t, fmt.Sprintf("the menu at height %d", h), m)
		if text := strings.Join(s, "\n"); !strings.Contains(text, "Close it now") && m.listHeight() >= 3 {
			t.Errorf("height %d lost the options:\n%s", h, text)
		}
		// Above the menu only whole cards show, so every box is whole.
		if text := strings.Join(s, "\n"); strings.Count(text, "╭") != strings.Count(text, "╰") {
			t.Errorf("height %d: a card cut short above the menu:\n%s", h, text)
		}
	}
}

// A question that comes while the mouse carries a card takes the mouse out
// of play: the card goes back to its place, and the button coming up opens
// nothing and moves nothing.
func TestAMenuLetsGoOfADrag(t *testing.T) {
	rows := sessions(4)
	for i := range rows {
		rows[i].Order = i
	}
	m, b := ctlAt(t, rows, 34, 12) // one-line rows
	from, to := lineOf(t, m, "task-000"), lineOf(t, m, "task-001")
	m = send(m, tea.MouseClickMsg{X: 3, Y: from, Button: tea.MouseLeft})
	m = send(m, tea.MouseMotionMsg{X: 3, Y: to, Button: tea.MouseLeft})
	if got := order(m); got[0] != "s000000000001" {
		t.Fatalf("while dragging: %v", got)
	}
	m = send(m, undoneMsg{dir: dirUndo, r: ipc.UndoResult{Label: `start "task-002"`, Did: ipc.DidAsk, Choice: busyChoice()}})
	if got := order(m); m.menu == nil || m.drag != nil || got[0] != "s000000000000" || selectedName(m) != "task-000" {
		t.Errorf("menu %v, drag %v, order %v, selection %s", m.menu != nil, m.drag, got, selectedName(m))
	}
	m = send(m, tea.MouseReleaseMsg{X: 3, Y: to, Button: tea.MouseLeft})
	if len(b.calls) != 0 || len(b.shows) != 0 || m.menu == nil {
		t.Errorf("the release: calls %v, shows %v", b.calls, b.shows)
	}
}
