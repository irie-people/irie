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
		{in: "+anon who runs example.com", want: SpawnArgs{Task: "who runs example.com", Anon: AnonNew}},
		{in: "+sonnet +anon:acme who runs it", want: SpawnArgs{Task: "who runs it", Profile: "sonnet", Anon: "acme"}},
		{in: "+tor find the forum", want: SpawnArgs{Task: "find the forum", Anon: AnonNew, AnonRoute: "tor"}},
		{in: "+tor:leaks read it", want: SpawnArgs{Task: "read it", Anon: "leaks", AnonRoute: "tor"}},
		{in: "+anon: read it", err: "compartment's name"},
		{in: "+anon +tor read it", err: "one of +anon and +tor"},
		{in: "@/a @/b fix it", err: "one @"},
		{in: "+a +b fix it", err: "one +"},
		{in: "@! fix it", err: "@"},
		// A lone @ or +, or a + before a digit or a sign, is text.
		{in: "meet @ noon", want: SpawnArgs{Task: "meet @ noon"}},
		{in: "+ fix it", want: SpawnArgs{Task: "+ fix it"}},
		{in: "add +5 to the total, +1", want: SpawnArgs{Task: "add +5 to the total, +1"}},
		// Not knowing the profiles, every + name picks one, but only where
		// it stands apart from the task's words.
		{in: "fix it +nope", want: SpawnArgs{Task: "fix it", Profile: "nope"}},
		{in: "why does +dory stall", want: SpawnArgs{Task: "why does +dory stall"}},
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

// A + word picks only when it names something Irie runs, and only where it
// stands apart from the task's words: at the start of the first line or at
// its end. Any other + is the task's, as typed (DESIGN §4.2, "Plus words").
// A jot such as "bind fn+x + fn+l" was once refused, "+ needs a profile
// name", and never started.
func TestPlusWords(t *testing.T) {
	v := View{Home: "/home/alice", Profiles: []string{"dory", "gpu", "haiku", "sonnet", "auto", "ollama:", "llama:"}}
	for _, c := range []struct {
		in   string
		want SpawnArgs
		err  string
	}{
		// Text: a lone +, a + before a digit or a sign, a + word that names
		// nothing.
		{in: "bind fn+x + fn+l + fn+r,  as emacs does", want: SpawnArgs{Task: "bind fn+x + fn+l + fn+r,  as emacs does"}},
		{in: "+1 to that, and the +/- buttons", want: SpawnArgs{Task: "+1 to that, and the +/- buttons"}},
		{in: "C++ and ++i, x += 1, a + b", want: SpawnArgs{Task: "C++ and ++i, x += 1, a + b"}},
		{in: "+ve feedback only", want: SpawnArgs{Task: "+ve feedback only"}},
		{in: "press cmd +k then +shift", want: SpawnArgs{Task: "press cmd +k then +shift"}},
		// Picks, at the start or the end, in any case, a sentence's mark
		// or a colon after them left out.
		{in: "+sonnet fix it", want: SpawnArgs{Task: "fix it", Profile: "sonnet"}},
		{in: "fix it +sonnet", want: SpawnArgs{Task: "fix it", Profile: "sonnet"}},
		{in: "fix  it +Sonnet.", want: SpawnArgs{Task: "fix it", Profile: "sonnet"}},
		{in: "+haiku: fix it", want: SpawnArgs{Task: "fix it", Profile: "haiku"}},
		{in: "+auto fix it", want: SpawnArgs{Task: "fix it", Profile: "auto"}},
		{in: "+gpu fix it", want: SpawnArgs{Task: "fix it", Profile: "gpu"}},
		{in: "+ollama:qwen3:8b summarise it", want: SpawnArgs{Task: "summarise it", Profile: "ollama:qwen3:8b"}},
		{in: "+ollama summarise it", want: SpawnArgs{Task: "summarise it", Profile: "ollama"}},
		{in: "fn + z for zen +haiku", want: SpawnArgs{Task: "fn + z for zen", Profile: "haiku"}},
		{in: "+Teach fix it", want: SpawnArgs{Task: "fix it", Tutor: "teach"}},
		{in: "fix it +haiku @web!", want: SpawnArgs{Task: "fix it", Profile: "haiku", Repo: "web", InPlace: true}},
		{in: "+haiku\nfix it", want: SpawnArgs{Task: "fix it", Profile: "haiku"}},
		// Among the task's words a + word talks about a profile, and so
		// does one at the end of a line that named one of its kind.
		{in: "why does +dory stall on long tasks", want: SpawnArgs{Task: "why does +dory stall on long tasks"}},
		{in: "compare +haiku and +sonnet", want: SpawnArgs{Task: "compare +haiku and +sonnet"}},
		{in: "+haiku compare it with +sonnet", want: SpawnArgs{Task: "compare it with +sonnet", Profile: "haiku"}},
		{in: "the +teach mode is broken +sonnet", want: SpawnArgs{Task: "the +teach mode is broken", Profile: "sonnet"}},
		// Two picks are still refused.
		{in: "+haiku +sonnet fix it", err: "one +profile"},
		{in: "fix it +haiku +sonnet", err: "one +profile"},
		// A slip from a name is refused where it would pick.
		{in: "+hiaku fix it", err: "did you mean +haiku? Nothing is called +hiaku"},
		{in: "fix it +sonet", err: "did you mean +sonnet?"},
		{in: "+olama:qwen3 summarise it", err: "did you mean +ollama:qwen3?"},
		{in: "fix it +teahc", err: "did you mean +teach?"},
		{in: "the +hiaku typo stays", want: SpawnArgs{Task: "the +hiaku typo stays"}},
		// +anon and +tor pick wherever they stand, and a slip from either
		// is refused wherever it stands.
		{in: "who runs +anon example.com", want: SpawnArgs{Task: "who runs example.com", Anon: AnonNew}},
		{in: "+anon:Acme. who runs it", want: SpawnArgs{Task: "who runs it", Anon: "Acme"}},
		{in: "find who runs it +aonn", err: "did you mean +anon?"},
		{in: "is +tro the way", err: "did you mean +tor?"},
		{in: "who runs +anno:acme now", err: "did you mean +anon:acme?"},
		{in: "+anon: read it", err: "compartment's name"},
	} {
		got, err := v.parseTask(c.in)
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
	// The input op reads the words against the profiles it is given, and
	// a jot with a + in it starts like any other.
	st, err := Read(InputArgs{Text: "bind fn+x + fn+l"}, v)
	if a, ok := st.Args.(SpawnArgs); err != nil || st.Op != OpSpawn || !ok || a.Task != "bind fn+x + fn+l" {
		t.Errorf("Read: %+v, %v", st, err)
	}
	if _, err := Read(InputArgs{Text: ":bench +hiaku fix it"}, v); err == nil || !strings.Contains(err.Error(), "+haiku") {
		t.Errorf(":bench +hiaku: %v, want the slip refused", err)
	}
	// A bench on another machine runs on its profiles, which may not be
	// this one's: every + name there is taken for one.
	if st, err := Read(InputArgs{Text: ":bench ^asahi +rocm fix it"}, v); err != nil || st.Args.(BenchArgs).Profile != "rocm" {
		t.Errorf(":bench on a peer: %+v, %v", st, err)
	}
}

func TestSlip(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"hiaku", "haiku", true},  // swapped
		{"sonet", "sonnet", true}, // left out
		{"haikuu", "haiku", true}, // added
		{"haiki", "haiku", true},  // changed
		{"tro", "tor", true},      // Irie's shortest word
		{"haiku", "haiku", false},
		{"hakiu", "haiku", true},
		{"haxyu", "haiku", false}, // two changed
		{"bug", "big", false},     // too short to tell
		{"ve", "dev", false},
		{"sonnets", "sonnet", true},
		{"son", "sonnet", false},
	} {
		if got := slip(c.a, c.b); got != c.want {
			t.Errorf("slip(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
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
	// A task quoted whole keeps its leading words as tokens: a +anon lost
	// in the task would run it outside any compartment (DESIGN §4.16).
	var q SpawnArgs
	rest, err = SpawnTokens(&q, []string{"+haiku +anon:acme find who runs  it\nand where", "too"}, "/home/alice")
	if err != nil || q.Profile != "haiku" || q.Anon != "acme" || !reflect.DeepEqual(rest, []string{"find who runs  it\nand where", "too"}) {
		t.Errorf("a quoted task: args %+v, rest %q, %v", q, rest, err)
	}
	var tor SpawnArgs
	if rest, err := SpawnTokens(&tor, []string{"+tor look"}, "/home/alice"); err != nil || tor.Anon != AnonNew || tor.AnonRoute != "tor" || !reflect.DeepEqual(rest, []string{"look"}) {
		t.Errorf("+tor quoted: args %+v, rest %q, %v", tor, rest, err)
	}
	// A + with no name after it starts the task, as typed.
	var plus SpawnArgs
	if rest, err := SpawnTokens(&plus, []string{"+Teach", "+1", "to", "it"}, "/home/alice"); err != nil || plus.Tutor != TutorTeach || !reflect.DeepEqual(rest, []string{"+1", "to", "it"}) {
		t.Errorf("+1: args %+v, rest %q, %v", plus, rest, err)
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
