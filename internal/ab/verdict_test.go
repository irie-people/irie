package ab

import (
	"testing"

	"github.com/irie-people/irie/internal/brand"
)

func TestRunsFindsTheCommandHoweverItIsNamed(t *testing.T) {
	s, env := brand.Slug, brand.Env("BIN")
	for _, c := range []string{
		s + " ab gold new",
		`"$` + env + `" ab -n 5 gold new -- x`,
		"${" + env + "} ab gold new",
		"~/.local/share/" + s + "/bin/" + s + " ab gold new",
		"cd /tmp && " + s + " ab --json gold new | tail -3",
	} {
		if !Runs(c) {
			t.Errorf("%q: not seen as a run", c)
		}
	}
	for _, c := range []string{s + " about", s + "-ab gold new", "grep -n ab " + s + ".go", s + " ctl list"} {
		if Runs(c) {
			t.Errorf("%q: seen as a run", c)
		}
	}
}

func TestReadTakesTheVerdictFromTheReport(t *testing.T) {
	table := "         gold   new   change\ntime     1.2s   0.4s  69% less\n"
	cases := []struct{ out, verdict, line string }{
		{table + "\nnew is faster, with the same output\n", Faster, "new is faster, with the same output"},
		{table + "\nnew is slower, with the same output\n", Slower, "new is slower, with the same output"},
		{table + "\nnew is neither clearly faster nor slower, with the same output\n", Even, "new is neither clearly faster nor slower, with the same output"},
		{"output  differs at line 2:\n" + brand.Slug + ": ab: new's output differs from gold's, so its times are for different work\n", Differs,
			brand.Slug + ": ab: new's output differs from gold's, so its times are for different work"},
		{brand.Slug + ": ab: gold's own output changes from run to run, from run 2, so a difference from new means nothing\n", Differs,
			brand.Slug + ": ab: gold's own output changes from run to run, from run 2, so a difference from new means nothing"},
		{`{"same": true, "time": {"unit": "s", "change": "lower"}}`, Faster, `"time": {"change": "lower"}`},
		{`{"same": true, "time": {"unit": "s", "change": "same"}}`, Even, `"time": {"change": "same"}`},
		{`{"same": false, "time": {"unit": "s", "change": "lower"}}`, Differs, `"same": false`},
		{brand.Slug + ": ab: name two builds, GOLD and NEW\n", "", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		if v, l := Read(c.out); v != c.verdict || l != c.line {
			t.Errorf("%q: got %q, %q; want %q, %q", c.out, v, l, c.verdict, c.line)
		}
	}
}

func TestSaysFasterAndSaysSameHearPlainClaimsOnly(t *testing.T) {
	for msg, want := range map[string]bool{
		"Done. The new build is 40% faster.":               true,
		"It now uses less CPU on big dumps.":               true,
		"This sped up the parser.":                         true,
		"It should be faster once the cache is warm.":      false,
		"It's not faster yet.":                             false,
		"I didn't measure whether it is faster.":           false,
		"Done. Committed and installed; the row is green.": false,
	} {
		if got := SaysFaster(msg); got != want {
			t.Errorf("SaysFaster(%q) = %v", msg, got)
		}
	}
	for msg, want := range map[string]bool{
		"The output is unchanged.":                         true,
		"Same output on every input.":                      true,
		"Results are byte-for-byte identical.":             true,
		"The output is not the same: the header moved.":    false,
		"The output should be identical after the rebase.": false,
	} {
		if got := SaysSame(msg); got != want {
			t.Errorf("SaysSame(%q) = %v", msg, got)
		}
	}
}
