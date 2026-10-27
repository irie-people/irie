package ui

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/irie-hq/irie/internal/ipc"
)

// repoSessions are sessions in three repos, two of them called irie, and
// one in none, started in the order given.
func repoSessions() []ipc.Session {
	rows := []ipc.Session{
		{ID: "s1", Name: "web-fix", Repo: "/home/alice/code/website", State: ipc.StateWorking},
		{ID: "s2", Name: "scratch-notes", State: ipc.StateIdle},
		{ID: "s3", Name: "irie-theme", Repo: "/home/alice/code/irie", State: ipc.StateNeedsYou, Detail: "Asks: may it run rm -rf build?"},
		{ID: "s4", Name: "fork-docs", Repo: "/home/alice/forks/irie", State: ipc.StateDone},
		{ID: "s5", Name: "web-copy", Repo: "/home/alice/code/website", State: ipc.StateFailed},
		{ID: "s6", Name: "irie-tests", Repo: "/home/alice/code/irie", State: ipc.StateStarting},
	}
	for i := range rows {
		rows[i].Created = now.Add(-time.Duration(len(rows)-i) * time.Minute)
		rows[i].Cwd = "/home/alice/code"
	}
	return rows
}

func repoModel(t *testing.T, w, h int) Model {
	t.Helper()
	m, _ := newModel(t, w, h, 0)
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameSnapshot, Sessions: repoSessions(), Header: &ipc.Header{}}})
	return send(m, keyMsg("esc"))
}

// outline is the list's entries: a group as "▾ title count" and a session
// by its name.
func outline(m Model) []string {
	var out []string
	for _, it := range m.items() {
		switch it.kind {
		case itemGroup:
			arrow := "▾ "
			if it.group.folded {
				arrow = "▸ "
			}
			out = append(out, arrow+it.group.title)
		default:
			out = append(out, entryName(m, it))
		}
	}
	return out
}

// The Repo view, where the sidebar opens: a group per repo, named by its
// folder and told apart by the parent folder when two share a name, in the
// order their first session started, then Not in a repo, then Outside Irie.
// Sessions keep their start order, so a state change moves nothing.
func TestTheRepoViewGroupsByRepo(t *testing.T) {
	m := repoModel(t, 34, 80)
	m = send(m, outsideFrame(outsideSessions(1), ""))
	if m.view != byRepo {
		t.Fatalf("the sidebar opens in %v", m.view)
	}
	want := []string{
		"▾ website", "web-fix", "web-copy",
		"▾ code/irie", "irie-theme", "irie-tests",
		"▾ forks/irie", "fork-docs",
		"▾ Not in a repo", "scratch-notes",
		"▾ Outside Irie", "outside-00",
	}
	if got := outline(m); !reflect.DeepEqual(got, want) {
		t.Errorf("outline %q, want %q", got, want)
	}
	if list := listText(t, m); list[0] != "▾ website 2" {
		t.Errorf("first header %q, want its count", list[0])
	}
	before := listText(t, m)
	rows := repoSessions()
	rows[0].State, rows[2].State = ipc.StateDone, ipc.StateWorking
	for i := range rows[:3] {
		m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameUpsert, Session: &rows[i]}})
	}
	if got := outline(m); !reflect.DeepEqual(got, want) {
		t.Errorf("after state changes the outline is %q", got)
	}
	if after := listText(t, m); len(after) != len(before) {
		t.Errorf("state changes moved lines: %d → %d", len(before), len(after))
	}
}

func TestRepoNamesTellReposApart(t *testing.T) {
	got := repoNames([]string{"/home/alice/code/irie", "/home/alice/forks/irie", "/home/alice/code/website", "/srv/a/x/api", "/srv/b/x/api"})
	want := map[string]string{
		"/home/alice/code/irie": "code/irie", "/home/alice/forks/irie": "forks/irie", "/home/alice/code/website": "website",
		"/srv/a/x/api": "a/x/api", "/srv/b/x/api": "b/x/api",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("names %v, want %v", got, want)
	}
}

// The List view: one list in the core's order, with no header, so a state
// change moves nothing. v switches, and so does a click on a tab.
func TestTheListViewKeepsOneOrder(t *testing.T) {
	m := repoModel(t, 34, 80)
	m = send(m, keyMsg("v"))
	if m.view != byList {
		t.Fatalf("v left the view at %v", m.view)
	}
	want := []string{"web-fix", "scratch-notes", "irie-theme", "fork-docs", "web-copy", "irie-tests"}
	if got := outline(m); !reflect.DeepEqual(got, want) {
		t.Errorf("outline %q, want %q", got, want)
	}
	before := listText(t, m)
	rows := repoSessions()
	rows[0].State, rows[2].State, rows[3].State = ipc.StateDone, ipc.StateWorking, ipc.StateNeedsYou
	for i := range rows[:4] {
		m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameUpsert, Session: &rows[i]}})
	}
	if got := outline(m); !reflect.DeepEqual(got, want) {
		t.Errorf("after state changes the outline is %q", got)
	}
	if after := listText(t, m); len(after) != len(before) {
		t.Errorf("state changes moved lines: %d → %d", len(before), len(after))
	}
	// The selection stays on its session across views.
	m = selectNamed(t, m, "fork-docs")
	m = send(m, keyMsg("v"))
	if m.view != byRepo || selectedName(m) != "fork-docs" {
		t.Errorf("view %v, selection %s", m.view, selectedName(m))
	}
	m = send(m, keyMsg("v"))
	if m.view != byList || selectedName(m) != "fork-docs" {
		t.Errorf("view %v, selection %s", m.view, selectedName(m))
	}
	m = send(m, keyMsg("v"))
	// Clicks on the tabs, under the header's two lines.
	lines, y := screen(t, m), m.tabsTop()
	if y != 2 || lines[y] != "  List │ Repo                     " {
		t.Fatalf("tab bar on line %d: %q", y, lines[y])
	}
	m = send(m, tea.MouseClickMsg{X: strings.Index(lines[y], "List"), Y: y, Button: tea.MouseLeft})
	if m.view != byList {
		t.Errorf("a click on List left the view at %v", m.view)
	}
	m = send(m, tea.MouseClickMsg{X: strings.Index(lines[y], "Repo") - 2, Y: y, Button: tea.MouseLeft})
	if m.view != byRepo {
		t.Errorf("a click on Repo left the view at %v", m.view)
	}
	// The tab in use is on the selection fill.
	raw := strings.Split(m.View().Content, "\n")[y]
	if c := cells(raw); c[strings.Index(lines[y], "List")].bg != rgb(m.pal.sidebar) || c[11].bg != rgb(m.pal.selection) {
		t.Errorf("tabs: List on %s, Repo on %s", c[2].bg, c[11].bg)
	}
}

// parkedSessions are repoSessions with web-copy, which failed, and
// scratch-notes parked.
func parkedSessions() []ipc.Session {
	rows := repoSessions()
	rows[1].Parked, rows[4].Parked = true, true
	return rows
}

// Parked comes after the view's groups, in either view, and before Outside
// Irie; a parked session shows only there. Its title is red while one of
// them needs you, and folded it says how many do.
func TestParkedComesLast(t *testing.T) {
	m := repoModel(t, 34, 80)
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameSnapshot, Sessions: parkedSessions(), Header: &ipc.Header{}}})
	m = send(m, outsideFrame(outsideSessions(1), ""))
	want := []string{
		"▾ website", "web-fix",
		"▾ code/irie", "irie-theme", "irie-tests",
		"▾ forks/irie", "fork-docs",
		"▾ Parked", "scratch-notes", "web-copy",
		"▾ Outside Irie", "outside-00",
	}
	if got := outline(m); !reflect.DeepEqual(got, want) {
		t.Errorf("Repo view %q, want %q", got, want)
	}
	m = send(m, keyMsg("v"))
	want = []string{
		"web-fix", "irie-theme", "fork-docs", "irie-tests",
		"▾ Parked", "scratch-notes", "web-copy",
		"▾ Outside Irie", "outside-00",
	}
	if got := outline(m); !reflect.DeepEqual(got, want) {
		t.Errorf("List view %q, want %q", got, want)
	}
	y := lineOf(t, m, "Parked")
	if row := screen(t, m)[y]; !strings.Contains(row, "▾ Parked  2") {
		t.Errorf("header %q", row)
	}
	// The colour of the P of Parked.
	title := func() string {
		y := lineOf(t, m, "Parked")
		line := screen(t, m)[y]
		x := len([]rune(line[:strings.Index(line, "Parked")]))
		return cells(strings.Split(m.View().Content, "\n")[y])[x].fg
	}
	if title() != rgb(m.pal.needs) {
		t.Errorf("Parked's title is %s with a failed session in it, want red", title())
	}
	m = selectNamed(t, m, "Parked")
	m = send(m, keyMsg("enter"))
	if row := screen(t, m)[lineOf(t, m, "Parked")]; !strings.Contains(row, "▸ Parked  2 · 1 needs you") {
		t.Errorf("folded header %q", row)
	}
	if got := statusText(t, m); !strings.Contains(got, "z unparks one") {
		t.Errorf("the details on Parked say %q", got)
	}
	// Both views have Parked, so its header stays selected across them.
	m = send(m, keyMsg("v"))
	if m.view != byRepo || selectedName(m) != "Parked" {
		t.Errorf("view %v, selection %s", m.view, selectedName(m))
	}
	m = send(m, keyMsg("v"))
	// With nothing in it that needs you, its title is ink.
	rows := parkedSessions()
	rows[4].State = ipc.StateExited
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameUpsert, Session: &rows[4]}})
	if title() != rgb(m.pal.ink) {
		t.Errorf("Parked's title is %s with nothing in it that needs you", title())
	}
	if row := screen(t, m)[lineOf(t, m, "Parked")]; strings.Contains(row, "needs you") {
		t.Errorf("folded header %q", row)
	}
	// With nothing parked, there is no Parked.
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameSnapshot, Sessions: repoSessions(), Header: &ipc.Header{}}})
	if got := strings.Join(outline(m), " "); strings.Contains(got, "Parked") {
		t.Errorf("outline %q", got)
	}
}

// A header folds its group and unfolds it again, from Enter, Space or a
// click. Folded, it says how many inside need you, in red, and the arrows
// walk only what shows.
func TestGroupsFold(t *testing.T) {
	m := repoModel(t, 34, 80)
	m = selectNamed(t, m, "code/irie")
	m = send(m, keyMsg("enter"))
	if got := outline(m); !reflect.DeepEqual(got[3:5], []string{"▸ code/irie", "▾ forks/irie"}) {
		t.Fatalf("after Enter: %q", got)
	}
	y := lineOf(t, m, "▸ code/irie")
	if row := screen(t, m)[y]; !strings.Contains(row, "▸ code/irie  2 · 1 needs you") {
		t.Errorf("folded header %q", row)
	}
	raw := strings.Split(m.View().Content, "\n")[y]
	if !hasFG(raw, m.pal.needs) {
		t.Errorf("the folded header's needs-you count is not red: %q", raw)
	}
	m = send(m, keyMsg("down"))
	if selectedName(m) != "forks/irie" {
		t.Errorf("down from a folded header went to %s", selectedName(m))
	}
	m = send(m, keyMsg("up"))
	m = send(m, keyMsg("space"))
	if got := outline(m); got[3] != "▾ code/irie" || got[4] != "irie-theme" {
		t.Errorf("after Space: %q", got)
	}
	// A click on a header selects and folds it.
	m = click(m, 4, lineOf(t, m, "website"))
	if selectedName(m) != "website" || outline(m)[0] != "▸ website" {
		t.Errorf("after a click: selection %s, outline %q", selectedName(m), outline(m))
	}
	// Folding the group of the selected session puts the selection on its
	// header.
	m = selectNamed(t, m, "fork-docs")
	m.fold("repo:/home/alice/forks/irie")
	if selectedName(m) != "forks/irie" {
		t.Errorf("folded around the selection, which is now on %s", selectedName(m))
	}
	// A frame from the core keeps a group folded.
	rows := repoSessions()
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameUpsert, Session: &rows[3]}})
	if !strings.Contains(strings.Join(outline(m), " "), "▸ forks/irie") {
		t.Errorf("an upsert unfolded a group: %q", outline(m))
	}
}

// The card under the pointer takes the hover fill, and so does an entry or
// a tab; the margins and the rest of the sidebar light nothing, and the
// light goes out when the pane loses focus.
func TestHoverLightsWhatIsUnderThePointer(t *testing.T) {
	m := repoModel(t, 34, 30)
	fill := func(y int) string { return cells(strings.Split(m.View().Content, "\n")[y])[4].bg }
	y := lineOf(t, m, "irie-tests")
	if fill(y) != rgb(m.pal.card) {
		t.Fatalf("a quiet card on %s", fill(y))
	}
	m = send(m, tea.MouseMotionMsg{X: 10, Y: y + 1})
	if m.hover != sessionKey("s6") || fill(y) != rgb(m.pal.hover) || fill(y+1) != rgb(m.pal.hover) {
		t.Errorf("hover %q, card on %s", m.hover, fill(y))
	}
	if fill(lineOf(t, m, "web-fix")) != rgb(m.pal.selection) {
		t.Errorf("the selected card lost its fill to the hover")
	}
	m = send(m, tea.MouseMotionMsg{X: 0, Y: y})
	if m.hover != "" || fill(y) != rgb(m.pal.card) {
		t.Errorf("in the margin: hover %q", m.hover)
	}
	m = send(m, tea.MouseMotionMsg{X: 10, Y: y})
	m = send(m, tea.BlurMsg{})
	if m.hover != "" {
		t.Errorf("after the pane lost focus: hover %q", m.hover)
	}
	// A header and a tab.
	m = send(m, tea.MouseMotionMsg{X: 6, Y: lineOf(t, m, "code/irie")})
	if m.hover != "repo:/home/alice/code/irie" {
		t.Errorf("over a header: hover %q", m.hover)
	}
	m = send(m, tea.MouseMotionMsg{X: 3, Y: m.tabsTop()})
	if raw := strings.Split(m.View().Content, "\n")[m.tabsTop()]; m.hover != tabKey(byList) || cells(raw)[3].bg != rgb(m.pal.hover) {
		t.Errorf("over the List tab: hover %q", m.hover)
	}
	m = send(m, tea.MouseMotionMsg{X: 3, Y: 0})
	if m.hover != "" {
		t.Errorf("over the header: hover %q", m.hover)
	}
	// The wheel scrolls under the pointer and keeps what it is over lit.
	m = send(m, tea.MouseWheelMsg{X: 10, Y: 6, Button: tea.MouseWheelDown})
	if it, _, _ := m.at(6); m.hover != m.items()[it].key {
		t.Errorf("after the wheel: hover %q", m.hover)
	}
}

// A card has the state's glyph, the name and the age, then what it does,
// then up to three of its helpers and how many more there are.
func TestCardsShowTheirHelpers(t *testing.T) {
	m := repoModel(t, 34, 40)
	rows := repoSessions()
	rows[0].Detail = "Running the tests"
	for _, kind := range []string{"Explore", "Plan", "", "general-purpose", "Explore"} {
		rows[0].Subagents = append(rows[0].Subagents, ipc.Subagent{Type: kind, Detail: "Reading DESIGN.md"})
	}
	rows[0].Subagents[1].NeedsYou, rows[0].Subagents[1].Detail = true, "Asks: may it edit go.mod?"
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameUpsert, Session: &rows[0]}})
	y := lineOf(t, m, "web-fix")
	got := screen(t, m)[y-1 : y+7]
	want := []string{
		" ╭──────────────────────────────╮ ",
		" │ ◐ web-fix                 6m │ ",
		" │ Running the tests            │ ",
		" │ ↳ Explore  Reading DESIGN.md │ ",
		" │ ↳ Plan  Asks: may it edit g… │ ",
		" │ ↳ agent  Reading DESIGN.md   │ ",
		" │ and 2 more                   │ ",
		" ╰──────────────────────────────╯ ",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("card:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if raw := strings.Split(m.View().Content, "\n")[y+3]; !hasFG(raw, m.pal.needs) {
		t.Errorf("a helper that needs you is not red: %q", raw)
	}
}

// Narrow panes, and lists too short for a card, draw one-line rows in the
// same palette, so DESIGN §4.1's edge cases hold; a growing command bar
// never turns cards into rows.
func TestSmallPanesFallBackToRows(t *testing.T) {
	for _, c := range []struct {
		w, h    int
		compact bool
	}{
		{34, 30, false}, {34, 15, false}, {24, 30, false},
		{34, 14, true}, {23, 30, true}, {34, 8, true}, {34, 5, true}, {34, 3, true},
	} {
		m := repoModel(t, c.w, c.h)
		if m.compact() != c.compact {
			t.Errorf("%d×%d: compact %v, want %v", c.w, c.h, m.compact(), c.compact)
			continue
		}
		lines := screen(t, m)
		if len(lines) != c.h || !strings.Contains(lines[0], "Irie") {
			t.Errorf("%d×%d: %d lines, header %q", c.w, c.h, len(lines), lines[0])
		}
		if m.listHeight() < 1 {
			t.Errorf("%d×%d: no list row", c.w, c.h)
		}
		if c.compact {
			for i, l := range lines[m.listTop() : m.listTop()+m.listHeight()] {
				if strings.ContainsAny(l, "╭╰") {
					t.Errorf("%d×%d: list line %d is a card's border: %q", c.w, c.h, i, l)
				}
			}
			if m.listHeight() > 1 {
				if y := lineOf(t, m, "web-fix"); !strings.HasPrefix(lines[y], "▌◐ web-fix") {
					t.Errorf("%d×%d: row %q", c.w, c.h, lines[y])
				}
			}
		}
	}
	m := repoModel(t, 34, 15)
	m = send(m, keyMsg("tab"))
	for range 7 {
		m = send(m, keyMsg("ctrl+j"))
	}
	if m.compact() {
		t.Error("a growing command bar turned the cards into rows")
	}
}

// Under spawn.close_when_done a finished session closes itself: the header
// has no To verify count. Both views show every session until it goes.
func TestCloseWhenDoneDropsTheCount(t *testing.T) {
	m := repoModel(t, 100, 80)
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameHeader, Header: &ipc.Header{CloseWhenDone: true}}})
	if header := screen(t, m)[0]; strings.Contains(header, "to verify") || !strings.Contains(header, "working") {
		t.Errorf("header %q", header)
	}
	for range 2 {
		if got := strings.Join(outline(m), " "); !strings.Contains(got, "fork-docs") {
			t.Errorf("the %v view lost the done session: %s", m.view, got)
		}
		m = send(m, keyMsg("v"))
	}
}
