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
	"image/color"
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
		// The menu takes the mouse out of play: a card it carried goes back.
		if d := m.drag; d != nil {
			m.drag = nil
			if d.moved {
				m.sortByOrder()
				m.sel = m.sessionIndex(d.id)
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
		return m.answer(mn.at)
	case "esc", "q":
		m.menu = nil
		m.setStatus("cancelled; nothing changed", false)
	default:
		if len(k) == 1 && k[0] >= '1' && int(k[0]-'1') < n {
			return m.answer(int(k[0] - '1'))
		}
	}
	return m, nil
}

// answer answers the question with option i. Cancel needs no word with the
// core: nothing changes.
func (m Model) answer(i int) (tea.Model, tea.Cmd) {
	mn := m.menu
	m.menu = nil
	o := mn.choice.Options[i]
	if o.Key == ipc.ChoiceCancel {
		m.setStatus("cancelled; nothing changed", false)
		return m, nil
	}
	return m, m.undoCmd(mn.dir, mn.choice.ID, o.Key)
}

// historyLine is the header's second line, `↶ close "api-fix"  ↷ –`, on
// the title bar's ground: what u and U would do, a dash for nothing. Long
// labels are cut, each to its share.
func (m Model) historyLine() string {
	p := m.pal
	band := func(fg color.Color) lipgloss.Style { return on(p.ground, fg) }
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
			return band(p.muted).Render("–")
		}
		return band(p.ink).Render(cut(label, w))
	}
	line := blank(p.ground, 1) + band(p.accentInk).Render("↶") + blank(p.ground, 1) + show(undo, uw) +
		blank(p.ground, 2) + band(p.accentInk).Render("↷") + blank(p.ground, 1) + show(redo, rw)
	if lipgloss.Width(line) > m.width {
		line = blank(p.ground, 1) + band(p.ink).Render(cut("↶ "+undo, m.width-1))
	}
	return line + blank(p.ground, m.width-lipgloss.Width(line))
}

// menuLines draws the menu in at most h lines. With room it is a box edged
// in the accent, as what has the keyboard is: the question, wrapped to two
// lines when there is room, the numbered options, the highlighted one on the
// selection fill with an edge and the recommended one saying so, then a hint
// when it fits. Without room for the box the options come first, the
// highlighted one always among them.
func (m Model) menuLines(h int) []string {
	p, mn := m.pal, m.menu
	n := len(mn.choice.Options)
	boxed := h >= n+3
	w := m.width - 2
	line := func(fill color.Color, ps ...piece) string {
		if boxed {
			return m.padded(p.accent, fill, ps...)
		}
		return blank(p.sidebar, 1) + spread(fill, m.width-1, ps...)
	}
	if boxed {
		w = m.width - 6
	}
	var opts []string
	for i, o := range mn.choice.Options {
		ps := []piece{{fmt.Sprintf("%d ", i+1), p.muted, false}, {o.Label, p.ink, i == mn.at}}
		if o.Recommended {
			ps = append(ps, piece{" (recommended)", p.muted, false})
		}
		switch {
		case i != mn.at:
			opts = append(opts, line(p.panel, ps...))
		case boxed:
			opts = append(opts, m.boxLine(p.accent, on(p.selection, p.accent).Render("▌")+spread(p.selection, m.width-5, ps...)))
		default:
			opts = append(opts, m.text(p.accent).Render("▌")+spread(p.selection, m.width-1, ps...))
		}
	}
	if !boxed {
		if n+1 <= h {
			return append([]string{line(p.sidebar, piece{mn.choice.Question, p.ink, true})}, opts...)
		}
		// Too short for the question: the options that fit, the highlighted
		// one among them.
		from := min(max(mn.at-h+1, 0), max(n-h, 0))
		return opts[from:min(from+h, n)]
	}
	question := wrap(mn.choice.Question, w)
	if len(question) > 2 || len(question)+n+2 > h {
		question = []string{cut(mn.choice.Question, w)}
	}
	lines := []string{m.boxTop(p.accent, "")}
	for _, q := range question {
		lines = append(lines, line(p.panel, piece{q, p.ink, true}))
	}
	lines = append(lines, opts...)
	if len(lines)+2 <= h {
		lines = append(lines, line(p.panel, piece{fmt.Sprintf("1–%d or Enter picks · Esc cancels", n), p.muted, false}))
	}
	return append(lines, m.boxBottom(p.accent))
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
