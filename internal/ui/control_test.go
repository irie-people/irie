package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/irie-hq/irie/internal/ipc"
)

// ctlBackend is a fake backend with Controls.
type ctlBackend struct {
	*fakeBackend
	calls []string
}

func (b *ctlBackend) Freeze(id string, on, all bool) error {
	b.calls = append(b.calls, fmt.Sprintf("freeze %s %v %v", id, on, all))
	return nil
}

func (b *ctlBackend) Priority(id string, level int, reason string) error {
	b.calls = append(b.calls, fmt.Sprintf("priority %s %d %s", id, level, reason))
	return nil
}

func (b *ctlBackend) Reorder(id, before string) error {
	b.calls = append(b.calls, fmt.Sprintf("reorder %s before %q", id, before))
	return nil
}

func ctlModel(t *testing.T, rows []ipc.Session) (Model, *ctlBackend) {
	t.Helper()
	b := &ctlBackend{fakeBackend: &fakeBackend{}}
	m := New(b, Options{Now: func() time.Time { return now }, Home: "/home/alice"})
	m = send(m, tea.WindowSizeMsg{Width: 34, Height: 24})
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameSnapshot, Sessions: rows, Header: &ipc.Header{}}})
	return m, b
}

func order(m Model) []string {
	var out []string
	for _, s := range m.sessions {
		out = append(out, s.ID)
	}
	return out
}

func TestFFreezesAndThawsAndShowsTheDiamond(t *testing.T) {
	m, b := ctlModel(t, sessions(3))
	m = send(m, keyMsg("down"))
	m = send(m, keyMsg("f"))
	if want := []string{"freeze s000000000001 true false"}; !slices.Equal(b.calls, want) {
		t.Fatalf("calls = %v, want %v", b.calls, want)
	}
	// At once, before the core's frame.
	if m.sessions[1].State != ipc.StateFrozen || !strings.Contains(screen(t, m)[m.listTop()+1], "◆") {
		t.Errorf("row 1 does not show frozen: %q", screen(t, m)[m.listTop()+1])
	}
	m = send(m, keyMsg("f"))
	m = send(m, keyMsg("F"))
	want := []string{"freeze s000000000001 true false", "freeze s000000000001 false false", "freeze  true true"}
	if !slices.Equal(b.calls, want) {
		t.Errorf("calls = %v, want %v", b.calls, want)
	}
	// With every session frozen, F thaws them all.
	rows := sessions(2)
	for i := range rows {
		rows[i].State = ipc.StateFrozen
	}
	m, b = ctlModel(t, rows)
	m = send(m, keyMsg("F"))
	if want := []string{"freeze  false true"}; !slices.Equal(b.calls, want) {
		t.Errorf("calls = %v, want %v", b.calls, want)
	}
	if !strings.Contains(screen(t, m)[0], "◆2") {
		t.Errorf("header does not count 2 frozen: %q", screen(t, m)[0])
	}
}

func TestBangPromotesToTheTopAtOnce(t *testing.T) {
	m, b := ctlModel(t, sessions(3))
	m = send(m, keyMsg("end"))
	m = send(m, keyMsg("!"))
	if got := order(m); !slices.Equal(got, []string{"s000000000002", "s000000000000", "s000000000001"}) {
		t.Errorf("order = %v, want the promoted row on top at once", got)
	}
	if m.sel != 0 || m.sessions[0].Priority != 1 {
		t.Errorf("selection %d, priority %d", m.sel, m.sessions[0].Priority)
	}
	if line := screen(t, m)[m.listTop()]; !strings.Contains(line, "↑task-002") {
		t.Errorf("no badge: %q", line)
	}
	m = send(m, keyMsg("!"))
	want := []string{"priority s000000000002 1 ", "priority s000000000002 0 "}
	if !slices.Equal(b.calls, want) {
		t.Errorf("calls = %v, want %v", b.calls, want)
	}
	if got := order(m); got[0] != "s000000000002" {
		t.Errorf("putting it back moved it: %v", got)
	}
	m = typeText(send(m, keyMsg("tab")), ":prio a demo at noon")
	m = send(m, keyMsg("enter"))
	if last := b.calls[len(b.calls)-1]; last != "priority s000000000002 1 a demo at noon" {
		t.Errorf(":prio called %q", last)
	}
}

func TestFramesKeepTheCoresOrder(t *testing.T) {
	rows := sessions(3)
	for i, o := range []int{2, 0, 1} {
		rows[i].Order = o
	}
	m, _ := ctlModel(t, rows)
	if got := order(m); !slices.Equal(got, []string{"s000000000001", "s000000000002", "s000000000000"}) {
		t.Errorf("order = %v", got)
	}
	// A state change alone moves nothing.
	r := rows[0]
	r.State = ipc.StateNeedsYou
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameUpsert, Session: &r}})
	if got := order(m); got[2] != "s000000000000" {
		t.Errorf("a state change moved a row: %v", got)
	}
	// Upserts of a new order put every row in its place.
	for i := range rows {
		r := rows[i]
		r.Order = i
		m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameUpsert, Session: &r}})
	}
	if got := order(m); !slices.Equal(got, []string{"s000000000000", "s000000000001", "s000000000002"}) {
		t.Errorf("after the upserts: %v", got)
	}
}

func TestDraggingARowReordersIt(t *testing.T) {
	m, b := ctlModel(t, sessions(4))
	top := m.listTop()
	m = send(m, tea.MouseClickMsg{X: 3, Y: top, Button: tea.MouseLeft})
	m = send(m, tea.MouseMotionMsg{X: 3, Y: top + 1, Button: tea.MouseLeft})
	m = send(m, tea.MouseMotionMsg{X: 3, Y: top + 2, Button: tea.MouseLeft})
	if got := order(m); !slices.Equal(got, []string{"s000000000001", "s000000000002", "s000000000000", "s000000000003"}) {
		t.Errorf("while dragging: %v", got)
	}
	// A frame meanwhile leaves the held row under the mouse.
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameSnapshot, Sessions: sessions(4), Header: &ipc.Header{}}})
	if got := order(m); got[2] != "s000000000000" {
		t.Errorf("a frame moved the held row: %v", got)
	}
	m = send(m, tea.MouseReleaseMsg{X: 3, Y: top + 2, Button: tea.MouseLeft})
	if want := []string{`reorder s000000000000 before "s000000000003"`}; !slices.Equal(b.calls, want) {
		t.Errorf("calls = %v, want %v", b.calls, want)
	}
	if len(b.shows) != 0 {
		t.Errorf("a drag opened the row: %v", b.shows)
	}
	// To the end: before nothing.
	m = send(m, tea.MouseClickMsg{X: 3, Y: top, Button: tea.MouseLeft})
	m = send(m, tea.MouseMotionMsg{X: 3, Y: top + 10, Button: tea.MouseLeft})
	m = send(m, tea.MouseReleaseMsg{X: 3, Y: top + 10, Button: tea.MouseLeft})
	if last := b.calls[len(b.calls)-1]; last != `reorder s000000000001 before ""` {
		t.Errorf("drag to the end: %q", last)
	}
}

func TestWithoutControlsTheKeysSaySo(t *testing.T) {
	m, _ := newModel(t, 34, 24, 2)
	m = send(m, keyMsg("f"))
	if !m.statusErr || !strings.Contains(m.status, "update") {
		t.Errorf("status = %q", m.status)
	}
}
