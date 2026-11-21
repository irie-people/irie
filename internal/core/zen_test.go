package core

import (
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/chain"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/keyring"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/tmux"
	"github.com/irie-people/irie/internal/zen"
)

// zenCore is a core with no tmux behind it, its files under home, with
// your zen loaded and its vault opened as a core that starts does, with
// its key in a file under home.
func zenCore(t *testing.T, home string) *Core { return zenCoreKeys(t, home, nil) }

// zenCoreKeys is zenCore with the vault's key from keys, unless nil.
func zenCoreKeys(t *testing.T, home string, keys keyring.Source) *Core {
	t.Helper()
	p, err := paths.New(func(k string) string { return map[string]string{"HOME": home}[k] }, 1000)
	if err != nil {
		t.Fatal(err)
	}
	c := newCore(Options{Config: config.Default(), Paths: p, Version: "test", Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, &tmux.Runner{})
	c.zen.keys = keys
	c.startZen()
	return c
}

// noKeys is a keychain that gives no key, as a locked one.
type noKeys struct{}

func (noKeys) Key(string) ([]byte, error) { return nil, errors.New("the keychain is locked") }

// zenFiles is each file's name in your zen's objects, sorted.
func zenFiles(t *testing.T, c *Core) []string {
	t.Helper()
	files, err := os.ReadDir(filepath.Join(c.paths.Zen(), "objects"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, f := range files {
		out = append(out, f.Name())
	}
	return out
}

// Your zen's versions are sealed on disk, the state file keeps your zen as
// it is, and the export gives each version's exact bytes, oldest first.
func TestYourZenIsKeptSealed(t *testing.T) {
	home := t.TempDir()
	c := zenCore(t, home)
	edge := "edge"
	v := c.zenDo(t, ipc.ZenArgs{Set: &ipc.ZenChange{Layout: &edge}, Why: "after a long night"}).Version
	for _, f := range zenFiles(t, c) {
		if !strings.HasSuffix(f, ".sealed") {
			t.Errorf("objects holds %s", f)
		}
	}
	filepath.WalkDir(c.paths.Zen(), func(path string, d fs.DirEntry, err error) error {
		if b, _ := os.ReadFile(path); err == nil && !d.IsDir() && strings.Contains(string(b), "long night") {
			t.Errorf("%s holds the why in plain", path)
		}
		return nil
	})
	if b, _ := os.ReadFile(c.paths.ZenState()); !strings.Contains(string(b), `"layout": "edge"`) || !strings.Contains(string(b), v.Name) {
		t.Errorf("the state file:\n%s", b)
	}
	r := c.zenDo(t, ipc.ZenArgs{Export: true})
	if len(r.Docs) != 1 || chain.Name([]byte(r.Docs[0])) != v.Name || !strings.Contains(r.Docs[0], `"why":"after a long night"`) {
		t.Fatalf("export: %q", r.Docs)
	}
	again := zenCore(t, home)
	if got := again.zenDo(t, ipc.ZenArgs{Log: 5}); got.Zen.Head != v.Name || len(got.Versions) != 1 || got.Versions[0].Why != "after a long night" {
		t.Errorf("after a restart: %+v", got)
	}
}

// A zen kept plain, by a core from before the vault, moves into it whole.
func TestAZenKeptPlainMovesIntoTheVault(t *testing.T) {
	home := t.TempDir()
	c := zenCore(t, home)
	dir := c.paths.Zen()
	os.RemoveAll(dir)
	os.Remove(c.paths.ZenState())
	t0 := time.Date(2026, 11, 21, 18, 4, 5, 0, time.UTC)
	old := zen.Open(dir)
	first, _, _ := old.Commit(zen.Default(), "Your zen as it was", t0)
	watch := "watch"
	head, _, err := old.Commit(zen.Apply(zen.Default(), ipc.ZenChange{Layout: &watch}), "Keep Watch", t0.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	moved := zenCore(t, home)
	if got := moved.zenDo(t, ipc.ZenArgs{}).Zen; got.Head != head.Name || got.Profile.Layout != "watch" {
		t.Errorf("zen after the move: %+v", got)
	}
	files := zenFiles(t, moved)
	if len(files) != 2 || !strings.HasSuffix(files[0], ".sealed") || !strings.HasSuffix(files[1], ".sealed") {
		t.Errorf("objects after the move: %v", files)
	}
	if docs := moved.zenDo(t, ipc.ZenArgs{Export: true}).Docs; len(docs) != 2 || chain.Name([]byte(docs[0])) != first.Name {
		t.Errorf("export after the move: %q", docs)
	}
	if b, _ := os.ReadFile(moved.paths.ZenState()); !strings.Contains(string(b), head.Name) {
		t.Errorf("the state file after the move:\n%s", b)
	}
}

// While the keychain gives no key, zen is as you left it, and a change
// keeps nothing rather than a version in plain, until the vault opens.
func TestALockedVaultKeepsYourZenAndNoChange(t *testing.T) {
	home := t.TempDir()
	watch := "watch"
	v := zenCore(t, home).zenDo(t, ipc.ZenArgs{Set: &ipc.ZenChange{Layout: &watch}}).Version
	c := zenCoreKeys(t, home, noKeys{})
	if got := c.zenDo(t, ipc.ZenArgs{}).Zen; got.Profile.Layout != "watch" || got.Head != v.Name {
		t.Errorf("zen with the vault locked: %+v", got)
	}
	edge := "edge"
	if _, err := c.zenOp(ipc.ZenArgs{Set: &ipc.ZenChange{Layout: &edge}}); !hasCode(err, ipc.CodeBusy) || !strings.Contains(err.Error(), "locked") {
		t.Errorf("a change with the vault locked: %v", err)
	}
	if _, err := c.zenOp(ipc.ZenArgs{Log: 5}); !hasCode(err, ipc.CodeBusy) {
		t.Errorf("the history with the vault locked: %v", err)
	}
	if files := zenFiles(t, c); len(files) != 1 || !strings.HasSuffix(files[0], ".sealed") {
		t.Errorf("objects: %v", files)
	}
	// Once the keychain gives the key, the vault opens and changes keep.
	c.mu.Lock()
	c.zen.keys = keyring.Default(c.paths.Keys())
	c.mu.Unlock()
	c.unlockZen()
	if r := c.zenDo(t, ipc.ZenArgs{Set: &ipc.ZenChange{Layout: &edge}}); r.Version == nil || r.Version.Parent != v.Name {
		t.Errorf("a change once the vault opened: %+v", r)
	}
}

// A vault its key doesn't open, as when the keychain lost the key, is set
// aside whole, and your zen as it was starts a new one.
func TestAVaultItsKeyDoesntOpenIsSetAside(t *testing.T) {
	home := t.TempDir()
	c := zenCore(t, home)
	watch := "watch"
	c.zenDo(t, ipc.ZenArgs{Set: &ipc.ZenChange{Layout: &watch}})
	other := strings.Repeat("ab", keyring.Size) + "\n"
	if err := os.WriteFile(filepath.Join(c.paths.Keys(), zenKeyName+".key"), []byte(other), 0o600); err != nil {
		t.Fatal(err)
	}
	again := zenCore(t, home)
	r := again.zenDo(t, ipc.ZenArgs{Log: 5})
	if r.Zen.Profile.Layout != "watch" || len(r.Versions) != 1 || r.Versions[0].Why != "Your zen as it was" || r.Versions[0].Parent != "" {
		t.Fatalf("zen in the new vault: %+v", r)
	}
	aside, _ := filepath.Glob(c.paths.Zen() + "-unreadable-*")
	if len(aside) != 1 {
		t.Fatalf("set aside: %v", aside)
	}
	if objects, _ := os.ReadDir(filepath.Join(aside[0], "objects")); len(objects) != 1 {
		t.Errorf("the vault set aside holds %v", objects)
	}
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

// Each client holds a zen of its own: the app's "off", which it says at
// each new selection outside zen, never ends the sidebar's, and zen ends
// once the last client leaves it.
func TestEachClientHoldsItsOwnZen(t *testing.T) {
	c := zenCore(t, t.TempDir())
	a, b := agentSession(c, "s1"), agentSession(c, "s2")
	r := c.zenDo(t, ipc.ZenArgs{On: on(true), Client: ipc.ZenSidebar, Session: "s1"})
	if !r.Zen.On || !slices.Equal(r.Zen.Clients, []string{ipc.ZenSidebar}) || a.zenNote != zen.GentleNote() {
		t.Fatalf("the sidebar's zen: %+v, note %q", r.Zen, a.zenNote)
	}
	if r := c.zenDo(t, ipc.ZenArgs{On: on(false), Session: "s2"}); !r.Zen.On || c.zen.session != "s1" {
		t.Fatalf("the app's off ended the sidebar's zen: %+v", r.Zen)
	}
	r = c.zenDo(t, ipc.ZenArgs{On: on(true), Client: ipc.ZenApp, Session: "s2"})
	if !slices.Equal(r.Zen.Clients, []string{ipc.ZenApp, ipc.ZenSidebar}) || c.zen.session != "s2" || b.zenNote != zen.GentleNote() {
		t.Fatalf("both in zen: %+v, session %s", r.Zen, c.zen.session)
	}
	c.zenDo(t, ipc.ZenArgs{On: on(false), Client: ipc.ZenSidebar})
	if !c.zen.on || c.zen.session != "s2" {
		t.Fatalf("the app's zen after the sidebar left: on %v, session %s", c.zen.on, c.zen.session)
	}
	a.zenNote, b.zenNote = "", ""
	if r := c.zenDo(t, ipc.ZenArgs{On: on(false)}); r.Zen.On || r.Zen.Clients != nil || a.zenNote != zen.LeftNote() || b.zenNote != zen.LeftNote() {
		t.Errorf("zen after the last left: %+v, notes %q %q", r.Zen, a.zenNote, b.zenNote)
	}
	if _, err := c.zenOp(ipc.ZenArgs{On: on(true), Client: "watch"}); !hasCode(err, ipc.CodeBadArgs) {
		t.Errorf("an unknown client: %v", err)
	}
}

// A client other than the app renews its zen; one that stops saying so
// lapses, and a core that restarts keeps it until then.
func TestAClientsZenLapsesUnlessRenewed(t *testing.T) {
	home := t.TempDir()
	c := zenCore(t, home)
	agentSession(c, "s1")
	c.zenDo(t, ipc.ZenArgs{On: on(true), Client: ipc.ZenSidebar, Session: "s1"})
	again := zenCore(t, home)
	if r := again.zenDo(t, ipc.ZenArgs{}); !r.Zen.On || !slices.Equal(r.Zen.Clients, []string{ipc.ZenSidebar}) || again.zen.session != "s1" {
		t.Fatalf("after a restart: %+v", r.Zen)
	}
	said := c.zen.holds[ipc.ZenSidebar].Said
	c.moment(said.Add(ipc.ZenRenew), 0)
	if !c.zen.on {
		t.Fatal("zen lapsed within a renewal")
	}
	c.mu.Lock()
	c.zenShow(true, ipc.ZenSidebar, "s1", 0, said.Add(90*time.Second))
	c.mu.Unlock()
	c.moment(said.Add(3*time.Minute), 0)
	if !c.zen.on {
		t.Fatal("a renewed zen lapsed")
	}
	c.moment(said.Add(90*time.Second+ipc.ZenLapse), 0)
	if c.zen.on {
		t.Error("zen stayed on once the sidebar stopped saying so")
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
