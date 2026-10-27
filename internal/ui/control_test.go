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
	return ctlAt(t, rows, 34, 24)
}

// ctlAt is a sidebar of w×h with Controls, showing rows.
func ctlAt(t *testing.T, rows []ipc.Session, w, h int) (Model, *ctlBackend) {
	t.Helper()
	b := &ctlBackend{fakeBackend: &fakeBackend{}}
	m := New(b, Options{Now: func() time.Time { return now }, Home: "/home/alice"})
	m = send(m, tea.WindowSizeMsg{Width: w, Height: h})
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
	if m.sessions[1].State != ipc.StateFrozen || !strings.Contains(screen(t, m)[lineOf(t, m, "task-001")], "◆ task-001") {
		t.Errorf("task-001 does not show frozen: %q", screen(t, m)[lineOf(t, m, "task-001")])
	}
	if selectedName(m) != "task-001" {
		t.Errorf("the selection moved to %s", selectedName(m))
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
	// The status strip and the details card say what frozen means.
	m.setStatus("", false)
	if got := statusText(t, m); !strings.Contains(got, "frozen; f thaws it") {
		t.Errorf("status %q", got)
	}
}

// ! promotes the selected session to the top of its group at once, with
// its badge; a second ! puts it back where it stands.
func TestBangPromotesToTheTopAtOnce(t *testing.T) {
	m, b := ctlModel(t, sessions(3))
	m = send(m, keyMsg("end"))
	m = send(m, keyMsg("!"))
	if got := order(m); !slices.Equal(got, []string{"s000000000002", "s000000000000", "s000000000001"}) {
		t.Errorf("order = %v, want the promoted row on top at once", got)
	}
	if selectedName(m) != "task-002" || m.sessions[0].Priority != 1 {
		t.Errorf("selection %s, priority %d", selectedName(m), m.sessions[0].Priority)
	}
	// The first card, whether or not the group's header scrolled away.
	got := listText(t, m)
	if i := slices.IndexFunc(got, func(l string) bool { return strings.Contains(l, "task-") }); i < 0 || got[i] != "◐↑task-002 1m" {
		t.Errorf("no badge on the first card: %q", got)
	}
	raw := strings.Split(m.View().Content, "\n")[lineOf(t, m, "↑task-002")]
	if c := cells(raw); c[4].r != "↑" || c[4].fg != rgb(m.pal.accent) || !c[4].bold {
		t.Errorf("badge %q in %s, bold %v", c[4].r, c[4].fg, c[4].bold)
	}
	if got := statusText(t, m); !strings.Contains(got, "promoted") {
		t.Errorf("details %q", got)
	}
	m = send(m, keyMsg("!"))
	want := []string{"priority s000000000002 1 ", "priority s000000000002 0 "}
	if !slices.Equal(b.calls, want) {
		t.Errorf("calls = %v, want %v", b.calls, want)
	}
	if got := order(m); got[0] != "s000000000002" || m.sessions[0].Priority != 0 {
		t.Errorf("putting it back moved it: %v", got)
	}
	m = typeText(send(m, keyMsg("tab")), ":prio a demo at noon")
	m = send(m, keyMsg("enter"))
	if last := b.calls[len(b.calls)-1]; last != "priority s000000000002 1 a demo at noon" {
		t.Errorf(":prio called %q", last)
	}
	// One-line rows carry the badge too.
	m, _ = ctlAt(t, sessions(3), 22, 30)
	m = send(m, keyMsg("down"))
	m = send(m, keyMsg("!"))
	if row := screen(t, m)[lineOf(t, m, "task-001")]; !strings.HasPrefix(row, "▌◐↑task-001") {
		t.Errorf("promoted row %q", row)
	}
}

// A promotion heads the session's own group: groups keep their places.
func TestAPromotionHeadsItsGroup(t *testing.T) {
	rows := repoSessions()
	for i := range rows {
		rows[i].Order = i
	}
	m, _ := ctlAt(t, rows, 34, 80)
	m = send(m, keyMsg("esc"))
	m = selectNamed(t, m, "irie-tests")
	m = send(m, keyMsg("!"))
	want := []string{
		"▾ website", "web-fix", "web-copy",
		"▾ code/irie", "irie-tests", "irie-theme",
		"▾ forks/irie", "fork-docs",
		"▾ Not in a repo", "scratch-notes",
	}
	if got := outline(m); !slices.Equal(got, want) {
		t.Errorf("outline %q, want %q", got, want)
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
	// The cards follow it within their group.
	if got := outline(m); !slices.Equal(got, []string{"▾ Not in a repo", "task-000", "task-001", "task-002"}) {
		t.Errorf("outline %q", got)
	}
}

// dragAlong presses on the line at y, moves with the button held through ys,
// and lets go on the last.
func dragAlong(m Model, y int, ys ...int) Model {
	m = send(m, tea.MouseClickMsg{X: 5, Y: y, Button: tea.MouseLeft})
	for _, to := range ys {
		m = send(m, tea.MouseMotionMsg{X: 5, Y: to, Button: tea.MouseLeft})
	}
	last := y
	if len(ys) > 0 {
		last = ys[len(ys)-1]
	}
	return send(m, tea.MouseReleaseMsg{X: 5, Y: last, Button: tea.MouseLeft})
}

func TestDraggingARowReordersIt(t *testing.T) {
	m, b := ctlAt(t, sessions(4), 34, 12) // one-line rows
	if !m.compact() {
		t.Fatal("34×12 draws cards")
	}
	y := func(name string) int { return lineOf(t, m, name) }
	m = send(m, tea.MouseClickMsg{X: 3, Y: y("task-000"), Button: tea.MouseLeft})
	m = send(m, tea.MouseMotionMsg{X: 3, Y: y("task-001"), Button: tea.MouseLeft})
	m = send(m, tea.MouseMotionMsg{X: 3, Y: y("task-002"), Button: tea.MouseLeft})
	if got := order(m); !slices.Equal(got, []string{"s000000000001", "s000000000002", "s000000000000", "s000000000003"}) {
		t.Errorf("while dragging: %v", got)
	}
	// A frame meanwhile leaves the held row under the mouse.
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameSnapshot, Sessions: sessions(4), Header: &ipc.Header{}}})
	if got := order(m); got[2] != "s000000000000" {
		t.Errorf("a frame moved the held row: %v", got)
	}
	m = send(m, tea.MouseReleaseMsg{X: 3, Y: y("task-000"), Button: tea.MouseLeft})
	if want := []string{`reorder s000000000000 before "s000000000003"`}; !slices.Equal(b.calls, want) {
		t.Errorf("calls = %v, want %v", b.calls, want)
	}
	if len(b.shows) != 0 {
		t.Errorf("a drag opened the row: %v", b.shows)
	}
	// To the end of its group: before nothing.
	m = dragAlong(m, y("task-001"), y("task-003"))
	if last := b.calls[len(b.calls)-1]; last != `reorder s000000000001 before ""` {
		t.Errorf("drag to the end: %q", last)
	}
	// A press that never moved is a click: it shows the row.
	m = dragAlong(m, y("task-002"))
	if len(b.shows) != 1 || b.shows[0].id != "s000000000002" {
		t.Errorf("a click on a row showed %+v", b.shows)
	}
}

// A card moves among its group's cards as the pointer crosses them; a
// drop on another group sends it back where it was.
func TestDraggingACardStaysInItsGroup(t *testing.T) {
	rows := []ipc.Session{
		{ID: "s0", Name: "a0", Repo: "/home/alice/code/a", State: ipc.StateWorking},
		{ID: "s1", Name: "a1", Repo: "/home/alice/code/a", State: ipc.StateWorking},
		{ID: "s2", Name: "a2", Repo: "/home/alice/code/a", State: ipc.StateWorking},
		{ID: "s3", Name: "b0", Repo: "/home/alice/code/b", State: ipc.StateWorking},
	}
	for i := range rows {
		rows[i].Order, rows[i].Created = i, now.Add(-time.Duration(4-i)*time.Minute)
	}
	m, b := ctlAt(t, rows, 34, 30)
	m = send(m, keyMsg("esc"))
	y := func(name string) int { return lineOf(t, m, name) }
	// Into another group: it waits in its own, and the drop sends it back.
	m = send(m, tea.MouseClickMsg{X: 5, Y: y("a0"), Button: tea.MouseLeft})
	m = send(m, tea.MouseMotionMsg{X: 5, Y: y("a1"), Button: tea.MouseLeft})
	if got := order(m); !slices.Equal(got, []string{"s1", "s0", "s2", "s3"}) {
		t.Fatalf("while dragging: %v", got)
	}
	m = send(m, tea.MouseMotionMsg{X: 5, Y: y("b0"), Button: tea.MouseLeft})
	if got := order(m); !slices.Equal(got, []string{"s1", "s0", "s2", "s3"}) {
		t.Errorf("over another group the card moved: %v", got)
	}
	m = send(m, tea.MouseReleaseMsg{X: 5, Y: y("b0"), Button: tea.MouseLeft})
	if got := order(m); !slices.Equal(got, []string{"s0", "s1", "s2", "s3"}) || len(b.calls) != 0 {
		t.Errorf("a drop on another group: order %v, calls %v", got, b.calls)
	}
	if !strings.Contains(m.status, "only among the others of its group") || selectedName(m) != "a0" {
		t.Errorf("status %q, selection %s", m.status, selectedName(m))
	}
	// Within it, the drop stays, and the core hears where.
	m = dragAlong(m, y("a0"), y("a1"), y("a2"))
	if got := order(m); !slices.Equal(got, []string{"s1", "s2", "s0", "s3"}) {
		t.Errorf("after the drop: %v", got)
	}
	if want := []string{`reorder s0 before "s3"`}; !slices.Equal(b.calls, want) {
		t.Errorf("calls = %v, want %v", b.calls, want)
	}
	if got := outline(m); !slices.Equal(got, []string{"▾ a", "a1", "a2", "a0", "▾ b", "b0"}) {
		t.Errorf("outline %q", got)
	}
	// Up to the top of its group.
	m = dragAlong(m, y("a0"), y("a2"), y("a1"))
	if last := b.calls[len(b.calls)-1]; last != `reorder s0 before "s1"` {
		t.Errorf("drag to the top: %q", last)
	}
}

// A card taller than the one it is dragged over moves past it only once the
// pointer reaches where it would land, so the two never trade back and forth.
func TestDraggingPastATallerCardHoldsStill(t *testing.T) {
	rows := sessions(2)
	rows[1].Subagents = []ipc.Subagent{{Type: "Explore"}, {Type: "Plan"}, {Type: "Explore"}}
	m, _ := ctlAt(t, rows, 34, 30)
	m = send(m, keyMsg("esc"))
	top := lineOf(t, m, "task-001") - 1 // its card's top border
	m = send(m, tea.MouseClickMsg{X: 5, Y: lineOf(t, m, "task-000"), Button: tea.MouseLeft})
	m = send(m, tea.MouseMotionMsg{X: 5, Y: top + 1, Button: tea.MouseLeft})
	if got := order(m); got[0] != "s000000000000" {
		t.Errorf("moved before the pointer reached where it lands: %v", got)
	}
	m = send(m, tea.MouseMotionMsg{X: 5, Y: top + 5, Button: tea.MouseLeft})
	if got := order(m); got[1] != "s000000000000" {
		t.Errorf("did not move past the taller card: %v", got)
	}
	m = send(m, tea.MouseMotionMsg{X: 5, Y: top + 1, Button: tea.MouseLeft})
	if got := order(m); got[1] != "s000000000000" {
		t.Errorf("traded back while the pointer is on the taller card: %v", got)
	}
}

// In the State view a card moves within its state's group the same way.
func TestDraggingInTheStateView(t *testing.T) {
	rows := sessions(3)
	rows[1].State = ipc.StateNeedsYou
	m, b := ctlAt(t, rows, 34, 30)
	m = send(m, keyMsg("esc"))
	m = send(m, keyMsg("v"))
	y := func(name string) int { return lineOf(t, m, name) }
	m = dragAlong(m, y("task-002"), y("task-000"))
	if want := []string{`reorder s000000000002 before "s000000000000"`}; !slices.Equal(b.calls, want) {
		t.Errorf("calls = %v, want %v", b.calls, want)
	}
	m = dragAlong(m, y("task-002"), y("task-001"))
	if len(b.calls) != 1 {
		t.Errorf("a drop on Needs you reordered: %v", b.calls)
	}
}

func TestWithoutControlsTheKeysSaySo(t *testing.T) {
	m, _ := newModel(t, 34, 24, 2)
	m = send(m, keyMsg("f"))
	if !m.statusErr || !strings.Contains(m.status, "update") {
		t.Errorf("status = %q", m.status)
	}
}

// A frozen session's ◆ shows next to a state that comes before frozen, as
// a flag (DESIGN §5.3).
func TestAFrozenFlagSitsBesideTheGlyph(t *testing.T) {
	at := now.Add(-time.Minute)
	rows := sessions(1)
	rows[0].State, rows[0].Frozen, rows[0].ExitCode = ipc.StateExited, &at, code(1)
	m, _ := ctlAt(t, rows, 34, 24)
	if row := screen(t, m)[lineOf(t, m, "task-000")]; !strings.Contains(row, "✕◆ task-000") {
		t.Errorf("row %q", row)
	}
}
