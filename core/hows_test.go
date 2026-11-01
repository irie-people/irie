package core

import "testing"

func TestParseHows(t *testing.T) {
	for in, want := range map[string]string{
		"hows the parser":         "parser",
		"How's my auth refactor?": "auth refactor",
		"how is tests going":      "tests",
		"HOWS X":                  "X",
		// The Mac app dropped a capital The, and the sidebar did not; now
		// both drop it, since both ask the core.
		"Hows The parser?": "parser",
	} {
		if got, ok := ParseHows(in); !ok || got != want {
			t.Errorf("ParseHows(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"how do I fix the parser", "hows", "show hows x", "fix hows the parser"} {
		if q, ok := ParseHows(in); ok {
			t.Errorf("ParseHows(%q) = %q; want no match", in, q)
		}
	}
}

func TestFindSessionAndHows(t *testing.T) {
	code := 2
	ss := []Session{
		{Name: "fix login bug", Branch: "irie/login", State: "working", Detail: "Running the tests"},
		{Name: "docs sweep", Branch: "irie/parser-docs", State: "needs_you", Reason: "Asks: may it run rm -rf build?",
			Subagents: []Subagent{{Type: "Explore", NeedsYou: true}}},
		{Name: "parser rewrite", State: "exited", ExitCode: &code},
	}
	if s := FindSession(ss, "parser"); s == nil || s.Name != "parser rewrite" {
		t.Errorf("name match should beat branch match, got %+v", s)
	}
	if s := FindSession(ss, "login bug"); s == nil || s.Name != "fix login bug" {
		t.Errorf("got %+v", s)
	}
	if s := FindSession(ss, "login docs"); s != nil {
		t.Errorf("every word must match, got %+v", s)
	}
	if s := FindSession(ss, "explore"); s == nil || s.Name != "docs sweep" {
		t.Errorf("a subagent's type should match, got %+v", s)
	}
	for i, want := range []string{
		"fix login bug: working · Running the tests",
		"docs sweep: needs you · Asks: may it run rm -rf build? · 1 subagent (one needs you)",
		"parser rewrite: exited · exit 2",
	} {
		if got := Hows(ss[i]); got != want {
			t.Errorf("Hows = %q; want %q", got, want)
		}
	}
}
