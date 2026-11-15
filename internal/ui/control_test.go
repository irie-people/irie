package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/irie-people/irie/internal/ipc"
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

func (b *ctlBackend) Park(id string, on bool, before string) error {
	call := fmt.Sprintf("park %s %v", id, on)
	if before != "" {
		call += fmt.Sprintf(" before %q", before)
	}
	b.calls = append(b.calls, call)
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
	b.controls = b
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
	// With every session frozen, F thaws them all, but only at a second F,
	// since each agent signs in again; it says what waking the cold costs.
	// Any other key in between cancels it, and so does waiting too long.
	rows := sessions(2)
	for i := range rows {
		rows[i].State = ipc.StateFrozen
	}
	rows[1].Cold = &ipc.Cold{USD: 2.4, Words: "~$2.40 to wake"}
	m, b = ctlModel(t, rows)
	m = send(m, keyMsg("F"))
	if ask := "Press F again to thaw 2 sessions, one every 30 s: each agent signs in again, and waking the one gone cold costs ~$2.40."; len(b.calls) != 0 || m.status != ask {
		t.Fatalf("one F: calls %v, status %q", b.calls, m.status)
	}
	m = send(m, keyMsg("j"))
	m = send(m, keyMsg("F"))
	at := m.now()
	m.now = func() time.Time { return at.Add(thawAllConfirm + time.Second) }
	m = send(m, keyMsg("F"))
	if len(b.calls) != 0 || !strings.Contains(m.status, "Press F again") {
		t.Fatalf("F j F, and a late F: calls %v, status %q", b.calls, m.status)
	}
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

// ! boosts the selected session at once, with its badge, and moves no row:
// the order is the user's; a second ! puts it back.
func TestBangBoostsWithoutMovingTheRow(t *testing.T) {
	m, b := ctlModel(t, sessions(3))
	m = send(m, keyMsg("end"))
	m = send(m, keyMsg("!"))
	want := []string{"s000000000000", "s000000000001", "s000000000002"}
	if got := order(m); !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v: a boost moves nothing", got, want)
	}
	if selectedName(m) != "task-002" || m.sessions[2].Priority != 1 {
		t.Errorf("selection %s, priority %d", selectedName(m), m.sessions[2].Priority)
	}
	got := listText(t, m)
	if i := slices.IndexFunc(got, func(l string) bool { return strings.Contains(l, "task-002") }); i < 0 || got[i] != "◐↑task-002 1m" {
		t.Errorf("no badge on its card: %q", got)
	}
	raw := strings.Split(m.View().Content, "\n")[lineOf(t, m, "↑task-002")]
	if c := cells(raw); c[4].r != "↑" || c[4].fg != rgb(m.pal.accent) || !c[4].bold {
		t.Errorf("badge %q in %s, bold %v", c[4].r, c[4].fg, c[4].bold)
	}
	if got := statusText(t, m); !strings.Contains(got, "boosted") {
		t.Errorf("details %q", got)
	}
	m = send(m, keyMsg("!"))
	calls := []string{"priority s000000000002 1 ", "priority s000000000002 0 "}
	if !slices.Equal(b.calls, calls) {
		t.Errorf("calls = %v, want %v", b.calls, calls)
	}
	if got := order(m); !slices.Equal(got, want) || m.sessions[2].Priority != 0 {
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
		t.Errorf("boosted row %q", row)
	}
}

// A boost leaves the row where it is in its group, and the groups where
// they are.
func TestABoostLeavesTheRowInItsPlace(t *testing.T) {
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
		"▾ code/irie", "irie-theme", "irie-tests",
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

// In the List view a card moves among all the unparked ones.
func TestDraggingInTheListView(t *testing.T) {
	rows := repoSessions()
	for i := range rows {
		rows[i].Order = i
	}
	m, b := ctlAt(t, rows, 34, 80)
	m = send(m, keyMsg("esc"))
	m = send(m, keyMsg("v"))
	y := func(name string) int { return lineOf(t, m, name) }
	m = dragAlong(m, y("irie-tests"), y("web-copy"), y("fork-docs"), y("irie-theme"), y("scratch-notes"), y("web-fix"))
	if want := []string{`reorder s6 before "s1"`}; !slices.Equal(b.calls, want) {
		t.Errorf("calls = %v, want %v", b.calls, want)
	}
	if got := outline(m); got[0] != "irie-tests" {
		t.Errorf("outline %q", got)
	}
}

// A card dragged across Parked's edge is parked there, and a parked one
// dragged back among its own group's cards is unparked; the core hears
// both, each as one park that says where it went. Into any other group it
// goes back, parked as it was.
func TestDraggingAcrossParkedsEdge(t *testing.T) {
	rows := parkedSessions()
	for i := range rows {
		rows[i].Order = i
	}
	m, b := ctlAt(t, rows, 34, 80)
	m = send(m, keyMsg("esc"))
	// Parked starts folded; Z opens it.
	m = send(m, keyMsg("Z"))
	y := func(name string) int { return lineOf(t, m, name) }
	// fork-docs into Parked, between scratch-notes and web-copy.
	m = dragAlong(m, y("fork-docs"), y("scratch-notes"))
	want := []string{`park s4 true before "s3"`}
	if !slices.Equal(b.calls, want) {
		t.Errorf("calls = %v, want %v", b.calls, want)
	}
	if got := outline(m); !slices.Equal(got[len(got)-4:], []string{"▾ Parked", "scratch-notes", "fork-docs", "web-copy"}) {
		t.Errorf("outline %q", got)
	}
	if !strings.Contains(m.status, "parked fork-docs") {
		t.Errorf("status %q", m.status)
	}
	// web-copy back among website's cards, above web-fix.
	b.calls = nil
	m = dragAlong(m, y("web-copy"), y("web-fix"))
	want = []string{`park s5 false before "s1"`}
	if !slices.Equal(b.calls, want) {
		t.Errorf("calls = %v, want %v", b.calls, want)
	}
	if got := outline(m); !slices.Equal(got[:3], []string{"▾ website", "web-copy", "web-fix"}) {
		t.Errorf("outline %q", got)
	}
	// A parked card waits in Parked over another repo's cards, and goes
	// back when dropped there.
	b.calls = nil
	before := outline(m)
	m = send(m, tea.MouseClickMsg{X: 5, Y: y("fork-docs"), Button: tea.MouseLeft})
	m = send(m, tea.MouseMotionMsg{X: 5, Y: y("irie-theme"), Button: tea.MouseLeft})
	if got := outline(m); !slices.Equal(got, before) {
		t.Errorf("over another repo the card moved: %q", got)
	}
	// Across into Parked's other card and back out to code/irie: neither
	// is its group, so the drop puts it back.
	m = send(m, tea.MouseMotionMsg{X: 5, Y: y("scratch-notes"), Button: tea.MouseLeft})
	m = send(m, tea.MouseReleaseMsg{X: 5, Y: y("irie-theme"), Button: tea.MouseLeft})
	if got := outline(m); !slices.Equal(got, before) || len(b.calls) != 0 {
		t.Errorf("a drop on another group: outline %q, calls %v", got, b.calls)
	}
	if s := m.sessions[m.index("s4")]; !s.Parked {
		t.Error("the drop that went back unparked it")
	}
}

// In the List view a card moves among the cards of its own group, those on
// both sides of a group inside it as one, and waits over the cards of a
// group inside its group, where a drop sends it back. Unparked into a
// group inside a folded one, a session's selection goes to the folded
// group's header.
func TestDraggingACardStaysInItsInnermostGroup(t *testing.T) {
	rows, head := treeRows()
	for i := range rows {
		rows[i].Order = i
	}
	m, b := ctlAt(t, rows, 34, 80)
	m = send(m, keyMsg("esc"))
	m = send(m, keyMsg("v"))
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameSnapshot, Sessions: rows, Header: head}})
	y := func(name string) int { return lineOf(t, m, name) }
	// fork-docs, after Acme, to the top of Clients.
	m = dragAlong(m, y("fork-docs"), y("scratch-notes"))
	if want := []string{`reorder s4 before "s2"`}; !slices.Equal(b.calls, want) {
		t.Errorf("calls = %v, want %v", b.calls, want)
	}
	want := []string{"web-fix", "▾ Clients", "fork-docs", "scratch-notes", "▾ Acme", "irie-theme", "▾ Home", "▾ Garden", "web-copy", "irie-tests"}
	if got := outline(m); !slices.Equal(got, want) {
		t.Errorf("outline %q, want %q", got, want)
	}
	// Over Acme's card scratch-notes waits in Clients, and dropped there
	// goes back.
	b.calls = nil
	m = send(m, tea.MouseClickMsg{X: 5, Y: y("scratch-notes"), Button: tea.MouseLeft})
	m = send(m, tea.MouseMotionMsg{X: 5, Y: y("fork-docs"), Button: tea.MouseLeft})
	if got := order(m); !slices.Equal(got, []string{"s1", "s2", "s4", "s3", "s5", "s6"}) {
		t.Fatalf("while dragging: %v", got)
	}
	m = send(m, tea.MouseMotionMsg{X: 5, Y: y("irie-theme"), Button: tea.MouseLeft})
	if got := order(m); !slices.Equal(got, []string{"s1", "s2", "s4", "s3", "s5", "s6"}) {
		t.Errorf("over Acme the card moved: %v", got)
	}
	m = send(m, tea.MouseReleaseMsg{X: 5, Y: y("irie-theme"), Button: tea.MouseLeft})
	if got := outline(m); !slices.Equal(got, want) || len(b.calls) != 0 {
		t.Errorf("a drop on Acme: outline %q, calls %v", got, b.calls)
	}
	if !strings.Contains(m.status, "only among the others of its group") || selectedName(m) != "scratch-notes" {
		t.Errorf("status %q, selection %s", m.status, selectedName(m))
	}
	// irie-theme unparked while Clients is folded.
	rows[2].Parked = true
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameSnapshot, Sessions: rows, Header: head}})
	m = send(m, keyMsg("Z"))
	m.fold(userKey("g1"))
	m = selectNamed(t, m, "irie-theme")
	m = send(m, keyMsg("z"))
	if last := b.calls[len(b.calls)-1]; last != "park s3 false" || selectedName(m) != "Clients" {
		t.Errorf("unparked: call %q, selection %s", last, selectedName(m))
	}
}

// A row held on the list's top line, or above it, scrolls the list up an
// entry at a time and comes along, to the top of its group; one held on
// the bottom line scrolls it down, to the end. The test's runtime waits out
// each step, so a hold here lasts until the list can go no further.
func TestDraggingOnTheListsEdgeScrollsIt(t *testing.T) {
	m, b := ctlAt(t, sessions(12), 34, 12) // one-line rows, more than fit
	m = send(m, keyMsg("esc"))
	m = send(m, keyMsg("end"))
	top, bottom := m.listTop(), m.listTop()+m.listHeight()-1
	if m.offset == 0 || lineOf(t, m, "task-011") != bottom {
		t.Fatalf("the list did not scroll to its end: offset %d", m.offset)
	}
	// Up from the last row, through the middle, onto the top line.
	m = send(m, tea.MouseClickMsg{X: 5, Y: bottom, Button: tea.MouseLeft})
	m = send(m, tea.MouseMotionMsg{X: 5, Y: bottom - 1, Button: tea.MouseLeft})
	m = send(m, tea.MouseMotionMsg{X: 5, Y: top, Button: tea.MouseLeft})
	if m.offset != 0 {
		t.Errorf("held on the top line, the list stopped at offset %d", m.offset)
	}
	if got := order(m); got[0] != "s000000000011" {
		t.Errorf("the held row did not come along to the top: %v", got)
	}
	m = send(m, tea.MouseReleaseMsg{X: 5, Y: top, Button: tea.MouseLeft})
	if want := []string{`reorder s000000000011 before "s000000000000"`}; !slices.Equal(b.calls, want) {
		t.Errorf("calls = %v, want %v", b.calls, want)
	}

	// Down from the first row onto the command bar, below the list.
	first := lineOf(t, m, "task-011")
	m = send(m, tea.MouseClickMsg{X: 5, Y: first, Button: tea.MouseLeft})
	m = send(m, tea.MouseMotionMsg{X: 5, Y: first + 1, Button: tea.MouseLeft})
	m = send(m, tea.MouseMotionMsg{X: 5, Y: m.barTop(), Button: tea.MouseLeft})
	if want := m.maxOffset(m.items()); m.offset != want {
		t.Errorf("held below the list, it stopped at offset %d, not %d", m.offset, want)
	}
	if got := order(m); got[len(got)-1] != "s000000000011" {
		t.Errorf("the held row did not come along to the end: %v", got)
	}
	m = send(m, tea.MouseReleaseMsg{X: 5, Y: m.barTop(), Button: tea.MouseLeft})
	if last := b.calls[len(b.calls)-1]; last != `reorder s000000000011 before ""` {
		t.Errorf("drop at the end: %q", last)
	}
}

// Once the list has eight lines, its edge is two lines deep: the line next
// to the edge scrolls it too, at half the pace of the edge's own line and
// the rows past it.
func TestTheListsEdgeIsTwoLinesDeep(t *testing.T) {
	m, b := ctlAt(t, sessions(30), 34, 30) // cards
	m = send(m, keyMsg("esc"))
	top, h := m.listTop(), m.listHeight()
	if h < 8 || m.compact() {
		t.Fatalf("a list of %d lines, compact %v", h, m.compact())
	}
	for _, c := range []struct {
		y, dir int
		every  time.Duration
	}{
		{top - 1, -1, dragScrollFast}, {top, -1, dragScrollFast}, {top + 1, -1, dragScrollSlow}, {top + 2, 0, 0},
		{top + h - 3, 0, 0}, {top + h - 2, 1, dragScrollSlow}, {top + h - 1, 1, dragScrollFast}, {top + h, 1, dragScrollFast},
	} {
		if dir, every := m.edge(c.y); dir != c.dir || every != c.every {
			t.Errorf("row %d (list %d to %d): %d every %v, want %d every %v", c.y, top, top+h-1, dir, every, c.dir, c.every)
		}
	}
	short, _ := ctlAt(t, sessions(12), 34, 12)
	if dir, _ := short.edge(short.listTop() + 1); short.listHeight() >= 8 || dir != 0 {
		t.Errorf("a list of %d lines scrolls from its second line: %d", short.listHeight(), dir)
	}
	// Held on the line next to the top edge, a card goes up past the three
	// cards scrolled out of sight, to the top of its group.
	m.scroll(3)
	from := lineOf(t, m, "task-004")
	m = send(m, tea.MouseClickMsg{X: 5, Y: from, Button: tea.MouseLeft})
	m = send(m, tea.MouseMotionMsg{X: 5, Y: from - 1, Button: tea.MouseLeft})
	m = send(m, tea.MouseMotionMsg{X: 5, Y: top + 1, Button: tea.MouseLeft})
	if m.offset != 0 || order(m)[0] != "s000000000004" {
		t.Errorf("held next to the edge: offset %d, order %v", m.offset, order(m)[:4])
	}
	m = send(m, tea.MouseReleaseMsg{X: 5, Y: top + 1, Button: tea.MouseLeft})
	if want := []string{`reorder s000000000004 before "s000000000000"`}; !slices.Equal(b.calls, want) {
		t.Errorf("calls = %v, want %v", b.calls, want)
	}
}

// A press on the list's edge that never leaves its row scrolls nothing, and
// a scroll still on its way when the drag is let go of moves nothing.
func TestAnEdgeWaitsForTheDragToMove(t *testing.T) {
	m, b := ctlAt(t, sessions(12), 34, 12)
	m = send(m, keyMsg("esc"))
	m = send(m, keyMsg("end"))
	top, at := m.listTop(), m.offset
	m = send(m, tea.MouseClickMsg{X: 5, Y: top, Button: tea.MouseLeft})
	m = send(m, tea.MouseMotionMsg{X: 9, Y: top, Button: tea.MouseLeft})
	if m.offset != at {
		t.Errorf("a press on the top line that stayed on it scrolled the list to %d", m.offset)
	}
	m = send(m, tea.MouseReleaseMsg{X: 9, Y: top, Button: tea.MouseLeft})
	// A scroll that comes after the drag ends moves nothing.
	d := &drag{id: "s000000000000", row: top, left: true}
	m.drag = d
	m = send(m, tea.MouseReleaseMsg{X: 5, Y: top + 2, Button: tea.MouseLeft})
	m = send(m, dragScrollMsg{d})
	if m.offset != at || len(b.calls) != 0 {
		t.Errorf("a late scroll moved the list to %d, calls %v", m.offset, b.calls)
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

// z parks the selected session at once, out of sight in Parked, which starts
// folded, and the selection goes to Parked's header; z again unparks it,
// last among the rest, just above Parked. Z opens Parked, and with it open
// the selection goes with a session parked, which goes last in Parked; Z
// folds it again, taking a selection inside it to its header. :park and
// :unpark do the same as z.
func TestZParksAndUnparks(t *testing.T) {
	m, b := ctlAt(t, sessions(4), 34, 60)
	m = send(m, keyMsg("esc"))
	m = send(m, keyMsg("Z"))
	if !strings.Contains(m.status, "nothing is parked") {
		t.Errorf("Z with nothing parked: %q", m.status)
	}
	m = selectNamed(t, m, "task-001")
	m = send(m, keyMsg("z"))
	if want := []string{"park s000000000001 true"}; !slices.Equal(b.calls, want) {
		t.Fatalf("calls = %v, want %v", b.calls, want)
	}
	want := []string{"▾ Not in a repo", "task-000", "task-002", "task-003", "▸ Parked"}
	if got := outline(m); !slices.Equal(got, want) {
		t.Errorf("outline %q, want %q", got, want)
	}
	if selectedName(m) != "Parked" || !strings.Contains(m.status, "parked task-001") {
		t.Errorf("selection %s, status %q", selectedName(m), m.status)
	}
	// Z opens Parked, and folds it around a selection inside it.
	m = send(m, keyMsg("Z"))
	want = []string{"▾ Not in a repo", "task-000", "task-002", "task-003", "▾ Parked", "task-001"}
	if got := outline(m); !slices.Equal(got, want) {
		t.Errorf("after Z: outline %q, want %q", got, want)
	}
	m = selectNamed(t, m, "task-001")
	m = send(m, keyMsg("Z"))
	if got := outline(m); got[len(got)-1] != "▸ Parked" || selectedName(m) != "Parked" {
		t.Errorf("after Z again: outline %q, selection %s", got, selectedName(m))
	}
	m = send(m, keyMsg("Z"))
	m = selectNamed(t, m, "task-001")
	m = send(m, keyMsg("z"))
	if got := outline(m); !slices.Equal(got, []string{"▾ Not in a repo", "task-000", "task-002", "task-003", "task-001"}) {
		t.Errorf("unparked, the outline is %q", got)
	}
	if last := b.calls[len(b.calls)-1]; last != "park s000000000001 false" || selectedName(m) != "task-001" {
		t.Errorf("last call %q, selection %s", last, selectedName(m))
	}
	// Parked stays open: the selection goes with a session parked into it.
	m = selectNamed(t, m, "task-003")
	m = send(m, keyMsg("z"))
	if selectedName(m) != "task-003" {
		t.Errorf("parked into an open Parked, the selection is on %s", selectedName(m))
	}
	// Parking into a folded Parked takes the selection to its header.
	m = send(m, keyMsg("Z"))
	m = selectNamed(t, m, "task-000")
	m = send(m, keyMsg("z"))
	if selectedName(m) != "Parked" {
		t.Errorf("parked into a folded Parked, the selection is on %s", selectedName(m))
	}
	// The commands.
	m = send(m, keyMsg("Z"))
	if got, want := outline(m), []string{"▾ Not in a repo", "task-002", "task-001", "▾ Parked", "task-003", "task-000"}; !slices.Equal(got, want) {
		t.Errorf("the last parked goes last: outline %q, want %q", got, want)
	}
	m = selectNamed(t, m, "task-002")
	m = typeText(send(m, keyMsg("tab")), ":park")
	m = send(m, keyMsg("enter"))
	if last := b.calls[len(b.calls)-1]; last != "park s000000000002 true" {
		t.Errorf(":park called %q", last)
	}
	// The core says it is parked, and the next :park reads that.
	parked := m.sessions[m.index("s000000000002")]
	parked.Parked = true
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameUpsert, Session: &parked}})
	m = typeText(send(m, keyMsg("tab")), ":park")
	m = send(m, keyMsg("enter"))
	if !m.statusErr || !strings.Contains(m.status, "must be not parked") {
		t.Errorf(":park of a parked session: %q", m.status)
	}
	m = send(m, keyMsg("esc"))
	m = typeText(send(m, keyMsg("tab")), ":unpark")
	m = send(m, keyMsg("enter"))
	if last := b.calls[len(b.calls)-1]; last != "park s000000000002 false" {
		t.Errorf(":unpark called %q", last)
	}
}

// Boosting a parked session leaves it in Parked, as the core does.
func TestBoostingAParkedSessionLeavesItParked(t *testing.T) {
	rows := sessions(3)
	rows[2].Parked = true
	m, _ := ctlAt(t, rows, 34, 60)
	m = send(m, keyMsg("esc"))
	m = send(m, keyMsg("Z"))
	m = selectNamed(t, m, "task-002")
	before := outline(m)
	m = send(m, keyMsg("!"))
	if got := outline(m); !slices.Equal(got, before) || !m.sessions[2].Parked || m.sessions[2].Priority != 1 {
		t.Errorf("outline %q, want %q; parked %v, priority %d", got, before, m.sessions[2].Parked, m.sessions[2].Priority)
	}
}

// A session that goes last goes after the others of its machine, as the
// core puts it: another machine's rows come after this one's.
func TestLastOfHostIsAfterItsMachinesSessions(t *testing.T) {
	rows := []ipc.Session{{ID: "s1"}, {ID: "s2"}, {ID: "asahi/s3", Host: "asahi"}, {ID: "asahi/s4", Host: "asahi"}}
	for _, c := range []struct {
		host   string
		at, to int
	}{
		{"", 0, 2},      // this machine's: after s2, before asahi's
		{"asahi", 2, 4}, // asahi's: after asahi/s4
		{"pi", 4, 4},    // the only one of its machine: where it was
	} {
		if got := lastOfHost(rows, c.host, c.at); got != c.to {
			t.Errorf("lastOfHost(%q, %d) = %d, want %d", c.host, c.at, got, c.to)
		}
	}
}
