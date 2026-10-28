package ui

// The Outside Irie group (DESIGN §4.6): Claude Code sessions that run in
// other terminals, listed after Irie's own groups under a header with their
// count. Each is one line, busy or idle by its glyph alone, in the order
// they started, so a change of state never moves one. The selected one's
// details go in the status strip. m m, or :move, moves an idle one in.

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/irie-people/irie/internal/ipc"
)

type movedMsg struct {
	name    string
	session ipc.Session
	err     error
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

// outsideLook draws an outside row as the Irie state it matches: busy as
// working, idle as idle, waiting for the user as needs you. Anything else is
// a dim dot.
func (m Model) outsideLook(o ipc.OutsideSession) look {
	switch o.Status {
	case ipc.StatusBusy:
		return m.look(ipc.Session{State: ipc.StateWorking})
	case ipc.StatusIdle:
		return m.look(ipc.Session{State: ipc.StateIdle})
	case ipc.StatusWaiting:
		return m.look(ipc.Session{State: ipc.StateNeedsYou})
	}
	return look{"·", m.pal.muted, m.pal.muted, false}
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

// outsideDoing says what an outside session does, and whether m moves it.
func outsideDoing(o ipc.OutsideSession) string {
	switch {
	case o.Movable:
		return "idle · m moves it in"
	case o.Status == ipc.StatusWaiting && o.WaitingFor != "":
		return "waiting for " + printable(o.WaitingFor)
	case o.Status == "":
		return "not running"
	}
	return printable(o.Status)
}

// outsideStatus is the status strip for one of the group's rows: what it
// does, its name, where it runs, and whether m moves it.
func (m Model) outsideStatus(o ipc.OutsideSession) string {
	p := m.pal
	parts := []string{outsideDoing(o), outsideLabel(o)}
	if o.Cwd != "" {
		parts = append(parts, printable(m.home2tilde(o.Cwd)))
	}
	parts = append(parts, where(o))
	c := p.muted
	if l := m.outsideLook(o); l.loud {
		c = l.name
	}
	return blank(p.sidebar, 1) + m.text(c).Render(cut(strings.Join(parts, " · "), m.width-2))
}

// open acts on the selected entry: it shows a session, folds or unfolds a
// group, and for a session outside Irie, which Irie cannot show, says why
// and what m does instead.
func (m *Model) open(focus bool) tea.Cmd {
	it, ok := m.selected()
	switch {
	case !ok:
		return nil
	case it.kind == itemSession:
		return m.showCmd(focus)
	case it.kind == itemGroup:
		m.fold(it.key)
		return nil
	}
	o := m.outside[it.o]
	msg := outsideLabel(o) + " runs outside Irie, " + where(o)
	switch {
	case o.Movable:
		msg += "; m moves it in here"
	case o.Why != "":
		msg += "; " + o.Why
	}
	m.setStatus(msg, false)
	return nil
}

// moveConfirm is how long a first m waits for the second.
const moveConfirm = 5 * time.Second

// moveSelected is m on a row. A move ends the outside copy and anything it
// runs in the background, so it takes a second m, as closing takes x x
// (DESIGN §4.1); any other key in between cancels it.
func (m *Model) moveSelected() tea.Cmd {
	o, ok := m.selOutside()
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
	m.pick(msg.session.ID)
}

// pickOutside finds the outside session that :move names: by its id, short
// id or name, or the selected one when it names none.
func (m Model) pickOutside(what string) (ipc.OutsideSession, error) {
	if what == "" {
		if o, ok := m.selOutside(); ok {
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
