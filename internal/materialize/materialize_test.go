package materialize

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)

const spec = `{"id":"status","title":"Where it stands","body":[{"type":"Text","text":"Writing the store."}]}`

func open(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cards.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return s, path
}

func status() Recipe {
	return Recipe{ID: "session/abc/status", Prompt: "Where does this session stand?", Catalog: 1, Inputs: "turns=3"}
}

// first makes r's first card as the core will: it runs the remake that
// Want claims.
func first(t *testing.T, s *Store, r Recipe, now time.Time) Card {
	t.Helper()
	_, _, rm, err := s.Want(r, now)
	if err != nil || rm == nil {
		t.Fatalf("want %s: remake %v, %v", r.ID, rm, err)
	}
	c, newest, err := s.Finish(*rm, json.RawMessage(spec), nil, now)
	if err != nil || !newest {
		t.Fatalf("finish %s: newest %v, %v", r.ID, newest, err)
	}
	return c
}

func TestTheHashVersionsWhatMakesTheCard(t *testing.T) {
	r := status()
	for name, other := range map[string]Recipe{
		"another prompt":  {Prompt: r.Prompt + "?", Catalog: r.Catalog},
		"another profile": {Prompt: r.Prompt, Profile: "local", Catalog: r.Catalog},
		"another catalog": {Prompt: r.Prompt, Catalog: r.Catalog + 1},
	} {
		if other.Hash() == r.Hash() {
			t.Errorf("%s: the same hash", name)
		}
	}
	same := r
	same.ID, same.Inputs, same.Every, same.Gap = "elsewhere", "turns=9", time.Hour, time.Minute
	if same.Hash() != r.Hash() {
		t.Error("a new name, inputs, Every or Gap changed the hash; they put a card out of date at most")
	}
	if (Recipe{Prompt: "a b", Profile: "c"}).Hash() == (Recipe{Prompt: "a", Profile: "b c"}).Hash() {
		t.Error("a space moved between two fields, and the hash stayed")
	}
}

func TestACardIsMadeOnceThenDrawnFromWhatIsKept(t *testing.T) {
	s, _ := open(t)
	r := status()
	c, st, rm, err := s.Want(r, t0)
	if err != nil || st != Cold || rm == nil || c.Spec != nil {
		t.Fatalf("first ask: %s, remake %v, spec %s, %v; want cold, with a remake to run", st, rm, c.Spec, err)
	}
	if _, st, again, _ := s.Want(r, t0.Add(time.Second)); st != Cold || again != nil {
		t.Fatalf("asked again while it is made: %s, remake %v; want cold, and none", st, again)
	}
	made := t0.Add(10 * time.Second)
	c, newest, err := s.Finish(*rm, json.RawMessage(spec), nil, made)
	if err != nil || !newest || string(c.Spec) != spec || c.Recipe != r.Hash() || c.Inputs != r.Inputs || !c.Made.Equal(made) {
		t.Fatalf("finish: %+v, newest %v, %v", c, newest, err)
	}
	if c, st, rm, _ := s.Want(r, t0.Add(time.Minute)); st != Fresh || rm != nil || string(c.Spec) != spec {
		t.Fatalf("asked after: %s, remake %v, spec %s; want fresh, no remake, the card", st, rm, c.Spec)
	}
}

func TestWhenACardIsOutOfDate(t *testing.T) {
	c := Card{Inputs: "turns=3", Made: t0}
	for _, tc := range []struct {
		name   string
		inputs string
		every  time.Duration
		gap    time.Duration
		age    time.Duration
		want   bool
	}{
		{"nothing moved", "turns=3", 0, 0, time.Hour, false},
		{"its inputs moved", "turns=4", 0, 0, time.Second, true},
		{"its inputs moved within the gap", "turns=4", 0, time.Minute, 30 * time.Second, false},
		{"its inputs moved, and the gap passed", "turns=4", 0, time.Minute, time.Minute, true},
		{"younger than every", "turns=3", time.Hour, 0, 59 * time.Minute, false},
		{"as old as every", "turns=3", time.Hour, 0, time.Hour, true},
		{"old, but within a longer gap", "turns=3", time.Minute, time.Hour, 30 * time.Minute, false},
		{"a clock that went back", "turns=3", time.Hour, time.Minute, -time.Hour, false},
	} {
		r := Recipe{Inputs: tc.inputs, Every: tc.every, Gap: tc.gap}
		if got := outOfDate(c, r, t0.Add(tc.age)); got != tc.want {
			t.Errorf("%s: out of date %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestAnOutOfDateCardStaysOnShowWhileItIsMadeAgain(t *testing.T) {
	s, _ := open(t)
	r := status()
	first(t, s, r, t0)
	r.Inputs = "turns=4"
	c, st, rm, _ := s.Want(r, t0.Add(time.Minute))
	if st != Refreshing || rm == nil || string(c.Spec) != spec {
		t.Fatalf("after its inputs moved: %s, remake %v, spec %s; want refreshing, with a remake, and the old card", st, rm, c.Spec)
	}
	if c, st, again, _ := s.Want(r, t0.Add(time.Minute+time.Second)); st != Refreshing || again != nil || string(c.Spec) != spec {
		t.Fatalf("asked again meanwhile: %s, remake %v; want refreshing, none, and the old card", st, again)
	}
	next := `{"id":"status","title":"Where it stands","body":[]}`
	if c, newest, _ := s.Finish(*rm, json.RawMessage(next), nil, t0.Add(2*time.Minute)); !newest || string(c.Spec) != next || c.Inputs != "turns=4" {
		t.Fatalf("finish: newest %v, spec %s, inputs %q", newest, c.Spec, c.Inputs)
	}
	if _, st, rm, _ := s.Want(r, t0.Add(3*time.Minute)); st != Fresh || rm != nil {
		t.Errorf("after the remake: %s, remake %v; want fresh", st, rm)
	}
}

func TestAFailedRemakeKeepsTheOldCard(t *testing.T) {
	s, _ := open(t)
	r := status()
	first(t, s, r, t0)
	r.Inputs = "turns=4"
	_, _, rm, _ := s.Want(r, t0.Add(time.Minute))
	failed := t0.Add(2 * time.Minute)
	if c, _, _ := s.Finish(*rm, nil, errors.New("the model timed out"), failed); string(c.Spec) != spec || c.Inputs != "turns=3" {
		t.Fatalf("a failure took the old card: spec %s, inputs %q", c.Spec, c.Inputs)
	}
	if c, st, rm, _ := s.Want(r, failed.Add(time.Second)); st != Stale || rm != nil || string(c.Spec) != spec {
		t.Fatalf("asked before the wait: %s, remake %v, spec %s; want stale, no remake, the old card", st, rm, c.Spec)
	}
	if _, st, rm, _ := s.Want(r, failed.Add(retryAfter)); st != Refreshing || rm == nil {
		t.Errorf("asked after the wait: %s, remake %v; want refreshing, with a remake", st, rm)
	}
}

func TestARecipeChangeLeavesTheCardCold(t *testing.T) {
	s, _ := open(t)
	r := status()
	first(t, s, r, t0)
	r.Prompt = "What is this session doing?"
	c, st, rm, _ := s.Want(r, t0.Add(time.Minute))
	if st != Cold || rm == nil {
		t.Fatalf("%s, remake %v; want cold, with a remake to run", st, rm)
	}
	if c.Spec != nil {
		t.Errorf("a cold card came with the old recipe's spec %s", c.Spec)
	}
}

func TestTheNewestRemakeWins(t *testing.T) {
	s, _ := open(t)
	r := status()
	_, _, old, _ := s.Want(r, t0)
	r.Prompt = "What is this session doing?"
	_, _, rm, _ := s.Want(r, t0.Add(time.Second))
	if rm == nil {
		t.Fatal("a changed recipe began no remake while the old one ran")
	}
	if _, newest, _ := s.Finish(*old, json.RawMessage(spec), nil, t0.Add(2*time.Second)); newest {
		t.Error("the outdated remake's card was kept")
	}
	c, newest, _ := s.Finish(*rm, json.RawMessage(`{"id":"status"}`), nil, t0.Add(3*time.Second))
	if !newest || c.Recipe != r.Hash() || string(c.Spec) != `{"id":"status"}` {
		t.Errorf("the newest remake: newest %v, recipe %s, spec %s", newest, c.Recipe, c.Spec)
	}
}

func TestAClaimThatRanOutLetsAnotherBegin(t *testing.T) {
	s, _ := open(t)
	r := status()
	_, _, hung, _ := s.Want(r, t0)
	if _, _, rm, _ := s.Want(r, t0.Add(claimTTL-time.Second)); rm != nil {
		t.Fatal("a second remake began while the first still held its claim")
	}
	_, _, rm, _ := s.Want(r, t0.Add(claimTTL))
	if rm == nil {
		t.Fatal("no remake began once the first one's claim ran out")
	}
	if _, newest, _ := s.Finish(*hung, json.RawMessage(spec), nil, t0.Add(claimTTL+time.Second)); newest {
		t.Error("the hung remake's card was kept over the newer remake")
	}
}

func TestAFailedRemakeWaitsBeforeTheNext(t *testing.T) {
	s, _ := open(t)
	r := status()
	_, _, rm, _ := s.Want(r, t0)
	failed := t0.Add(time.Second)
	c, newest, _ := s.Finish(*rm, nil, errors.New("the model timed out"), failed)
	if !newest || c.Err != "the model timed out" || c.Spec != nil || !c.Failed.Equal(failed) {
		t.Fatalf("finish: %+v, newest %v; want the error kept", c, newest)
	}
	if c, st, rm, _ := s.Want(r, failed.Add(retryAfter-time.Second)); st != Cold || rm != nil || c.Err == "" {
		t.Fatalf("asked before the wait: %s, remake %v, err %q; want cold, no remake, the error", st, rm, c.Err)
	}
	_, _, rm, _ = s.Want(r, failed.Add(retryAfter))
	if rm == nil {
		t.Fatal("no remake began after the wait")
	}
	if c, _, _ := s.Finish(*rm, json.RawMessage(spec), nil, failed.Add(retryAfter)); c.Err != "" || !c.Failed.IsZero() {
		t.Errorf("a card made after a failure still carries it: %q at %v", c.Err, c.Failed)
	}
}

func TestFinishRefusesWhatIsNotJSON(t *testing.T) {
	s, _ := open(t)
	_, _, rm, _ := s.Want(status(), t0)
	c, _, err := s.Finish(*rm, json.RawMessage(`{"id":`), nil, t0)
	if err != nil || c.Err == "" || c.Spec != nil {
		t.Errorf("err %q, spec %s, %v; want the half card refused as a failure", c.Err, c.Spec, err)
	}
}

func TestASpecIsKeptCompacted(t *testing.T) {
	s, _ := open(t)
	_, _, rm, _ := s.Want(status(), t0)
	c, _, _ := s.Finish(*rm, json.RawMessage("{ \"id\": \"status\",\n  \"body\": [] }"), nil, t0)
	if string(c.Spec) != `{"id":"status","body":[]}` {
		t.Errorf("kept %s; want it compacted", c.Spec)
	}
}

func TestForgetDropsACardAndItsRemake(t *testing.T) {
	s, _ := open(t)
	r := status()
	_, _, rm, _ := s.Want(r, t0)
	if err := s.Forget(r.ID); err != nil {
		t.Fatal(err)
	}
	_, _, next, _ := s.Want(r, t0.Add(time.Second))
	if next == nil || next.Gen == rm.Gen {
		t.Fatalf("the remake after Forget: %v; want a new one, numbered after %d", next, rm.Gen)
	}
	if _, newest, _ := s.Finish(*rm, json.RawMessage(spec), nil, t0.Add(2*time.Second)); newest {
		t.Error("the remake of a forgotten card was kept")
	}
}

func TestCardsOutliveARestart(t *testing.T) {
	s, path := open(t)
	r := status()
	first(t, s, r, t0)
	other := Recipe{ID: "repo/irie/head", Prompt: "What changed on main?", Catalog: 1}
	if _, _, rm, _ := s.Want(other, t0); rm == nil {
		t.Fatal("no remake began for a second card")
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if m := fi.Mode().Perm(); m != 0o600 {
		t.Errorf("the file's mode is %v; want 0600, the user's alone", m)
	}
	// The core stops while the second card is made, and starts again.
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if c, st, rm, _ := again.Want(r, t0.Add(time.Minute)); st != Fresh || rm != nil || string(c.Spec) != spec {
		t.Errorf("the kept card after a restart: %s, remake %v, spec %s; want it fresh", st, rm, c.Spec)
	}
	_, _, rm, _ := again.Want(other, t0.Add(time.Minute))
	if rm == nil {
		t.Fatal("a claim from before the restart held back the next remake")
	}
	if rm.Gen <= 2 {
		t.Errorf("the remake after a restart has number %d, one the file had used", rm.Gen)
	}
}
