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
)

var now = time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)

type showCall struct {
	id    string
	focus bool
}

type fakeBackend struct {
	spawns   []ipc.SpawnArgs
	shows    []showCall
	spawnErr error
}

func (f *fakeBackend) Spawn(a ipc.SpawnArgs) (ipc.Session, error) {
	f.spawns = append(f.spawns, a)
	if f.spawnErr != nil {
		return ipc.Session{}, f.spawnErr
	}
	return ipc.Session{ID: fmt.Sprintf("snew%08d", len(f.spawns)), Name: "new"}, nil
}

func (f *fakeBackend) Show(id string, focus bool) error {
	f.shows = append(f.shows, showCall{id, focus})
	return nil
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
	want := []ipc.SpawnArgs{{Task: "describe this repo", Cwd: "/home/alice/code/website"}}
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
	if got := screen(t, m)[0]; !strings.Contains(got, "●2 working") {
		t.Errorf("wide header = %q", got)
	}
	m = send(m, tea.WindowSizeMsg{Width: 34, Height: 10})
	if got := screen(t, m)[0]; !strings.Contains(got, "●2") || strings.Contains(got, "working") {
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
	if row := screen(t, m)[m.listTop()]; !strings.HasPrefix(row, "▌● task-000") {
		t.Errorf("selected row = %q", row)
	}
	if row := screen(t, m)[m.listTop()+1]; !strings.HasPrefix(row, " ● task-001") {
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
		{in: "@/home/alice/code/x fix it", want: ipc.SpawnArgs{Task: "fix it", Cwd: "/home/alice/code/x"}},
		{in: "fix it @~/code/x", want: ipc.SpawnArgs{Task: "fix it", Cwd: "/home/alice/code/x"}},
		{in: "@~ list my files", want: ipc.SpawnArgs{Task: "list my files", Cwd: "/home/alice"}},
		{in: "fix @/abs   the bug", want: ipc.SpawnArgs{Task: "fix the bug", Cwd: "/abs"}},
		{in: "@/abs first line\nsecond @/not/a/token\n", want: ipc.SpawnArgs{Task: "first line\nsecond @/not/a/token\n", Cwd: "/abs"}},
		{in: "@/abs\nthe task is on line two", want: ipc.SpawnArgs{Task: "the task is on line two", Cwd: "/abs"}},
		{in: "mail alice@example.com about it", want: ipc.SpawnArgs{Task: "mail alice@example.com about it"}},
		{in: "  keep   inner  spacing  ", want: ipc.SpawnArgs{Task: "keep   inner  spacing"}},
		{in: "@website fix it", err: "@website"},
		{in: "@/a @/b fix it", err: "one @"},
		{in: "@/abs", err: "empty"},
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

func TestOversizedPasteIsRefusedNotCut(t *testing.T) {
	m, _ := newModel(t, 34, 24, 0)
	big := strings.Repeat("x\n", 20000)
	m = send(m, tea.PasteMsg{Content: big})
	if m.bar.Value() != "" {
		t.Errorf("bar holds %d bytes of a paste it could not hold whole", len(m.bar.Value()))
	}
	if !m.statusErr || !strings.Contains(m.status, "paste") {
		t.Errorf("status = %q", m.status)
	}
	m = send(m, tea.PasteMsg{Content: "small\npaste"})
	if m.bar.Value() != "small\npaste" {
		t.Errorf("bar = %q", m.bar.Value())
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
		"failed-row": "✕", "exited-ok": "·", "exited-bad": "·", "needs-row": "●", "stalled-row": "!",
		"working-row": "●", "starting-row": "◌", "done-row": "✓", "idle-row": "○",
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

func TestHeaderCountsWhatNeedsYou(t *testing.T) {
	m := statesModel(t, Options{})
	m = send(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	header := screen(t, m)[0]
	for _, want := range []string{"●1 working", "●2 needs you", "✓1 done"} {
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
