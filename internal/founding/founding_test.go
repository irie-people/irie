package founding

import (
	"regexp"
	"slices"
	"strconv"
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
// amendments in order and 39 signers; the Bill of Rights its ten; Magna
// Carta its 63 chapters; the English Bill of Rights its title and the
// rights it declares; the Virginia Declaration its sixteen sections; the
// Articles their thirteen; the Universal Declaration its preamble and
// thirty articles; the Privacy Act its subsections (a) to (w).
func TestTheDocuments(t *testing.T) {
	d := docs(t)
	if len(d) != len(Documents) {
		t.Fatalf("%d documents, want %d", len(d), len(Documents))
	}
	for _, x := range Documents {
		got := d[x.ID]
		if got.Short != x.Short || got.When != x.When || got.Kind != x.Kind || len(got.When) < 4 {
			t.Errorf("%s: short %q, when %q, kind %q", x.ID, got.Short, got.When, got.Kind)
		}
	}
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

	// Magna Carta: a preamble, then 63 chapters, each a paragraph starting
	// with its number, in order.
	mc := d["magna-carta"]
	if mc.Title != "Magna Carta" || !strings.Contains(mc.About, "McKechnie") || !strings.Contains(mc.About, "1215") {
		t.Errorf("Magna Carta: %q, %q", mc.Title, mc.About)
	}
	chapters := 0
	for _, b := range mc.Blocks {
		if b.Kind == "text" && strings.HasPrefix(b.Text, strconv.Itoa(chapters+1)+". ") {
			chapters++
		}
	}
	if chapters != 63 || len(mc.Blocks) != 64 || !strings.HasPrefix(mc.Blocks[0].Text, "John, by the grace of God") {
		t.Errorf("Magna Carta: %d chapters in order, %d blocks", chapters, len(mc.Blocks))
	}

	// The English Bill of Rights: the Act's title first, then its words
	// under the Statutes of the Realm's marginal titles; the rights it
	// declares each start "That", twelve of them, and one "And that".
	ebr := d["english-rights"]
	if ebr.Title != "The English Bill of Rights" || !strings.Contains(ebr.About, "legislation.gov.uk") || !strings.Contains(ebr.About, "1 Will. & Mar. sess. 2 c. 2") {
		t.Errorf("the English Bill of Rights: %q, %q", ebr.Title, ebr.About)
	}
	if len(ebr.Blocks) == 0 || ebr.Blocks[0].Kind != "text" || ebr.Blocks[0].Text != "An Act declareing the Rights and Liberties of the Subject and Setleing the Succession of the Crowne." {
		t.Errorf("the English Bill of Rights' title: %+v", ebr.Blocks[:1])
	}
	if n := count(ebr, "text", func(s string) bool { return strings.HasPrefix(s, "That ") }); n != 12 {
		t.Errorf("the English Bill of Rights: %d rights starting \"That\", want 12", n)
	}
	if n := count(ebr, "h2", func(string) bool { return true }); n != 35 {
		t.Errorf("the English Bill of Rights: %d marginal titles, want 35", n)
	}

	// The Virginia Declaration: its opening, then Sections 1 to 16 in order.
	va := d["virginia"]
	if va.Title != "The Virginia Declaration of Rights" || !strings.Contains(va.About, "National Archives") || !strings.Contains(va.About, "modern spelling") {
		t.Errorf("Virginia: %q, %q", va.Title, va.About)
	}
	var secs []string
	for _, b := range va.Blocks {
		if b.Kind == "h2" && strings.HasPrefix(b.Text, "Section ") {
			secs = append(secs, strings.TrimPrefix(b.Text, "Section "))
		}
	}
	opening := slices.IndexFunc(va.Blocks, func(b ipc.FoundingBlock) bool { return b.Kind == "h2" && b.Text == "A Declaration of Rights" })
	if strings.Join(secs, " ") != "1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16" || opening < 0 || opening > 1 {
		t.Errorf("Virginia's sections: %v, the opening at %d", secs, opening)
	}

	// The Articles: Article I. to Article XIII., in order.
	art := d["articles"]
	if art.Title != "The Articles of Confederation" || !strings.Contains(art.About, "National Archives") {
		t.Errorf("the Articles: %q, %q", art.Title, art.About)
	}
	var arts []string
	for _, b := range art.Blocks {
		if b.Kind == "h2" && strings.HasPrefix(b.Text, "Article ") {
			arts = append(arts, strings.TrimSuffix(strings.TrimPrefix(b.Text, "Article "), "."))
		}
	}
	if strings.Join(arts, " ") != strings.Join(romans[:13], " ") {
		t.Errorf("the Articles: %v", arts)
	}

	// The Petition of Right: its long title first, its seven numbered
	// sections under the Statutes of the Realm's marginal titles, and the
	// edition's two footnotes as notes.
	pr := d["petition"]
	if pr.Title != "The Petition of Right" || !strings.Contains(pr.About, "legislation.gov.uk") || !strings.Contains(pr.About, "3 Cha. I c. 1") {
		t.Errorf("the Petition of Right: %q, %q", pr.Title, pr.About)
	}
	if len(pr.Blocks) == 0 || pr.Blocks[0].Kind != "text" || !strings.HasPrefix(pr.Blocks[0].Text, "The Peticion Exhibited to His Majestie") {
		t.Errorf("the Petition of Right's title: %+v", pr.Blocks[:1])
	}
	numbered := count(pr, "h2", func(s string) bool { return regexp.MustCompile(`^(II|III|IV|V|VI|VII|VIII)\. `).MatchString(s) })
	if h2 := count(pr, "h2", func(string) bool { return true }); numbered != 7 || h2 != 9 || count(pr, "note", func(string) bool { return true }) != 2 || count(pr, "text", func(string) bool { return true }) != 14 {
		t.Errorf("the Petition of Right: %d numbered sections of %d headings, %d notes", numbered, h2, count(pr, "note", func(string) bool { return true }))
	}

	// The Habeas Corpus Act: its long title first, its sections under the
	// Statutes of the Realm's marginal titles, numbered II to XX after the
	// first, and the edition's footnotes as notes, one a mark.
	hc := d["habeas"]
	if hc.Title != "The Habeas Corpus Act" || !strings.Contains(hc.About, "British History Online") || !strings.Contains(hc.About, "31 Cha. II c. 2") {
		t.Errorf("the Habeas Corpus Act: %q, %q", hc.Title, hc.About)
	}
	if len(hc.Blocks) == 0 || hc.Blocks[0].Text != "An Act for the better secureing the Liberty of the Subject and for Prevention of Imprisonments beyond the Seas." {
		t.Errorf("the Habeas Corpus Act's title: %+v", hc.Blocks[:1])
	}
	hcNumbered := count(hc, "h2", func(s string) bool {
		return regexp.MustCompile(`^(II|III|IV|V|VI|VII|VIII|IX|X|XI|XII|XIII|XIV|XV|XVI|XVII|XVIII|XIX|XX)\. `).MatchString(s)
	})
	if hcNumbered != 19 || count(hc, "h2", func(string) bool { return true }) != 20 || count(hc, "text", func(string) bool { return true }) != 21 || count(hc, "note", func(string) bool { return true }) != 26 {
		t.Errorf("the Habeas Corpus Act: %d numbered sections, %d blocks", hcNumbered, len(hc.Blocks))
	}

	// The Northwest Ordinance: its title, fourteen sections and six articles
	// of compact, each a paragraph starting with the Archives' bold label,
	// then the repeal and the closing; the source sets no headings.
	nw := d["northwest"]
	if nw.Title != "The Northwest Ordinance" || !strings.Contains(nw.About, "National Archives") || !strings.Contains(nw.About, "July 13, 1787") {
		t.Errorf("the Northwest Ordinance: %q, %q", nw.Title, nw.About)
	}
	sections := count(nw, "text", func(s string) bool { return strings.HasPrefix(s, "**Sec") })
	compact := count(nw, "text", func(s string) bool { return strings.HasPrefix(s, "**Art. ") })
	if sections != 14 || compact != 6 || len(nw.Blocks) != 23 || count(nw, "h2", func(string) bool { return true }) != 0 || !strings.HasPrefix(nw.Blocks[0].Text, "**An Ordinance for the government of the Territory") {
		t.Errorf("the Northwest Ordinance: %d sections, %d articles, %d blocks", sections, compact, len(nw.Blocks))
	}

	// The Declaration of Sentiments: the declaration with its grievances,
	// the convention's resolutions, and the signers, women then men.
	ds := d["seneca"]
	if ds.Title != "The Declaration of Sentiments" || !strings.Contains(ds.About, "National Park Service") || !strings.Contains(ds.About, "1848") {
		t.Errorf("the Declaration of Sentiments: %q, %q", ds.Title, ds.About)
	}
	var dsHeads []string
	for _, b := range ds.Blocks {
		if b.Kind == "h2" {
			dsHeads = append(dsHeads, b.Text)
		}
	}
	// Sixteen grievances: fourteen "He …", "Having deprived her …" and
	// "After depriving her …".
	he := count(ds, "text", func(s string) bool { return strings.HasPrefix(s, "He ") })
	other := count(ds, "text", func(s string) bool {
		return strings.HasPrefix(s, "Having deprived her") || strings.HasPrefix(s, "After depriving her")
	})
	resolved := count(ds, "text", func(s string) bool { return strings.HasPrefix(s, "Resolved, ") })
	if strings.Join(dsHeads, "|") != "Resolutions|Signers" || he != 14 || other != 2 || resolved != 12 || items(ds) != 100 || count(ds, "note", func(string) bool { return true }) != 3 {
		t.Errorf("the Declaration of Sentiments: headings %v, %d+%d grievances, %d resolutions, %d signers", dsHeads, he, other, resolved, items(ds))
	}

	// The Emancipation Proclamation: its opening lines, its paragraphs and
	// the signatures, all as paragraphs, as the Archives prints them.
	ep := d["emancipation"]
	if ep.Title != "The Emancipation Proclamation" || !strings.Contains(ep.About, "National Archives") || !strings.Contains(ep.About, "January 1, 1863") {
		t.Errorf("the Emancipation Proclamation: %q, %q", ep.Title, ep.About)
	}
	if len(ep.Blocks) != 14 || count(ep, "text", func(string) bool { return true }) != 14 || ep.Blocks[0].Text != "By the President of the United States of America:" || ep.Blocks[1].Text != "A Proclamation." || !strings.HasPrefix(ep.Blocks[13].Text, "By the President: ABRAHAM LINCOLN") {
		t.Errorf("the Emancipation Proclamation: %d blocks, %+v", len(ep.Blocks), ep.Blocks)
	}

	// The Gettysburg Address, the Bliss copy: Lincoln's heading, the three
	// paragraphs as the copy breaks them, and the signature and date.
	ga := d["gettysburg"]
	if ga.Title != "The Gettysburg Address" || !strings.Contains(ga.About, "Bliss copy") || !strings.Contains(ga.About, "November 19, 1863") {
		t.Errorf("the Gettysburg Address: %q, %q", ga.Title, ga.About)
	}
	if len(ga.Blocks) != 5 || count(ga, "text", func(string) bool { return true }) != 5 || ga.Blocks[0].Text != "Address delivered at the dedication of the cemetery at Gettysburg." || !strings.HasPrefix(ga.Blocks[1].Text, "Four score and seven years ago") || !strings.HasPrefix(ga.Blocks[4].Text, "Abraham Lincoln") {
		t.Errorf("the Gettysburg Address: %d blocks, %+v", len(ga.Blocks), ga.Blocks)
	}

	// The Universal Declaration: its preamble, then Articles 1 to 30 in order.
	ud := d["udhr"]
	if ud.Title != "The Universal Declaration of Human Rights" || !strings.Contains(ud.About, "10 December 1948") || !strings.Contains(ud.About, "A/RES/217(III)") {
		t.Errorf("the Universal Declaration: %q, %q", ud.Title, ud.About)
	}
	var h2s []string
	for _, b := range ud.Blocks {
		if b.Kind == "h2" {
			h2s = append(h2s, b.Text)
		}
	}
	want := []string{"Preamble"}
	for i := 1; i <= 30; i++ {
		want = append(want, "Article "+strconv.Itoa(i))
	}
	if !slices.Equal(h2s, want) {
		t.Errorf("the Universal Declaration's parts: %v", h2s)
	}

	// The Privacy Act: § 552a's 23 subsections, (a) to (w), each starting a
	// paragraph with its catchline, in order, and the Code's source credit
	// at its foot.
	pa := d["privacy"]
	if pa.Title != "The Privacy Act of 1974" || !strings.Contains(pa.About, "Government Publishing Office's 2024 edition") {
		t.Errorf("the Privacy Act: %q, %q", pa.Title, pa.About)
	}
	if len(pa.Blocks) == 0 || pa.Blocks[0].Kind != "h2" || pa.Blocks[0].Text != "§ 552a. Records maintained on individuals" {
		t.Fatalf("the Privacy Act's heading: %+v", pa.Blocks[:1])
	}
	at = 0
	for _, start := range []string{"(a) Definitions.—", "(b) Conditions of Disclosure.—", "(c) Accounting of Certain Disclosures.—", "(d) Access to Records.—",
		"(e) Agency Requirements.—", "(f) Agency Rules.—", "(g)(1) Civil Remedies.—", "(h) Rights of Legal Guardians.—", "(i)(1) Criminal Penalties.—",
		"(j) General Exemptions.—", "(k) Specific Exemptions.—", "(l)(1) Archival Records.—", "(m)(1) Government Contractors.—", "(n) Mailing Lists.—",
		"(o) Matching Agreements.—", "(p) Verification and Opportunity to Contest Findings.—", "(q) Sanctions.—", "(r) Report on New Systems and Matching Programs.—",
		"(s) Biennial Report.—", "(t)(1) Effect of Other Laws.—", "(u) Data Integrity Boards.—", "(v) Office of Management and Budget Responsibilities.—",
		"(w) Applicability to Bureau of Consumer Financial Protection.—"} {
		i := slices.IndexFunc(pa.Blocks[at:], func(b ipc.FoundingBlock) bool { return b.Kind == "text" && strings.HasPrefix(b.Text, start) })
		if i < 0 {
			t.Errorf("the Privacy Act: no subsection %q after block %d", start, at)
			continue
		}
		at += i
	}
	if last := pa.Blocks[len(pa.Blocks)-1]; last.Kind != "note" || !strings.HasPrefix(last.Text, "The Code's source credit") {
		t.Errorf("the Privacy Act's foot: %+v", last)
	}
}

// TestTheRightToInformationAct: India's Act, as India Code sets it out:
// its number and date, its long title, its six chapters and two schedules
// in order, its 31 sections in order, and India Code's footnotes as notes.
func TestTheRightToInformationAct(t *testing.T) {
	r := docs(t)["rti"]
	if r.Title != "The Right to Information Act, 2005" || !strings.Contains(r.About, "India Code") || !strings.Contains(r.About, "18 November 2025") || !strings.Contains(r.About, "section 52(1)(q)(ii)") {
		t.Errorf("the Right to Information Act: %q, %q", r.Title, r.About)
	}
	if len(r.Blocks) < 3 || r.Blocks[0].Text != "ACT NO. 22 OF 2005" || r.Blocks[1].Text != "[15th June, 2005.]" || !strings.HasPrefix(r.Blocks[2].Text, "An Act to provide for setting out the practical regime of right to information") {
		t.Errorf("the Act's head: %+v", r.Blocks[:min(3, len(r.Blocks))])
	}
	var heads []string
	for _, b := range r.Blocks {
		if b.Kind == "h2" {
			heads = append(heads, b.Text)
		}
	}
	want := []string{"Chapter I. Preliminary", "Chapter II. Right to information and obligations of public authorities", "Chapter III. The Central Information Commission",
		"Chapter IV. The State Information Commission", "Chapter V. Powers and functions of the Information Commissions, appeal and penalties", "Chapter VI. Miscellaneous",
		"The First Schedule", "The Second Schedule"}
	if !slices.Equal(heads, want) {
		t.Errorf("the Act's parts: %v", heads)
	}
	at := 0
	for n := 1; n <= 31; n++ {
		start := strconv.Itoa(n) + ". "
		i := slices.IndexFunc(r.Blocks[at:], func(b ipc.FoundingBlock) bool { return b.Kind == "text" && strings.HasPrefix(b.Text, start) })
		if i < 0 {
			t.Errorf("the Act: no section %d after block %d", n, at)
			continue
		}
		at += i
	}
	if n := count(r, "note", func(string) bool { return true }); n != 21 {
		t.Errorf("the Act: %d notes, want 21", n)
	}
}

// TestTheLinksAreInTheTexts: each link joins two of the texts, the later to
// the earlier, by one of the known kinds, once; every text is in the graph;
// and every quotation in a link's why, in curly double quotes, is in one of
// its two texts word for word, so the inference can be checked against the
// chain itself.
func TestTheLinksAreInTheTexts(t *testing.T) {
	var files map[string][]byte
	for _, s := range skills.Bundled() {
		if s.Name == Skill {
			files = s.Files
		}
	}
	year := func(d Document) int {
		y, err := strconv.Atoi(d.When[:4])
		if err != nil {
			t.Fatalf("%s: when %q", d.ID, d.When)
		}
		return y
	}
	seen := map[string]bool{}
	inGraph := map[string]bool{}
	for _, l := range Links {
		from, okFrom := ByID(l.From)
		to, okTo := ByID(l.To)
		if !okFrom || !okTo {
			t.Errorf("%s → %s: not both texts", l.From, l.To)
			continue
		}
		inGraph[l.From], inGraph[l.To] = true, true
		if year(from) < year(to) || (year(from) == year(to) && from.When <= to.When) {
			t.Errorf("%s → %s: the link runs forward in time, %s to %s", l.From, l.To, from.When, to.When)
		}
		if !slices.Contains(Hows, l.How) {
			t.Errorf("%s → %s: how %q", l.From, l.To, l.How)
		}
		if seen[l.From+">"+l.To] {
			t.Errorf("%s → %s: linked twice", l.From, l.To)
		}
		seen[l.From+">"+l.To] = true
		if !strings.HasSuffix(l.Why, ".") || strings.Contains(l.Why, "'") && !strings.Contains(l.Why, "'s") && !strings.Contains(l.Why, "s' ") {
			t.Errorf("%s → %s: why %q", l.From, l.To, l.Why)
		}
		both := string(files[from.File]) + "\n" + string(files[to.File])
		for _, q := range quoted(l.Why) {
			if !strings.Contains(both, q) {
				t.Errorf("%s → %s: %q isn't in either text", l.From, l.To, q)
			}
		}
	}
	for _, d := range Documents {
		if !inGraph[d.ID] {
			t.Errorf("%s is in no link", d.ID)
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
		"declaration": {"unalienable Rights", "to compleat the works of death", "our Brittish brethren"},
		"constitution": {"shall chuse their Speaker", "the Independance of the United States", "shall neither be encreased nor diminished", "A well regulated Militia, being necessary to the security of a free State, the right of the people to keep and bear Arms, shall not be infringed.",
			// Where the Archives' transcription differs from the parchments, as
			// their scans show: verification.md, "Settled from the parchments".
			"the Census or Enumeration herein before", "as they think proper; in the President alone", "Citizens of another State;—between Citizens of different States", "In suits at common law"},
		"rights": {"or of the press; or the right", "In suits at common law"},
		// Where the Department of Justice's copy differs, the Code reads so.
		"foia": {"unless the court otherwise directs for good cause shown", "compiled by criminal law enforcement authority", "This section does not authorize withholding of information", "provide copies of such index on request", "which shall be made available"},
		// The printed page's words where Wikisource's transcription slipped.
		"magna-carta":    {"No freeman shall be taken or imprisoned", "one measure of corn", "both clergy and laymen"},
		"english-rights": {"cruell and unusuall Punishments inflicted", "excessive Baile ought not to be required", "Setleing the Succession of the Crowne"},
		"petition":       {"by due p[ro]cesse of Lawe", "The great Charter of the Liberties of England", "Soit droit fait come est desire"},
		// The edition's readings as British History Online keys them.
		"habeas":   {"Writts of Habeas Corpus", "better secureing the Liberty of the Subject", "by standingh out an Alias and Pluries"},
		"virginia": {"nor can he be compelled to give evidence against himself", "the free exercise of religion"},
		"articles": {"The Stile of this confederacy shall be", "a firm league of friendship", "con-firmed by the legislatures of every state"},
		// The transcription's own readings, kept: "descent" where the journal has "descend".
		"northwest": {"shall descent to, and be distributed among their children", "There shall be neither slavery nor involuntary servitude in the said territory"},
		// The Park Service's readings, where the 1848 printing differs.
		"seneca": {"self-evident; that all men and women are created equal", "not their ignorance", "Resolved, Therefore, That"},
		// The Archives' transcription's own readings, where the 1863 print differs.
		"emancipation": {"are, and henceforward shall be free", "St. James Ascension", "Commander-in-Chief, of the Army and Navy"},
		// The Bliss copy's readings, where the other copies differ.
		"gettysburg": {"for which they gave the last full measure of devotion", "a new nation, conceived in Liberty", "battle-field", "under God"},
		// The official record's readings where the UN's web text differs.
		"udhr":    {"life, liberty and the security of person", "the borders of each State", "in co-operation with the United Nations"},
		"privacy": {"to the Director of the Congressional Budget Office", "required under section 552 of this title"},
		// India Code's own readings, where the 2005 Gazette differs.
		"rti": {"all citizens shall have the right to information", "it is expendient", "suomotu", "information which relates to personal information;"},
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
	for _, f := range Documents {
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
		"choose their Speaker":                "constitution: Article. I. › Section. 2.",
		"the common defense":                  "constitution: Preamble",
		"THE UNANIMOUS DECLARATION":           "declaration: ",
		"complete the works of death":         "declaration: ",
		"our British brethren":                "declaration: ",
		"unreasonable searches":               "rights: Amendments › Amendment IV",
		"vice president elect":                "constitution: Amendments › Amendment XX › Section 3.",
		"the Vice President":                  "constitution: Amendments › Amendment XII",
		"chief FOIA officer":                  "foia: § 552. Public information; agency rules, opinions, orders, records, and proceedings",
		"cruel and unusual punishments":       "english-rights: Excessive Bail.",
		"freedom of speech and debates":       "english-rights: Freedom of Speech.",
		"law of the land":                     "magna-carta: ",
		"style of this confederacy":           "articles: Article I.",
		"cooperation with the United Nations": "udhr: Preamble",
		"records maintained on individuals":   "privacy: § 552a. Records maintained on individuals",
		"lawful judgment of his peers":        "petition: III. Reciting 9 Hen. III. M.C. c. 29.",
		"securing the liberty of the subject": "habeas: ",
		"all men and women are created equal": "seneca: ",
		"right to information":                "rti: Chapter II. Right to information and obligations of public authorities",
	} {
		if got := find(q); !got[want] {
			t.Errorf("Find(%q) = %v, want %q among them", q, got, want)
		}
	}
	// The Declaration of Sentiments takes the phrase from the Declaration.
	for d := range find("pursuit of happiness") {
		if !strings.HasPrefix(d, "declaration") && !strings.HasPrefix(d, "seneca") {
			t.Errorf("pursuit of happiness found in %s", d)
		}
	}
	if got := Find(all, "  ,.;  "); got != nil {
		t.Errorf("punctuation alone found %v", got)
	}
	// Whole words only: "arm" is a word of the Articles ("cloath, arm and
	// equip"), and nowhere else, where "Arms" and "Armies" are.
	if got := find("arm"); len(got) != 1 || !got["articles: Article IX."] {
		t.Errorf(`"arm" found %v: whole words only`, got)
	}
}
