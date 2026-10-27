package ui

// The Outside Irie group (DESIGN §4.6): Claude Code sessions that run in
// other terminals, listed after Irie's own rows under a header with their
// count. Each is one line, busy or idle by its glyph alone, in the order
// they started, so a change of state never moves one. The selected one's
// details go in the status strip. m m, or :move, moves an idle one in.

import (
	"errors"
	"fmt"
	"image/color"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/irie-hq/irie/internal/ipc"
)

type movedMsg struct {
	name    string
	session ipc.Session
	err     error
}

// groupKey names the Outside Irie header when the selection is on it.
const groupKey = "\x00outside"

// rowCount is how many list rows there are: Irie's sessions, then the
// Outside Irie header and its rows when there are any.
func (m Model) rowCount() int {
	if len(m.outside) == 0 {
		return len(m.sessions)
	}
	return len(m.sessions) + 1 + len(m.outside)
}

func (m Model) isGroupHeader(i int) bool { return len(m.outside) > 0 && i == len(m.sessions) }

// outsideAt returns the outside session on list row i.
func (m Model) outsideAt(i int) (ipc.OutsideSession, bool) {
	if j := i - len(m.sessions) - 1; len(m.outside) > 0 && j >= 0 && j < len(m.outside) {
		return m.outside[j], true
	}
	return ipc.OutsideSession{}, false
}

// outsideKey names list row i when it belongs to the group, else "".
func (m Model) outsideKey(i int) string {
	if m.isGroupHeader(i) {
		return groupKey
	}
	if o, ok := m.outsideAt(i); ok {
		return o.Key()
	}
	return ""
}

// outsideIndex is the list row that key names, or -1.
func (m Model) outsideIndex(key string) int {
	if key == groupKey && len(m.outside) > 0 {
		return len(m.sessions)
	}
	for j, o := range m.outside {
		if key != "" && o.Key() == key {
			return len(m.sessions) + 1 + j
		}
	}
	return -1
}

// setOutside takes an outside frame. A problem shows in the status strip
// once, when it is new.
func (m *Model) setOutside(f ipc.Frame) {
	m.outside = slices.Clone(f.Outside)
	if f.Problem != m.outsideProblem {
		m.outsideProblem = f.Problem
		if f.Problem != "" {
			m.setStatus(f.Problem, true)
		}
	}
}

// line draws list row i, whichever kind it is.
func (m Model) line(i int) string {
	switch {
	case i < len(m.sessions):
		return m.row(i)
	case m.isGroupHeader(i):
		return m.groupHeader(i)
	}
	o, _ := m.outsideAt(i)
	g, c, dim, loud := m.outsideGlyph(o)
	a := ""
	if !o.Started.IsZero() {
		a = age(m.now().Sub(o.Started))
	}
	return m.paint(i, g, c, dim, loud, outsideLabel(o), a)
}

// groupHeader draws the group's title and count, then a rule.
func (m Model) groupHeader(i int) string {
	edge, st := " ", styleDim
	if i == m.sel {
		edge, st = styleTitle.Render("▌"), styleBold
		if m.focus != focusList {
			edge = styleTitle.Faint(true).Render("▌")
		}
	}
	title := cut(fmt.Sprintf("Outside Irie %d", len(m.outside)), m.width-2)
	out := edge + st.Render(title)
	if rule := m.width - lipgloss.Width(title) - 3; rule > 0 {
		out += " " + styleRule.Render(strings.Repeat("─", rule)) + " "
	}
	return out
}

// outsideGlyph draws an outside row as the Irie state it matches: busy as
// working, idle as idle, waiting for the user as needs you. Anything else is
// a dim dot.
func (m Model) outsideGlyph(o ipc.OutsideSession) (glyph string, c color.Color, dim, loud bool) {
	switch o.Status {
	case ipc.StatusBusy:
		return m.state(ipc.Session{State: ipc.StateWorking})
	case ipc.StatusIdle:
		return m.state(ipc.Session{State: ipc.StateIdle})
	case ipc.StatusWaiting:
		return m.state(ipc.Session{State: ipc.StateNeedsYou})
	}
	return "·", nil, true, false
}

// outsideLabel is the name an outside row shows.
func outsideLabel(o ipc.OutsideSession) string {
	switch {
	case o.Name != "":
		return printable(o.Name)
	case o.Cwd != "":
		return printable(filepath.Base(o.Cwd))
	}
	return o.Key()
}

// where says how an outside session runs.
func where(o ipc.OutsideSession) string {
	switch {
	case o.Kind == ipc.KindBackground && o.ID != "":
		return "in the background as " + o.ID
	case o.Kind == ipc.KindBackground:
		return "in the background"
	case o.PID != 0:
		return fmt.Sprintf("in a terminal, pid %d", o.PID)
	}
	return "in a terminal"
}

// outsideStatus is the status strip for the group's header or one of its
// rows: what it does, its name, where it runs, and whether m moves it.
func (m Model) outsideStatus() string {
	if m.isGroupHeader(m.sel) {
		return " " + styleDim.Render(cut("Claude Code sessions in other terminals · m moves an idle one in", m.width-2))
	}
	o, _ := m.outsideAt(m.sel)
	var doing string
	switch {
	case o.Movable:
		doing = "idle · m moves it in"
	case o.Status == ipc.StatusWaiting && o.WaitingFor != "":
		doing = "waiting for " + o.WaitingFor
	case o.Status == "":
		doing = "not running"
	default:
		doing = o.Status
	}
	parts := []string{doing, outsideLabel(o)}
	if o.Cwd != "" {
		parts = append(parts, printable(m.home2tilde(o.Cwd)))
	}
	parts = append(parts, where(o))
	st := styleDim
	if _, c, _, loud := m.outsideGlyph(o); loud {
		st = lipgloss.NewStyle().Foreground(c)
	}
	return " " + st.Render(cut(strings.Join(parts, " · "), m.width-2))
}

// open shows the selected session; for the group, which Irie cannot show,
// it says why and what m does instead.
func (m *Model) open(focus bool) tea.Cmd {
	if m.sel >= 0 && m.sel < len(m.sessions) {
		return m.showCmd(focus)
	}
	if m.isGroupHeader(m.sel) {
		m.setStatus("These Claude Code sessions run outside Irie; m moves an idle one in", false)
		return nil
	}
	if o, ok := m.outsideAt(m.sel); ok {
		msg := outsideLabel(o) + " runs outside Irie, " + where(o)
		switch {
		case o.Movable:
			msg += "; m moves it in here"
		case o.Why != "":
			msg += "; " + o.Why
		}
		m.setStatus(msg, false)
	}
	return nil
}

// moveConfirm is how long a first m waits for the second.
const moveConfirm = 5 * time.Second

// moveSelected is m on a row. A move ends the outside copy and anything it
// runs in the background, so it takes a second m, as closing takes x x
// (DESIGN §4.1); any other key in between cancels it.
func (m *Model) moveSelected() tea.Cmd {
	o, ok := m.outsideAt(m.sel)
	if !ok {
		m.setStatus("m moves a session from Outside Irie into Irie", false)
		return nil
	}
	if !o.Movable || m.moving {
		m.confirming = ""
		return m.moveCmd(o)
	}
	if m.confirming != o.Key() || m.now().Sub(m.confirmedAt) > moveConfirm {
		m.confirming, m.confirmedAt = o.Key(), m.now()
		m.setStatus("Press m again to move "+outsideLabel(o)+" into Irie. "+endsWarning, false)
		return nil
	}
	m.confirming = ""
	return m.moveCmd(o)
}

// endsWarning says what a move ends.
const endsWarning = "Its copy outside Irie ends, and anything it runs in the background stops."

// moveCmd asks the core to move o in, when it can move.
func (m *Model) moveCmd(o ipc.OutsideSession) tea.Cmd {
	name := outsideLabel(o)
	switch {
	case m.moving:
		return nil
	case !o.Movable:
		m.setStatus(name+" cannot move yet: "+o.Why, true)
		return nil
	}
	m.moving = true
	m.setStatus("moving "+name+" into Irie…", false)
	b, id := m.backend, o.VendorID
	return func() tea.Msg {
		s, err := b.Move(id, "")
		return movedMsg{name: name, session: s, err: err}
	}
}

// moved selects the session a move made, once its row is in.
func (m *Model) moved(msg movedMsg) {
	m.moving = false
	if msg.err != nil {
		m.setStatus(msg.err.Error(), true)
		return
	}
	m.setStatus("moved "+msg.name+" into Irie", false)
	m.pending = msg.session.ID
	if i := m.index(msg.session.ID); i >= 0 {
		m.moveTo(i)
		m.pending = ""
	}
}

// barCommand runs the bar's input when it is a command this version knows,
// `:move [name]`, and reports whether it was.
func (m *Model) barCommand() (tea.Cmd, bool) {
	line := strings.TrimSpace(m.bar.Value())
	cmd, arg, _ := strings.Cut(strings.TrimPrefix(line, ":"), " ")
	if !strings.HasPrefix(line, ":") || cmd != "move" || strings.Contains(line, "\n") {
		return nil, false
	}
	o, err := m.pickOutside(strings.TrimSpace(arg))
	if err != nil {
		m.setStatus(err.Error(), true)
		return nil, true
	}
	m.bar.Reset()
	m.chips = nil
	m.setFocus(focusList)
	return m.moveCmd(o), true
}

// pickOutside finds the outside session that :move names: by its id, short
// id or name, or the selected one when it names none.
func (m Model) pickOutside(what string) (ipc.OutsideSession, error) {
	if what == "" {
		if o, ok := m.outsideAt(m.sel); ok {
			return o, nil
		}
		return ipc.OutsideSession{}, errors.New(":move takes the selected session under Outside Irie, or one you name")
	}
	var named []ipc.OutsideSession
	for _, o := range m.outside {
		switch {
		case o.VendorID == what || o.ID == what:
			return o, nil
		case o.Name == what:
			named = append(named, o)
		}
	}
	switch len(named) {
	case 0:
		return ipc.OutsideSession{}, fmt.Errorf("no session %q outside Irie", what)
	case 1:
		return named[0], nil
	}
	return ipc.OutsideSession{}, fmt.Errorf("%d sessions outside Irie are named %q; select one and press m", len(named), what)
}

// printable replaces control characters, which would steer the terminal.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '?'
		}
		return r
	}, s)
}
