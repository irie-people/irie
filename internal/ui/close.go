package ui

// Closing sessions (DESIGN §4.1, §4.5). x x closes the selected session: the
// first x says what it closes, and any other key cancels it, as m m does for
// a move. :close closes it at once, and :reopen brings back the session
// closed last.

import (
	tea "charm.land/bubbletea/v2"

	"github.com/irie-hq/irie/internal/ipc"
)

type (
	closedMsg struct {
		name string
		err  error
	}
	reopenedMsg struct {
		session ipc.Session
		err     error
	}
)

// closeConfirm is how long a first x waits for the second.
const closeConfirm = moveConfirm

// closeSelected is x on a row: the first press asks, the second closes.
func (m *Model) closeSelected() tea.Cmd {
	if m.sel < 0 || m.sel >= len(m.sessions) {
		m.closing = ""
		m.setStatus("x x closes one of Irie's sessions; select one first", false)
		return nil
	}
	s := m.sessions[m.sel]
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

// closeNamed is :close: the selected session, at once.
func (m *Model) closeNamed() tea.Cmd {
	if m.sel < 0 || m.sel >= len(m.sessions) {
		m.setStatus(":close closes the selected session; select one of Irie's", true)
		return nil
	}
	return m.closeCmd(m.sessions[m.sel])
}

// reopenCmd is :reopen: the core brings back the session closed last.
func (m *Model) reopenCmd() tea.Cmd {
	if m.reopening {
		return nil
	}
	m.reopening = true
	m.setStatus("reopening…", false)
	b := m.backend
	return func() tea.Msg {
		s, err := b.Reopen("")
		return reopenedMsg{session: s, err: err}
	}
}

func (m *Model) closedDone(msg closedMsg) {
	if msg.err != nil {
		m.setStatus(msg.err.Error(), true)
		return
	}
	m.setStatus("closed "+msg.name+"; :reopen brings it back", false)
}

// reopened selects the session that came back, once its row is in.
func (m *Model) reopened(msg reopenedMsg) {
	m.reopening = false
	if msg.err != nil {
		m.setStatus(msg.err.Error(), true)
		return
	}
	m.setStatus("reopened "+msg.session.Name, false)
	m.pending = msg.session.ID
	if i := m.index(msg.session.ID); i >= 0 {
		m.moveTo(i)
		m.pending = ""
	}
}
