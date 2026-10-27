package ui

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/irie-hq/irie/internal/ipc"
)

func (f *fakeBackend) Move(vendorID, profile string) (ipc.Session, error) {
	f.moves = append(f.moves, vendorID)
	if f.moveErr != nil {
		return ipc.Session{}, f.moveErr
	}
	return ipc.Session{ID: "smoved000001", Name: "moved", State: ipc.StateStarting, Created: now}, nil
}

// outsideSessions are n sessions outside Irie, started in order, every
// third one busy.
func outsideSessions(n int) []ipc.OutsideSession {
	out := make([]ipc.OutsideSession, n)
	for i := range out {
		o := ipc.OutsideSession{
			VendorID: fmt.Sprintf("00000000-0000-4000-8000-%012d", i),
			Name:     fmt.Sprintf("outside-%02d", i),
			Kind:     ipc.KindInteractive, Status: ipc.StatusIdle, PID: 4000 + i,
			Cwd:     "/home/alice/code/api",
			Started: now.Add(-time.Duration(n-i) * time.Hour),
			Movable: true,
		}
		if i%3 == 1 {
			o.Status, o.Movable, o.Why = ipc.StatusBusy, false, "it is busy; it can move once it is idle"
		}
		out[i] = o
	}
	return out
}

func outsideFrame(list []ipc.OutsideSession, problem string) FrameMsg {
	return FrameMsg{Frame: ipc.Frame{T: ipc.FrameOutside, Outside: list, Problem: problem}}
}

func withOutside(t *testing.T, w, h, n, outside int) (Model, *fakeBackend) {
	t.Helper()
	m, b := newModel(t, w, h, n)
	return send(m, outsideFrame(outsideSessions(outside), "")), b
}

func TestTheOutsideGroupComesLastAtEveryHeight(t *testing.T) {
	for _, h := range []int{6, 24, 80} {
		m, _ := withOutside(t, 34, h, 3, 2)
		lines := screen(t, m)
		if len(lines) != h {
			t.Fatalf("34×%d: %d lines", h, len(lines))
		}
		if !strings.Contains(lines[0], "☀ Irie") || !strings.HasPrefix(lines[h-1], barPrompt) {
			t.Errorf("34×%d: header %q, bar %q", h, lines[0], lines[h-1])
		}
		for i, l := range lines {
			if w := lipgloss.Width(l); w > 34 {
				t.Errorf("34×%d: line %d is %d columns: %q", h, i, w, l)
			}
		}
		// Scrolled to the end, the group's header and rows follow Irie's.
		m = send(m, keyMsg("end"))
		all := strings.Join(screen(t, m), "\n")
		if !strings.Contains(all, "outside-01") {
			t.Errorf("34×%d: the last row is not on screen:\n%s", h, all)
		}
		if m.listHeight() >= 6 {
			head, first, last := strings.Index(all, "Outside Irie 2"), strings.Index(all, "task-002"), strings.Index(all, "outside-00")
			if head < 0 || first < 0 || last < 0 || !(first < head && head < last) {
				t.Errorf("34×%d: want the sessions, the header, then its rows:\n%s", h, all)
			}
		}
	}
	// With the list room for every row, each is one line, with the glyph of
	// the Irie state it matches.
	m, _ := withOutside(t, 34, 24, 1, 2)
	m = send(m, keyMsg("tab"))
	lines := screen(t, m)
	top := m.listTop()
	busy, _, _, _ := m.state(ipc.Session{State: ipc.StateWorking})
	idle, _, _, _ := m.state(ipc.Session{State: ipc.StateIdle})
	if want := []string{busy + " task-000", "Outside Irie 2 ─", idle + " outside-00", busy + " outside-01"}; !hasPrefixes(lines[top:top+4], want) {
		t.Errorf("rows %q, want them to start %q", lines[top:top+4], want)
	}
}

// hasPrefixes reports whether each line starts with its prefix, after the
// selection's edge.
func hasPrefixes(lines, prefixes []string) bool {
	for i, p := range prefixes {
		if !strings.HasPrefix(strings.TrimLeft(lines[i], " ▌"), p) {
			return false
		}
	}
	return true
}

func TestAnOutsideRowsStatusSaysWhereItRuns(t *testing.T) {
	m, _ := withOutside(t, 120, 24, 1, 2)
	status := func() string { return screen(t, m)[len(screen(t, m))-2] }
	m = send(m, keyMsg("down")) // the header
	if !strings.Contains(status(), "other terminals") {
		t.Errorf("header status = %q", status())
	}
	m = send(m, keyMsg("down"))
	for _, want := range []string{"idle · m moves it in", "outside-00", "~/code/api", "in a terminal, pid 4000"} {
		if !strings.Contains(status(), want) {
			t.Errorf("status %q lacks %q", status(), want)
		}
	}
	m = send(m, keyMsg("down"))
	if s := status(); !strings.HasPrefix(strings.TrimSpace(s), "busy") || strings.Contains(s, "m moves") {
		t.Errorf("busy row's status = %q", s)
	}
}

func TestEnterOnAnOutsideRowExplains(t *testing.T) {
	m, b := withOutside(t, 120, 24, 1, 2)
	for range 2 {
		m = send(m, keyMsg("down"))
	}
	m = send(m, keyMsg("enter"))
	m = send(m, keyMsg("space"))
	if len(b.shows) != 0 {
		t.Errorf("shows = %+v, want none for a session outside Irie", b.shows)
	}
	if !strings.Contains(m.status, "runs outside Irie") || !strings.Contains(m.status, "m moves it in") || m.statusErr {
		t.Errorf("status = %q", m.status)
	}
	m = send(m, tea.MouseClickMsg{X: 3, Y: m.listTop() + 3, Button: tea.MouseLeft})
	if len(b.shows) != 0 || m.sel != 3 || !strings.Contains(m.status, "outside-01 runs outside Irie") || !strings.Contains(m.status, "busy") {
		t.Errorf("click: shows %+v, sel %d, status %q", b.shows, m.sel, m.status)
	}
}

func TestMMovesAnIdleOutsideSessionIn(t *testing.T) {
	m, b := withOutside(t, 120, 24, 1, 2)
	m = send(m, keyMsg("m")) // on an Irie row it only explains
	if len(b.moves) != 0 || !strings.Contains(m.status, "Outside Irie") {
		t.Errorf("m on an Irie row: moves %v, status %q", b.moves, m.status)
	}
	m = send(m, keyMsg("end")) // the busy one
	m = send(m, keyMsg("m"))
	if len(b.moves) != 0 || !m.statusErr || !strings.Contains(m.status, "busy") {
		t.Errorf("m on a busy row: moves %v, status %q", b.moves, m.status)
	}
	m = send(m, keyMsg("up"))
	m = send(m, keyMsg("m"))
	if want := []string{"00000000-0000-4000-8000-000000000000"}; !reflect.DeepEqual(b.moves, want) {
		t.Fatalf("moves = %v, want %v", b.moves, want)
	}
	if !strings.Contains(m.status, "moved outside-00 into Irie") {
		t.Errorf("status = %q", m.status)
	}
	// The core then drops it from the group and adds its row, which is
	// selected.
	m = send(m, outsideFrame(outsideSessions(2)[1:], ""))
	moved := ipc.Session{ID: "smoved000001", Name: "outside-00", State: ipc.StateIdle, Created: now}
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameUpsert, Session: &moved}})
	if m.sel != 1 || m.sessions[1].ID != moved.ID {
		t.Errorf("selection %d in %+v, want the moved session", m.sel, m.sessions)
	}

	// A refusal from the core shows as an error.
	m, b = withOutside(t, 120, 24, 0, 1)
	b.moveErr = errors.New("outside-00 cannot move now: it is busy")
	m = send(m, keyMsg("esc")) // with no Irie rows the bar has focus
	m = send(m, keyMsg("down"))
	m = send(m, keyMsg("m"))
	if !m.statusErr || !strings.Contains(m.status, "cannot move now") || m.moving {
		t.Errorf("status %q (error %v), moving %v", m.status, m.statusErr, m.moving)
	}
}

func TestColonMoveInTheBar(t *testing.T) {
	m, b := withOutside(t, 120, 24, 1, 3)
	m = send(m, keyMsg("tab"))
	m = typeText(m, ":move outside-02")
	m = send(m, keyMsg("enter"))
	if want := []string{"00000000-0000-4000-8000-000000000002"}; !reflect.DeepEqual(b.moves, want) {
		t.Fatalf(":move by name: moves %v", b.moves)
	}
	if m.bar.Value() != "" || m.focus != focusList || len(b.spawns) != 0 {
		t.Errorf("bar %q, focus %v, spawns %v", m.bar.Value(), m.focus, b.spawns)
	}
	// Without a name it takes the selected row.
	m = send(m, keyMsg("end"))
	m = send(m, keyMsg(":"))
	m = typeText(m, "move")
	m = send(m, keyMsg("enter"))
	if len(b.moves) != 2 || b.moves[1] != "00000000-0000-4000-8000-000000000002" {
		t.Errorf(":move on the selection: moves %v", b.moves)
	}
	// A name that fits none is an error, and stays in the bar.
	m = send(m, keyMsg(":"))
	m = typeText(m, "move nothing-here")
	m = send(m, keyMsg("enter"))
	if !m.statusErr || !strings.Contains(m.status, "nothing-here") || len(b.moves) != 2 || m.bar.Value() == "" {
		t.Errorf("status %q, moves %v, bar %q", m.status, b.moves, m.bar.Value())
	}
	// Other commands are still unknown.
	m.bar.Reset()
	m = typeText(m, ":pause")
	m = send(m, keyMsg("enter"))
	if !strings.Contains(m.status, "unknown command :pause") {
		t.Errorf("status = %q", m.status)
	}
}

// TestOutsideRowsKeepTheirPlace: a state change never moves or regroups a
// row, and the selection stays on its session.
func TestOutsideRowsKeepTheirPlace(t *testing.T) {
	m, _ := withOutside(t, 34, 24, 2, 3)
	m = send(m, keyMsg("end"))
	m = send(m, keyMsg("up")) // outside-01, busy
	before := screen(t, m)
	flipped := outsideSessions(3)
	for i := range flipped {
		if flipped[i].Status == ipc.StatusIdle {
			flipped[i].Status = ipc.StatusBusy
		} else {
			flipped[i].Status = ipc.StatusIdle
		}
	}
	m = send(m, outsideFrame(flipped, ""))
	after := screen(t, m)
	for i := range before {
		if strip := func(s string) string { return strings.NewReplacer("●", "", "◐", "", "○", "").Replace(s) }; strip(before[i]) != strip(after[i]) && i < len(before)-2 {
			t.Errorf("line %d moved: %q → %q", i, before[i], after[i])
		}
	}
	if o, ok := m.outsideAt(m.sel); !ok || o.Name != "outside-01" {
		t.Errorf("selection moved to %+v", o)
	}
	// An Irie row that comes or goes keeps the selection on its session too.
	added := ipc.Session{ID: "s000000000099", Name: "task-new", State: ipc.StateWorking, Created: now}
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameUpsert, Session: &added}})
	if o, ok := m.outsideAt(m.sel); !ok || o.Name != "outside-01" {
		t.Errorf("after a new Irie row the selection is on %+v", o)
	}
	// When the selected outside session goes, the selection moves on.
	m = send(m, outsideFrame(flipped[:1], ""))
	if m.sel >= m.rowCount() {
		t.Errorf("selection %d past %d rows", m.sel, m.rowCount())
	}
}

func TestAProblemListingShowsOnce(t *testing.T) {
	m, _ := withOutside(t, 120, 24, 1, 1)
	m = send(m, outsideFrame(outsideSessions(1), "sessions outside Irie: `claude agents --json` did not answer within 5s"))
	if !m.statusErr || !strings.Contains(m.status, "did not answer") {
		t.Fatalf("status = %q", m.status)
	}
	m = send(m, keyMsg("down")) // a key clears it
	m = send(m, outsideFrame(outsideSessions(1), "sessions outside Irie: `claude agents --json` did not answer within 5s"))
	if strings.Contains(m.status, "did not answer") {
		t.Errorf("the same problem showed again: %q", m.status)
	}
	// The last list stays while the problem lasts.
	if len(m.outside) != 1 {
		t.Errorf("outside = %+v", m.outside)
	}
}

func TestOutsideNamesCannotSteerTheTerminal(t *testing.T) {
	list := outsideSessions(1)
	list[0].Name, list[0].Cwd = "evil\x1b[2Jname", "/home/alice/\x1b]0;title\x07dir"
	m, _ := newModel(t, 120, 24, 0)
	m = send(m, outsideFrame(list, ""))
	m = send(m, keyMsg("tab"))
	m = send(m, keyMsg("down"))
	for _, l := range strings.Split(m.View().Content, "\n") {
		if strings.Contains(l, "\x1b[2J") || strings.Contains(l, "\x1b]0;") {
			t.Errorf("a control sequence from an outside session reached the screen: %q", l)
		}
	}
}
