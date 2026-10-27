package ui

// What the sidebar does to a session as a whole, and to its place in the
// list (DESIGN §4.1, §4.3). f freezes or thaws the selected session and F
// every session; ! promotes it to the top or puts it back; a row dragged
// with the mouse goes where it is dropped. The core keeps the one order both
// sidebars draw, so a state change never moves a row. Each change shows at
// once and the core's frames confirm it.

import (
	"cmp"
	"slices"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/irie-hq/irie/internal/ipc"
)

// Controls is what the sidebar asks of the core for these. A Backend
// without it, such as an older one, leaves the keys saying so.
type Controls interface {
	Freeze(id string, on, all bool) error
	Priority(id string, level int, reason string) error
	Reorder(id, before string) error
}

// controlledMsg reports how a control went.
type controlledMsg struct {
	done string // what the status strip says when it worked
	err  error
}

// drag is a row the mouse holds.
type drag struct {
	id    string // the session
	moved bool   // it left its row
}

// styleBadge marks a promoted row.
var styleBadge = lipgloss.NewStyle().Foreground(sun).Bold(true)

// sortByOrder puts the rows in the core's order (DESIGN §4.1). The sort is
// stable, so a core that sends no order keeps the order rows came in.
func (m *Model) sortByOrder() {
	slices.SortStableFunc(m.sessions, func(a, b ipc.Session) int { return cmp.Compare(a.Order, b.Order) })
	if d := m.drag; d != nil && d.moved {
		// The row under the mouse stays under it.
		if i := m.index(d.id); i >= 0 && m.sel >= 0 && m.sel < len(m.sessions) && i != m.sel {
			s := m.sessions[i]
			m.sessions = slices.Insert(slices.Delete(m.sessions, i, i+1), m.sel, s)
		}
	}
}

// renumber gives the rows their places as they now stand.
func (m *Model) renumber() {
	for i := range m.sessions {
		m.sessions[i].Order = i
	}
}

// noControls is what the keys say when the backend lacks Controls.
const noControls = "this Irie cannot freeze, promote or reorder; update it"

func (m Model) controls() (Controls, bool) {
	c, ok := m.backend.(Controls)
	return c, ok
}

// live reports whether a session runs, so that it can freeze.
func live(s ipc.Session) bool {
	return s.State != ipc.StateExited && s.State != ipc.StateFailed
}

// freezeKey is f (all false) or F (all true): it thaws what is frozen, and
// otherwise freezes; F freezes every session unless all are frozen.
func (m Model) freezeKey(all bool) (tea.Model, tea.Cmd) {
	c, ok := m.controls()
	if !ok {
		m.setStatus(noControls, true)
		return m, nil
	}
	if all {
		on := slices.ContainsFunc(m.sessions, func(s ipc.Session) bool { return live(s) && s.State != ipc.StateFrozen })
		done := "froze every session"
		if !on {
			done = "thawed every session"
		}
		return m, func() tea.Msg { return controlledMsg{done: done, err: c.Freeze("", on, true)} }
	}
	if m.sel < 0 || m.sel >= len(m.sessions) {
		m.setStatus("f freezes one of Irie's sessions; select one", true)
		return m, nil
	}
	s := &m.sessions[m.sel]
	on := s.State != ipc.StateFrozen
	if on && !live(*s) {
		m.setStatus(s.Name+" has ended, so there is nothing to freeze", true)
		return m, nil
	}
	id, done := s.ID, "froze "+s.Name+"; f thaws it"
	if on {
		s.State = ipc.StateFrozen
	} else {
		done = "thawed " + s.Name
	}
	return m, func() tea.Msg { return controlledMsg{done: done, err: c.Freeze(id, on, false)} }
}

// promoteKey is !: it promotes the selected session to the top, or puts a
// promoted one back among the rest, where it stays. A reason, from :prio,
// goes with a promotion.
func (m Model) promoteKey(reason string) (tea.Model, tea.Cmd) {
	c, ok := m.controls()
	if !ok {
		m.setStatus(noControls, true)
		return m, nil
	}
	if m.sel < 0 || m.sel >= len(m.sessions) {
		m.setStatus("! promotes one of Irie's sessions; select one", true)
		return m, nil
	}
	s := m.sessions[m.sel]
	level, done := 1, "promoted "+s.Name+": it has the machine first; ! puts it back"
	if s.Priority > 0 {
		level, done = 0, s.Name+" is back among the rest"
	} else {
		m.sessions = slices.Insert(slices.Delete(m.sessions, m.sel, m.sel+1), 0, s)
		m.sel = 0
		m.renumber()
		m.reveal()
	}
	m.sessions[m.sel].Priority = level
	return m, func() tea.Msg { return controlledMsg{done: done, err: c.Priority(s.ID, level, reason)} }
}

// listRow is the session row at screen row y, if one is there.
func (m Model) listRow(y int) (int, bool) {
	row := y - m.listTop()
	if row < 0 || row >= m.listHeight() {
		return 0, false
	}
	i := m.offset + row
	return i, i < len(m.sessions)
}

// press takes hold of session row i: a click opens it when the button
// comes up where it went down, and a drag carries it elsewhere.
func (m *Model) press(i int) {
	m.sel = i
	m.setFocus(focusList)
	m.drag = &drag{id: m.sessions[i].ID}
}

// dragTo carries the held row to the row under the mouse, among Irie's
// sessions.
func (m *Model) dragTo(y int) {
	d := m.drag
	if d == nil {
		return
	}
	from := m.index(d.id)
	if from < 0 {
		m.drag = nil
		return
	}
	row := min(max(y-m.listTop(), 0), max(m.listHeight()-1, 0))
	to := min(m.offset+row, len(m.sessions)-1)
	if to == from {
		return
	}
	s := m.sessions[from]
	m.sessions = slices.Insert(slices.Delete(m.sessions, from, from+1), to, s)
	m.sel, d.moved = to, true
	m.setStatus("drop "+s.Name+" where it goes", false)
}

// release ends a press: a row dragged elsewhere stays there, and the core
// is told; one that never left its row opens, as a click does.
func (m Model) release() (tea.Model, tea.Cmd) {
	d := m.drag
	m.drag = nil
	if d == nil {
		return m, nil
	}
	i := m.index(d.id)
	if i < 0 {
		return m, nil
	}
	if !d.moved {
		m.sel = i
		cmd := m.open(true)
		return m, cmd
	}
	c, ok := m.controls()
	if !ok {
		m.setStatus(noControls, true)
		return m, nil
	}
	before := ""
	if i+1 < len(m.sessions) {
		before = m.sessions[i+1].ID
	}
	m.renumber()
	name := m.sessions[i].Name
	m.setStatus("", false)
	return m, func() tea.Msg { return controlledMsg{done: "moved " + name, err: c.Reorder(d.id, before)} }
}

func (m *Model) controlled(msg controlledMsg) {
	if msg.err != nil {
		m.setStatus(msg.err.Error(), true)
		return
	}
	m.setStatus(msg.done, false)
}

// controlCommand runs :freeze [all], :thaw [all] and :prio [reason], and
// reports whether it was one of them.
func (m Model) controlCommand(name, arg string) (tea.Model, tea.Cmd, bool) {
	switch {
	case (name == "freeze" || name == "thaw") && (arg == "" || arg == "all"):
		on := name == "freeze"
		if arg == "all" {
			c, ok := m.controls()
			if !ok {
				m.setStatus(noControls, true)
				return m, nil, true
			}
			m.bar.Reset()
			return m, func() tea.Msg { return controlledMsg{done: name + " every session: done", err: c.Freeze("", on, true)} }, true
		}
		if m.sel < 0 || m.sel >= len(m.sessions) || (m.sessions[m.sel].State == ipc.StateFrozen) == on {
			what := "frozen"
			if on {
				what = "running"
			}
			m.setStatus(":"+name+" takes the selected session, which must be "+what+"; or :"+name+" all", true)
			return m, nil, true
		}
		m.bar.Reset()
		model, cmd := m.freezeKey(false)
		return model, cmd, true
	case name == "prio":
		m.bar.Reset()
		model, cmd := m.promoteKey(arg)
		return model, cmd, true
	}
	return m, nil, false
}

// frozenLine is what the status strip says first about a frozen or promoted
// session, if anything.
func frozenLine(s ipc.Session) string {
	var out string
	switch {
	case s.State == ipc.StateFrozen && s.Freezing:
		out = "frozen; its agent stops at its next tool call (it started before Irie could freeze it whole); f thaws"
	case s.State == ipc.StateFrozen:
		out = "frozen; f thaws it"
	}
	if s.Priority > 0 {
		if out != "" {
			out += " · "
		}
		out += "promoted: it has the machine first"
	}
	return out
}

func (b socketBackend) Freeze(id string, on, all bool) error {
	op := ipc.OpThaw
	if on {
		op = ipc.OpFreeze
	}
	return ipc.Call(b.socket, callTimeout, op, ipc.FreezeArgs{Session: id, All: all}, nil)
}

func (b socketBackend) Priority(id string, level int, reason string) error {
	return ipc.Call(b.socket, callTimeout, ipc.OpPriority, ipc.PriorityArgs{Session: id, Level: level, Reason: reason}, nil)
}

func (b socketBackend) Reorder(id, before string) error {
	return ipc.Call(b.socket, callTimeout, ipc.OpReorder, ipc.ReorderArgs{Session: id, Before: before}, nil)
}
