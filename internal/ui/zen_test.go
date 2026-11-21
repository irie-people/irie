package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/irie-people/irie/internal/ipc"
)

func (f *fakeBackend) Zen(a ipc.ZenArgs) (ipc.ZenResult, error) {
	f.zens = append(f.zens, a)
	return ipc.ZenResult{}, nil
}

// zenModel is a sidebar of n sessions whose clock the test moves.
func zenModel(t *testing.T, n int) (Model, *fakeBackend, *time.Time) {
	t.Helper()
	clock := now
	b := &fakeBackend{}
	m := New(b, Options{Now: func() time.Time { return clock }, Home: "/home/alice"})
	m = send(m, tea.WindowSizeMsg{Width: 40, Height: 30})
	m = send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameSnapshot, Sessions: sessions(n), Header: &ipc.Header{Zen: &ipc.Zen{}}}})
	return m, b, &clock
}

// header sends the header, with zen as given.
func withZen(m Model, z ipc.Zen) Model {
	return send(m, FrameMsg{Frame: ipc.Frame{T: ipc.FrameHeader, Header: &ipc.Header{Zen: &z}}})
}

func lastZen(t *testing.T, b *fakeBackend) ipc.ZenArgs {
	t.Helper()
	if len(b.zens) == 0 {
		t.Fatal("the sidebar said nothing of zen")
	}
	return b.zens[len(b.zens)-1]
}

func TestZenShowsTheSessionOnShowAlone(t *testing.T) {
	m, b, _ := zenModel(t, 5)
	m = selectNamed(t, m, "task-002")
	m = send(m, keyMsg("~"))
	a := lastZen(t, b)
	if a.On == nil || !*a.On || a.Client != ipc.ZenSidebar || a.Session != m.sessions[2].ID {
		t.Fatalf("zen said %+v", a)
	}
	if len(b.shows) == 0 || b.shows[len(b.shows)-1].id != m.sessions[2].ID {
		t.Errorf("the slot wasn't given the session: %+v", b.shows)
	}
	text := strings.Join(screen(t, m), "\n")
	if !strings.Contains(text, "task-002") || strings.Contains(text, "task-001") || strings.Contains(text, "task-003") {
		t.Errorf("zen shows more than its session:\n%s", text)
	}
	head := screen(t, m)[0]
	if !strings.Contains(head, "zen") || !strings.Contains(head, "~ leaves") || strings.Contains(head, tagline) {
		t.Errorf("zen's title bar: %q", head)
	}
	// j goes on to the next session, which the slot shows.
	m = send(m, keyMsg("j"))
	if a := lastZen(t, b); a.Session != m.sessions[3].ID || !strings.Contains(strings.Join(screen(t, m), "\n"), "task-003") {
		t.Errorf("j: %+v", a)
	}
	// Keys that change the list wait until zen ends.
	m = send(m, keyMsg("x"))
	if len(b.closes) > 0 {
		t.Error("x closed a session in zen")
	}
	m = send(m, keyMsg("~"))
	if a := lastZen(t, b); a.On == nil || *a.On || a.Client != ipc.ZenSidebar || m.zen {
		t.Errorf("leaving: %+v", a)
	}
	if text := strings.Join(screen(t, m), "\n"); !strings.Contains(text, "task-001") {
		t.Errorf("the list didn't come back:\n%s", text)
	}
}

func TestZenSaysSoftlyWhenAnotherSessionWouldLikeYou(t *testing.T) {
	m, b, _ := zenModel(t, 4)
	m = selectNamed(t, m, "task-001")
	m = send(m, keyMsg("~"))
	if strings.Contains(strings.Join(screen(t, m), "\n"), "would like you") {
		t.Fatal("a soft line with no one waiting")
	}
	m.sessions[3].State = ipc.StateNeedsYou
	if text := strings.Join(screen(t, m), "\n"); !strings.Contains(text, "Another session would like you") {
		t.Fatalf("no soft line:\n%s", text)
	}
	m.sessions[2].State = ipc.StateNeedsYou
	if text := strings.Join(screen(t, m), "\n"); !strings.Contains(text, "2 other sessions would like you") {
		t.Errorf("two waiting:\n%s", text)
	}
	m = send(m, keyMsg("o"))
	if a := lastZen(t, b); a.Session != m.sessions[2].ID {
		t.Errorf("o showed %s", a.Session)
	}
}

func TestZenOffersOneThingWithYAndN(t *testing.T) {
	m, b, _ := zenModel(t, 3)
	m = send(m, keyMsg("~"))
	m = withZen(m, ipc.Zen{On: true, Clients: []string{ipc.ZenSidebar}, Offer: &ipc.ZenOffer{
		ID: "eyes", Kind: ipc.ZenKindLearn, Text: "Look far away for a moment.", Yes: "Done", No: "Not Now"}})
	text := strings.Join(screen(t, m), "\n")
	if !strings.Contains(text, "Look far away for a moment.") || !strings.Contains(text, "y Done") || !strings.Contains(text, "n Not Now") {
		t.Fatalf("the offer:\n%s", text)
	}
	m = send(m, keyMsg("y"))
	if a := lastZen(t, b); a.Offer != "eyes" || a.Answer != ipc.ZenYes {
		t.Errorf("y answered %+v", a)
	}
	m = withZen(m, ipc.Zen{On: true, Clients: []string{ipc.ZenSidebar}, Offer: &ipc.ZenOffer{
		ID: ipc.ZenRest, Kind: ipc.ZenKindRest, Text: "Rest your eyes.", Yes: "Back to It", No: "In 10 Minutes"}})
	text = strings.Join(screen(t, m), "\n")
	if strings.Count(text, "●") != 1 || strings.Count(text, "·") < breathRows-1 {
		t.Errorf("the breath's dot:\n%s", text)
	}
	m = send(m, keyMsg("n"))
	if a := lastZen(t, b); a.Offer != ipc.ZenRest || a.Answer != ipc.ZenNo {
		t.Errorf("n answered %+v", a)
	}
}

// A sidebar that restarts comes back into zen from the header and says so
// again; one whose zen lapsed in the core leaves it, once a header has had
// time to show what it said.
func TestZenFollowsTheCore(t *testing.T) {
	m, b, clock := zenModel(t, 3)
	m.sessions[1].Shown = true
	m = withZen(m, ipc.Zen{On: true, Clients: []string{ipc.ZenApp, ipc.ZenSidebar}})
	if !m.zen || m.zenShows != m.sessions[1].ID {
		t.Fatalf("zen after a restart: %v %q", m.zen, m.zenShows)
	}
	// Each tick runs zenRenew. A tick here would run the next one, and so
	// on for good, so the test runs zenRenew itself.
	renew := func() { m = run(m, m.zenRenew()) }
	renew()
	if a := lastZen(t, b); a.On == nil || !*a.On || a.Session != m.sessions[1].ID {
		t.Fatalf("the first tick said %+v", a)
	}
	said := len(b.zens)
	*clock = clock.Add(30 * time.Second)
	renew()
	*clock = clock.Add(ipc.ZenRenew)
	renew()
	if len(b.zens) != said+1 {
		t.Errorf("renewals: %d, want one more than %d", len(b.zens), said)
	}
	// A header from before the core heard it doesn't end zen; one after,
	// that leaves the sidebar out, does.
	m = withZen(m, ipc.Zen{Clients: []string{ipc.ZenApp}})
	if !m.zen {
		t.Fatal("zen ended within its grace")
	}
	*clock = clock.Add(zenGrace + time.Second)
	m = withZen(m, ipc.Zen{Clients: []string{ipc.ZenApp}})
	if m.zen {
		t.Error("the sidebar stayed in zen the core had ended")
	}
}

func TestTheBreathRisesForFourAndFallsForSix(t *testing.T) {
	at := time.Unix(0, 0)
	var rows []int
	for s := range 11 {
		rows = append(rows, breathRow(at.Add(time.Duration(s)*time.Second)))
	}
	if want := []int{4, 3, 2, 1, 0, 1, 1, 2, 3, 3, 4}; !slices.Equal(rows, want) {
		t.Errorf("rows %v, want %v", rows, want)
	}
}
