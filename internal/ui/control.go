package ui

// What the sidebar does to a session as a whole, and to its place in the
// list (DESIGN §4.1, §4.3). f freezes or thaws the selected session and F
// every session; ! promotes it to the top or puts it back; a card or row
// dragged with the mouse goes where it is dropped, among the others of its
// group. The core keeps the one order both sidebars draw, so a state change
// never moves a row. Each change shows at once and the core's frames
// confirm it.

import (
	"cmp"
	"slices"

	tea "charm.land/bubbletea/v2"

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

// drag is what the mouse holds: the entry the button went down on, and when
// that is one of Irie's sessions, where a drag has carried it.
type drag struct {
	key   string // the entry pressed
	id    string // its session; "" for a header or a row outside Irie
	group string // the session's group, which it moves within
	at    int    // where it is in m.sessions while it moves
	moved bool   // it left its place
}

// sortByOrder puts the rows in the core's order (DESIGN §4.1). The sort is
// stable, so a core that sends no order keeps the order rows came in.
func (m *Model) sortByOrder() {
	slices.SortStableFunc(m.sessions, func(a, b ipc.Session) int { return cmp.Compare(a.Order, b.Order) })
	if d := m.drag; d != nil && d.moved {
		// The card under the mouse stays under it.
		if i := m.index(d.id); i >= 0 && i != d.at {
			s := m.sessions[i]
			m.sessions = slices.Insert(slices.Delete(m.sessions, i, i+1), min(d.at, len(m.sessions)-1), s)
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
	sel, ok := m.selSession()
	if !ok {
		m.setStatus("f freezes one of Irie's sessions; select one", true)
		return m, nil
	}
	s := &m.sessions[m.index(sel.ID)]
	on := s.State != ipc.StateFrozen
	if on && !live(*s) {
		m.setStatus(s.Name+" has ended, so there is nothing to freeze", true)
		return m, nil
	}
	id, done := s.ID, "froze "+s.Name+"; f thaws it"
	if on {
		// It shows frozen at once. In the State view that moves it to
		// Working; the selection goes with it.
		s.State = ipc.StateFrozen
		m.sel = m.sessionIndex(id)
	} else {
		done = "thawed " + s.Name
	}
	return m, func() tea.Msg { return controlledMsg{done: done, err: c.Freeze(id, on, false)} }
}

// promoteKey is !: it promotes the selected session to the top of its
// group, or puts a promoted one back among the rest, where it stays. A
// reason, from :prio, goes with a promotion.
func (m Model) promoteKey(reason string) (tea.Model, tea.Cmd) {
	c, ok := m.controls()
	if !ok {
		m.setStatus(noControls, true)
		return m, nil
	}
	s, ok := m.selSession()
	if !ok {
		m.setStatus("! promotes one of Irie's sessions; select one", true)
		return m, nil
	}
	i := m.index(s.ID)
	level, done := 1, "promoted "+s.Name+": it has the machine first; ! puts it back"
	if s.Priority > 0 {
		level, done = 0, s.Name+" is back among the rest"
		m.sessions[i].Priority = 0
	} else {
		// The core puts it first in its one order, so it heads its group.
		s.Priority = 1
		m.sessions = slices.Insert(slices.Delete(m.sessions, i, i+1), 0, s)
		m.renumber()
		m.moveTo(m.sessionIndex(s.ID))
	}
	return m, func() tea.Msg { return controlledMsg{done: done, err: c.Priority(s.ID, level, reason)} }
}

// press takes hold of entry i: a click opens it when the button comes up
// on it, and a session's card or row can be dragged to another place in its
// group.
func (m *Model) press(i int) {
	it := m.items()[i]
	m.sel = i
	m.setFocus(focusList)
	d := &drag{key: it.key}
	if it.kind == itemSession {
		d.id, d.group, d.at = m.sessions[it.s].ID, it.group.key, it.s
	}
	m.drag = d
}

// dragTo carries the held session to where the pointer is, among the
// sessions of its group: past each card it crosses. Anywhere else it waits
// where it is. It moves only when it would land under the pointer, so
// cards of different heights never trade places back and forth.
func (m *Model) dragTo(y int) {
	d := m.drag
	if d == nil || d.id == "" {
		return
	}
	from := m.index(d.id)
	if from < 0 {
		m.drag = nil
		return
	}
	i, _, ok := m.at(y)
	if !ok {
		return
	}
	it := m.items()[i]
	if it.kind != itemSession || it.group.key != d.group || it.s == from {
		return
	}
	s := m.sessions[from]
	moved := slices.Insert(slices.Delete(slices.Clone(m.sessions), from, from+1), it.s, s)
	trial := *m
	trial.sessions = moved
	if j, _, ok := trial.at(y); !ok || trial.items()[j].key != sessionKey(d.id) {
		return
	}
	m.sessions, d.at, d.moved = moved, it.s, true
	m.sel = m.sessionIndex(d.id)
	m.setStatus("drop "+s.Name+" where it goes, among "+it.group.title, false)
}

// release ends a press. A press that never moved anything opens what it
// went down on, when the button comes up there, as a click does. A dragged
// session stays where it was carried, and the core is told; dropped on
// another group, it goes back.
func (m Model) release(y int) (tea.Model, tea.Cmd) {
	d := m.drag
	m.drag = nil
	if d == nil {
		return m, nil
	}
	i, _, on := m.at(y)
	if !d.moved {
		if on && m.items()[i].key == d.key {
			m.sel = i
			cmd := m.open(true)
			m.reveal()
			return m, cmd
		}
		return m, nil
	}
	if on && m.items()[i].group.key != d.group {
		m.sortByOrder()
		m.sel = m.sessionIndex(d.id)
		m.setStatus("a session moves only among the others of its group", false)
		return m, nil
	}
	c, ok := m.controls()
	if !ok {
		m.sortByOrder()
		m.sel = m.sessionIndex(d.id)
		m.setStatus(noControls, true)
		return m, nil
	}
	at := m.index(d.id)
	if at < 0 {
		return m, nil
	}
	before := ""
	if at+1 < len(m.sessions) {
		before = m.sessions[at+1].ID
	}
	m.renumber()
	name := m.sessions[at].Name
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
		if s, ok := m.selSession(); !ok || (s.State == ipc.StateFrozen) == on {
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
