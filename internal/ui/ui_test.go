package ui

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/route"
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
	tools     []string
	routes    []string
	choice    route.Choice // what Route returns
}

func (f *fakeBackend) Rate(id, verdict string) error {
	f.rates = append(f.rates, id+" "+verdict)
	return nil
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
			if !strings.HasPrefix(lines[h-1], barPrompt) {
				t.Errorf("34×%d, %d rows: last line %q is not the command bar", h, n, lines[h-1])
			}
			for i, l := range lines {
				if w := lipgloss.Width(l); w > 34 {
					t.Errorf("34×%d: line %d is %d columns: %q", h, i, w, l)
				}
			}
			if top := m.listTop(); n > 0 && !strings.Contains(lines[top], "task-000") {
				t.Errorf("34×%d: line %d = %q, want the first row", h, top, lines[top])
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
	if m.sel != 99 {
		t.Fatalf("selection = %d, want 99", m.sel)
	}
	if !strings.Contains(strings.Join(screen(t, m), "\n"), "task-099") {
		t.Error("the selected last row is not on screen")
	}

	// Walking down one row at a time keeps it visible too.
	m, _ = newModel(t, 34, 24, 100)
	for range 99 {
		m = send(m, keyMsg("down"))
	}
	if m.sel != 99 || !strings.Contains(strings.Join(screen(t, m), "\n"), "task-099") {
		t.Errorf("selection %d, offset %d: last row hidden", m.sel, m.offset)
	}
}

func TestWheelMovesTheOffsetNotTheSelection(t *testing.T) {
	m, _ := newModel(t, 34, 24, 100)
	sel, off := m.sel, m.offset
	m = send(m, tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown})
	if m.offset <= off {
		t.Errorf("offset %d → %d after wheel down", off, m.offset)
	}
	if m.sel != sel {
		t.Errorf("selection moved %d → %d", sel, m.sel)
	}
	if strings.Contains(screen(t, m)[m.listTop()], "task-000") {
		t.Error("first row still on top after scrolling")
	}
	m = send(m, tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelUp})
	if m.offset != off || m.sel != sel {
		t.Errorf("after wheel up: offset %d selection %d", m.offset, m.sel)
	}
	// The wheel stops at the ends.
	for range 200 {
		m = send(m, tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown})
	}
	if max := 100 - m.listHeight(); m.offset != max {
		t.Errorf("offset %d after scrolling past the end, want %d", m.offset, max)
	}
}

// What the core sends never scrolls the list: you can read it while the
// agents work.
func TestFramesLeaveTheListWhereItIs(t *testing.T) {
	m, _ := newModel(t, 34, 24, 100)
	for range 10 {
		m = send(m, tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown})
	}
	first := func() string { return screen(t, m)[m.listTop()] }
	if !strings.Contains(first(), "task-010") {
		t.Fatalf("first row after scrolling = %q", first())
	}
	// A row changes state while the selection is off screen.
	row := sessions(100)[50]
	row.State = ipc.StateNeedsYou
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameUpsert, Session: &row}})
	if !strings.Contains(first(), "task-010") {
		t.Errorf("an upsert scrolled the list: first row %q", first())
	}
	// A row above the screen goes away: the rows on screen stay put.
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameRemove, ID: "s000000000003"}})
	if !strings.Contains(first(), "task-010") {
		t.Errorf("a removal above the screen moved the rows: first row %q", first())
	}
	if m.sel != 0 {
		t.Errorf("selection moved to %d", m.sel)
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
	lines := screen(t, m)
	if len(lines) != 24 {
		t.Fatalf("%d lines", len(lines))
	}
	if !strings.Contains(lines[21], "one") || !strings.Contains(lines[23], "three") {
		t.Errorf("bar lines = %q", lines[21:])
	}
}

// A growing command bar shows fewer rows but never scrolls them, even when
// the selected row drops out of sight.
func TestTypingLeavesTheListWhereItIs(t *testing.T) {
	m, _ := newModel(t, 34, 24, 100)
	h := m.listHeight()
	for range h - 1 {
		m = send(m, keyMsg("down")) // the selection is on the last row on screen
	}
	m = send(m, keyMsg("tab"))
	m = typeText(m, "one")
	m = send(m, keyMsg("ctrl+j"))
	m = typeText(m, "two")
	m = send(m, keyMsg("ctrl+j"))
	m = typeText(m, "three")
	if first := screen(t, m)[m.listTop()]; m.offset != 0 || !strings.Contains(first, "task-000") {
		t.Errorf("typing scrolled the list: offset %d, first row %q", m.offset, first)
	}
	// Back in the list, the selected row is on screen again.
	m = send(m, keyMsg("esc"))
	rows := strings.Join(screen(t, m)[m.listTop():m.listTop()+m.listHeight()], "\n")
	if want := fmt.Sprintf("task-%03d", h-1); !strings.Contains(rows, want) {
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
	if want := (tea.Position{X: lipgloss.Width(barPrompt) + 3, Y: 23}); c.Position != want {
		t.Errorf("cursor at %+v, want %+v", c.Position, want)
	}
	if c.Color != nil {
		t.Errorf("cursor colour %v; the terminal's own suits its theme", c.Color)
	}
	m = send(m, keyMsg("ctrl+j"))
	m = typeText(m, "de")
	if c := m.View().Cursor; c == nil || c.Position != (tea.Position{X: lipgloss.Width(barPrompt) + 2, Y: 23}) {
		t.Errorf("second line: cursor %+v", c)
	}
	m = send(m, keyMsg("esc"))
	if c := m.View().Cursor; c != nil {
		t.Errorf("cursor at %+v after leaving the bar", c.Position)
	}
}

// A task left in the bar while the list has the keyboard keeps the
// terminal's own colour, which reads on a light theme too.
func TestABlurredTaskKeepsTheTerminalsColour(t *testing.T) {
	m, _ := newModel(t, 34, 24, 3)
	m = send(m, keyMsg("tab"))
	m = typeText(m, "half a task")
	m = send(m, tea.MouseClickMsg{X: 3, Y: m.listTop(), Button: tea.MouseLeft})
	raw := strings.Split(m.View().Content, "\n")
	bar := raw[len(raw)-1]
	if !strings.Contains(ansiRE.ReplaceAllString(bar, ""), "half a task") {
		t.Fatalf("bar = %q", bar)
	}
	if sgr(37).MatchString(bar) {
		t.Errorf("blurred task drawn in light grey: %q", bar)
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

func TestClickShowsTheRow(t *testing.T) {
	m, b := newModel(t, 34, 24, 3)
	m = send(m, tea.MouseClickMsg{X: 3, Y: m.listTop() + 2, Button: tea.MouseLeft})
	if len(b.shows) != 0 {
		t.Errorf("shown before the button came up: %+v", b.shows)
	}
	m = send(m, tea.MouseReleaseMsg{X: 3, Y: m.listTop() + 2, Button: tea.MouseLeft})
	if want := []showCall{{"s000000000002", true}}; !reflect.DeepEqual(b.shows, want) {
		t.Errorf("shows = %+v, want %+v", b.shows, want)
	}
	if m.sel != 2 {
		t.Errorf("selection = %d", m.sel)
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
	if m.sel != 0 || m.sessions[0].ID != created.ID {
		t.Errorf("new row not selected: sel %d sessions %+v", m.sel, m.sessions)
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
	if m.sel != 1 || m.sessions[m.sel].ID != "s000000000002" {
		t.Errorf("selection = %d (%v)", m.sel, m.sessions)
	}
}

func TestLongNamesAreCut(t *testing.T) {
	m, _ := newModel(t, 34, 24, 0)
	long := ipc.Session{ID: "s1", Name: "a-very-long-session-name-that-will-not-fit", State: ipc.StateWorking, Created: now}
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameUpsert, Session: &long}})
	lines := screen(t, m)
	if row := lines[m.listTop()]; !strings.Contains(row, "…") {
		t.Errorf("row = %q, want an ellipsis", row)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "a-very-long-session-name-that") {
		t.Errorf("full name not in the status strip:\n%s", strings.Join(lines, "\n"))
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
	if red.MatchString(header) {
		t.Errorf("header %q has red with nothing needing you", header)
	}
}

func TestRulesNeedRoom(t *testing.T) {
	isRule := func(l string) bool { return strings.Contains(l, "────") }
	m, _ := newModel(t, 34, 9, 3)
	for i, l := range screen(t, m) {
		if isRule(l) {
			t.Errorf("height 9: line %d is a rule", i)
		}
	}
	m, _ = newModel(t, 34, 10, 3)
	lines := screen(t, m)
	if !isRule(lines[1]) || !isRule(lines[len(lines)-3]) {
		t.Errorf("height 10: no rules under the header and above the status strip:\n%s", strings.Join(lines, "\n"))
	}
	if !strings.Contains(lines[2], "task-000") {
		t.Errorf("height 10: line 2 = %q, want the first row", lines[2])
	}
}

func TestSelectionHasAnEdge(t *testing.T) {
	m, _ := newModel(t, 34, 24, 3)
	if row := screen(t, m)[m.listTop()]; !strings.HasPrefix(row, "▌◐ task-000") {
		t.Errorf("selected row = %q", row)
	}
	if row := screen(t, m)[m.listTop()+1]; !strings.HasPrefix(row, " ◐ task-001") {
		t.Errorf("other row = %q", row)
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
	status := screen(t, m)[len(screen(t, m))-2]
	for _, want := range []string{"irie/fix-it", "copied .env, CLAUDE.local.md", "2 uncommitted changes"} {
		if !strings.Contains(status, want) {
			t.Errorf("status strip %q lacks %q", status, want)
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
	}
	for i := range rows {
		rows[i].ID = fmt.Sprintf("s%012d", i)
		rows[i].Created = now.Add(-time.Duration(len(rows)-i) * time.Minute)
	}
	return rows
}

func statesModel(t *testing.T, o Options) Model {
	t.Helper()
	o.Now = func() time.Time { return now }
	m := New(&fakeBackend{}, o)
	m = send(m, tea.WindowSizeMsg{Width: 34, Height: 24})
	return send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameSnapshot, Sessions: everyState(), Header: &ipc.Header{}}})
}

// rowLine returns the raw (styled) screen line of the row named name.
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
	m := statesModel(t, Options{})
	m = send(m, keyMsg("tab")) // the selection bar would hide nothing, but keep rows plain
	for name, glyph := range map[string]string{
		"failed-row": "✕", "exited-ok": "·", "exited-bad": "✕", "needs-row": "●", "stalled-row": "!",
		"working-row": "◐", "starting-row": "◌", "done-row": "✓", "idle-row": "○",
	} {
		plain := strings.TrimLeft(ansiRE.ReplaceAllString(rowLine(t, m, name), ""), " ▌")
		if !strings.HasPrefix(plain, glyph+" "+name) {
			t.Errorf("row %q, want glyph %s", plain, glyph)
		}
	}
}

// sgr matches an SGR sequence that sets the basic colour n, alone or with
// other attributes such as faint.
func sgr(n int) *regexp.Regexp {
	return regexp.MustCompile(fmt.Sprintf(`\x1b\[(?:[0-9]+;)*%d(?:;[0-9]+)*m`, n))
}

var red, green = sgr(31), sgr(32)

func TestRedMarksOnlyTheRowsThatNeedYou(t *testing.T) {
	m := statesModel(t, Options{})
	m = send(m, keyMsg("tab"))
	for name, wantRed := range map[string]bool{
		"failed-row": true, "exited-bad": true, "needs-row": true, "stalled-row": true,
		"exited-ok": false, "working-row": false, "starting-row": false, "done-row": false, "idle-row": false,
	} {
		if got := red.MatchString(rowLine(t, m, name)); got != wantRed {
			t.Errorf("%s: red %v, want %v: %q", name, got, wantRed, rowLine(t, m, name))
		}
	}
	if !green.MatchString(rowLine(t, m, "done-row")) {
		t.Errorf("done row is not green: %q", rowLine(t, m, "done-row"))
	}
	// ui.red_on_done shows done in red instead.
	m = statesModel(t, Options{RedOnDone: true})
	m = send(m, keyMsg("tab"))
	if !red.MatchString(rowLine(t, m, "done-row")) {
		t.Errorf("red_on_done: done row %q", rowLine(t, m, "done-row"))
	}
}

// Without colour (NO_COLOR, a monochrome terminal, colour blindness) a red
// row still looks different from every row that is not red.
func TestRedIsNeverTheOnlyCue(t *testing.T) {
	m := statesModel(t, Options{})
	m = send(m, keyMsg("tab"))
	rows := map[bool]map[string]string{true: {}, false: {}} // red → glyph → row
	for _, s := range everyState() {
		raw := rowLine(t, m, s.Name)
		glyph := strings.Fields(strings.TrimLeft(ansiRE.ReplaceAllString(raw, ""), " ▌"))[0]
		rows[red.MatchString(raw)][glyph] = s.Name
	}
	for glyph, name := range rows[true] {
		if other, ok := rows[false][glyph]; ok {
			t.Errorf("%s and %s are both %s: only red tells them apart", name, other, glyph)
		}
	}
}

func TestHeaderCountsWhatNeedsYou(t *testing.T) {
	m := statesModel(t, Options{})
	m = send(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	header := screen(t, m)[0]
	for _, want := range []string{"◐1 working", "●2 needs you", "✓1 done"} {
		if !strings.Contains(header, want) {
			t.Errorf("header %q lacks %q", header, want)
		}
	}
	if raw := strings.Split(m.View().Content, "\n")[0]; !red.MatchString(raw) {
		t.Errorf("header %q: the needs-you count is not red", raw)
	}
}

func TestStatusStripShowsWhyAndWhat(t *testing.T) {
	m := statesModel(t, Options{})
	status := func() string { return screen(t, m)[len(screen(t, m))-2] }
	for i, want := range []string{
		"failed: exit 1", "exited 0", "exited 2", "asks: Which colour", "stalled: no hooks", "Bash · go test", "starting-row", "done-row", "idle-row",
	} {
		m.sel = i
		if !strings.Contains(status(), want) {
			t.Errorf("row %d: status strip %q, want %q", i, status(), want)
		}
	}
	// A warning shows when there is nothing more pressing to say.
	rows := everyState()
	rows[6].Warnings = []string{"pause off: Claude Code 2.1.200 is older than 2.1.295"}
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameUpsert, Session: &rows[6]}})
	m.sel = 6
	if !strings.Contains(status(), "pause off: Claude Code 2.1.200") {
		t.Errorf("status strip %q, want the warning", status())
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
	status := func() string { s := screen(t, m); return s[len(s)-2] }
	m.sel = 0
	if !strings.Contains(status(), "$1.23 ctx 41% 640 MB · task-000") {
		t.Errorf("status strip %q", status())
	}
	m.sel = 1
	if !strings.Contains(status(), "local 512 MB · task-001") {
		t.Errorf("status strip %q", status())
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
	m.sel = 1
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
	m.sel = 2
	m = send(m, keyMsg("+"))
	m = send(m, keyMsg("-"))
	if want := []string{m.sessions[2].ID + " good", m.sessions[2].ID + " bad"}; !reflect.DeepEqual(b.rates, want) {
		t.Errorf("rates = %v, want %v", b.rates, want)
	}
	if !strings.Contains(m.status, "the router learns from it") {
		t.Errorf("status = %q", m.status)
	}
}

func TestStatusStripSaysHowASessionWasRouted(t *testing.T) {
	m, _ := newModel(t, 90, 24, 2)
	rows := sessions(2)
	rows[0].Routed, rows[0].Class, rows[0].Profile, rows[0].Rated = true, route.Small, "local", ipc.Good
	rows[1].Parent = rows[0].ID
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameSnapshot, Sessions: rows, Header: &ipc.Header{}}})
	m = send(m, keyMsg("esc"))
	status := func() string { s := screen(t, m); return s[len(s)-2] }
	m.sel = 0
	if !strings.Contains(status(), "small→local rated good · task-000") {
		t.Errorf("status strip %q", status())
	}
	m.sel = 1
	if !strings.Contains(status(), "↳ delegated · task-001") {
		t.Errorf("status strip %q", status())
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
}
