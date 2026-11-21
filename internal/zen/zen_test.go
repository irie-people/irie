package zen

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/chain"
	"github.com/irie-people/irie/internal/ipc"
)

func TestTheFirstZenIsCalmAndValid(t *testing.T) {
	p := Default()
	if err := Check(p); err != nil {
		t.Fatal(err)
	}
	if p.Breaks != 0 || !p.Gentle || !p.Night {
		t.Errorf("rests start off and the rest on: %+v", p)
	}
	for _, bad := range []ipc.ZenProfile{
		Apply(p, ipc.ZenChange{Layout: ptr("wide")}),
		Apply(p, ipc.ZenChange{Columns: ptr(90)}),
		Apply(p, ipc.ZenChange{TextSize: ptr(8)}),
		Apply(p, ipc.ZenChange{Breaks: ptr(5)}),
		Apply(p, ipc.ZenChange{Breaks: ptr(181)}),
	} {
		if Check(bad) == nil {
			t.Errorf("took %+v", bad)
		}
	}
}

func ptr[T any](v T) *T { return &v }

func TestWhySaysWhatChanged(t *testing.T) {
	a := Default()
	b := Apply(a, ipc.ZenChange{Layout: ptr("edge"), TextSize: ptr(16), Breaks: ptr(50)})
	if got := Why(a, b); got != "Edge to Edge, text at 16 pt, a rest every 50 minutes" {
		t.Errorf("got %q", got)
	}
	if got := Why(b, a); got != "Centred, the window's text size, no rests" {
		t.Errorf("got %q", got)
	}
	if got := CleanWhy("  two\nlines\t\x07here "); got != "two lines here" {
		t.Errorf("got %q", got)
	}
	if got := CleanWhy(strings.Repeat("ab ", 200)); len([]rune(got)) != ipc.MaxZenWhy || !strings.HasSuffix(got, "…") {
		t.Errorf("long: %d", len([]rune(got)))
	}
}

func TestTheBookKeepsAChainOfYourZens(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "zen")
	k := Open(dir)
	if _, ok, err := k.Head(); ok || err != nil {
		t.Fatalf("an empty book has a head: %v", err)
	}
	t0 := time.Date(2026, 11, 21, 18, 4, 5, 0, time.UTC)
	first, made, err := k.Commit(Default(), "Your zen as it was", t0)
	if err != nil || !made || first.Parent != "" {
		t.Fatalf("first: %+v %v %v", first, made, err)
	}
	if again, made, _ := k.Commit(Default(), "the same", t0.Add(time.Minute)); made || again.Name != first.Name {
		t.Error("the same zen made a version")
	}
	edge := Apply(Default(), ipc.ZenChange{Layout: ptr("edge")})
	second, _, err := k.Commit(edge, "Edge to Edge", t0.Add(time.Hour))
	if err != nil || second.Parent != first.Name {
		t.Fatalf("second: %+v %v", second, err)
	}
	back, _, err := k.Commit(Default(), "Back to "+chain.Short(first.Name), t0.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if back.State != first.State || back.Name == first.Name || back.State == second.State {
		t.Errorf("states: back %s, first %s, second %s", back.State, first.State, second.State)
	}
	log, err := k.Log(0)
	if err != nil || len(log) != 3 || log[0].Name != back.Name || log[2].Name != first.Name || !log[1].Created.Equal(t0.Add(time.Hour)) {
		t.Fatalf("log: %+v %v", log, err)
	}
	if v, err := k.Find(chain.Short(second.Name)); err != nil || v.Zen.Layout != "edge" {
		t.Errorf("find: %+v %v", v, err)
	}
	if _, err := k.Find("0123456789ab"); !IsNone(err) {
		t.Errorf("find none: %v", err)
	}
	if _, _, err := k.Commit(Apply(edge, ipc.ZenChange{Columns: ptr(7)}), "bad", t0); err == nil {
		t.Error("a bad zen was kept")
	}
}

// In its vault your zen's book keeps the names it had, and its export is
// each version's exact bytes, oldest first, each the SHA-512 of its name.
func TestTheBookInItsVaultExportsEachVersionsBytes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "zen")
	t0 := time.Date(2026, 11, 21, 18, 4, 5, 0, time.UTC)
	first, _, err := Open(dir).Commit(Default(), "Your zen as it was", t0)
	if err != nil {
		t.Fatal(err)
	}
	if docs, err := (Open(t.TempDir())).Export(); docs != nil || err != nil {
		t.Errorf("an empty book's export: %q, %v", docs, err)
	}
	kek := bytes.Repeat([]byte{5}, 32)
	k, err := OpenVault(dir, kek)
	if err != nil || !k.Sealed() || Open(dir).Sealed() {
		t.Fatalf("open: %v", err)
	}
	if n, err := k.Seal(); n != 1 || err != nil {
		t.Fatalf("seal = %d, %v", n, err)
	}
	second, _, err := k.Commit(Apply(Default(), ipc.ZenChange{Layout: ptr("edge")}), "Edge to Edge", t0.Add(time.Hour))
	if err != nil || second.Parent != first.Name {
		t.Fatalf("second: %+v, %v", second, err)
	}
	docs, err := k.Export()
	if err != nil || len(docs) != 2 || chain.Name([]byte(docs[0])) != first.Name || chain.Name([]byte(docs[1])) != second.Name {
		t.Fatalf("export = %q, %v", docs, err)
	}
	if Format == "irie-zen/1" && chain.Name([]byte(docs[0])) != pinnedName {
		t.Errorf("the first version's name moved in the vault: %s", chain.Name([]byte(docs[0])))
	}
	if _, _, err := Open(dir).Head(); !IsLocked(err) {
		t.Errorf("a shut vault's head: %v", err)
	}
	if _, _, err := Open(dir).Commit(Default(), "plain", t0.Add(2*time.Hour)); !IsLocked(err) {
		t.Errorf("a commit with the vault shut: %v", err)
	}
	if _, err := OpenVault(dir, bytes.Repeat([]byte{6}, 32)); !IsWrongKey(err) {
		t.Errorf("another key: %v", err)
	}
}

// A version's name is the SHA-512 of its canonical bytes, so the form
// never changes by accident: a change here would make every kept zen
// unreadable to the tools that check them.
func TestAVersionsBytesAndNameStayPut(t *testing.T) {
	doc := Version{Created: "2026-11-21T18:04:05Z", Format: Format, Why: "Your zen as it was", Zen: Default()}
	b, err := chain.Canonical(doc)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"created":"2026-11-21T18:04:05Z","format":"` + Format + `","why":"Your zen as it was",` +
		`"zen":{"breaks":0,"columns":100,"full_screen":true,"gentle":true,"layout":"centred","night":true,"text_size":0}}`
	if string(b) != want {
		t.Errorf("bytes:\n%s\nwant\n%s", b, want)
	}
	// The name is pinned under the product's own name; a copy under
	// another (internal/brand) writes another format, so another name.
	if got := chain.Name(b); Format == "irie-zen/1" && got != pinnedName {
		t.Errorf("name: %s", got)
	}
}

const pinnedName = "sha512:63f3edf57d88d2c951225960ffe3da17dd5000bcbfce720360fba770f315574f14ff8b16822172c922ed8fcf76e3af59951322352b72ca8092a3b0992585ca26"

func TestAVersionOfAnotherFormIsRefused(t *testing.T) {
	dir := t.TempDir()
	k := Open(dir)
	b, _ := chain.Canonical(Version{Created: "2026-11-21T18:04:05Z", Format: "irie-zen/9", Why: "x", Zen: Default()})
	s := chain.Store{Dir: dir}
	name, err := s.Put(b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k.Get(name); err == nil || !strings.Contains(err.Error(), "irie-zen/9") {
		t.Errorf("got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "objects")); err != nil {
		t.Fatal(err)
	}
}

func TestThePathReadsAndGoesInOrder(t *testing.T) {
	steps := Path()
	if len(steps) < 5 || steps[0].ID != "welcome" {
		t.Fatalf("path: %d steps", len(steps))
	}
	p := Default()
	g := Progress{}
	at := time.Date(2026, 11, 21, 15, 0, 0, 0, time.Local)
	var order []string
	for range len(steps) + 2 {
		s, ok := Next(p, 13, g)
		if !ok {
			break
		}
		order = append(order, s.ID)
		g[s.ID] = Answered{How: Done, At: at}
	}
	if len(order) != len(steps) || order[2] != "breaks" {
		t.Errorf("order: %v", order)
	}
	if g.Today(at) != len(steps) || g.Today(at.Add(24*time.Hour)) != 0 {
		t.Errorf("today: %d", g.Today(at))
	}
}

func TestAStepPutOffComesBackOnceAfterTheFreshOnes(t *testing.T) {
	g := Progress{"welcome": {How: Later, At: time.Now(), Times: 1}}
	if s, _ := Next(Default(), 13, g); s.ID != "eyes" {
		t.Errorf("first: %s", s.ID)
	}
	for _, s := range Path() {
		if s.ID != "welcome" {
			g[s.ID] = Answered{How: Done}
		}
	}
	if s, ok := Next(Default(), 13, g); !ok || s.ID != "welcome" {
		t.Errorf("again: %s %v", s.ID, ok)
	}
	g["welcome"] = Answered{How: Later, Times: 2}
	if s, ok := Next(Default(), 13, g); ok {
		t.Errorf("put off twice, offered again: %s", s.ID)
	}
}

// A change that would change nothing is passed over: rests already on, or
// text as large as zen goes.
func TestAChangeThatChangesNothingIsPassedOver(t *testing.T) {
	g := Progress{"welcome": {How: Done}, "eyes": {How: Done}}
	p := Apply(Default(), ipc.ZenChange{Breaks: ptr(50)})
	if s, _ := Next(p, 13, g); s.ID == "breaks" {
		t.Error("offered rests already on")
	}
	text, _ := StepByID("text")
	if got := text.Apply(Default(), 15).TextSize; got != 16 {
		t.Errorf("from the window's 15: %d", got)
	}
	if got := text.Apply(Apply(Default(), ipc.ZenChange{TextSize: ptr(MaxText)}), 13).TextSize; got != MaxText {
		t.Errorf("past the largest: %d", got)
	}
	if got := text.Apply(Default(), 0).TextSize; got != 14 {
		t.Errorf("no window size said: %d", got)
	}
}

func TestOffersAskOneThingInPlainWords(t *testing.T) {
	for _, s := range Path() {
		o := s.Offer()
		if o.ID != s.ID || o.Text == "" || o.Yes == "" {
			t.Errorf("%s: %+v", s.ID, o)
		}
		if s.Change() && (o.Kind != ipc.ZenKindTry || s.KeepOffer().Kind != ipc.ZenKindKeep || s.KeepOffer().Text == "") {
			t.Errorf("%s: a change is tried, then kept", s.ID)
		}
	}
	if _, err := parsePath("[[step]]\nid = \"x\"\ntext = \"y\"\nbreaks = 50\n"); err == nil {
		t.Error("a change with no keep or why was taken")
	}
	if _, err := parsePath("[[step]]\nid = \"rest\"\ntext = \"y\"\n"); err == nil {
		t.Error("a step named rest was taken")
	}
}

func TestStepsComeOnlyAtACalmMoment(t *testing.T) {
	noon := time.Date(2026, 11, 21, 12, 0, 0, 0, time.Local)
	calm := Moment{Now: noon, On: 10 * time.Minute, Answered: noon.Add(-time.Hour), Working: true}
	if !MayStep(calm) {
		t.Fatal("a calm moment had no step")
	}
	for name, m := range map[string]Moment{
		"zen just began":   with(calm, func(m *Moment) { m.On = time.Minute }),
		"just answered":    with(calm, func(m *Moment) { m.Answered = noon.Add(-5 * time.Minute) }),
		"three today":      with(calm, func(m *Moment) { m.Today = PerDay }),
		"it needs you":     with(calm, func(m *Moment) { m.NeedsYou = true }),
		"typing, no agent": with(calm, func(m *Moment) { m.Working = false; m.Idle = 2 * time.Second }),
		"late at night":    with(calm, func(m *Moment) { m.Now = noon.Add(11*time.Hour + 30*time.Minute) }),
	} {
		if MayStep(m) {
			t.Errorf("%s: a step came", name)
		}
	}
	if !MayStep(with(calm, func(m *Moment) { m.Working = false; m.Idle = Typing })) {
		t.Error("hands at rest had no step")
	}
}

func with(m Moment, f func(*Moment)) Moment {
	f(&m)
	return m
}

func TestARestComesAtAPauseOnceTheMinutesChosenHavePassed(t *testing.T) {
	noon := time.Date(2026, 11, 21, 12, 0, 0, 0, time.Local)
	fifty := Apply(Default(), ipc.ZenChange{Breaks: ptr(50)})
	paused := Moment{Now: noon, AtDesk: 50 * time.Minute, Idle: Typing}
	if !RestDue(fifty, paused) {
		t.Fatal("no rest after 50 minutes, at a pause")
	}
	for name, c := range map[string]struct {
		p ipc.ZenProfile
		m Moment
	}{
		"rests off":         {Default(), with(paused, func(m *Moment) { m.AtDesk = 5 * time.Hour })},
		"too soon":          {fifty, with(paused, func(m *Moment) { m.AtDesk = 49 * time.Minute })},
		"mid-sentence":      {fifty, with(paused, func(m *Moment) { m.Idle = 2 * time.Second })},
		"night, still soon": {fifty, with(paused, func(m *Moment) { m.Now = noon.Add(11 * time.Hour); m.AtDesk = 24 * time.Minute })},
	} {
		if RestDue(c.p, c.m) {
			t.Errorf("%s: a rest came", name)
		}
	}
	for name, m := range map[string]Moment{
		"the agent works, hands busy": with(paused, func(m *Moment) { m.Working = true; m.Idle = time.Second }),
		"a question waits on you":     with(paused, func(m *Moment) { m.NeedsYou = true }),
		"at night, at half the time":  with(paused, func(m *Moment) { m.Now = noon.Add(11 * time.Hour); m.AtDesk = 25 * time.Minute }),
	} {
		if !RestDue(fifty, m) {
			t.Errorf("%s: no rest", name)
		}
	}
	// At night never sooner than the least there may be between rests.
	fifteen := Apply(Default(), ipc.ZenChange{Breaks: ptr(MinBreaks)})
	late := with(paused, func(m *Moment) { m.Now = noon.Add(11 * time.Hour); m.AtDesk = 10 * time.Minute })
	if RestDue(fifteen, late) || !RestDue(fifteen, with(late, func(m *Moment) { m.AtDesk = 15 * time.Minute })) {
		t.Error("a rest at night came sooner than 15 minutes, or not at 15")
	}
}

func TestTheNightWordComesOnceANight(t *testing.T) {
	late := time.Date(2026, 11, 21, 23, 30, 0, 0, time.Local)
	m := Moment{Now: late, On: time.Hour}
	if !NightDue(Default(), m, "") {
		t.Fatal("no word at 23:30")
	}
	if NightDue(Default(), m, NightOf(late)) {
		t.Error("twice in a night")
	}
	small := with(m, func(m *Moment) { m.Now = late.Add(90 * time.Minute) })
	if NightOf(small.Now) != NightOf(late) || NightDue(Default(), small, NightOf(late)) {
		t.Error("01:00 is the same night")
	}
	if NightDue(Apply(Default(), ipc.ZenChange{Night: ptr(false)}), m, "") {
		t.Error("a word at night while it is off")
	}
	if NightDue(Default(), with(m, func(m *Moment) { m.Now = late.Add(-5 * time.Hour) }), "") {
		t.Error("a word at 18:30")
	}
}

func TestTheNotesSayOnlyThatZenIsOn(t *testing.T) {
	n := GentleNote()
	for _, want := range []string{"wellbeing comes first", "one question at a time", "ctl zen set KEY=VALUE", "At their word only"} {
		if !strings.Contains(n, want) {
			t.Errorf("the note lacks %q", want)
		}
	}
	if len(n) > 2000 {
		t.Errorf("the note is %d characters, more than a note may be", len(n))
	}
	if !strings.Contains(LeftNote(), "left zen") {
		t.Error(LeftNote())
	}
}
