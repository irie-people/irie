package founding

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/skills"
)

func docs(t *testing.T) map[string]ipc.FoundingDoc {
	t.Helper()
	all, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]ipc.FoundingDoc{}
	for _, d := range all {
		out[d.ID] = d
	}
	return out
}

func count(d ipc.FoundingDoc, kind string, match func(string) bool) int {
	n := 0
	for _, b := range d.Blocks {
		if b.Kind == kind && match(b.Text) {
			n++
		}
	}
	return n
}

func items(d ipc.FoundingDoc) int {
	n := 0
	for _, b := range d.Blocks {
		n += len(b.Items)
	}
	return n
}

// TestTheDocuments: each document is whole. The Declaration has its 27
// grievances and 56 signers; the Constitution its seven articles, 27
// amendments in order and 39 signers; the Bill of Rights its ten.
func TestTheDocuments(t *testing.T) {
	d := docs(t)
	dec, con, bor, foia := d["declaration"], d["constitution"], d["rights"], d["foia"]
	if dec.Title != "The Declaration of Independence" || con.Title != "The Constitution of the United States" || bor.Title != "The Bill of Rights" {
		t.Fatalf("titles: %q, %q, %q", dec.Title, con.Title, bor.Title)
	}
	for _, x := range []ipc.FoundingDoc{dec, con, bor} {
		if !strings.Contains(x.About, "National Archives") {
			t.Errorf("%s: About = %q, want its source", x.ID, x.About)
		}
	}
	if foia.Title != "The Freedom of Information Act" || !strings.Contains(foia.About, "Government Publishing Office's 2024 edition") {
		t.Errorf("FOIA: %q, %q", foia.Title, foia.About)
	}
	grievances := count(dec, "text", func(s string) bool { return strings.HasPrefix(s, "He ") || strings.HasPrefix(s, "For ") })
	if grievances != 27 {
		t.Errorf("Declaration: %d grievances, want 27", grievances)
	}
	if n := items(dec); n != 56 {
		t.Errorf("Declaration: %d signers, want 56", n)
	}
	articles := count(con, "h2", func(s string) bool { return strings.HasPrefix(s, "Article. ") })
	if articles != 7 {
		t.Errorf("Constitution: %d articles, want 7", articles)
	}
	romans := []string{"I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX", "X", "XI", "XII", "XIII", "XIV",
		"XV", "XVI", "XVII", "XVIII", "XIX", "XX", "XXI", "XXII", "XXIII", "XXIV", "XXV", "XXVI", "XXVII"}
	var got []string
	for _, b := range con.Blocks {
		if b.Kind == "h3" && strings.HasPrefix(b.Text, "Amendment ") {
			got = append(got, strings.TrimPrefix(b.Text, "Amendment "))
		}
	}
	if strings.Join(got, " ") != strings.Join(romans, " ") {
		t.Errorf("Constitution's amendments: %v", got)
	}
	// Washington signs first, apart from the states' lists.
	if n := items(con); n != 38 || count(con, "text", func(s string) bool { return strings.HasPrefix(s, "G°. Washington") }) != 1 {
		t.Errorf("Constitution: %d signers in the states' lists, want 38, and Washington's signature", n)
	}
	if n := count(bor, "h3", func(s string) bool { return strings.HasPrefix(s, "Amendment ") }); n != 10 {
		t.Errorf("Bill of Rights: %d amendments, want 10", n)
	}
	// FOIA: § 552's thirteen subsections in order, (a) to (m), each starting
	// a paragraph, and the Code's source credit at its foot.
	if len(foia.Blocks) == 0 || foia.Blocks[0].Kind != "h2" || foia.Blocks[0].Text != "§ 552. Public information; agency rules, opinions, orders, records, and proceedings" {
		t.Fatalf("FOIA's heading: %+v", foia.Blocks[:1])
	}
	at := 0
	for _, start := range []string{"(a) ", "(b) ", "(c)(1) ", "(d) ", "(e)(1) ", "(f) ", "(g) ", "(h)(1) ", "(i) The Government Accountability Office", "(j)(1) ", "(k)(1) ", "(l) ", "(m)(1) "} {
		i := slices.IndexFunc(foia.Blocks[at:], func(b ipc.FoundingBlock) bool { return b.Kind == "text" && strings.HasPrefix(b.Text, start) })
		if i < 0 {
			t.Errorf("FOIA: no subsection %q after block %d", start, at)
			continue
		}
		at += i
	}
	if last := foia.Blocks[len(foia.Blocks)-1]; last.Kind != "note" || !strings.HasPrefix(last.Text, "The Code's source credit") {
		t.Errorf("FOIA's foot: %+v", last)
	}
}

// TestTheTenAgree: the Bill of Rights' ten amendments are the
// Constitution's first ten, word for word.
func TestTheTenAgree(t *testing.T) {
	d := docs(t)
	ten := func(x ipc.FoundingDoc) map[string]string {
		out, at := map[string]string{}, ""
		for _, b := range x.Blocks {
			switch {
			case b.Kind == "h3" && strings.HasPrefix(b.Text, "Amendment "):
				at = b.Text
			case b.Kind != "text":
				if b.Kind != "note" {
					at = ""
				}
			case at != "":
				out[at] += b.Text
			}
		}
		return out
	}
	con, bor := ten(d["constitution"]), ten(d["rights"])
	if len(bor) != 10 {
		t.Fatalf("Bill of Rights: %d amendments", len(bor))
	}
	for name, text := range bor {
		if con[name] != text {
			t.Errorf("%s differs:\nconstitution: %q\nbill of rights: %q", name, con[name], text)
		}
	}
}

// TestTheWordsAreTheOriginals: a few readings only the originals have,
// which a modernized copy would lose.
func TestTheWordsAreTheOriginals(t *testing.T) {
	d := docs(t)
	for id, want := range map[string][]string{
		"declaration":  {"unalienable Rights", "to compleat the works of death", "our Brittish brethren"},
		"constitution": {"shall chuse their Speaker", "the Independance of the United States", "shall neither be encreased nor diminished", "A well regulated Militia, being necessary to the security of a free State, the right of the people to keep and bear Arms, shall not be infringed."},
		"rights":       {"or of the press; or the right", "In Suits at common law"},
		// Where the Department of Justice's copy differs, the Code reads so.
		"foia": {"unless the court otherwise directs for good cause shown", "compiled by criminal law enforcement authority", "This section does not authorize withholding of information", "provide copies of such index on request", "which shall be made available"},
	} {
		var all strings.Builder
		for _, b := range d[id].Blocks {
			all.WriteString(b.Text + "\n")
		}
		for _, w := range want {
			if !strings.Contains(all.String(), w) {
				t.Errorf("%s lacks %q", id, w)
			}
		}
	}
}

// TestTheFingerprints: each file's SHA-512 is the one verification.md
// gives, so no text changes without its record changing with it.
func TestTheFingerprints(t *testing.T) {
	var files map[string][]byte
	for _, s := range skills.Bundled() {
		if s.Name == Skill {
			files = s.Files
		}
	}
	record := string(files["verification.md"])
	row := regexp.MustCompile("(?m)^\\| ([a-z-]+\\.md) \\| `([0-9a-f]{128})` \\|$")
	listed := map[string]string{}
	for _, m := range row.FindAllStringSubmatch(record, -1) {
		listed[m[1]] = m[2]
	}
	for _, f := range Files {
		got := Digest(files[f.File])
		if listed[f.File] != got {
			t.Errorf("%s: SHA-512 %s, but verification.md gives %q", f.File, got, listed[f.File])
		}
	}
	for _, x := range docs(t) {
		if len(x.SHA512) != 128 {
			t.Errorf("%s: SHA512 = %q", x.ID, x.SHA512)
		}
	}
}

func TestParseRefuses(t *testing.T) {
	for name, text := range map[string]string{
		"no title":        "Some words.\n",
		"two titles":      "# One\n\nWords.\n\n# Two\n",
		"carriage return": "# One\r\n\r\nWords.\r\n",
		"no text":         "# One\n\n_A note._\n",
	} {
		if _, err := Parse("x", []byte(text)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
	d, err := Parse("x", []byte("# One\n\n_About it._\n\n## Part\n\nWords\nand more.\n\n_A note._\n\n- Ann\n- Bo\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []ipc.FoundingBlock{{Kind: "h2", Text: "Part"}, {Kind: "text", Text: "Words\nand more."}, {Kind: "note", Text: "A note."}, {Kind: "list", Items: []string{"Ann", "Bo"}}}
	if d.About != "About it." || len(d.Blocks) != len(want) {
		t.Fatalf("got %+v", d)
	}
	for i := range want {
		if d.Blocks[i].Kind != want[i].Kind || d.Blocks[i].Text != want[i].Text || strings.Join(d.Blocks[i].Items, ",") != strings.Join(want[i].Items, ",") {
			t.Errorf("block %d: %+v, want %+v", i, d.Blocks[i], want[i])
		}
	}
}

// TestFindAnySpelling: a search finds a passage whatever its spelling,
// capitals or punctuation, and only where the words are.
func TestFindAnySpelling(t *testing.T) {
	all, _ := Load()
	find := func(q string) map[string]bool {
		out := map[string]bool{}
		for _, h := range Find(all, q) {
			out[h.Doc+": "+h.Where] = true
		}
		return out
	}
	for q, want := range map[string]string{
		"choose their Speaker":        "constitution: Article. I. › Section. 2.",
		"the common defense":          "constitution: Preamble",
		"THE UNANIMOUS DECLARATION":   "declaration: ",
		"complete the works of death": "declaration: ",
		"our British brethren":        "declaration: ",
		"unreasonable searches":       "rights: Amendments › Amendment IV",
		"vice president elect":        "constitution: Amendments › Amendment XX › Section 3.",
		"the Vice President":          "constitution: Amendments › Amendment XII",
		"chief FOIA officer":          "foia: § 552. Public information; agency rules, opinions, orders, records, and proceedings",
	} {
		if got := find(q); !got[want] {
			t.Errorf("Find(%q) = %v, want %q among them", q, got, want)
		}
	}
	for d := range find("pursuit of happiness") {
		if !strings.HasPrefix(d, "declaration") {
			t.Errorf("pursuit of happiness found in %s", d)
		}
	}
	if got := Find(all, "  ,.;  "); got != nil {
		t.Errorf("punctuation alone found %v", got)
	}
	if got := find("arm"); len(got) != 0 {
		t.Errorf(`"arm" found %v: whole words only`, got)
	}
}
