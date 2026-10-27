package ui

// Undo and redo (DESIGN §4.1, §4.5). u undoes and U redoes, as :undo and
// :redo do; the header's second line says what each would do next. When the
// core is not sure, it asks: a small menu over the bottom of the list, one
// question and numbered options, the recommended one first. 1 to 4 or the
// arrows and Enter pick, Esc cancels, and the status strip says what the
// highlighted option will do.

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/irie-hq/irie/internal/ipc"
)

// Directions, as the ops name them.
const (
	dirUndo = ipc.OpUndo
	dirRedo = ipc.OpRedo
)

type undoneMsg struct {
	dir string
	r   ipc.UndoResult
	err error
}

// choiceMenu is a question the core asked about an undo or redo.
type choiceMenu struct {
	dir    string
	choice ipc.Choice
	at     int // the highlighted option
}

// undoCmd asks the core to undo or redo, by dir, answering a question when
// choiceID is set.
func (m *Model) undoCmd(dir, choiceID, key string) tea.Cmd {
	b := m.backend
	a := ipc.UndoArgs{ChoiceID: choiceID, Choice: key}
	if dir == dirUndo {
		m.setStatus("undoing…", false)
	} else {
		m.setStatus("redoing…", false)
	}
	return func() tea.Msg {
		var r ipc.UndoResult
		var err error
		if dir == dirUndo {
			r, err = b.Undo(a)
		} else {
			r, err = b.Redo(a)
		}
		return undoneMsg{dir: dir, r: r, err: err}
	}
}

// undone says what an undo or redo did, or opens the menu when the core
// asks.
func (m *Model) undone(msg undoneMsg) {
	if msg.err != nil {
		// Nothing to undo is news, not a failure.
		var e *ipc.Error
		m.setStatus(msg.err.Error(), !(errors.As(msg.err, &e) && e.Code == ipc.CodeNotFound))
		return
	}
	r := msg.r
	past := map[string]string{dirUndo: "undone", dirRedo: "redone"}[msg.dir]
	switch r.Did {
	case ipc.DidAsk:
		if r.Choice == nil || len(r.Choice.Options) == 0 {
			m.setStatus("Irie asked about "+r.Label+" but offered nothing to pick", true)
			return
		}
		m.menu = &choiceMenu{dir: msg.dir, choice: *r.Choice}
		for i, o := range r.Choice.Options {
			if o.Recommended {
				m.menu.at = i
				break
			}
		}
		m.setStatus("", false)
	case ipc.DidUndo:
		m.setStatus("undid "+r.Label, false)
	case ipc.DidRedo:
		m.setStatus("redid "+r.Label, false)
	case ipc.DidSettle:
		m.setStatus(fmt.Sprintf("%s counted as %s; nothing needed to change", r.Label, past), false)
	case ipc.DidSkip:
		m.setStatus("dropped "+r.Label+" from the history; nothing else changed", false)
	default:
		m.setStatus("nothing changed", false)
	}
}

// menuKey takes every key while the menu is open.
func (m Model) menuKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	mn := m.menu
	n := len(mn.choice.Options)
	switch k := msg.String(); k {
	case "up", "k":
		mn.at = max(mn.at-1, 0)
	case "down", "j", "tab":
		mn.at = min(mn.at+1, n-1)
	case "enter", "space":
		return m.pick(mn.at)
	case "esc", "q":
		m.menu = nil
		m.setStatus("cancelled; nothing changed", false)
	default:
		if len(k) == 1 && k[0] >= '1' && int(k[0]-'1') < n {
			return m.pick(int(k[0] - '1'))
		}
	}
	return m, nil
}

// pick answers the question with option i. Cancel needs no word with the
// core: nothing changes.
func (m Model) pick(i int) (tea.Model, tea.Cmd) {
	mn := m.menu
	m.menu = nil
	o := mn.choice.Options[i]
	if o.Key == ipc.ChoiceCancel {
		m.setStatus("cancelled; nothing changed", false)
		return m, nil
	}
	return m, m.undoCmd(mn.dir, mn.choice.ID, o.Key)
}

// historyLine is the header's second line, `↶ close "api-fix"  ↷ –`: what u
// and U would do, a dash for nothing. Long labels are cut, each to its share.
func (m Model) historyLine() string {
	undo, redo := m.undoLabel, m.redoLabel
	room := m.width - 2 - lipgloss.Width("↶   ↷ ")
	uw, rw := lipgloss.Width(undo), lipgloss.Width(redo)
	if undo == "" {
		uw = 1
	}
	if redo == "" {
		rw = 1
	}
	if uw+rw > room {
		half := max(room/2, 1)
		switch {
		case uw <= half:
			rw = room - uw
		case rw <= half:
			uw = room - rw
		default:
			uw, rw = half, room-half
		}
	}
	show := func(label string, w int) string {
		if label == "" {
			return styleDim.Render("–")
		}
		return cut(label, w)
	}
	glyph := lipgloss.NewStyle().Foreground(sun)
	line := " " + glyph.Render("↶") + " " + show(undo, uw) + "  " + glyph.Render("↷") + " " + show(redo, rw)
	if lipgloss.Width(line) > m.width {
		return cut(" ↶ "+undo, m.width)
	}
	return line
}

// menuLines draws the menu in at most h lines: the question, wrapped to two
// lines when there is room, the numbered options, the highlighted one marked
// with an edge and the recommended one saying so, then a hint when it fits.
func (m Model) menuLines(h int) []string {
	mn := m.menu
	w := m.width - 2
	var opts []string
	for i, o := range mn.choice.Options {
		text := fmt.Sprintf("%d %s", i+1, o.Label)
		if o.Recommended {
			text += " (recommended)"
		}
		text = cut(text, w)
		if i == mn.at {
			bar := lipgloss.NewStyle().Background(deepGreen)
			opts = append(opts, bar.Foreground(sun).Render("▌")+bar.Foreground(onGreen).Render(text+spaces(w-lipgloss.Width(text)))+bar.Render(" "))
		} else {
			opts = append(opts, "  "+text)
		}
	}
	question := wrap(mn.choice.Question, w)
	if len(question) > 2 || len(question)+len(opts) > h {
		question = []string{cut(mn.choice.Question, w)}
	}
	var lines []string
	for _, q := range question {
		lines = append(lines, " "+styleBold.Render(q))
	}
	lines = append(lines, opts...)
	if hint := " " + styleDim.Render(cut("1–"+fmt.Sprint(len(opts))+" or Enter picks · Esc cancels", w)); len(lines) < h {
		lines = append(lines, hint)
	}
	if len(lines) > h {
		// Too short for all of it: the options matter most.
		lines = lines[max(0, len(lines)-h):]
	}
	return lines
}

// wrap breaks s into lines of at most w columns at spaces.
func wrap(s string, w int) []string {
	var out []string
	line := ""
	for _, word := range strings.Fields(s) {
		switch {
		case line == "":
			line = word
		case lipgloss.Width(line+" "+word) <= w:
			line += " " + word
		default:
			out = append(out, line)
			line = word
		}
	}
	if line != "" {
		out = append(out, line)
	}
	for i := range out {
		out[i] = cut(out[i], w)
	}
	return out
}
