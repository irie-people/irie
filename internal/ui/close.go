package ui

// Closing sessions (DESIGN §4.1, §4.5). x x closes the selected session: the
// first x says what it closes, and any other key cancels it, as m m does for
// a move. The core reads :close, which closes it at once, and :reopen, which
// brings back the session closed last.

import (
	tea "charm.land/bubbletea/v2"

	"github.com/irie-people/irie/internal/ipc"
)

type closedMsg struct {
	name string
	err  error
}

// closeConfirm is how long a first x waits for the second.
const closeConfirm = moveConfirm

// closeSelected is x on a row: the first press asks, the second closes.
func (m *Model) closeSelected() tea.Cmd {
	s, ok := m.selSession()
	if !ok {
		m.closing = ""
		m.setStatus("x x closes one of Irie's sessions; select one first", false)
		return nil
	}
	if m.closing != s.ID || m.now().Sub(m.closingAt) > closeConfirm {
		m.closing, m.closingAt = s.ID, m.now()
		m.setStatus("Press x again to close "+s.Name+". Its folder stays, and :reopen brings it back.", false)
		return nil
	}
	m.closing = ""
	return m.closeCmd(s)
}

// closeCmd asks the core to close s.
func (m *Model) closeCmd(s ipc.Session) tea.Cmd {
	m.setStatus("closing "+s.Name+"…", false)
	b, id, name := m.backend, s.ID, s.Name
	return func() tea.Msg { return closedMsg{name: name, err: b.Close(id)} }
}

func (m *Model) closedDone(msg closedMsg) {
	if msg.err != nil {
		m.setStatus(msg.err.Error(), true)
		return
	}
	m.setStatus("closed "+msg.name+"; :reopen brings it back", false)
}
