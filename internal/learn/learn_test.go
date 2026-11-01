package learn

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestTheFileLoads(t *testing.T) {
	if setErr != nil {
		t.Fatal(setErr)
	}
	s := Get()
	if len(s.Tips) == 0 || len(s.Resources) == 0 || len(s.Glossary) == 0 {
		t.Fatalf("tips %d, resources %d, terms %d: each needs some", len(s.Tips), len(s.Resources), len(s.Glossary))
	}
	for _, r := range s.Resources {
		if r.Yours || r.Here {
			t.Errorf("%s: yours and here are set only when served", r.Title)
		}
	}
	// Every language Irie can tell has its tutorial.
	for _, m := range markers {
		if !slices.ContainsFunc(s.Resources, func(r Resource) bool { return r.Lang == m.lang }) {
			t.Errorf("no tutorial for %s", m.lang)
		}
	}
}

func TestCheckRefusesWhatClaudeCodeWould(t *testing.T) {
	for name, bad := range map[string]string{
		"an id with a space": "[[tip]]\nid = \"a b\"\ntext = \"x\"\n",
		"a taken id":         "[[tip]]\nid = \"a\"\ntext = \"x\"\n[[tip]]\nid = \"a\"\ntext = \"y\"\n",
		"two lines":          "[[tip]]\nid = \"a\"\ntext = \"x\\ny\"\n",
		"a long tip":         "[[tip]]\nid = \"a\"\ntext = \"" + strings.Repeat("x", MaxTipText+1) + "\"\n",
		"a plain http link":  "[[resource]]\ngroup = \"g\"\ntitle = \"t\"\nurl = \"http://example.com\"\nabout = \"a\"\n",
		"no about":           "[[resource]]\ngroup = \"g\"\ntitle = \"t\"\nurl = \"https://example.com\"\n",
		"a language unknown": "[[resource]]\ngroup = \"g\"\ntitle = \"t\"\nurl = \"https://example.com\"\nabout = \"a\"\nlang = \"cobol\"\n",
		"a term twice":       "[[term]]\nterm = \"Park\"\nmeans = \"x\"\n[[term]]\nterm = \"park\"\nmeans = \"y\"\n",
		"an unknown key":     "[[term]]\nterm = \"a\"\nmeans = \"x\"\nsee = \"b\"\n",
	} {
		if _, err := parse(bad); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

func TestLanguagesReadsTheProjectsTop(t *testing.T) {
	dir := t.TempDir()
	if got := Languages(dir); len(got) != 0 {
		t.Errorf("an empty folder: %v", got)
	}
	for _, f := range []string{"go.mod", "requirements.txt", "pyproject.toml"} {
		if err := os.WriteFile(filepath.Join(dir, f), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// A folder named like a marker is not one.
	if err := os.Mkdir(filepath.Join(dir, "package.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got, want := Languages(dir), []string{"go", "python"}; !slices.Equal(got, want) {
		t.Errorf("Languages = %v, want %v", got, want)
	}
	if got := Languages(""); got != nil {
		t.Errorf("no folder: %v", got)
	}
}

func TestResourcesPutYoursAndTheLanguageAtHandFirst(t *testing.T) {
	got := Resources(map[string]string{"Team handbook": "https://example.com/handbook", "A wiki": "https://example.com/wiki"}, []string{"swift"})
	if got[0].Title != "A wiki" || got[1].Title != "Team handbook" || !got[0].Yours || got[0].Group != "Your links" {
		t.Fatalf("your links first, by title: %+v", got[:2])
	}
	if got[2].Lang != "swift" || !got[2].Here {
		t.Errorf("then the language at hand: %+v", got[2])
	}
	last := got[len(got)-1]
	if last.Lang == "" || last.Here {
		t.Errorf("the other languages' tutorials come last: %+v", last)
	}
	if n := len(got); n != len(Get().Resources)+2 {
		t.Errorf("%d resources, want every one and both links", n)
	}
}
