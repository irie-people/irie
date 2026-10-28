package ui

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

func (f *fakeBackend) Close(id string) error {
	f.closes = append(f.closes, id)
	return nil
}

func (f *fakeBackend) Reopen(id string) (ipc.Session, error) {
	f.reopens = append(f.reopens, id)
	if f.reopenErr != nil {
		return ipc.Session{}, f.reopenErr
	}
	return ipc.Session{ID: "s000000000001", Name: "task-001", State: ipc.StateStarting, Created: now}, nil
}

func TestXXClosesTheSelectedSession(t *testing.T) {
	m, b := newModel(t, 34, 24, 3)
	m = send(m, keyMsg("down"))
	m = send(m, keyMsg("x"))
	if len(b.closes) != 0 || !strings.Contains(m.status, "Press x again to close task-001") {
		t.Fatalf("one x closed %v, status %q", b.closes, m.status)
	}
	m = send(m, keyMsg("x"))
	if !slices.Equal(b.closes, []string{"s000000000001"}) {
		t.Fatalf("x x closed %v", b.closes)
	}
	if !strings.Contains(m.status, "closed task-001") || m.statusErr {
		t.Errorf("status %q", m.status)
	}
	// The row goes when the core says so, and the selection moves on.
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameRemove, ID: "s000000000001"}})
	if s, ok := m.selSession(); len(m.sessions) != 2 || !ok || s.ID != "s000000000002" {
		t.Errorf("after the remove: sel %d of %+v", m.sel, m.sessions)
	}
}

func TestAnotherKeyCancelsAClose(t *testing.T) {
	m, b := newModel(t, 34, 24, 3)
	m = send(m, keyMsg("x"))
	m = send(m, keyMsg("j"))
	m = send(m, keyMsg("x"))
	if len(b.closes) != 0 {
		t.Errorf("x j x closed %v", b.closes)
	}
	// So does waiting too long.
	m, b = newModel(t, 34, 24, 3)
	m = send(m, keyMsg("x"))
	m.now = func() time.Time { return now.Add(closeConfirm + time.Second) }
	m = send(m, keyMsg("x"))
	if len(b.closes) != 0 || !strings.Contains(m.status, "Press x again") {
		t.Errorf("a late second x closed %v, status %q", b.closes, m.status)
	}
}

func TestCloseAndReopenCommands(t *testing.T) {
	m, b := newModel(t, 34, 24, 3)
	m = send(m, keyMsg("tab"))
	m = typeText(m, ":close")
	m = send(m, keyMsg("enter"))
	if !slices.Equal(b.closes, []string{"s000000000000"}) || m.bar.Value() != "" {
		t.Fatalf(":close closed %v, bar %q", b.closes, m.bar.Value())
	}

	m = send(m, keyMsg("tab"))
	m = typeText(m, ":reopen")
	m = send(m, keyMsg("enter"))
	if !slices.Equal(b.reopens, []string{""}) || !strings.Contains(m.status, "reopened task-001") {
		t.Fatalf(":reopen asked for %q, status %q", b.reopens, m.status)
	}
	if s, ok := m.selSession(); !ok || s.ID != "s000000000001" {
		t.Errorf("the reopened row is not selected: %d", m.sel)
	}

	b.reopenErr = errors.New("no closed session to reopen")
	m = send(m, keyMsg("tab"))
	m = typeText(m, ":reopen")
	m = send(m, keyMsg("enter"))
	if !m.statusErr || !strings.Contains(m.status, "no closed session") {
		t.Errorf("status %q", m.status)
	}
}

func TestXOnAnOutsideRowClosesNothing(t *testing.T) {
	m, b := withOutside(t, 34, 24, 1, 2)
	m = send(m, keyMsg("end"))
	m = send(m, keyMsg("x"))
	m = send(m, keyMsg("x"))
	if len(b.closes) != 0 || !strings.Contains(m.status, "x x closes one of Irie's sessions") {
		t.Errorf("closed %v, status %q", b.closes, m.status)
	}
}
