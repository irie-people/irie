package route

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	for _, c := range []struct{ task, want string }{
		{"fix the typo in README", Small},
		{"rename parseConfig to loadConfig in config.go", Small},
		{"describe this repo in one paragraph", Small},
		{"add docstrings to the exported functions of ipc.go", Small},
		{"add a --verbose flag to the CLI", Medium},
		{"fix the bug where an empty task is accepted", Medium},
		{"write a test for slug()", Medium},
		{"refactor the core so state lives in one place", Hard},
		{"review this branch before I merge it", Hard},
		{"debug why the sidebar flickers on resize", Hard},
		{"orchestrate the M4 milestone: park, rename, undo and redo, with tests", Hard},
		{"explain why the build fails", Medium},
		{"add caching", Medium},
		{"Plan and build the plugin system:\n- a manifest\n- loading\n- sandboxing\n- docs\n- tests", Hard},
	} {
		got, why := Classify(c.task, DefaultWords)
		if got != c.want {
			t.Errorf("%q = %s (%s), want %s", c.task, got, why, c.want)
		}
	}
	// A long task does more.
	long := "add a flag that " + strings.Repeat("handles one more case, ", 60)
	if got, _ := Classify(long, DefaultWords); got != Hard {
		t.Errorf("a long task = %s", got)
	}
	// The word lists are the config's.
	if got, why := Classify("chore: tidy go.mod", Words{Small: []string{"chore"}}); got != Small || why != "chore" {
		t.Errorf("own words: %s (%s)", got, why)
	}
}

func TestAWordMatchesAtTheStartOfAWord(t *testing.T) {
	if !hasWord("debugging the core", "debug") || hasWord("a bad bug", "ad bug") || hasWord("prefix", "fix") {
		t.Error("hasWord")
	}
}

func TestFingerprintIgnoresCaseAndSpacing(t *testing.T) {
	if Fingerprint("Fix the  typo\n") != Fingerprint("fix the typo") || Fingerprint("fix the typo") == Fingerprint("fix a typo") {
		t.Error("Fingerprint")
	}
}

func TestAccuracyMovesWithOutcomes(t *testing.T) {
	if got := Accuracy(0.85, 10, Counts{}); got != 0.85 {
		t.Errorf("no outcomes = %v", got)
	}
	// One miss weighs one eleventh.
	if got := Accuracy(0.85, 10, Counts{Bad: 1}); math.Abs(got-8.5/11) > 1e-9 {
		t.Errorf("one bad = %v", got)
	}
	// Many outcomes outweigh the guess.
	if got := Accuracy(0.4, 10, Counts{Good: 90}); got < 0.9 {
		t.Errorf("90 good = %v", got)
	}
	if got := Accuracy(0.4, 0, Counts{Good: 1, Bad: 3}); got != 0.25 {
		t.Errorf("no prior weight = %v", got)
	}
}

func TestStats(t *testing.T) {
	var s Stats
	one, two := 1.0, 2.0
	s.Add("sonnet", Medium, true, &one)
	s.Add("sonnet", Medium, false, &two)
	if c := s.Counts("sonnet", Medium); c.Good != 1 || c.Bad != 1 {
		t.Errorf("counts = %+v", c)
	}
	if _, ok := s.MeanCost("sonnet", Medium); ok {
		t.Error("a mean cost from two outcomes")
	}
	s.Add("sonnet", Medium, true, &two)
	if m, ok := s.MeanCost("sonnet", Medium); !ok || math.Abs(m-5.0/3) > 1e-9 {
		t.Errorf("mean cost = %v, %v", m, ok)
	}
	if c := s.Counts("claude", Hard); c != (Counts{}) {
		t.Errorf("no outcomes = %+v", c)
	}
}

func cands() []Candidate {
	return []Candidate{
		{Profile: "claude", Accuracy: 0.9, Cost: 1, Usable: true},
		{Profile: "sonnet", Accuracy: 0.65, Cost: 0.25, Usable: true},
		{Profile: "local", Accuracy: 0.05, Cost: 0, Usable: true},
	}
}

func TestChooseNormalisesCostByAccuracy(t *testing.T) {
	// Hard work: only claude reaches the floor.
	ch := Choose(Hard, "review", cands(), 0.85)
	if ch.Profile != "claude" || !strings.Contains(ch.Reason, "claude is the cheapest per success") {
		t.Errorf("hard = %+v", ch)
	}
	// Small work: everything is accurate enough, so the free one.
	small := []Candidate{
		{Profile: "claude", Accuracy: 0.98, Cost: 1, Usable: true},
		{Profile: "haiku", Accuracy: 0.92, Cost: 0.02, Usable: true},
		{Profile: "local", Accuracy: 0.85, Cost: 0.006, Usable: true},
	}
	ch = Choose(Small, "typo", small, 0.85)
	if ch.Profile != "local" || ch.Candidates[0].Profile != "local" {
		t.Errorf("small = %+v", ch)
	}
	// A cheap profile that cannot run the task now is passed over.
	small[2].Usable, small[2].Note = false, "no model yet"
	if ch = Choose(Small, "typo", small, 0.85); ch.Profile != "haiku" {
		t.Errorf("small without local = %+v", ch)
	}
	// Cost per success, not cost: a cheaper profile that fails more can lose.
	tie := []Candidate{
		{Profile: "a", Accuracy: 0.9, Cost: 0.45, Usable: true},
		{Profile: "b", Accuracy: 0.86, Cost: 0.44, Usable: true},
	}
	if ch = Choose(Medium, "", tie, 0.85); ch.Profile != "a" {
		t.Errorf("per success = %+v", ch)
	}
	// None reaches the floor: the most accurate usable one.
	low := []Candidate{{Profile: "x", Accuracy: 0.5, Cost: 0, Usable: true}, {Profile: "y", Accuracy: 0.7, Cost: 1, Usable: true}}
	if ch = Choose(Hard, "", low, 0.85); ch.Profile != "y" || !strings.Contains(ch.Reason, "none reaches 85%") {
		t.Errorf("below the floor = %+v", ch)
	}
	if ch = Choose(Hard, "", []Candidate{{Profile: "x", Accuracy: 0.9}}, 0.85); ch.Profile != "" {
		t.Errorf("nothing usable = %+v", ch)
	}
}

func TestCheaper(t *testing.T) {
	priors := map[string]Prior{"local": {Cost: 0}, "haiku": {Cost: 0.02}, "sonnet": {Cost: 0.25}, "claude": {Cost: 1}}
	order := []string{"local", "haiku", "sonnet", "claude"}
	if got := Cheaper("claude", order, priors); !reflect.DeepEqual(got, []string{"local", "haiku", "sonnet"}) {
		t.Errorf("cheaper than claude = %v", got)
	}
	if got := Cheaper("local", order, priors); len(got) != 0 {
		t.Errorf("cheaper than local = %v", got)
	}
	// A profile without a prior counts as the dearest.
	if got := Cheaper("mine", order, priors); !reflect.DeepEqual(got, []string{"local", "haiku", "sonnet"}) {
		t.Errorf("cheaper than an unknown profile = %v", got)
	}
}

func TestFormats(t *testing.T) {
	if Percent(0.854) != "85%" || Relative(0) != "free" || Relative(0.25) != "0.25×" || Relative(0.0054) != "0.005×" || Relative(math.Inf(1)) != "never" {
		t.Error("formats")
	}
}
