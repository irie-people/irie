package core

import (
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/tmux"
	"github.com/irie-people/irie/internal/zen"
)

// zenCore is a core with no tmux behind it, its files under home, with
// your zen loaded as a core that starts loads it.
func zenCore(t *testing.T, home string) *Core {
	t.Helper()
	p, err := paths.New(func(k string) string { return map[string]string{"HOME": home}[k] }, 1000)
	if err != nil {
		t.Fatal(err)
	}
	c := newCore(Options{Config: config.Default(), Paths: p, Version: "test", Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, &tmux.Runner{})
	c.loadZen()
	return c
}

func (c *Core) zenDo(t *testing.T, a ipc.ZenArgs) ipc.ZenResult {
	t.Helper()
	r, err := c.zenOp(a)
	if err != nil {
		t.Fatalf("zen %+v: %v", a, err)
	}
	return r
}

func on(b bool) *bool { return &b }

// hasCode says whether err is an ipc error with that code.
func hasCode(err error, code string) bool {
	var e *ipc.Error
	return errors.As(err, &e) && e.Code == code
}

func TestYourZenKeepsEachChangeAsAVersion(t *testing.T) {
	home := t.TempDir()
	c := zenCore(t, home)
	r := c.zenDo(t, ipc.ZenArgs{})
	if r.Zen.Profile != zen.Default() || r.Zen.Head != "" {
		t.Fatalf("a first zen: %+v", r.Zen)
	}
	edge, fifty := "edge", 50
	first := c.zenDo(t, ipc.ZenArgs{Set: &ipc.ZenChange{Layout: &edge}})
	if first.Version == nil || first.Version.Why != "Edge to Edge" || first.Zen.Profile.Layout != "edge" || first.Zen.Head != first.Version.Name {
		t.Fatalf("set: %+v", first)
	}
	second := c.zenDo(t, ipc.ZenArgs{Set: &ipc.ZenChange{Breaks: &fifty}, Why: "my own words"})
	if second.Version.Parent != first.Version.Name || second.Version.Why != "my own words" {
		t.Fatalf("second: %+v", second.Version)
	}
	if same := c.zenDo(t, ipc.ZenArgs{Set: &ipc.ZenChange{Breaks: &fifty}}); same.Version != nil {
		t.Error("a change to what is there made a version")
	}
	if _, err := c.zenOp(ipc.ZenArgs{Set: &ipc.ZenChange{Columns: &fifty}}); err == nil {
		t.Error("50 columns were taken")
	}
	back := c.zenDo(t, ipc.ZenArgs{Restore: first.Version.Name[7:19], Log: 10})
	if back.Version == nil || back.Version.State != first.Version.State || back.Zen.Profile.Breaks != 0 ||
		!strings.HasPrefix(back.Version.Why, "Back to your zen of ") || len(back.Versions) != 3 {
		t.Fatalf("restore: %+v", back)
	}
	if _, err := c.zenOp(ipc.ZenArgs{Restore: "0123456789ab"}); !hasCode(err, ipc.CodeNotFound) {
		t.Errorf("restore of none: %v", err)
	}
	// A core that starts again finds your zen as it was.
	again := zenCore(t, home)
	if got := again.zenDo(t, ipc.ZenArgs{}).Zen; got.Head != back.Version.Name || got.Profile.Layout != "edge" {
		t.Errorf("after a restart: %+v", got)
	}
}

// agentSession is a session whose harness takes notes at its hooks.
func agentSession(c *Core, id string) *session {
	x := &session{id: id, observed: true}
	c.sessions[id] = x
	return x
}

func TestZenTellsEachAgentItShowsOnceAndTakesItBack(t *testing.T) {
	c := zenCore(t, t.TempDir())
	a, b := agentSession(c, "s1"), agentSession(c, "s2")
	c.zenDo(t, ipc.ZenArgs{On: on(true), Session: "s1"})
	if a.zenNote != zen.GentleNote() {
		t.Fatalf("s1 was not told: %q", a.zenNote)
	}
	c.mu.Lock()
	got := c.takeNote(a, []event.Event{{Kind: event.ToolStarted}})
	c.mu.Unlock()
	if got != zen.GentleNote() || a.zenNote != "" {
		t.Fatalf("delivered %q", got)
	}
	// On to another session, and back: each told once.
	c.zenDo(t, ipc.ZenArgs{On: on(true), Session: "s2"})
	c.zenDo(t, ipc.ZenArgs{On: on(true), Session: "s1"})
	if a.zenNote != "" || b.zenNote != zen.GentleNote() {
		t.Fatalf("s1 %q, s2 %q", a.zenNote, b.zenNote)
	}
	// After a compaction, the session on show hears it again.
	c.mu.Lock()
	if c.zenStartNote(a) != zen.GentleNote() || c.zenStartNote(b) != "" {
		t.Error("the start note")
	}
	c.mu.Unlock()
	c.zenDo(t, ipc.ZenArgs{On: on(false)})
	if a.zenNote != zen.LeftNote() {
		t.Errorf("s1, told, was not told zen ended: %q", a.zenNote)
	}
	if b.zenNote != "" {
		t.Errorf("s2's note had not gone out, so it is taken back: %q", b.zenNote)
	}
}

func TestZenTellsNoAgentWhenYourZenSaysNot(t *testing.T) {
	c := zenCore(t, t.TempDir())
	x := agentSession(c, "s1")
	c.zenDo(t, ipc.ZenArgs{Set: &ipc.ZenChange{Gentle: on(false)}})
	c.zenDo(t, ipc.ZenArgs{On: on(true), Session: "s1"})
	if x.zenNote != "" {
		t.Errorf("told: %q", x.zenNote)
	}
}

// A core that restarts while zen is on remembers whom it told, so an
// install never tells an agent twice, and zen ending after it still says
// so.
func TestARestartedCoreTellsNoAgentTwice(t *testing.T) {
	home := t.TempDir()
	c := zenCore(t, home)
	agentSession(c, "s1")
	c.zenDo(t, ipc.ZenArgs{On: on(true), Session: "s1"})
	c.sessions["s1"].zenNote = "" // delivered
	again := zenCore(t, home)
	x := agentSession(again, "s1")
	again.zenDo(t, ipc.ZenArgs{On: on(true), Session: "s1"})
	if x.zenNote != "" {
		t.Errorf("told twice: %q", x.zenNote)
	}
	again.zenDo(t, ipc.ZenArgs{On: on(false)})
	if x.zenNote != zen.LeftNote() {
		t.Errorf("not told it ended: %q", x.zenNote)
	}
}

// moment brings zen up to now, as its sweep does, with the hands still for
// idle.
func (c *Core) moment(now time.Time, idle time.Duration) *ipc.ZenOffer {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.zenMoment(now, idle)
	return c.zenHeader().Offer
}

func TestZenOffersOneStepAtACalmMomentAndTriesAChangeLive(t *testing.T) {
	c := zenCore(t, t.TempDir())
	c.appViews = 1
	start := time.Now()
	c.zenDo(t, ipc.ZenArgs{On: on(true), WindowText: 15})
	if o := c.moment(start.Add(time.Minute), time.Minute); o != nil {
		t.Fatalf("an offer as zen began: %+v", o)
	}
	at := start.Add(5 * time.Minute)
	if zen.Night(at) {
		t.Skip("the path keeps quiet at night")
	}
	o := c.moment(at, time.Minute)
	if o == nil || o.ID != "welcome" || o.Kind != ipc.ZenKindLearn {
		t.Fatalf("first: %+v", o)
	}
	c.zenDo(t, ipc.ZenArgs{Offer: "welcome", Answer: ipc.ZenYes})
	if o := c.moment(at.Add(time.Minute), time.Minute); o != nil {
		t.Fatalf("a second step a minute after the first: %+v", o)
	}
	if _, err := c.zenOp(ipc.ZenArgs{Offer: "welcome", Answer: ipc.ZenYes}); !hasCode(err, ipc.CodeStale) {
		t.Errorf("an answer to nothing: %v", err)
	}
	// A change: Try It shows it at once, then Keep or Go Back.
	c.mu.Lock()
	s, _ := zen.StepByID("text")
	offer := s.Offer()
	c.zen.offer = &offer
	c.mu.Unlock()
	r := c.zenDo(t, ipc.ZenArgs{Offer: "text", Answer: ipc.ZenYes})
	if r.Zen.Profile.TextSize != 16 || r.Zen.Trying != "text" || r.Zen.Offer == nil || r.Zen.Offer.Kind != ipc.ZenKindKeep || r.Zen.Head != "" {
		t.Fatalf("on trial: %+v", r.Zen)
	}
	r = c.zenDo(t, ipc.ZenArgs{Offer: "text", Answer: ipc.ZenYes})
	if r.Version == nil || r.Version.Why != "Text a point larger" || r.Zen.Profile.TextSize != 16 || r.Zen.Trying != "" {
		t.Fatalf("kept: %+v", r)
	}
	// Tried and gone back: your zen as it was, and the step done.
	c.mu.Lock()
	s, _ = zen.StepByID("breaks")
	offer = s.Offer()
	c.zen.offer = &offer
	c.mu.Unlock()
	c.zenDo(t, ipc.ZenArgs{Offer: "breaks", Answer: ipc.ZenYes})
	r = c.zenDo(t, ipc.ZenArgs{Offer: "breaks", Answer: ipc.ZenNo})
	if r.Version != nil || r.Zen.Profile.Breaks != 0 || c.zen.saved.Path["breaks"].How != zen.Done {
		t.Fatalf("gone back: %+v", r)
	}
}

func TestZenLeavingDropsATrialAndAnOffer(t *testing.T) {
	c := zenCore(t, t.TempDir())
	c.zenDo(t, ipc.ZenArgs{On: on(true)})
	c.mu.Lock()
	s, _ := zen.StepByID("breaks")
	offer := s.Offer()
	c.zen.offer = &offer
	c.mu.Unlock()
	c.zenDo(t, ipc.ZenArgs{Offer: "breaks", Answer: ipc.ZenYes})
	r := c.zenDo(t, ipc.ZenArgs{On: on(false)})
	if r.Zen.On || r.Zen.Offer != nil || r.Zen.Trying != "" || r.Zen.Profile.Breaks != 0 {
		t.Errorf("after zen: %+v", r.Zen)
	}
}

func TestARestOfferedGoesWhenThePersonIsAway(t *testing.T) {
	c := zenCore(t, t.TempDir())
	c.appViews = 1
	now := time.Now()
	c.zenDo(t, ipc.ZenArgs{On: on(true)})
	c.mu.Lock()
	o := zen.RestOffer()
	c.zen.offer = &o
	c.mu.Unlock()
	if got := c.moment(now.Add(time.Minute), zen.Away); got != nil {
		t.Errorf("a rest stays while they are away: %+v", got)
	}
	if !c.zen.desk.IsZero() {
		t.Error("away, yet at the computer")
	}
	c.moment(now.Add(2*time.Minute), time.Second)
	if c.zen.desk.IsZero() {
		t.Error("back, yet not at the computer")
	}
	// In 10 Minutes holds the next rest back.
	c.zen.offer = &o
	c.zenDo(t, ipc.ZenArgs{Offer: ipc.ZenRest, Answer: ipc.ZenNo})
	if c.zen.snooze.Before(now.Add(zen.Snooze)) {
		t.Errorf("snooze %v", c.zen.snooze)
	}
}

// With rests on, zen offers one once the person has been at the computer
// that long, and Back to It starts the count again.
func TestZenOffersARestAndBackToItStartsTheCountAgain(t *testing.T) {
	c := zenCore(t, t.TempDir())
	c.appViews = 1
	fifty := 50
	c.zenDo(t, ipc.ZenArgs{Set: &ipc.ZenChange{Breaks: &fifty}})
	now := time.Now()
	c.zenDo(t, ipc.ZenArgs{On: on(true)})
	c.mu.Lock()
	c.zen.desk = now.Add(-51 * time.Minute)
	c.zen.answered = now // no step crowds in
	c.mu.Unlock()
	o := c.moment(now, zen.Typing)
	if o == nil || o.ID != ipc.ZenRest || o.Kind != ipc.ZenKindRest {
		t.Fatalf("no rest after 51 minutes: %+v", o)
	}
	c.zenDo(t, ipc.ZenArgs{Offer: ipc.ZenRest, Answer: ipc.ZenYes})
	if o := c.moment(now.Add(time.Minute), zen.Typing); o != nil && o.ID == ipc.ZenRest {
		t.Errorf("a rest again a minute after Back to It: %+v", o)
	}
}

func TestZenEndsOnceTheAppIsGoneAWhile(t *testing.T) {
	c := zenCore(t, t.TempDir())
	x := agentSession(c, "s1")
	c.zenDo(t, ipc.ZenArgs{On: on(true), Session: "s1"})
	x.zenNote = "" // delivered
	now := time.Now()
	c.moment(now, 0)
	if !c.zen.on {
		t.Fatal("zen ended at once as the app went")
	}
	c.moment(now.Add(zenGone), 0)
	if c.zen.on || x.zenNote != zen.LeftNote() {
		t.Errorf("zen on %v, note %q", c.zen.on, x.zenNote)
	}
}

func TestZenIsInTheHeader(t *testing.T) {
	c := zenCore(t, t.TempDir())
	c.mu.Lock()
	h := c.headerNow()
	c.mu.Unlock()
	if h.Zen == nil || h.Zen.Profile != zen.Default() {
		t.Fatalf("header: %+v", h.Zen)
	}
	var e *ipc.Error
	if _, err := c.zenOp(ipc.ZenArgs{Offer: "x", Answer: "maybe"}); !errors.As(err, &e) {
		t.Errorf("%v", err)
	}
}
