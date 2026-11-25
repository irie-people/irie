package schools

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/founding"
	"github.com/irie-people/irie/internal/skills"
)

// files are a bundled skill's files, by name.
func files(t *testing.T, skill string) map[string][]byte {
	t.Helper()
	for _, s := range skills.Bundled() {
		if s.Name == skill {
			return s.Files
		}
	}
	t.Fatalf("no %s skill", skill)
	return nil
}

// TestTheTexts: every text reads, by date, once; each passage the page
// lights is in its file word for word; and every text has a place among
// the pies.
func TestTheTexts(t *testing.T) {
	docs, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != len(Documents) {
		t.Fatalf("%d texts, want %d", len(docs), len(Documents))
	}
	seen := map[string]bool{}
	for i, d := range Documents {
		if seen[d.ID] {
			t.Errorf("%s twice", d.ID)
		}
		seen[d.ID] = true
		if i > 0 && before(d.When, Documents[i-1].When) {
			t.Errorf("%s (%s) is before %s (%s)", d.ID, d.When, Documents[i-1].ID, Documents[i-1].When)
		}
		if d.Place == "" || d.Kind == "" || d.Short == "" || (d.Lens != "began" && d.Lens != "church") {
			t.Errorf("%s: %+v", d.ID, d)
		}
		x := docs[i]
		if x.Title == "" || x.About == "" || len(x.Blocks) == 0 {
			t.Errorf("%s: title %q, about %q, %d blocks", d.ID, x.Title, x.About, len(x.Blocks))
		}
		body := string(files(t, d.skill())[d.File])
		if len(d.Lit) == 0 {
			t.Errorf("%s lights no passage", d.ID)
		}
		for _, l := range d.Lit {
			if !strings.Contains(body, l) {
				t.Errorf("%s: %q isn't in %s", d.ID, l, d.File)
			}
		}
	}
	placed := map[string]bool{}
	for _, l := range Lenses() {
		walk(l.Root, nil, func(n, _ *Node) {
			for _, id := range n.Texts {
				placed[id] = true
			}
		})
	}
	for _, d := range Documents {
		if !placed[d.ID] {
			t.Errorf("%s is in no pie's card", d.ID)
		}
	}
}

// before says whether date a is before b, where either may be a year
// alone, which is before neither date of its own year.
func before(a, b string) bool {
	if len(a) == 4 || len(b) == 4 {
		return a[:4] < b[:4]
	}
	return a < b
}

// TestTheFingerprints: each file's SHA-512 is the one verification.md
// gives, so no text changes without its record changing with it. A text
// read from the charters is pinned in theirs.
func TestTheFingerprints(t *testing.T) {
	record := string(files(t, Skill)["verification.md"])
	row := regexp.MustCompile("(?m)^\\| ([a-z0-9-]+\\.md) \\| `([0-9a-f]{128})` \\|$")
	listed := map[string]string{}
	for _, m := range row.FindAllStringSubmatch(record, -1) {
		listed[m[1]] = m[2]
	}
	for _, d := range Documents {
		if d.skill() != Skill {
			continue
		}
		got := founding.Digest(files(t, Skill)[d.File])
		if listed[d.File] != got {
			t.Errorf("%s: SHA-512 %s, but verification.md gives %q", d.File, got, listed[d.File])
		}
	}
}

// quoted gives the quotations in s, the spans in curly double quotes.
func quoted(s string) []string {
	var out []string
	for {
		i := strings.Index(s, "“")
		if i < 0 {
			return out
		}
		s = s[i+len("“"):]
		j := strings.Index(s, "”")
		if j < 0 {
			return out
		}
		out = append(out, s[:j])
		s = s[j+len("”"):]
	}
}

// TestTheLinksAreInTheTexts: every link runs from a later text back to an
// earlier one, by a known kind, once, and every quotation in its why is in
// one of its two texts word for word; every text is in a link.
func TestTheLinksAreInTheTexts(t *testing.T) {
	seen := map[string]bool{}
	linked := map[string]bool{}
	for _, l := range Links {
		from, okFrom := ByID(l.From)
		to, okTo := ByID(l.To)
		if !okFrom || !okTo {
			t.Errorf("%s → %s: not both texts", l.From, l.To)
			continue
		}
		linked[l.From], linked[l.To] = true, true
		if before(from.When, to.When) || (from.When == to.When && l.How != founding.Beside) {
			t.Errorf("%s → %s: the link runs forward in time, %s to %s", l.From, l.To, from.When, to.When)
		}
		if !slices.Contains(founding.Hows, l.How) {
			t.Errorf("%s → %s: how %q", l.From, l.To, l.How)
		}
		if seen[l.From+">"+l.To] {
			t.Errorf("%s → %s: linked twice", l.From, l.To)
		}
		seen[l.From+">"+l.To] = true
		if !strings.HasSuffix(l.Why, ".") {
			t.Errorf("%s → %s: why %q", l.From, l.To, l.Why)
		}
		both := string(files(t, from.skill())[from.File]) + "\n" + string(files(t, to.skill())[to.File])
		qs := quoted(l.Why)
		if len(qs) == 0 {
			t.Errorf("%s → %s: the why quotes neither text", l.From, l.To)
		}
		for _, q := range qs {
			if !strings.Contains(both, q) {
				t.Errorf("%s → %s: %q isn't in either text", l.From, l.To, q)
			}
		}
	}
	for _, d := range Documents {
		if !linked[d.ID] {
			t.Errorf("%s is in no link", d.ID)
		}
	}
}
