package ui

// Closing sessions (DESIGN §4.1, §4.5). x closes the selected session at
// once, as ⌘W does in the Mac app: a close stays inside Irie, and u brings
// it back, so it never asks. The core reads :close, which does the same,
// and :reopen, which brings back the session closed last.

import (
	tea "charm.land/bubbletea/v2"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
)

type closedMsg struct {
	name string
	err  error
}

// closeSelected is x on a row: it closes the session at once.
func (m *Model) closeSelected() tea.Cmd {
	s, ok := m.selSession()
	if !ok {
		m.setStatus("x closes one of "+brand.Name+"'s sessions; select one first", false)
		return nil
	}
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
	m.setStatus("closed "+msg.name+"; u brings it back", false)
}
