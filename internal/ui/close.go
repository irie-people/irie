package ui

// Closing sessions (DESIGN §4.1, §4.5). x closes the selected session at
// once, as ⌘W does in the Mac app: a close stays inside Irie, and u brings
// it back, so it never asks, but for one thing. A card whose commits have
// not all reached its repo's land branch, such as live, would leave them
// where nothing brings them to mind again, so x first says which, and a
// second x closes it. The core reads :close, which does the same but says
// it after, and :reopen, which brings back the session closed last.

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
)

// Landing is what the sidebar asks before it closes a card: what of the
// work of the sessions a close would close has not landed. A backend
// without it closes at once.
type Landing interface {
	Unlanded(ipc.CloseArgs) (ipc.UnlandedResult, error)
}

// closeConfirm is how long a first x that said what has not landed waits
// for the second.
const closeConfirm = moveConfirm

type closedMsg struct {
	name string
	err  error
}

// landedMsg is what the core said of the work of the card x asked to close.
type landedMsg struct {
	s   ipc.Session
	r   ipc.UnlandedResult
	err error
}

// closeSelected is x on a row. It closes the session at once, unless the
// core says commits of its work have not landed: then the status says
// which, and a second x closes it. Any other key in between keeps it open.
func (m *Model) closeSelected() tea.Cmd {
	s, ok := m.selSession()
	if !ok {
		m.setStatus("x closes one of "+brand.Name+"'s sessions; select one first", false)
		return nil
	}
	if m.closing == s.ID && m.now().Sub(m.closingAt) <= closeConfirm {
		m.closing = ""
		return m.closeCmd(s)
	}
	m.closing = ""
	l, ok := m.backend.(Landing)
	if !ok {
		return m.closeCmd(s)
	}
	m.setStatus("closing "+s.Name+"…", false)
	return func() tea.Msg {
		r, err := l.Unlanded(ipc.CloseArgs{Session: s.ID})
		return landedMsg{s: s, r: r, err: err}
	}
}

// landedDone closes the card x asked to close when all of its work has
// landed, or the core could not say, and otherwise asks for a second x.
func (m *Model) landedDone(msg landedMsg) tea.Cmd {
	if msg.err != nil || len(msg.r.NotLive) == 0 {
		return m.closeCmd(msg.s)
	}
	m.closing, m.closingAt = msg.s.ID, m.now()
	m.setStatus(notLiveAsk(msg.r.NotLive[0]), false)
	return nil
}

// notLiveAsk is what a first x on a card whose work has not landed says.
func notLiveAsk(n ipc.NotLive) string {
	return "Not landed: " + n.Words() + ". Press x again to close it anyway; its branch keeps them."
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

// Unlanded asks the core what of the work of the sessions a close of a
// would close has not landed.
func (b socketBackend) Unlanded(a ipc.CloseArgs) (ipc.UnlandedResult, error) {
	var r ipc.UnlandedResult
	err := ipc.Call(b.socket, 15*time.Second, ipc.OpUnlanded, a, &r)
	return r, err
}
