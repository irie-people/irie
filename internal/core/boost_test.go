package core

import (
	"github.com/irie-people/irie/internal/brand"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/journal"
	"github.com/irie-people/irie/internal/proc"
	"github.com/irie-people/irie/internal/testutil"
)

// boosts are the boost.on and boost.off a core recorded for session id,
// each as its kind and why.
func (c *Core) boosts(t *testing.T, id string) []string {
	t.Helper()
	var out []string
	for _, e := range c.journalOf(t, id) {
		switch e.Kind {
		case event.BoostOn:
			out = append(out, e.Kind+" "+e.Str("error"))
		case event.BoostOff:
			out = append(out, e.Kind+" "+e.Str("why"))
		}
	}
	return out
}

// journalOf reads back the events a core's journal has of session id.
func (c *Core) journalOf(t *testing.T, id string) []event.Event {
	t.Helper()
	evs, err := journal.ReadSessions(c.paths.Journal(), time.Time{}, id)
	if err != nil {
		t.Fatal(err)
	}
	return evs
}

// Two turns in a row that fail on an API error that may pass boost the
// session, once; the first turn that ends without one ends the boost. A
// usage limit, which only the user can mend, breaks the run.
func TestTwoAPIErrorsInARowBoostASessionUntilATurnEndsWell(t *testing.T) {
	c := historyCore(t)
	x := &session{id: "s0000000000b1", name: "alpha", harness: "generic"}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessions[x.id] = x
	turn := func(kind, err string) bool {
		t.Helper()
		e := event.Event{Source: event.SourceHook, Agent: event.Main, Kind: kind, Data: map[string]any{}}
		if err != "" {
			e.Data["error"] = err
		}
		c.record(x, e)
		return c.boostTurn(x, e)
	}
	prompt := func() { turn(event.PromptSubmitted, "") }

	prompt()
	if turn(event.TurnFailed, "server_error") || x.track.boost != nil {
		t.Fatal("one API error boosted it")
	}
	prompt()
	if turn(event.TurnEnded, "") || x.track.apiFails != 0 {
		t.Fatalf("a turn that ended left %d failures counted", x.track.apiFails)
	}
	prompt()
	turn(event.TurnFailed, "overloaded")
	prompt()
	turn(event.TurnFailed, "rate_limit")
	prompt()
	if turn(event.TurnFailed, "server_error") || x.track.boost != nil {
		t.Fatal("a usage limit between two API errors left the run unbroken")
	}
	prompt()
	if !turn(event.TurnFailed, "server_error") || x.track.boost == nil {
		t.Fatal("two API errors in a row did not boost it")
	}
	// Still boosted, the next failure starts no second boost.
	prompt()
	if turn(event.TurnFailed, "unknown") {
		t.Error("a third failure boosted it again")
	}
	if got := x.wire().Priority; got != 0 {
		t.Errorf("a boost after API errors set the priority to %d: it shows no badge", got)
	}
	prompt()
	if !turn(event.TurnEnded, "") || x.track.boost != nil {
		t.Fatal("a turn that ended well left the boost on")
	}
	if got, want := c.boosts(t, x.id), []string{"boost.on server_error", "boost.off turn"}; !slices.Equal(got, want) {
		t.Errorf("journal %v, want %v", got, want)
	}
	// A replay of the journal, as a restarted core makes, ends where the
	// core did.
	var replay track
	for _, e := range c.journalOf(t, x.id) {
		replay.apply(e)
	}
	if replay.boost != nil || replay.apiFails != 0 {
		t.Errorf("replayed: boost %v, %d failures", replay.boost, replay.apiFails)
	}
}

// A stop the jot dog or the trod bot sends on is a turn that came to its
// end without an API error.
func TestAStopSentOnEndsTheBoost(t *testing.T) {
	for _, e := range []event.Event{
		{Kind: event.TurnEnded},
		{Kind: event.DogNudged},
		{Kind: event.TrodSent, Data: map[string]any{"how": trodAtStop}},
	} {
		if !endsWell(e) {
			t.Errorf("%s %v does not end the boost", e.Kind, e.Data)
		}
	}
	for _, e := range []event.Event{
		{Kind: event.TurnFailed, Data: map[string]any{"error": "server_error"}},
		{Kind: event.TrodSent, Data: map[string]any{"how": trodTyped}},
		{Kind: event.PromptSubmitted},
	} {
		if endsWell(e) {
			t.Errorf("%s %v ends the boost", e.Kind, e.Data)
		}
	}
}

func TestABoostEndsWhenItsTimeIsUp(t *testing.T) {
	c := historyCore(t)
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	boosted := func(id string, ago time.Duration) *session {
		x := &session{id: id, name: id, harness: "generic"}
		c.sessions[id] = x
		c.record(x, event.Event{Source: event.SourceIrie, Agent: event.Main, Kind: event.BoostOn, TS: event.Stamp(now.Add(-ago)),
			Data: map[string]any{"error": "server_error"}})
		return x
	}
	old, fresh := boosted("s0000000000b2", boostFor+time.Second), boosted("s0000000000b3", boostFor-time.Minute)
	if !c.endBoosts(now) {
		t.Fatal("no boost ended")
	}
	if old.track.boost != nil || fresh.track.boost == nil {
		t.Errorf("after the sweep: old %v, fresh %v", old.track.boost, fresh.track.boost)
	}
	if got := c.boosts(t, old.id); !slices.Equal(got, []string{"boost.on server_error", "boost.off time"}) {
		t.Errorf("journal %v", got)
	}
	if c.endBoosts(now) {
		t.Error("a second sweep ended a boost again")
	}
}

// The card says the boost in plain words, before what the session does.
func TestABoostedCardSaysSo(t *testing.T) {
	var tr track
	for _, e := range []event.Event{
		{Source: event.SourceHook, Agent: event.Main, Kind: event.PromptSubmitted},
		{Source: event.SourceIrie, Agent: event.Main, Kind: event.BoostOn},
		{Source: event.SourceHook, Agent: event.Main, Kind: event.ToolStarted, Data: map[string]any{"tool": "Bash", "phrase": "Running the tests"}},
	} {
		if e.Data == nil {
			e.Data = map[string]any{}
		}
		tr.apply(e)
	}
	var s ipc.Session
	tr.describe(&s, false, 0)
	if s.Detail != "Boosted after two API errors · Running the tests" || s.Priority != 0 {
		t.Errorf("detail %q, priority %d", s.Detail, s.Priority)
	}
	tr.apply(event.Event{Source: event.SourceIrie, Agent: event.Main, Kind: event.BoostOff, Data: map[string]any{}})
	s = ipc.Session{}
	tr.describe(&s, false, 0)
	if s.Detail != "Running the tests" {
		t.Errorf("after the boost, detail %q", s.Detail)
	}
}

// A fake agent whose turns fail: the user carries the first failure on
// with a message, which send now takes; the turn fails again, and Irie
// boosts the session and types its agent one message to carry on. The
// turn that follows ends well, and the boost with it.
func TestTwoFailedTurnsBoostASessionAndCarryItOn(t *testing.T) {
	c := setupWith(t, agentConfig{env: map[string]string{"IRIE_FAKE_TURNS": "stopfail:server_error,sleep:3s+stop"}})
	// box: its idle prompt shows, which the carry-on types into.
	s := c.spawn("prompt,box,stopfail:server_error")

	if r := c.waitState(s.ID, ipc.StateNeedsYou); r.Detail != "turn failed: server_error" {
		t.Fatalf("detail %q", r.Detail)
	}
	if err := c.call(ipc.OpSend, ipc.SendArgs{Session: s.ID, Text: "continue"}, nil); err != nil {
		t.Fatalf("send to a session whose turn failed: %v", err)
	}
	testutil.Eventually(t, 15*time.Second, "the card never said it was boosted", func() bool {
		r, _ := c.row(s.ID)
		return strings.HasPrefix(r.Detail, boostedLine)
	})
	if r, _ := c.row(s.ID); r.Priority != 0 {
		t.Errorf("a boost after API errors shows a promotion's badge: priority %d", r.Priority)
	}
	testutil.Eventually(t, 20*time.Second, "the boost never ended", func() bool {
		return slices.ContainsFunc(c.events(s.ID), func(e event.Event) bool { return e.Kind == event.BoostOff })
	})
	c.waitState(s.ID, ipc.StateDone)
	if typed := c.typedTo(s.ID); !slices.Equal(typed, []string{"continue", carryOnText}) {
		t.Errorf("typed %q", typed)
	}
	var kinds []string
	for _, e := range c.events(s.ID) {
		switch e.Kind {
		case event.PromptSubmitted, event.TurnFailed, event.TurnEnded:
			kinds = append(kinds, e.Kind)
		case event.BoostOn, event.BoostOff:
			kinds = append(kinds, e.Kind+" "+e.Str("why"))
		}
	}
	want := []string{event.PromptSubmitted, event.TurnFailed, event.PromptSubmitted, event.TurnFailed, "boost.on ",
		event.PromptSubmitted, event.TurnEnded, "boost.off turn"}
	if !slices.Equal(kinds, want) {
		t.Errorf("events %q, want %q", kinds, want)
	}
	if r, _ := c.row(s.ID); strings.Contains(r.Detail, "Boosted") {
		t.Errorf("after the boost the card still says %q", r.Detail)
	}
}

// While another session is promoted, a session boosted after its API
// errors has its tools out of the background with its agent, and they
// step back again once the boost ends.
func TestABoostAfterAPIErrorsTakesItsToolsOutOfTheBackground(t *testing.T) {
	c := setupWith(t, agentConfig{env: map[string]string{"IRIE_FAKE_TURNS": "stopfail:server_error,sleep:6s+stop"}})
	alpha := c.spawnArgs(ipc.SpawnArgs{Task: "prompt,kids,box,stopfail:server_error", Name: "alpha"})
	agent := c.kids(alpha)
	beta := c.spawnArgs(ipc.SpawnArgs{Task: "prompt", Name: "beta"})
	if err := c.call(ipc.OpPriority, ipc.PriorityArgs{Session: beta.ID, Level: 1}, nil); err != nil {
		t.Fatal(err)
	}
	if !proc.CanBackground {
		t.Skip("no background state here")
	}
	if pri(t, os.Getpid()) == "4" {
		t.Skip("the test runs in the background, as a session's processes do while another is promoted")
	}
	c.waitState(alpha.ID, ipc.StateNeedsYou)
	if agentBack, _, up := c.backState(alpha, agent); agentBack || len(up) > 0 {
		t.Fatalf("before the boost: agent in the background %v, tools out %v", agentBack, up)
	}
	if err := c.call(ipc.OpSend, ipc.SendArgs{Session: alpha.ID, Text: "continue"}, nil); err != nil {
		t.Fatal(err)
	}
	testutil.Eventually(t, 15*time.Second, "the boost never took the tools out of the background", func() bool {
		agentBack, back, _ := c.backState(alpha, agent)
		return !agentBack && len(back) == 0
	})
	skipIfDemoted(t)
	testutil.Eventually(t, 20*time.Second, "the tools never stepped back after the boost", func() bool {
		agentBack, _, up := c.backState(alpha, agent)
		return !agentBack && len(up) == 0 && slices.ContainsFunc(c.events(alpha.ID), func(e event.Event) bool { return e.Kind == event.BoostOff })
	})
}

// A session boosted after its API errors ranks with the rest of the
// user's focus (gabriel.go): out of the background for a promotion and for
// Gabriel, pushing no one back itself, and back for the tab on top while
// that works the machine.
func TestABoostAfterAPIErrorsRanksWithTheRestOfTheFocus(t *testing.T) {
	c := &Core{sessions: map[string]*session{
		"s-boost": {id: "s-boost", track: track{boost: &event.Event{Kind: event.BoostOn}}},
		"s-other": {id: "s-other"},
	}}
	raised := map[string]bool{"s-promoted": true}
	if c.stepsBack("s-boost", raised) || !c.stepsBack("s-other", raised) {
		t.Error("a promotion: the boosted session should stay out, the other step back")
	}
	if c.stepsBack("s-other", nil) {
		t.Error("a boost alone put another session in the background")
	}
	c.gab = gabState{holding: true}
	if c.stepsBack("s-boost", nil) || !c.stepsBack("s-other", nil) {
		t.Error(brand.Gabriel + " holding back: the boosted session should stay out, the other step back")
	}
	c.gab = gabState{holding: true, topBusy: true, top: map[string]bool{"s-top": true}}
	if !c.stepsBack("s-boost", nil) {
		t.Error("the tab on top at work: the boosted session should step back for it")
	}
	c.gab = gabState{}
	if !c.stepsBack("s-gone", raised) {
		t.Error("a session the core does not have stays out, as if it were boosted")
	}
}

// Failures during a boost count toward no other, and a helper's never do:
// after a boost that ran out of time, one more failure starts none, two do.
func TestFailuresDuringABoostCountTowardNoOther(t *testing.T) {
	c := historyCore(t)
	x := &session{id: "s0000000000b4", name: "alpha", harness: "generic"}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessions[x.id] = x
	fail := func(agent string) bool {
		t.Helper()
		e := event.Event{Source: event.SourceHook, Agent: agent, Kind: event.TurnFailed, Data: map[string]any{"error": "server_error"}}
		c.record(x, e)
		return c.boostTurn(x, e)
	}
	fail(event.Main)
	if !fail(event.Main) {
		t.Fatal("two failures did not boost it")
	}
	fail(event.Main)
	fail(event.Main)
	if !c.endBoosts(time.Now().Add(boostFor)) || x.track.apiFails != 0 {
		t.Fatalf("after the boost's time: %d failures counted", x.track.apiFails)
	}
	if fail(event.Main) || fail("sub:a1b2c3") || x.track.boost != nil {
		t.Fatal("one failure after a boost, and a helper's, started another")
	}
	if !fail(event.Main) {
		t.Fatal("two failures after a boost did not start another")
	}
}

// A late event belongs to the session only if it came after the last the
// core heard from its agent, and after the session last started.
func TestALateEventFitsOnlyAfterWhatTheCoreHeard(t *testing.T) {
	at := time.Date(2026, 11, 14, 15, 14, 7, 0, time.UTC)
	x := &session{}
	x.track.requested, x.track.heard = at.Add(-time.Hour), at.Add(-time.Minute)
	for _, tc := range []struct {
		at   time.Time
		fits bool
	}{
		{at, true},
		{at.Add(-time.Minute), false},
		{at.Add(-2 * time.Minute), false},
	} {
		if got := lateFits(x, tc.at); got != tc.fits {
			t.Errorf("lateFits(%v) = %v, want %v", tc.at, got, tc.fits)
		}
	}
	// Reopened since: what its agent kept before is not this session's.
	x.track = track{requested: at.Add(time.Second)}
	if lateFits(x, at) {
		t.Error("an event from before a reopen fits")
	}
}
