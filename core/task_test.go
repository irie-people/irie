package core

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseTask(t *testing.T) {
	for _, c := range []struct {
		in   string
		want SpawnArgs
		err  string
	}{
		{in: "fix the flaky test", want: SpawnArgs{Task: "fix the flaky test"}},
		{in: "@/home/alice/code/x fix it", want: SpawnArgs{Task: "fix it", Repo: "/home/alice/code/x"}},
		{in: "fix it @~/code/x", want: SpawnArgs{Task: "fix it", Repo: "/home/alice/code/x"}},
		{in: "@~ list my files", want: SpawnArgs{Task: "list my files", Repo: "/home/alice"}},
		{in: "fix @/abs   the bug", want: SpawnArgs{Task: "fix the bug", Repo: "/abs"}},
		{in: "@/abs first line\nsecond @/not/a/token\n", want: SpawnArgs{Task: "first line\nsecond @/not/a/token\n", Repo: "/abs"}},
		{in: "@/abs\nthe task is on line two", want: SpawnArgs{Task: "the task is on line two", Repo: "/abs"}},
		{in: "mail alice@example.com about it", want: SpawnArgs{Task: "mail alice@example.com about it"}},
		{in: "  keep   inner  spacing  ", want: SpawnArgs{Task: "keep   inner  spacing"}},
		{in: "@website fix it", want: SpawnArgs{Task: "fix it", Repo: "website"}},
		{in: "@clients/api fix it", want: SpawnArgs{Task: "fix it", Repo: "clients/api"}},
		{in: "@website! fix it", want: SpawnArgs{Task: "fix it", Repo: "website", InPlace: true}},
		{in: "@~/code/x! fix it", want: SpawnArgs{Task: "fix it", Repo: "/home/alice/code/x", InPlace: true}},
		{in: "+fast fix it", want: SpawnArgs{Task: "fix it", Profile: "fast"}},
		{in: "fix it +aider @web!", want: SpawnArgs{Task: "fix it", Repo: "web", InPlace: true, Profile: "aider"}},
		{in: "c++ is fine, and so is a+b", want: SpawnArgs{Task: "c++ is fine, and so is a+b"}},
		{in: "+teach build the upload form", want: SpawnArgs{Task: "build the upload form", Tutor: "teach"}},
		{in: "+sonnet +explain fix it", want: SpawnArgs{Task: "fix it", Profile: "sonnet", Tutor: "explain"}},
		{in: "+explain +teach fix it", err: "one of +explain and +teach"},
		{in: "@/a @/b fix it", err: "one @"},
		{in: "+a +b fix it", err: "one +"},
		{in: "@ fix it", err: "@"},
		{in: "+ fix it", err: "+"},
		{in: "@web!! fix it", err: "@web!!"},
		{in: "@./src fix it", err: "@./src"},
		{in: "@/abs", err: "empty"},
		{in: "@web +fast", err: "empty"},
		{in: " \n\t", err: "empty"},
		{in: ":pause", err: ":pause"},
	} {
		got, err := ParseTask(c.in, "/home/alice")
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%q: error %v, want one mentioning %q", c.in, err, c.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("%q: got %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestSpawnTokensLeadTheWords(t *testing.T) {
	var a SpawnArgs
	rest, err := SpawnTokens(&a, []string{"@web!", "+fast", "fix", "@it", "+now"}, "/home/alice")
	if err != nil {
		t.Fatal(err)
	}
	if want := (SpawnArgs{Repo: "web", InPlace: true, Profile: "fast"}); a != want {
		t.Errorf("args = %+v, want %+v", a, want)
	}
	// Words after the first that is not a token belong to the task.
	if want := []string{"fix", "@it", "+now"}; !reflect.DeepEqual(rest, want) {
		t.Errorf("rest = %q, want %q", rest, want)
	}
	if _, err := SpawnTokens(&SpawnArgs{}, []string{"@a", "@b", "x"}, "/home/alice"); err == nil {
		t.Error("two @ tokens accepted")
	}
}

// A paste goes back whole in place of its label, after the words are read,
// so its own @ and + words stay text.
func TestUnpastePutsEachPasteBackInPlace(t *testing.T) {
	big := strings.Repeat("@Override\n", 1000)
	label := "[paste 10.0 KB · 1,000 lines]"
	in := InputArgs{Text: "@web review " + label + " and say why", Pastes: []Paste{{Label: label, Text: big}}}
	st, err := Read(in, View{Home: "/home/alice"})
	if err != nil {
		t.Fatal(err)
	}
	if a := st.Args.(SpawnArgs); a.Repo != "web" || a.Task != "review "+big+" and say why" {
		t.Errorf("spawn args: repo %q, task of %d bytes; want web and the whole paste in place", a.Repo, len(a.Task))
	}
	one, two := strings.Repeat("a", 9000), strings.Repeat("b", 9000)
	same := []Paste{{Label: "[x]", Text: one}, {Label: "[x]", Text: two}}
	if got := Unpaste("[x] then [x]", same); got != one+" then "+two {
		t.Errorf("two pastes with one label: %.20q…, want both in order", got)
	}
	// A label typed by hand, with no paste left for it, stays text.
	if got := Unpaste("[x] and [x] and [x]", same); !strings.HasSuffix(got, " and [x]") {
		t.Errorf("a third [x] became %.20q…", got[len(got)-20:])
	}
	if got := Unpaste("only typed", same); got != "only typed" {
		t.Errorf("deleted labels: %q", got)
	}
}
