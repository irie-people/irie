package ui

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/irie-people/irie/internal/brand"
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

// x closes the selected session at once, as ⌘W does in the Mac app: a
// close stays inside Irie and u brings it back, so it never asks (DESIGN
// §4.5).
func TestXClosesTheSelectedSessionAtOnce(t *testing.T) {
	m, b := newModel(t, 34, 24, 3)
	m = send(m, keyMsg("down"))
	m = send(m, keyMsg("x"))
	if !slices.Equal(b.closes, []string{"s000000000001"}) {
		t.Fatalf("x closed %v", b.closes)
	}
	if !strings.Contains(m.status, "closed task-001; u brings it back") || m.statusErr {
		t.Errorf("status %q", m.status)
	}
	// The row goes when the core says so, and the selection moves on.
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameRemove, ID: "s000000000001"}})
	if s, ok := m.selSession(); len(m.sessions) != 2 || !ok || s.ID != "s000000000002" {
		t.Errorf("after the remove: sel %d of %+v", m.sel, m.sessions)
	}
}

// landBackend is a fake backend that says what of a card's work has not
// landed.
type landBackend struct {
	*fakeBackend
	notLive map[string]ipc.NotLive
	asked   []string
	err     error
}

func (b *landBackend) Unlanded(a ipc.CloseArgs) (ipc.UnlandedResult, error) {
	b.asked = append(b.asked, a.Session)
	r := ipc.UnlandedResult{Sessions: []string{a.Session}, NotLive: []ipc.NotLive{}}
	if n, ok := b.notLive[a.Session]; ok {
		r.NotLive = append(r.NotLive, n)
	}
	return r, b.err
}

// A card whose commits have not all landed takes a second x, once the
// status has said which; another key in between keeps it open. A card
// whose work has all landed closes at once, as does one the core can say
// nothing of.
func TestXSaysWhatHasNotLandedBeforeItCloses(t *testing.T) {
	b := &landBackend{fakeBackend: &fakeBackend{}, notLive: map[string]ipc.NotLive{
		"s000000000001": {Session: "s000000000001", Name: "task-001", Branch: "irie/task-001", Land: "live", Ahead: 2,
			Commits: []string{"abc1234 Fix the typo", "def5678 Test it"}},
	}}
	m := New(b, Options{Now: func() time.Time { return now }, Home: "/home/alice"})
	m = send(m, tea.WindowSizeMsg{Width: 34, Height: 24})
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameSnapshot, Sessions: sessions(3), Header: &ipc.Header{}}})

	m = send(m, keyMsg("down"))
	m = send(m, keyMsg("x"))
	if len(b.closes) != 0 || !slices.Equal(b.asked, []string{"s000000000001"}) {
		t.Fatalf("a first x closed %v, asked %v", b.closes, b.asked)
	}
	want := "Not landed: 2 commits on irie/task-001 not in live: abc1234 Fix the typo; def5678 Test it. Press x again to close it anyway"
	if !strings.Contains(m.status, want) || m.statusErr {
		t.Errorf("status %q, want %q", m.status, want)
	}
	// Another key keeps it open, and the next x asks again.
	m = send(m, keyMsg("down"))
	m = send(m, keyMsg("up"))
	m = send(m, keyMsg("x"))
	if len(b.closes) != 0 || len(b.asked) != 2 {
		t.Fatalf("after another key, x closed %v, asked %v", b.closes, b.asked)
	}
	m = send(m, keyMsg("x"))
	if !slices.Equal(b.closes, []string{"s000000000001"}) || !strings.Contains(m.status, "closed task-001") {
		t.Fatalf("a second x closed %v, status %q", b.closes, m.status)
	}

	// All of its work has landed: it closes at once.
	m = send(m, keyMsg("down"))
	m = send(m, keyMsg("x"))
	if !slices.Equal(b.closes, []string{"s000000000001", "s000000000002"}) {
		t.Fatalf("a card whose work landed: closed %v", b.closes)
	}
	// A core that can't say closes it at once too.
	b.err = errors.New("unknown op unlanded")
	m = send(m, keyMsg("up"))
	m = send(m, keyMsg("x"))
	if len(b.closes) != 3 {
		t.Fatalf("a core that can't say: closed %v", b.closes)
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
	if !slices.Equal(b.reopens, []string{""}) || !strings.Contains(m.status, "Reopened task-001") {
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
	if len(b.closes) != 0 || !strings.Contains(m.status, "x closes one of "+brand.Name+"'s sessions") {
		t.Errorf("closed %v, status %q", b.closes, m.status)
	}
}
