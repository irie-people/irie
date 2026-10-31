package ui

import (
	"errors"
	"fmt"
	"image/color"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/route"
)

var now = time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)

type showCall struct {
	id    string
	focus bool
}

type fakeBackend struct {
	spawns    []ipc.SpawnArgs
	shows     []showCall
	spawnErr  error
	spawned   *ipc.Session // what Spawn returns, when set
	completes []string
	items     map[string][]string // Complete's answers
	notes     map[string][]string // and its notes
	moves     []string            // the vendor ids Move was given
	moveErr   error
	rates     []string // id verdict
	renames   []string // id name
	tools     []string
	routes    []string
	choice    route.Choice // what Route returns
	closes    []string     // the ids Close was given
	reopens   []string     // the ids Reopen was given
	reopenErr error
	undos     []string // dir choice_id choice, each Undo and Redo
	undoReply func(dir string, a ipc.UndoArgs) (ipc.UndoResult, error)
}

func (f *fakeBackend) Rate(id, verdict string) error {
	f.rates = append(f.rates, id+" "+verdict)
	return nil
}

// Rename numbers the name, as the core does when another row has it.
func (f *fakeBackend) Rename(id, name string) (ipc.Session, error) {
	f.renames = append(f.renames, id+" "+name)
	return ipc.Session{ID: id, Name: name + "-2"}, nil
}

func (f *fakeBackend) Tool(name string) (ipc.Session, error) {
	f.tools = append(f.tools, name)
	return ipc.Session{ID: "s0000000tool0", Name: name}, nil
}

func (f *fakeBackend) Route(task string) (route.Choice, error) {
	f.routes = append(f.routes, task)
	return f.choice, nil
}

func (f *fakeBackend) Spawn(a ipc.SpawnArgs) (ipc.Session, error) {
	f.spawns = append(f.spawns, a)
	if f.spawnErr != nil {
		return ipc.Session{}, f.spawnErr
	}
	if f.spawned != nil {
		return *f.spawned, nil
	}
	return ipc.Session{ID: fmt.Sprintf("snew%08d", len(f.spawns)), Name: "new"}, nil
}

func (f *fakeBackend) Show(id string, focus bool) error {
	f.shows = append(f.shows, showCall{id, focus})
	return nil
}

func (f *fakeBackend) Complete(text string) (ipc.CompleteResult, error) {
	f.completes = append(f.completes, text)
	return ipc.CompleteResult{Items: f.items[text], Notes: f.notes[text]}, nil
}

func sessions(n int) []ipc.Session {
	out := make([]ipc.Session, n)
	for i := range out {
		out[i] = ipc.Session{
			ID:      fmt.Sprintf("s%012d", i),
			Name:    fmt.Sprintf("task-%03d", i),
			State:   ipc.StateWorking,
			Created: now.Add(-time.Duration(n-i) * time.Minute),
			Cwd:     "/home/alice/code/website",
		}
	}
	return out
}

// send feeds msg to m and runs the commands it returns, feeding their
// messages back, the way the Bubble Tea runtime would.
func send(m Model, msg tea.Msg) Model {
	next, cmd := m.Update(msg)
	return run(next.(Model), cmd)
}

func run(m Model, cmd tea.Cmd) Model {
	if cmd == nil {
		return m
	}
	switch msg := cmd().(type) {
	case nil:
		return m
	case tea.BatchMsg:
		for _, c := range msg {
			m = run(m, c)
		}
		return m
	default:
		return send(m, msg)
	}
}

func keyMsg(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "home":
		return tea.KeyPressMsg{Code: tea.KeyHome}
	case "end":
		return tea.KeyPressMsg{Code: tea.KeyEnd}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "ctrl+j":
		return tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl}
	}
	r := []rune(s)
	return tea.KeyPressMsg{Code: r[0], Text: s}
}

func typeText(m Model, s string) Model {
	for _, r := range s {
		m = send(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

func newModel(t *testing.T, w, h, n int) (Model, *fakeBackend) {
	t.Helper()
	b := &fakeBackend{}
	m := New(b, Options{Now: func() time.Time { return now }, Home: "/home/alice"})
	m = send(m, tea.WindowSizeMsg{Width: w, Height: h})
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameSnapshot, Sessions: sessions(n), Header: &ipc.Header{}}})
	return m, b
}

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;:?<=>]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)`)

func screen(t *testing.T, m Model) []string {
	t.Helper()
	return strings.Split(ansiRE.ReplaceAllString(m.View().Content, ""), "\n")
}

// selectNamed selects the session, the session outside Irie or the group
// header named name, as the arrow keys would.
func selectNamed(t *testing.T, m Model, name string) Model {
	t.Helper()
	for i, it := range m.items() {
		if entryName(m, it) == name {
			m.sel = i
			m.reveal()
			return m
		}
	}
	t.Fatalf("no entry %s", name)
	return m
}

func entryName(m Model, it item) string {
	switch it.kind {
	case itemSession:
		return m.sessions[it.s].Name
	case itemOutside:
		return outsideLabel(m.outside[it.o])
	}
	return it.group.title
}

// selectedName is the name of the selected entry.
func selectedName(m Model) string {
	if it, ok := m.selected(); ok {
		return entryName(m, it)
	}
	return ""
}

// boxRunes are the runes of borders and edges, which tests read past.
const boxRunes = "│╭╮╰╯─▌"

// plain is a line's text without its borders and edges, with each run of
// spaces made one.
func plain(l string) string {
	return strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if strings.ContainsRune(boxRunes, r) {
			return ' '
		}
		return r
	}, l)), " ")
}

// statusText is what the status strip, or the details card with its title,
// says, as one line of text.
func statusText(t *testing.T, m Model) string {
	t.Helper()
	top := m.listTop() + m.listHeight()
	var parts []string
	for _, l := range screen(t, m)[top : top+m.statusHeight()] {
		if p := plain(l); p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " ")
}

// barText is the command bar's lines of text.
func barText(t *testing.T, m Model) []string {
	t.Helper()
	return screen(t, m)[m.barTop() : m.barTop()+m.barHeight()]
}

// lineOf is the screen row of the first line that holds s.
func lineOf(t *testing.T, m Model, s string) int {
	t.Helper()
	for i, l := range screen(t, m) {
		if strings.Contains(l, s) {
			return i
		}
	}
	t.Fatalf("%q is not on screen:\n%s", s, strings.Join(screen(t, m), "\n"))
	return -1
}

// listText is the list's lines, read past borders and edges.
func listText(t *testing.T, m Model) []string {
	t.Helper()
	var out []string
	for _, l := range screen(t, m)[m.listTop() : m.listTop()+m.listHeight()] {
		if p := plain(l); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func TestHeaderAndCommandBarArePinned(t *testing.T) {
	for _, h := range []int{6, 24, 80} {
		for _, n := range []int{0, 3, 100} {
			m, _ := newModel(t, 34, h, n)
			lines := screen(t, m)
			if len(lines) != h {
				t.Errorf("34×%d, %d rows: %d lines, want %d", h, n, len(lines), h)
				continue
			}
			if !strings.Contains(lines[0], "☀ Irie") {
				t.Errorf("34×%d, %d rows: first line %q is not the header", h, n, lines[0])
			}
			// The command bar's text, in its box when it has one, ends the pane.
			bottom := m.barTop() + m.barHeight() + b2i(m.tabbed())
			if bar := barText(t, m); bottom != h || !strings.Contains(bar[0], barPrompt) {
				t.Errorf("34×%d, %d rows: bar %q ends on row %d", h, n, bar, bottom)
			}
			if m.tabbed() && !strings.HasPrefix(lines[h-1], " ╰") {
				t.Errorf("34×%d: last line %q is not the bar's box", h, lines[h-1])
			}
			for i, l := range lines {
				if w := lipgloss.Width(l); w > 34 {
					t.Errorf("34×%d: line %d is %d columns: %q", h, i, w, l)
				}
			}
			if list := listText(t, m); n > 0 && !strings.Contains(strings.Join(list, "\n"), "task-000") {
				t.Errorf("34×%d: list %q lacks the first row", h, list)
			}
		}
	}
}

func TestShortPaneHidesTheStatusStrip(t *testing.T) {
	for _, h := range []int{3, 4, 5} {
		m, _ := newModel(t, 34, h, 10)
		if m.statusVisible() {
			t.Errorf("height %d: status strip shown", h)
		}
		if m.listHeight() < 1 {
			t.Errorf("height %d: no list row", h)
		}
		m = send(m, keyMsg("tab"))
		m = typeText(m, "a")
		m = send(m, keyMsg("ctrl+j"))
		m = typeText(m, "b")
		if got := m.barHeight(); got != 1 {
			t.Errorf("height %d: command bar is %d lines, want 1", h, got)
		}
		if lines := screen(t, m); len(lines) != h {
			t.Errorf("height %d: %d lines", h, len(lines))
		}
	}
	m, _ := newModel(t, 34, 6, 10)
	if !m.statusVisible() {
		t.Error("height 6: status strip hidden")
	}
}

func TestSelectedLastRowIsVisible(t *testing.T) {
	m, _ := newModel(t, 34, 24, 100)
	m = send(m, keyMsg("end"))
	if got := selectedName(m); got != "task-099" {
		t.Fatalf("selection = %s, want task-099", got)
	}
	if !strings.Contains(strings.Join(listText(t, m), "\n"), "task-099") {
		t.Error("the selected last row is not on screen")
	}

	// Walking down one row at a time keeps it visible too.
	m, _ = newModel(t, 34, 24, 100)
	for range 99 {
		m = send(m, keyMsg("down"))
	}
	if selectedName(m) != "task-099" || !strings.Contains(strings.Join(listText(t, m), "\n"), "task-099") {
		t.Errorf("selection %s, offset %d: last row hidden", selectedName(m), m.offset)
	}
}

func TestWheelMovesTheOffsetNotTheSelection(t *testing.T) {
	for _, h := range []int{12, 24} { // one-line rows, then cards
		m, _ := newModel(t, 34, h, 100)
		sel, off := m.sel, m.offset
		m = send(m, tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown})
		if m.offset <= off {
			t.Errorf("34×%d: offset %d → %d after wheel down", h, off, m.offset)
		}
		if m.sel != sel {
			t.Errorf("34×%d: selection moved %d → %d", h, sel, m.sel)
		}
		if strings.Contains(listText(t, m)[0], "Not in a repo") {
			t.Errorf("34×%d: the group's header still on top after scrolling", h)
		}
		m = send(m, tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelUp})
		if m.offset != off || m.sel != sel {
			t.Errorf("34×%d: after wheel up: offset %d selection %d", h, m.offset, m.sel)
		}
		// The wheel stops at the end, with the last row whole on screen.
		for range 200 {
			m = send(m, tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown})
		}
		items := m.items()
		if want := m.maxOffset(items); m.offset != want || m.span(items, m.offset, len(items)) > m.listHeight() {
			t.Errorf("34×%d: offset %d after scrolling past the end, want %d", h, m.offset, want)
		}
		if list := listText(t, m); !strings.Contains(list[len(list)-1], "task-099") && !strings.Contains(strings.Join(list[max(0, len(list)-2):], " "), "task-099") {
			t.Errorf("34×%d: the last row is not at the bottom: %q", h, list)
		}
	}
}

// What the core sends never scrolls the list: you can read it while the
// agents work.
func TestFramesLeaveTheListWhereItIs(t *testing.T) {
	m, _ := newModel(t, 34, 24, 100)
	for range 10 {
		m = send(m, tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown})
	}
	first := func() string { return listText(t, m)[0] }
	if !strings.Contains(first(), "task-009") {
		t.Fatalf("first card after scrolling = %q", first())
	}
	// A row changes state while the selection is off screen.
	row := sessions(100)[50]
	row.State = ipc.StateNeedsYou
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameUpsert, Session: &row}})
	if !strings.Contains(first(), "task-009") {
		t.Errorf("an upsert scrolled the list: first card %q", first())
	}
	// A row above the screen goes away: the rows on screen stay put.
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameRemove, ID: "s000000000003"}})
	if !strings.Contains(first(), "task-009") {
		t.Errorf("a removal above the screen moved the rows: first card %q", first())
	}
	if selectedName(m) != "task-000" {
		t.Errorf("selection moved to %s", selectedName(m))
	}
	// Nor in the List view: the card on top that finishes stays on top.
	m, _ = newModel(t, 34, 24, 100)
	m = send(m, keyMsg("v"))
	for range 5 {
		m = send(m, tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown})
	}
	if !strings.Contains(first(), "task-005") {
		t.Fatalf("first card after scrolling = %q", first())
	}
	row = sessions(100)[5]
	row.State = ipc.StateDone
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameUpsert, Session: &row}})
	if !strings.Contains(first(), "task-005") {
		t.Errorf("a card that finished moved: first card %q", first())
	}
}

func TestTypingThreeLinesGrowsTheBarAndShrinksTheList(t *testing.T) {
	m, _ := newModel(t, 34, 24, 100)
	m = send(m, keyMsg("tab"))
	list, bar := m.listHeight(), m.barHeight()
	if bar != 1 {
		t.Fatalf("empty bar is %d lines", bar)
	}
	m = typeText(m, "one")
	m = send(m, keyMsg("ctrl+j"))
	m = typeText(m, "two")
	m = send(m, keyMsg("ctrl+j"))
	m = typeText(m, "three")
	if got := m.barHeight(); got != 3 {
		t.Errorf("bar is %d lines, want 3", got)
	}
	if got := m.listHeight(); got != list-2 {
		t.Errorf("list is %d rows, want %d", got, list-2)
	}
	if len(screen(t, m)) != 24 {
		t.Fatalf("%d lines", len(screen(t, m)))
	}
	if bar := barText(t, m); !strings.Contains(bar[0], "one") || !strings.Contains(bar[2], "three") {
		t.Errorf("bar lines = %q", bar)
	}
	if m.compact() {
		t.Error("cards turned into rows while typing")
	}
}

// A growing command bar shows fewer rows but never scrolls them, even when
// the selected row drops out of sight.
func TestTypingLeavesTheListWhereItIs(t *testing.T) {
	m, _ := newModel(t, 34, 24, 100)
	for m.sel < m.offset+m.visible(m.items())-1 {
		m = send(m, keyMsg("down")) // the selection is on the last card on screen
	}
	want := selectedName(m)
	m = send(m, keyMsg("tab"))
	m = typeText(m, "one")
	m = send(m, keyMsg("ctrl+j"))
	m = typeText(m, "two")
	m = send(m, keyMsg("ctrl+j"))
	m = typeText(m, "three")
	if first := listText(t, m)[0]; m.offset != 0 || !strings.Contains(first, "Not in a repo") {
		t.Errorf("typing scrolled the list: offset %d, first line %q", m.offset, first)
	}
	// Back in the list, the selected card is on screen again.
	m = send(m, keyMsg("esc"))
	if rows := strings.Join(listText(t, m), "\n"); !strings.Contains(rows, want) {
		t.Errorf("%s not on screen after Esc:\n%s", want, rows)
	}
}

// The terminal's own cursor sits where you type, so a screen magnifier or
// reader can follow it.
func TestTheTerminalCursorIsInTheCommandBar(t *testing.T) {
	m, _ := newModel(t, 34, 24, 3)
	if c := m.View().Cursor; c != nil {
		t.Errorf("cursor at %+v while the list has the keyboard", c.Position)
	}
	m = send(m, keyMsg("tab"))
	m = typeText(m, "abc")
	c := m.View().Cursor
	if c == nil {
		t.Fatal("no cursor in the command bar")
	}
	// Inside the box: a column of margin and one of border, and the line
	// above the box's bottom border.
	if want := (tea.Position{X: 2 + lipgloss.Width(barPrompt) + 3, Y: 22}); c.Position != want {
		t.Errorf("cursor at %+v, want %+v", c.Position, want)
	}
	if got := []rune(screen(t, m)[22])[c.Position.X-1]; got != 'c' {
		t.Errorf("the cursor follows %q, want the c typed last", got)
	}
	if c.Color != nil {
		t.Errorf("cursor colour %v; the terminal's own suits its theme", c.Color)
	}
	m = send(m, keyMsg("ctrl+j"))
	m = typeText(m, "de")
	if c := m.View().Cursor; c == nil || c.Position != (tea.Position{X: 2 + lipgloss.Width(barPrompt) + 2, Y: 22}) {
		t.Errorf("second line: cursor %+v", c)
	}
	m = send(m, keyMsg("esc"))
	if c := m.View().Cursor; c != nil {
		t.Errorf("cursor at %+v after leaving the bar", c.Position)
	}
	// Without the box, below 10 rows, it sits on the last line.
	m, _ = newModel(t, 34, 8, 3)
	m = send(m, keyMsg("tab"))
	m = typeText(m, "abc")
	if c := m.View().Cursor; c == nil || c.Position != (tea.Position{X: lipgloss.Width(barPrompt) + 3, Y: 7}) {
		t.Errorf("34×8: cursor %+v", c)
	}
}

// A task left in the bar while the list has the keyboard stays in ink, not
// the placeholder's muted colour, so it still reads as what you typed.
func TestABlurredTaskKeepsTheInk(t *testing.T) {
	m, _ := newModel(t, 34, 24, 3)
	m = send(m, keyMsg("tab"))
	m = typeText(m, "half a task")
	m = send(m, tea.MouseClickMsg{X: 3, Y: lineOf(t, m, "task-001"), Button: tea.MouseLeft})
	if m.focus != focusList {
		t.Fatal("a click on a card left the keyboard in the bar")
	}
	bar := strings.Split(m.View().Content, "\n")[m.barTop()]
	if !strings.Contains(ansiRE.ReplaceAllString(bar, ""), "half a task") {
		t.Fatalf("bar = %q", bar)
	}
	for _, c := range cells(bar) {
		if strings.TrimSpace(c.r) != "" && !strings.Contains("›"+boxRunes, c.r) && c.fg != rgb(m.pal.ink) {
			t.Errorf("blurred task: %q in %s, want ink %s", c.r, c.fg, rgb(m.pal.ink))
			break
		}
	}
}

func TestBarStopsGrowingAtEightLines(t *testing.T) {
	m, _ := newModel(t, 34, 24, 5)
	m = send(m, keyMsg("tab"))
	for i := range 12 {
		if i > 0 {
			m = send(m, keyMsg("ctrl+j"))
		}
		m = typeText(m, fmt.Sprintf("line %d", i))
	}
	if got := m.barHeight(); got != 8 {
		t.Errorf("bar is %d lines, want 8", got)
	}
	if got := strings.Count(m.bar.Value(), "\n"); got != 11 {
		t.Errorf("bar holds %d newlines, want 11: typing was capped", got)
	}
}

func TestEnterShowsWithFocusAndSpaceWithout(t *testing.T) {
	m, b := newModel(t, 34, 24, 3)
	if m.focus != focusList {
		t.Fatal("list does not have focus")
	}
	m = send(m, keyMsg("down"))
	m = send(m, keyMsg("enter"))
	m = send(m, keyMsg("space"))
	want := []showCall{{"s000000000001", true}, {"s000000000001", false}}
	if !reflect.DeepEqual(b.shows, want) {
		t.Errorf("shows = %+v, want %+v", b.shows, want)
	}
}

// click presses the left button at x, y and lets it come up there.
func click(m Model, x, y int) Model {
	m = send(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	return send(m, tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
}

// A click shows a row, or a card from any of its lines, when the button
// comes up on it.
func TestClickShowsTheRow(t *testing.T) {
	for _, h := range []int{12, 24} { // a row, then any line of a card
		m, b := newModel(t, 34, h, 3)
		y := lineOf(t, m, "task-002")
		if !m.compact() {
			y-- // the card's top border
		}
		m = send(m, tea.MouseClickMsg{X: 3, Y: y, Button: tea.MouseLeft})
		if len(b.shows) != 0 {
			t.Errorf("34×%d: shown before the button came up: %+v", h, b.shows)
		}
		m = send(m, tea.MouseReleaseMsg{X: 3, Y: y, Button: tea.MouseLeft})
		if want := []showCall{{"s000000000002", true}}; !reflect.DeepEqual(b.shows, want) {
			t.Errorf("34×%d: shows = %+v, want %+v", h, b.shows, want)
		}
		if selectedName(m) != "task-002" {
			t.Errorf("34×%d: selection = %s", h, selectedName(m))
		}
		// The button coming up elsewhere opens nothing.
		m = send(m, tea.MouseClickMsg{X: 3, Y: lineOf(t, m, "task-000"), Button: tea.MouseLeft})
		m = send(m, tea.MouseReleaseMsg{X: 3, Y: 0, Button: tea.MouseLeft})
		if len(b.shows) != 1 {
			t.Errorf("34×%d: a press that came up elsewhere opened %+v", h, b.shows[1:])
		}
	}
}

func TestSubmitSpawnsAndSelectsTheNewRow(t *testing.T) {
	m, b := newModel(t, 34, 24, 0)
	if m.focus != focusBar {
		t.Fatal("empty list should start in the command bar")
	}
	m = typeText(m, "@/home/alice/code/website describe this repo")
	m = send(m, keyMsg("enter"))
	want := []ipc.SpawnArgs{{Task: "describe this repo", Repo: "/home/alice/code/website"}}
	if !reflect.DeepEqual(b.spawns, want) {
		t.Fatalf("spawns = %+v, want %+v", b.spawns, want)
	}
	if m.bar.Value() != "" {
		t.Errorf("bar still holds %q", m.bar.Value())
	}
	if m.focus != focusList {
		t.Error("focus did not move to the list")
	}
	created := ipc.Session{ID: "snew00000001", Name: "describe-this-repo", State: ipc.StateWorking, Created: now}
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameUpsert, Session: &created}})
	if s, ok := m.selSession(); !ok || s.ID != created.ID {
		t.Errorf("new row not selected: sel %d sessions %+v", m.sel, m.sessions)
	}
}

// A task started in a folded group unfolds it, so its card shows.
func TestANewTaskUnfoldsItsGroup(t *testing.T) {
	m, b := newModel(t, 34, 24, 3)
	m = selectNamed(t, m, "Not in a repo")
	m = send(m, keyMsg("enter"))
	if !m.folded[notInARepo] {
		t.Fatal("Enter on the header did not fold it")
	}
	b.spawned = &ipc.Session{ID: "snew00000001", Name: "fresh"}
	m = send(m, keyMsg("tab"))
	m = typeText(m, "fresh")
	m = send(m, keyMsg("enter"))
	fresh := ipc.Session{ID: "snew00000001", Name: "fresh", State: ipc.StateStarting, Created: now}
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameUpsert, Session: &fresh}})
	if m.folded[notInARepo] || selectedName(m) != "fresh" || !strings.Contains(strings.Join(listText(t, m), "\n"), "fresh") {
		t.Errorf("folded %v, selection %s, list %q", m.folded, selectedName(m), listText(t, m))
	}
}

func TestSpawnErrorStaysInTheBar(t *testing.T) {
	m, b := newModel(t, 34, 24, 0)
	b.spawnErr = errors.New("no such directory: /home/alice/missing")
	m = typeText(m, "@/home/alice/missing fix it")
	m = send(m, keyMsg("enter"))
	if !strings.Contains(m.status, "no such directory") || !m.statusErr {
		t.Errorf("status = %q", m.status)
	}
	if m.bar.Value() == "" {
		t.Error("the task was lost after a failed spawn")
	}
}

func TestEscClearsAndLeavesTheBar(t *testing.T) {
	m, _ := newModel(t, 34, 24, 2)
	m = send(m, keyMsg("tab"))
	m = typeText(m, "half a task")
	m = send(m, keyMsg("esc"))
	if m.focus != focusList || m.bar.Value() != "" {
		t.Errorf("focus %v, bar %q", m.focus, m.bar.Value())
	}
	m = send(m, keyMsg(":"))
	if m.focus != focusBar || m.bar.Value() != ":" {
		t.Errorf("':' gave focus %v, bar %q", m.focus, m.bar.Value())
	}
}

func TestOfflineHeader(t *testing.T) {
	m, _ := newModel(t, 34, 24, 2)
	m = send(m, OfflineMsg{})
	if !strings.Contains(screen(t, m)[0], "core offline") {
		t.Errorf("header = %q", screen(t, m)[0])
	}
}

func TestSelectionFollowsARemovedRow(t *testing.T) {
	m, _ := newModel(t, 34, 24, 3)
	m = send(m, keyMsg("down"))
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameRemove, ID: "s000000000001"}})
	if s, ok := m.selSession(); !ok || s.ID != "s000000000002" {
		t.Errorf("selection = %d (%v)", m.sel, m.sessions)
	}
}

// A name too long for its card or row is cut with an ellipsis; the status
// strip, or the details card's title, shows more of it.
func TestLongNamesAreCut(t *testing.T) {
	for _, c := range []struct {
		h    int
		more string
	}{{20, "a-very-long-session-name-that"}, {24, "a-very-long-session-name-"}} {
		m, _ := newModel(t, 34, c.h, 0)
		long := ipc.Session{ID: "s1", Name: "a-very-long-session-name-that-will-not-fit", State: ipc.StateWorking, Created: now}
		m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameUpsert, Session: &long}})
		if row := screen(t, m)[lineOf(t, m, "a-very-long")]; !strings.Contains(row, "…") {
			t.Errorf("34×%d: row = %q, want an ellipsis", c.h, row)
		}
		if got := statusText(t, m); !strings.Contains(got, c.more) {
			t.Errorf("34×%d: the name is not in the status: %q", c.h, got)
		}
	}
}

func TestHeaderCounts(t *testing.T) {
	m, _ := newModel(t, 80, 10, 0)
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameSnapshot, Sessions: []ipc.Session{
		{ID: "s1", Name: "a", State: ipc.StateWorking, Created: now},
		{ID: "s2", Name: "b", State: ipc.StateWorking, Created: now},
		{ID: "s3", Name: "c", State: ipc.StateExited, Created: now},
	}}})
	if got := screen(t, m)[0]; !strings.Contains(got, "◐2 working") {
		t.Errorf("wide header = %q", got)
	}
	m = send(m, tea.WindowSizeMsg{Width: 34, Height: 10})
	if got := screen(t, m)[0]; !strings.Contains(got, "◐2") || strings.Contains(got, "working") {
		t.Errorf("narrow header = %q", got)
	}
}

func TestZeroCountsAreNotRed(t *testing.T) {
	m, _ := newModel(t, 34, 24, 2)
	header := strings.Split(m.View().Content, "\n")[0]
	if hasFG(header, m.pal.needs) {
		t.Errorf("header %q has red with nothing needing you", header)
	}
}

// The tab bar and the command bar's box take three rows, so they wait for a
// pane of 10.
func TestTabsAndTheBoxNeedRoom(t *testing.T) {
	m, _ := newModel(t, 34, 9, 3)
	for i, l := range screen(t, m) {
		if strings.Contains(l, "List │ Repo") || strings.ContainsAny(l, "╭╰") {
			t.Errorf("height 9: line %d = %q", i, l)
		}
	}
	m, _ = newModel(t, 34, 10, 3)
	lines := screen(t, m)
	// The header has its second line, what undo and redo would do, above
	// the tabs.
	top := m.listTop()
	if !strings.Contains(lines[1], "↶") || lines[2] != "  List │ Repo                     " || !strings.HasPrefix(lines[9], " ╰") || !strings.HasPrefix(lines[7], " ╭") {
		t.Errorf("height 10: no history line and tabs under the header, or no box around the bar:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[top], "Not in a repo") || !strings.Contains(lines[top+1], "task-000") {
		t.Errorf("height 10: lines %d and %d = %q, %q, want the group and its first row", top, top+1, lines[top], lines[top+1])
	}
}

// The selected row has the accent edge on the selection fill; the selected
// card the selection fill inside an accent border. With the keyboard in the
// command bar, the accent goes there.
func TestTheSelectionStandsOut(t *testing.T) {
	m, _ := newModel(t, 34, 12, 3)
	if row := screen(t, m)[lineOf(t, m, "task-000")]; !strings.HasPrefix(row, "▌◐ task-000") {
		t.Errorf("selected row = %q", row)
	}
	if row := screen(t, m)[lineOf(t, m, "task-001")]; !strings.HasPrefix(row, " ◐ task-001") {
		t.Errorf("other row = %q", row)
	}
	if c := cells(strings.Split(m.View().Content, "\n")[lineOf(t, m, "task-000")]); c[0].fg != rgb(m.pal.accent) || c[3].bg != rgb(m.pal.selection) {
		t.Errorf("selected row: edge %s on %s", c[0].fg, c[3].bg)
	}

	m, _ = newModel(t, 34, 24, 3)
	raw := strings.Split(m.View().Content, "\n")
	card := func(name string) (edge, fill string) {
		y := lineOf(t, m, name)
		c := cells(raw[y])
		return c[1].fg, c[4].bg
	}
	if e, f := card("task-000"); e != rgb(m.pal.accent) || f != rgb(m.pal.selection) {
		t.Errorf("selected card: border %s, fill %s", e, f)
	}
	if e, f := card("task-001"); e != rgb(m.pal.edge) || f != rgb(m.pal.card) {
		t.Errorf("other card: border %s, fill %s", e, f)
	}
	box := func() string { return cells(strings.Split(m.View().Content, "\n")[m.barTop()])[1].fg }
	if box() != rgb(m.pal.edge) {
		t.Errorf("the bar's box is %s while the list has the keyboard", box())
	}
	m = send(m, keyMsg("tab"))
	raw = strings.Split(m.View().Content, "\n")
	if e, f := card("task-000"); e != rgb(m.pal.edge) || f != rgb(m.pal.selection) {
		t.Errorf("selected card, keyboard in the bar: border %s, fill %s", e, f)
	}
	if box() != rgb(m.pal.accent) {
		t.Errorf("the bar's box is %s while it has the keyboard", box())
	}
	if !strings.Contains(barText(t, m)[0], "What should we work on?") {
		t.Errorf("placeholder %q", barText(t, m)[0])
	}
}

func TestParseTask(t *testing.T) {
	for _, c := range []struct {
		in   string
		want ipc.SpawnArgs
		err  string
	}{
		{in: "fix the flaky test", want: ipc.SpawnArgs{Task: "fix the flaky test"}},
		{in: "@/home/alice/code/x fix it", want: ipc.SpawnArgs{Task: "fix it", Repo: "/home/alice/code/x"}},
		{in: "fix it @~/code/x", want: ipc.SpawnArgs{Task: "fix it", Repo: "/home/alice/code/x"}},
		{in: "@~ list my files", want: ipc.SpawnArgs{Task: "list my files", Repo: "/home/alice"}},
		{in: "fix @/abs   the bug", want: ipc.SpawnArgs{Task: "fix the bug", Repo: "/abs"}},
		{in: "@/abs first line\nsecond @/not/a/token\n", want: ipc.SpawnArgs{Task: "first line\nsecond @/not/a/token\n", Repo: "/abs"}},
		{in: "@/abs\nthe task is on line two", want: ipc.SpawnArgs{Task: "the task is on line two", Repo: "/abs"}},
		{in: "mail alice@example.com about it", want: ipc.SpawnArgs{Task: "mail alice@example.com about it"}},
		{in: "  keep   inner  spacing  ", want: ipc.SpawnArgs{Task: "keep   inner  spacing"}},
		{in: "@website fix it", want: ipc.SpawnArgs{Task: "fix it", Repo: "website"}},
		{in: "@clients/api fix it", want: ipc.SpawnArgs{Task: "fix it", Repo: "clients/api"}},
		{in: "@website! fix it", want: ipc.SpawnArgs{Task: "fix it", Repo: "website", InPlace: true}},
		{in: "@~/code/x! fix it", want: ipc.SpawnArgs{Task: "fix it", Repo: "/home/alice/code/x", InPlace: true}},
		{in: "+fast fix it", want: ipc.SpawnArgs{Task: "fix it", Profile: "fast"}},
		{in: "fix it +aider @web!", want: ipc.SpawnArgs{Task: "fix it", Repo: "web", InPlace: true, Profile: "aider"}},
		{in: "c++ is fine, and so is a+b", want: ipc.SpawnArgs{Task: "c++ is fine, and so is a+b"}},
		{in: "@/a @/b fix it", err: "one @"},
		{in: "+a +b fix it", err: "one +"},
		{in: "@ fix it", err: "@"},
		{in: "+ fix it", err: "+"},
		{in: "@web!! fix it", err: "@web!!"},
		{in: "@./src fix it", err: "@./src"},
		{in: "@/abs", err: "empty"},
		{in: "@web +fast", err: "empty"},
		{in: " \n\t", err: "empty"},
		{in: ":pause", err: ":pause"},
	} {
		got, err := ParseTask(c.in, "/home/alice")
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%q: error %v, want one mentioning %q", c.in, err, c.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("%q: got %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestSpawnTokensLeadTheWords(t *testing.T) {
	var a ipc.SpawnArgs
	rest, err := SpawnTokens(&a, []string{"@web!", "+fast", "fix", "@it", "+now"}, "/home/alice")
	if err != nil {
		t.Fatal(err)
	}
	if want := (ipc.SpawnArgs{Repo: "web", InPlace: true, Profile: "fast"}); a != want {
		t.Errorf("args = %+v, want %+v", a, want)
	}
	// Words after the first that is not a token belong to the task.
	if want := []string{"fix", "@it", "+now"}; !reflect.DeepEqual(rest, want) {
		t.Errorf("rest = %q, want %q", rest, want)
	}
	if _, err := SpawnTokens(&ipc.SpawnArgs{}, []string{"@a", "@b", "x"}, "/home/alice"); err == nil {
		t.Error("two @ tokens accepted")
	}
}

// big is a paste over the chip threshold: 9,000 bytes on 1,000 lines.
var big = strings.Repeat("12345678\n", 1000)

func TestABigPasteBecomesAChipAndIsSentWhole(t *testing.T) {
	m, b := newModel(t, 34, 24, 0)
	m = typeText(m, "@web fix this: ")
	m = send(m, tea.PasteMsg{Content: big})
	m = typeText(m, " and say why")
	if v := m.bar.Value(); v != "@web fix this: [paste 9.0 KB · 1,000 lines] and say why" {
		t.Errorf("bar = %q", v)
	}
	m = send(m, keyMsg("enter"))
	if len(b.spawns) != 1 {
		t.Fatalf("spawns = %d", len(b.spawns))
	}
	if a := b.spawns[0]; a.Repo != "web" || a.Task != "fix this: "+big+" and say why" {
		t.Errorf("spawned repo %q, task of %d bytes, want the whole paste in place", a.Repo, len(a.Task))
	}
	// The chip goes with the submitted task.
	m = send(m, keyMsg("tab"))
	m = typeText(m, "next")
	m = send(m, keyMsg("enter"))
	if a := b.spawns[1]; a.Task != "next" {
		t.Errorf("second task = %q", a.Task)
	}
}

func TestSmallPastesGoInTheBar(t *testing.T) {
	m, _ := newModel(t, 34, 24, 0)
	small := strings.Repeat("x", 8<<10)
	m = send(m, tea.PasteMsg{Content: small})
	if m.bar.Value() != small {
		t.Errorf("an 8 KiB paste did not go into the bar as text")
	}
}

func TestChipsWithTheSameLabelKeepTheirOrder(t *testing.T) {
	m, b := newModel(t, 34, 24, 0)
	one, two := strings.Repeat("a", 9000), strings.Repeat("b", 9000)
	m = send(m, tea.PasteMsg{Content: one})
	m = typeText(m, " then ")
	m = send(m, tea.PasteMsg{Content: two})
	m = send(m, keyMsg("enter"))
	if a := b.spawns[0]; a.Task != one+" then "+two {
		t.Errorf("task = %.20q…, want both pastes in order", a.Task)
	}
}

func TestEscDropsTheChips(t *testing.T) {
	m, b := newModel(t, 34, 24, 2)
	m = send(m, keyMsg("tab"))
	m = send(m, tea.PasteMsg{Content: big})
	m = send(m, keyMsg("esc"))
	m = send(m, keyMsg("tab"))
	m = typeText(m, "[paste 9.0 KB · 1,000 lines]")
	m = send(m, keyMsg("enter"))
	if a := b.spawns[0]; a.Task != "[paste 9.0 KB · 1,000 lines]" {
		t.Errorf("task = %.40q…, want only what was typed", a.Task)
	}
}

func TestChipLabels(t *testing.T) {
	for content, want := range map[string]string{
		strings.Repeat("x", 9000):                                 "[paste 9.0 KB · 1 line]",
		strings.Repeat("a line of text\n", 140000) + "and a tail": "[paste 2.1 MB · 140,001 lines]",
		// DESIGN §4.2's example.
		strings.Repeat(strings.Repeat("x", 66)+"\n", 31204): "[paste 2.1 MB · 31,204 lines]",
	} {
		if got := chipLabel(content); got != want {
			t.Errorf("chipLabel(%d bytes) = %q, want %q", len(content), got, want)
		}
	}
}

func TestChipsEndLinesWithLF(t *testing.T) {
	m, b := newModel(t, 34, 24, 0)
	crlf := strings.Repeat("windows line\r\n", 700) + strings.Repeat("old mac line\r", 300)
	m = send(m, tea.PasteMsg{Content: crlf})
	m = send(m, keyMsg("enter"))
	want := strings.Repeat("windows line\n", 700) + strings.Repeat("old mac line\n", 300)
	if a := b.spawns[0]; a.Task != want {
		t.Errorf("task has %d CRs, want every line ended by LF", strings.Count(a.Task, "\r"))
	}
}

func TestTabCompletesTheTokenAtTheCursor(t *testing.T) {
	m, b := newModel(t, 34, 24, 0)
	b.items = map[string][]string{
		"@we":   {"@website"},
		"@cl":   {"@clients/acme", "@clients/api"},
		"@~/co": {"@~/code/"},
		"+fa":   {"+fast"},
	}
	m = typeText(m, "@we")
	m = send(m, keyMsg("tab"))
	if v := m.bar.Value(); v != "@website " {
		t.Errorf("one candidate: bar = %q", v)
	}
	m = typeText(m, "+fa")
	m = send(m, keyMsg("tab"))
	if v := m.bar.Value(); v != "@website +fast " {
		t.Errorf("a profile: bar = %q", v)
	}

	// Several candidates: their common prefix, and the list in the status strip.
	m.bar.Reset()
	m = typeText(m, "@cl")
	m = send(m, keyMsg("tab"))
	if v := m.bar.Value(); v != "@clients/a" {
		t.Errorf("two candidates: bar = %q", v)
	}
	if !strings.Contains(m.status, "@clients/acme") || !strings.Contains(m.status, "@clients/api") {
		t.Errorf("status = %q", m.status)
	}

	// A directory keeps the cursor inside the path.
	m.bar.Reset()
	m = typeText(m, "@~/co")
	m = send(m, keyMsg("tab"))
	if v := m.bar.Value(); v != "@~/code/" {
		t.Errorf("a directory: bar = %q", v)
	}

	// No candidates leaves the bar and says so.
	m.bar.Reset()
	m = typeText(m, "@zz")
	m = send(m, keyMsg("tab"))
	if v := m.bar.Value(); v != "@zz" || !strings.Contains(m.status, "no match") {
		t.Errorf("no match: bar %q, status %q", v, m.status)
	}

	// Tab after a plain word asks nothing.
	m.bar.Reset()
	b.completes = nil
	m = typeText(m, "fix it")
	m = send(m, keyMsg("tab"))
	if len(b.completes) != 0 || m.bar.Value() != "fix it" {
		t.Errorf("completes %q, bar %q", b.completes, m.bar.Value())
	}
}

func TestTabCompletesInsideTheFirstLine(t *testing.T) {
	m, b := newModel(t, 34, 24, 0)
	b.items = map[string][]string{"@we": {"@website"}}
	m = typeText(m, "@we fix it")
	for range len(" fix it") {
		m = send(m, keyMsg("left"))
	}
	m = send(m, keyMsg("tab"))
	if v := m.bar.Value(); v != "@website fix it" {
		t.Errorf("bar = %q", v)
	}
	m = typeText(m, "!")
	if v := m.bar.Value(); v != "@website! fix it" {
		t.Errorf("the cursor did not stay after the token: bar = %q", v)
	}
}

func TestTheStatusStripSaysWhereATaskWent(t *testing.T) {
	m, b := newModel(t, 80, 24, 0)
	b.spawned = &ipc.Session{
		ID: "snew00000001", Name: "fix-it", Branch: "irie/fix-it", Worktree: "/home/alice/.local/share/irie/worktrees/website/fix-it",
		Copied:   []string{".env", "CLAUDE.local.md"},
		Warnings: []string{"2 uncommitted changes in the main checkout were not carried over"},
	}
	m = typeText(m, "@website fix it")
	m = send(m, keyMsg("enter"))
	for _, h := range []int{24, 20} { // the details card, then the strip
		m = send(m, tea.WindowSizeMsg{Width: 80, Height: h})
		status := statusText(t, m)
		for _, want := range []string{"irie/fix-it", "copied .env, CLAUDE.local.md", "2 uncommitted changes"} {
			if !strings.Contains(status, want) {
				t.Errorf("80×%d: status %q lacks %q", h, status, want)
			}
		}
	}
}

func TestAge(t *testing.T) {
	for _, c := range []struct {
		d    time.Duration
		want string
	}{
		{0, "now"}, {5 * time.Second, "now"}, {42 * time.Second, "42s"}, {3 * time.Minute, "3m"},
		{2*time.Hour + 5*time.Minute, "2h"}, {50 * time.Hour, "2d"},
	} {
		if got := age(c.d); got != c.want {
			t.Errorf("age(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestPlaceholderShowsTheMarkWhenThereIsRoom(t *testing.T) {
	view := func(w, h int) string {
		next, _ := placeholder{focusKey: keyLabel("C-]")}.Update(tea.WindowSizeMsg{Width: w, Height: h})
		return ansiRE.ReplaceAllString(next.(placeholder).View().Content, "")
	}
	tall := view(80, 30)
	for _, want := range []string{"( ☀ )", "～～～", "Irie", "[Ctrl+]]", "[Tab]", "[↑][↓]", "show it, stay in the sidebar"} {
		if !strings.Contains(tall, want) {
			t.Errorf("80×30 placeholder lacks %q:\n%s", want, tall)
		}
	}
	if short := view(80, 14); strings.Contains(short, "☀") || !strings.Contains(short, "[Tab]") {
		t.Errorf("80×14 placeholder:\n%s", short)
	}
}

func code(n int) *int { return &n }

// everyState is one row per state of DESIGN §5.3.
func everyState() []ipc.Session {
	rows := []ipc.Session{
		{Name: "failed-row", State: ipc.StateFailed, ExitCode: code(1), Reason: "exited before it was ready", Detail: "failed: exit 1 · error: unknown option"},
		{Name: "exited-ok", State: ipc.StateExited, ExitCode: code(0), Detail: "exited 0"},
		{Name: "exited-bad", State: ipc.StateExited, ExitCode: code(2), Detail: "exited 2"},
		{Name: "needs-row", State: ipc.StateNeedsYou, Reason: "question", Detail: "asks: Which colour should the button be?"},
		{Name: "stalled-row", State: ipc.StateStalled, Reason: "stalled", Detail: "stalled: no hooks within 30s · Do you trust the files in this folder?"},
		{Name: "working-row", State: ipc.StateWorking, Detail: "Bash · go test ./..."},
		{Name: "starting-row", State: ipc.StateStarting},
		{Name: "done-row", State: ipc.StateDone},
		{Name: "idle-row", State: ipc.StateIdle},
		{Name: "frozen-row", State: ipc.StateFrozen},
		{Name: "paused-row", State: ipc.StatePaused},
		{Name: "pausing-row", State: ipc.StatePausing},
	}
	for i := range rows {
		rows[i].ID = fmt.Sprintf("s%012d", i)
		rows[i].Created = now.Add(-time.Duration(len(rows)-i) * time.Minute)
	}
	return rows
}

func statesModel(t *testing.T, o Options) Model {
	t.Helper()
	return statesAt(t, o, 34, 24)
}

// statesAt is every state in a pane of w×h.
func statesAt(t *testing.T, o Options, w, h int) Model {
	t.Helper()
	o.Now = func() time.Time { return now }
	m := New(&fakeBackend{}, o)
	m = send(m, tea.WindowSizeMsg{Width: w, Height: h})
	return send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameSnapshot, Sessions: everyState(), Header: &ipc.Header{}}})
}

// layouts are panes that show every state at once: one-line rows in a
// narrow pane, and cards in a tall one.
var layouts = [][2]int{{22, 40}, {34, 80}}

// rowLine returns the raw (styled) screen line of the row named name, or
// of the line of its card that has its name.
func rowLine(t *testing.T, m Model, name string) string {
	t.Helper()
	for _, l := range strings.Split(m.View().Content, "\n") {
		if strings.Contains(ansiRE.ReplaceAllString(l, ""), name) {
			return l
		}
	}
	t.Fatalf("no row %s", name)
	return ""
}

func TestEveryStateHasItsGlyph(t *testing.T) {
	for _, size := range layouts {
		m := statesAt(t, Options{}, size[0], size[1])
		if m.compact() != (size[0] < cardMinWidth) {
			t.Fatalf("%d×%d: compact %v", size[0], size[1], m.compact())
		}
		m = send(m, keyMsg("tab")) // the selection bar would hide nothing, but keep rows plain
		for name, glyph := range map[string]string{
			"failed-row": "✕", "exited-ok": "·", "exited-bad": "✕", "needs-row": "●", "stalled-row": "!",
			"working-row": "◐", "starting-row": "◌", "done-row": "✓", "idle-row": "○",
			"frozen-row": "◆", "paused-row": "‖", "pausing-row": "‖",
		} {
			plain := strings.TrimLeft(ansiRE.ReplaceAllString(rowLine(t, m, name), ""), " ▌│")
			if !strings.HasPrefix(plain, glyph+" "+name) {
				t.Errorf("%d×%d: row %q, want glyph %s", size[0], size[1], plain, glyph)
			}
		}
	}
}

// cell is one character on screen with the colours it is drawn in: "r;g;b",
// or "" for the terminal's own.
type cell struct {
	r      string
	fg, bg string
	bold   bool
}

// cells reads a drawn line the way a terminal does: SGR sequences set the
// colours and a reset drops them all.
func cells(line string) []cell {
	var out []cell
	var cur cell
	for i := 0; i < len(line); {
		if line[i] == 0x1b && i+1 < len(line) && line[i+1] == '[' {
			j := i + 2
			for j < len(line) && (line[j] < 0x40 || line[j] > 0x7e) {
				j++
			}
			if j < len(line) && line[j] == 'm' {
				cur = sgrApply(cur, line[i+2:j])
			}
			i = j + 1
			continue
		}
		if loc := ansiRE.FindStringIndex(line[i:]); loc != nil && loc[0] == 0 {
			i += loc[1]
			continue
		}
		r, n := utf8.DecodeRuneInString(line[i:])
		c := cur
		c.r = string(r)
		out = append(out, c)
		i += n
	}
	return out
}

func sgrApply(c cell, params string) cell {
	ps := strings.FieldsFunc(params, func(r rune) bool { return r == ';' || r == ':' })
	if len(ps) == 0 {
		return cell{}
	}
	for k := 0; k < len(ps); k++ {
		switch p := ps[k]; {
		case p == "0":
			c = cell{}
		case p == "1":
			c.bold = true
		case p == "22":
			c.bold = false
		case p == "39":
			c.fg = ""
		case p == "49":
			c.bg = ""
		case (p == "38" || p == "48") && k+4 < len(ps) && ps[k+1] == "2":
			if v := strings.Join(ps[k+2:k+5], ";"); p == "38" {
				c.fg = v
			} else {
				c.bg = v
			}
			k += 4
		case (p == "38" || p == "48") && k+2 < len(ps) && ps[k+1] == "5":
			if v := "5;" + ps[k+2]; p == "38" {
				c.fg = v
			} else {
				c.bg = v
			}
			k += 2
		case len(p) == 2 && (p[0] == '3' || p[0] == '9') && p[1] <= '7':
			c.fg = "ansi " + p
		case len(p) == 2 && p[0] == '4' && p[1] <= '7', len(p) == 3 && strings.HasPrefix(p, "10"):
			c.bg = "ansi " + p
		}
	}
	return c
}

// rgb is how cells writes c.
func rgb(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("%d;%d;%d", r>>8, g>>8, b>>8)
}

// hasFG reports whether any character of a drawn line is in c.
func hasFG(line string, c color.Color) bool {
	for _, x := range cells(line) {
		if strings.TrimSpace(x.r) != "" && x.fg == rgb(c) {
			return true
		}
	}
	return false
}

// glyphAndName returns the colours of a row's glyph and of the first letter
// of its name.
func glyphAndName(t *testing.T, m Model, name string) (glyph, nameFG string) {
	t.Helper()
	cs := cells(rowLine(t, m, name))
	for i, c := range cs {
		if strings.TrimSpace(c.r) == "" || strings.Contains(boxRunes, c.r) {
			continue
		}
		for j := i + 1; j < len(cs); j++ {
			if strings.TrimSpace(cs[j].r) != "" {
				return c.fg, cs[j].fg
			}
		}
	}
	t.Fatalf("row %s has no glyph and name", name)
	return "", ""
}

// Each state is drawn in its colours of DESIGN §5.3, from the palette: red
// is needs, green done, yellow paused, cyan frozen, default ink and dim
// muted. A dim red or dim yellow row has the colour in its glyph and a muted
// name.
func TestEveryStateHasItsColours(t *testing.T) {
	for _, size := range layouts {
		m := statesAt(t, Options{}, size[0], size[1])
		m = send(m, keyMsg("tab"))
		p := m.pal
		for name, want := range map[string][2]color.Color{
			"failed-row":   {p.needs, p.needs},
			"exited-ok":    {p.muted, p.muted},
			"exited-bad":   {p.needs, p.muted},
			"frozen-row":   {p.frozen, p.ink},
			"paused-row":   {p.paused, p.ink},
			"needs-row":    {p.needs, p.needs},
			"stalled-row":  {p.needs, p.needs},
			"pausing-row":  {p.paused, p.muted},
			"working-row":  {p.ink, p.ink},
			"starting-row": {p.muted, p.muted},
			"done-row":     {p.done, p.ink},
			"idle-row":     {p.muted, p.muted},
		} {
			if g, n := glyphAndName(t, m, name); g != rgb(want[0]) || n != rgb(want[1]) {
				t.Errorf("%d×%d %s: glyph %s and name %s, want %s and %s", size[0], size[1], name, g, n, rgb(want[0]), rgb(want[1]))
			}
		}
		m = statesAt(t, Options{RedOnDone: true}, size[0], size[1])
		m = send(m, keyMsg("tab"))
		if g, n := glyphAndName(t, m, "done-row"); g != rgb(m.pal.needs) || n != rgb(m.pal.needs) {
			t.Errorf("%d×%d red_on_done: done row's glyph %s and name %s, want the needs colour", size[0], size[1], g, n)
		}
	}
}

func TestRedMarksOnlyTheRowsThatNeedYou(t *testing.T) {
	for _, size := range layouts {
		m := statesAt(t, Options{}, size[0], size[1])
		m = send(m, keyMsg("tab"))
		for name, wantRed := range map[string]bool{
			"failed-row": true, "exited-bad": true, "needs-row": true, "stalled-row": true,
			"exited-ok": false, "working-row": false, "starting-row": false, "done-row": false, "idle-row": false,
			"frozen-row": false, "paused-row": false, "pausing-row": false,
		} {
			if got := hasFG(rowLine(t, m, name), m.pal.needs); got != wantRed {
				t.Errorf("%d×%d %s: red %v, want %v: %q", size[0], size[1], name, got, wantRed, rowLine(t, m, name))
			}
		}
		if !hasFG(rowLine(t, m, "done-row"), m.pal.done) {
			t.Errorf("done row is not green: %q", rowLine(t, m, "done-row"))
		}
		// ui.red_on_done shows done in red instead.
		m = statesAt(t, Options{RedOnDone: true}, size[0], size[1])
		m = send(m, keyMsg("tab"))
		if !hasFG(rowLine(t, m, "done-row"), m.pal.needs) {
			t.Errorf("red_on_done: done row %q", rowLine(t, m, "done-row"))
		}
	}
}

// Without colour (NO_COLOR, a monochrome terminal, colour blindness) a red
// row still looks different from every row that is not red.
func TestRedIsNeverTheOnlyCue(t *testing.T) {
	for _, size := range layouts {
		m := statesAt(t, Options{}, size[0], size[1])
		m = send(m, keyMsg("tab"))
		rows := map[bool]map[string]string{true: {}, false: {}} // red → glyph → row
		for _, s := range everyState() {
			raw := rowLine(t, m, s.Name)
			glyph := strings.Fields(strings.TrimLeft(ansiRE.ReplaceAllString(raw, ""), " ▌│"))[0]
			rows[hasFG(raw, m.pal.needs)][glyph] = s.Name
		}
		for glyph, name := range rows[true] {
			if other, ok := rows[false][glyph]; ok {
				t.Errorf("%d×%d: %s and %s are both %s: only red tells them apart", size[0], size[1], name, other, glyph)
			}
		}
	}
}

// A card that needs you is tinted and edged in red, and says what it needs
// in red; any other card says what it does in muted.
func TestACardThatNeedsYouIsTinted(t *testing.T) {
	m := statesAt(t, Options{}, 34, 80)
	m = send(m, keyMsg("tab"))
	p := m.pal
	for name, want := range map[string][3]color.Color{
		"needs-row":   {p.needs, p.needsTint, p.needs},
		"stalled-row": {p.needs, p.needsTint, p.needs},
		"working-row": {p.edge, p.card, p.muted},
		"exited-bad":  {p.edge, p.card, p.muted},
	} {
		raw := strings.Split(m.View().Content, "\n")
		y := lineOf(t, m, name)
		line1, line2 := cells(raw[y]), cells(raw[y+1])
		if line1[1].fg != rgb(want[0]) || line1[4].bg != rgb(want[1]) || line2[3].fg != rgb(want[2]) {
			t.Errorf("%s: border %s, fill %s, detail %s", name, line1[1].fg, line1[4].bg, line2[3].fg)
		}
	}
	// The second line falls back to the state in words.
	if row := screen(t, m)[lineOf(t, m, "starting-row")+1]; !strings.Contains(row, "Starting") {
		t.Errorf("starting-row's second line = %q", row)
	}
}

// The header counts, in DESIGN §4.1's order: needs you counts every red row,
// failed ones too; paused counts pausing too.
func TestHeaderCountsWhatNeedsYou(t *testing.T) {
	m := statesModel(t, Options{})
	m = send(m, tea.WindowSizeMsg{Width: 100, Height: 24})
	header := screen(t, m)[0]
	want := "◐1 working  ●3 needs you  ✓1 to verify  ‖2 paused  ◆1 frozen"
	if !strings.Contains(header, want) {
		t.Errorf("header %q lacks %q", header, want)
	}
	raw := strings.Split(m.View().Content, "\n")[0]
	for _, c := range []color.Color{m.pal.needs, m.pal.done, m.pal.paused, m.pal.frozen} {
		if !hasFG(raw, c) {
			t.Errorf("header %q: no count in %s", raw, rgb(c))
		}
	}
}

// A parked session is out of the way: To verify leaves it out, though
// needs you still counts one, and its finished turn says Parked under a
// quiet name.
func TestAParkedSessionIsNotToVerify(t *testing.T) {
	m := statesAt(t, Options{}, 100, 80)
	rows := everyState()
	for _, i := range []int{3, 7} { // needs-row and done-row
		rows[i].Parked = true
		m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameUpsert, Session: &rows[i]}})
	}
	header := screen(t, m)[0]
	if want := "●3 needs you  ✓0 to verify"; !strings.Contains(header, want) {
		t.Errorf("header %q lacks %q", header, want)
	}
	m = send(m, keyMsg("esc"))
	m = send(m, keyMsg("Z"))
	y := lineOf(t, m, "done-row")
	if row := screen(t, m)[y+1]; !strings.Contains(row, "Parked") || strings.Contains(row, "To verify") {
		t.Errorf("a parked finished turn's second line is %q, want Parked", row)
	}
	line := screen(t, m)[y]
	x := len([]rune(line[:strings.Index(line, "done-row")]))
	if fg := cells(strings.Split(m.View().Content, "\n")[y])[x].fg; fg != rgb(m.pal.muted) {
		t.Errorf("a parked finished turn's name is %s, want muted", fg)
	}
}

// A zero count is faint: muted, never its state's colour.
func TestZeroCountsAreFaint(t *testing.T) {
	m, _ := newModel(t, 100, 24, 0)
	raw := strings.Split(m.View().Content, "\n")[0]
	if !strings.Contains(ansiRE.ReplaceAllString(raw, ""), "◐0 working  ●0 needs you  ✓0 to verify  ‖0 paused  ◆0 frozen") {
		t.Fatalf("header %q", ansiRE.ReplaceAllString(raw, ""))
	}
	for _, c := range cells(raw) {
		if strings.ContainsAny(c.r, "◐●✓‖◆0") && c.fg != rgb(m.pal.muted) {
			t.Errorf("zero count %q in %s, want muted", c.r, c.fg)
		}
	}
}

func TestStatusStripShowsWhyAndWhat(t *testing.T) {
	for _, h := range []int{20, 24} { // the strip, then the details card
		m := statesAt(t, Options{}, 80, h)
		for name, want := range map[string]string{
			"failed-row": "failed: exit 1", "exited-ok": "exited 0", "exited-bad": "exited 2", "needs-row": "asks: Which colour",
			"stalled-row": "stalled: no hooks", "working-row": "Bash · go test", "starting-row": "starting-row", "done-row": "done-row", "idle-row": "idle-row",
		} {
			m = selectNamed(t, m, name)
			if got := statusText(t, m); !strings.Contains(got, want) {
				t.Errorf("80×%d %s: status %q, want %q", h, name, got, want)
			}
		}
		// A warning shows when there is nothing more pressing to say.
		rows := everyState()
		rows[6].Warnings = []string{"pause off: Claude Code 2.1.200 is older than 2.1.295"}
		m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameUpsert, Session: &rows[6]}})
		m = selectNamed(t, m, "starting-row")
		if got := statusText(t, m); !strings.Contains(got, "pause off: Claude Code 2.1.200") {
			t.Errorf("80×%d: status %q, want the warning", h, got)
		}
	}
}

// From 24 rows the status strip is a details card: the selected session's
// name in its border, then its state with what it costs, what it is doing,
// the exact call, and its branch and folder.
func TestTheDetailsCard(t *testing.T) {
	m, _ := newModel(t, 60, 24, 0)
	cost := 0.5
	s := ipc.Session{
		ID: "s1", Name: "fix-flaky-test", State: ipc.StateWorking, Created: now, CostUSD: &cost,
		Detail: "Running the tests", Command: "Bash · go test ./...",
		Branch: "irie/fix-flaky-test", Cwd: "/home/alice/code/irie",
	}
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameSnapshot, Sessions: []ipc.Session{s}, Header: &ipc.Header{}}})
	lines := screen(t, m)
	top := m.listTop() + m.listHeight()
	want := []string{
		" ╭─ fix-flaky-test ─",
		" │ ◐ Working · $0.50",
		" │ Running the tests",
		" │ Bash · go test ./...",
		" │ irie/fix-flaky-test · ~/code/irie",
		" ╰─",
	}
	if m.statusHeight() != len(want) {
		t.Fatalf("details card of %d lines", m.statusHeight())
	}
	for i, w := range want {
		if !strings.HasPrefix(lines[top+i], w) {
			t.Errorf("details line %d = %q, want it to start %q", i, lines[top+i], w)
		}
	}
	if c := cells(strings.Split(m.View().Content, "\n")[top+1]); c[3].bg != rgb(m.pal.panel) {
		t.Errorf("details card on %s, want the panel", c[3].bg)
	}
	// Below 24 rows it is one line again.
	m = send(m, tea.WindowSizeMsg{Width: 60, Height: 23})
	if m.statusHeight() != 1 || !strings.Contains(statusText(t, m), "Running the tests (Bash · go test ./...)") {
		t.Errorf("60×23: status %q", statusText(t, m))
	}
}

func TestTabSaysWhatAProfileCosts(t *testing.T) {
	m, b := newModel(t, 60, 24, 0)
	b.items = map[string][]string{"+lo": {"+local"}, "+": {"+claude", "+local", "+sonnet"}}
	b.notes = map[string][]string{
		"+lo": {"qwen3.5:9b · local, $0 · ~8.4 GB"},
		"+":   {"opus-5-5 max · plan 5h 62%", "qwen3.5:9b · local, $0 · ~8.4 GB", "sonnet-5-5 medium"},
	}
	m = typeText(m, "+lo")
	m = send(m, keyMsg("tab"))
	if v := m.bar.Value(); v != "+local " {
		t.Errorf("bar = %q", v)
	}
	if m.status != "qwen3.5:9b · local, $0 · ~8.4 GB" {
		t.Errorf("status = %q, want what +local runs and costs", m.status)
	}
	// Several candidates are listed by name.
	m.bar.Reset()
	m = typeText(m, "+")
	m = send(m, keyMsg("tab"))
	if m.status != "+claude  +local  +sonnet" {
		t.Errorf("status = %q", m.status)
	}
}

func TestStatusStripSaysWhatASessionCosts(t *testing.T) {
	m, _ := newModel(t, 80, 24, 2)
	cost, ctx := 1.234, 41.0
	rows := sessions(2)
	rows[0].CostUSD, rows[0].Context, rows[0].Memory = &cost, &ctx, 640_000_000
	rows[1].Local, rows[1].Model, rows[1].Memory = true, "qwen3.5:9b", 512_000_000
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameSnapshot, Sessions: rows, Header: &ipc.Header{}}})
	m = send(m, keyMsg("esc"))
	m = selectNamed(t, m, "task-000")
	if got := statusText(t, m); !strings.Contains(got, "Working · $1.23 ctx 41% 640 MB") {
		t.Errorf("details card %q", got)
	}
	m = send(m, tea.WindowSizeMsg{Width: 80, Height: 20})
	if got := statusText(t, m); !strings.Contains(got, "$1.23 ctx 41% 640 MB · task-000") {
		t.Errorf("status strip %q", got)
	}
	m = selectNamed(t, m, "task-001")
	if got := statusText(t, m); !strings.Contains(got, "local 512 MB · task-001") {
		t.Errorf("status strip %q", got)
	}
}

func TestColonCommands(t *testing.T) {
	m, b := newModel(t, 60, 24, 2)
	enter := func(line string) {
		m.setFocus(focusBar)
		m.bar.Reset()
		m = typeText(m, line)
		m = send(m, keyMsg("enter"))
	}
	// Local setup runs in a session of its own, shown with the focus, so
	// its questions can be answered.
	enter(":setup local")
	if !reflect.DeepEqual(b.tools, []string{"setup"}) || len(b.shows) != 1 || !b.shows[0].focus || b.shows[0].id != "s0000000tool0" {
		t.Errorf("tools %v, shows %+v", b.tools, b.shows)
	}
	if len(b.spawns) != 0 {
		t.Errorf("a command spawned a task: %+v", b.spawns)
	}
	enter(":models")
	if !reflect.DeepEqual(b.tools, []string{"setup", "models"}) {
		t.Errorf("tools %v", b.tools)
	}
	// A rating goes to the selected session.
	m = selectNamed(t, m, "task-001")
	enter(":rate bad")
	if !reflect.DeepEqual(b.rates, []string{m.sessions[1].ID + " bad"}) || !strings.Contains(m.status, "rated task-001 bad") {
		t.Errorf("rates %v, status %q", b.rates, m.status)
	}
	// A dry run of the router says where the task would go.
	b.choice = route.Choice{Class: route.Small, Profile: "local", Reason: "small: local is the cheapest per success"}
	enter(":route fix the typo")
	if !reflect.DeepEqual(b.routes, []string{"fix the typo"}) || m.status != "small: local is the cheapest per success" {
		t.Errorf("routes %v, status %q", b.routes, m.status)
	}
	enter(":bogus")
	if !m.statusErr || !strings.Contains(m.status, ":setup, :models") {
		t.Errorf("unknown command: %q", m.status)
	}
}

func TestPlusAndMinusRateTheSelectedRow(t *testing.T) {
	m, b := newModel(t, 60, 24, 3)
	m = send(m, keyMsg("esc"))
	m = selectNamed(t, m, "task-002")
	m = send(m, keyMsg("+"))
	m = send(m, keyMsg("-"))
	if want := []string{m.sessions[2].ID + " good", m.sessions[2].ID + " bad"}; !reflect.DeepEqual(b.rates, want) {
		t.Errorf("rates = %v, want %v", b.rates, want)
	}
	if !strings.Contains(m.status, "the router learns from it") {
		t.Errorf("status = %q", m.status)
	}
}

// r puts the selected row's name in the command bar, to edit: Enter renames
// that row, Esc leaves it as it was.
func TestRRenamesTheSelectedRow(t *testing.T) {
	m, b := newModel(t, 60, 24, 3)
	m = send(m, keyMsg("esc"))
	m = selectNamed(t, m, "task-001")
	m = send(m, keyMsg("r"))
	if m.focus != focusBar || m.bar.Value() != ":rename task-001" {
		t.Fatalf("after r: focus %v, bar %q", m.focus, m.bar.Value())
	}
	m = send(m, keyMsg("esc"))
	if len(b.renames) != 0 || m.focus != focusList || m.bar.Value() != "" {
		t.Errorf("Esc: renames %v, focus %v, bar %q", b.renames, m.focus, m.bar.Value())
	}
	m = send(m, keyMsg("r"))
	for range len("task-001") {
		m = send(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	m = typeText(m, "Fix the login")
	m = send(m, keyMsg("enter"))
	if want := []string{m.sessions[1].ID + " Fix the login"}; !reflect.DeepEqual(b.renames, want) {
		t.Errorf("renames = %v, want %v", b.renames, want)
	}
	if m.focus != focusList || m.bar.Value() != "" || m.status != "renamed task-001 to Fix the login-2" {
		t.Errorf("after Enter: focus %v, bar %q, status %q", m.focus, m.bar.Value(), m.status)
	}
	if len(b.spawns) != 0 {
		t.Errorf("a rename spawned a task: %+v", b.spawns)
	}

	// Outside Irie's rows keep the name Claude Code gives them.
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameOutside, Outside: outsideSessions(1)}})
	m.sel = m.rowCount() - 1
	m = send(m, keyMsg("r"))
	if m.focus != focusList || !m.statusErr {
		t.Errorf("r on an outside row: focus %v, status %q", m.focus, m.status)
	}
}

func TestStatusStripSaysHowASessionWasRouted(t *testing.T) {
	m, _ := newModel(t, 90, 24, 2)
	rows := sessions(2)
	rows[0].Routed, rows[0].Class, rows[0].Profile, rows[0].Rated = true, route.Small, "local", ipc.Good
	rows[1].Parent = rows[0].ID
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameSnapshot, Sessions: rows, Header: &ipc.Header{}}})
	m = send(m, keyMsg("esc"))
	m = selectNamed(t, m, "task-000")
	if got := statusText(t, m); !strings.Contains(got, "Working · small→local rated good") {
		t.Errorf("details card %q", got)
	}
	m = send(m, tea.WindowSizeMsg{Width: 90, Height: 20})
	if got := statusText(t, m); !strings.Contains(got, "small→local rated good · task-000") {
		t.Errorf("status strip %q", got)
	}
	m = selectNamed(t, m, "task-001")
	if got := statusText(t, m); !strings.Contains(got, "↳ delegated · task-001") {
		t.Errorf("status strip %q", got)
	}
}

func TestHeaderShowsPlanUseAndLowMemory(t *testing.T) {
	m, _ := newModel(t, 90, 24, 0)
	header := func() string { return screen(t, m)[0] }
	if strings.Contains(header(), "5h") || strings.Contains(header(), "mem") {
		t.Fatalf("header %q before any use is known", header())
	}
	h := ipc.Header{Plan: &ipc.Plan{FiveHour: &ipc.Window{Used: 62}}, Memory: &ipc.Memory{Total: 18 << 30, Available: 1 << 30, Low: true}}
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameHeader, Header: &h}})
	if !strings.Contains(header(), "mem low 5h62%") || !strings.Contains(header(), "◐0 working") {
		t.Errorf("header %q", header())
	}
	// Narrow, the counts lose their labels first, then the plan goes.
	m = send(m, tea.WindowSizeMsg{Width: 34, Height: 24})
	if got := header(); !strings.Contains(got, "mem low") || !strings.Contains(got, "◐0") {
		t.Errorf("narrow header %q", got)
	}
	// A snapshot carries the header too.
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameSnapshot, Header: &ipc.Header{}}})
	if strings.Contains(header(), "mem low") {
		t.Errorf("header %q after memory recovered", header())
	}
	// A busy machine, where the session on show comes first, says so after
	// low memory.
	m = send(m, tea.WindowSizeMsg{Width: 90, Height: 24})
	h = ipc.Header{Plan: &ipc.Plan{FiveHour: &ipc.Window{Used: 62}}, Memory: &ipc.Memory{Total: 18 << 30, Available: 1 << 30, Low: true}, Busy: "memory"}
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameHeader, Header: &h}})
	if !strings.Contains(header(), "mem low busy 5h62%") {
		t.Errorf("busy header %q", header())
	}
}
