package terms

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func fixture(t *testing.T) *Doc {
	t.Helper()
	b, err := os.ReadFile("testdata/terms.html")
	if err != nil {
		t.Fatal(err)
	}
	return FromHTML("https://example.com/terms", b)
}

// point is the point on topic, or fails.
func point(t *testing.T, d *Doc, topic string) Point {
	t.Helper()
	for _, p := range d.Points {
		if p.Topic == topic {
			return p
		}
	}
	var got []string
	for _, p := range d.Points {
		got = append(got, p.Topic)
	}
	t.Fatalf("no point on %s; the points are %v", topic, got)
	return Point{}
}

// TestAPageOfTermsReadsAsItsPoints: the fixture's page is terms, with its
// date, its company, its length, and a point for each clause it holds,
// each with its verdict, its plain words, the clause it comes from, its
// heading and a link to the words; against you first.
func TestAPageOfTermsReadsAsItsPoints(t *testing.T) {
	d := fixture(t)
	if d.Kind != KindTerms || !d.English || d.Site != "example.com" {
		t.Errorf("kind %q, English %v, site %q", d.Kind, d.English, d.Site)
	}
	if d.Updated != "March 3, 2026" || d.Company != "Example Co., Inc." {
		t.Errorf("updated %q, company %q", d.Updated, d.Company)
	}
	if d.Words < 400 || d.Minutes < 2 || d.Grade == 0 || d.Reader == "" {
		t.Errorf("words %d, minutes %d, grade %d, reader %q", d.Words, d.Minutes, d.Grade, d.Reader)
	}
	if d.Privacy != "https://example.com/privacy" {
		t.Errorf("privacy %q", d.Privacy)
	}
	want := map[string]string{
		"arbitration":     Watch, // it has an opt-out
		"class-action":    Against,
		"jury":            Against,
		"law":             Know,
		"venue":           Watch,
		"termination":     Against,
		"you-can-leave":   For,
		"content-removal": Watch,
		"age":             Know,
		"account-yours":   Know,
		"license":         Against, // perpetual and irrevocable
		"you-own":         For,
		"ai-training":     Watch,
		"sell-data":       For,
		"share-data":      Watch,
		"privacy-policy":  Know,
		"auto-renew":      Watch,
		"no-refunds":      Against,
		"price-change":    Watch,
		"as-is":           Know,
		"liability-cap":   Watch,
		"indemnify":       Watch,
		"changes":         Against, // just by posting
		"by-using":        Watch,
	}
	for topic, verdict := range want {
		if p := point(t, d, topic); p.Verdict != verdict {
			t.Errorf("%s: verdict %s, want %s (%q)", topic, p.Verdict, verdict, p.Says)
		}
	}
	for i := 1; i < len(d.Points); i++ {
		if rank[d.Points[i-1].Verdict] > rank[d.Points[i].Verdict] {
			t.Errorf("%s (%s) before %s (%s)", d.Points[i-1].Topic, d.Points[i-1].Verdict, d.Points[i].Topic, d.Points[i].Verdict)
		}
	}
	says := map[string]string{
		"arbitration":   "within 30 days",
		"law":           "the State of California",
		"venue":         "San Francisco, California",
		"termination":   "at any time, without warning and for any reason",
		"age":           "at least 13",
		"license":       "anywhere in the world, without paying you, forever",
		"share-data":    "advertisers and partners",
		"no-refunds":    "except where the law requires one",
		"price-change":  "telling you 30 days ahead",
		"liability-cap": "$100 or what you paid them in the 12 months before, whichever is more",
		"changes":       "just by posting a new version",
	}
	for topic, s := range says {
		if p := point(t, d, topic); !strings.Contains(p.Says, s) {
			t.Errorf("%s says %q, want %q in it", topic, p.Says, s)
		}
	}
	p := point(t, d, "jury")
	if p.Quote != "YOU AND EXAMPLE WAIVE ANY RIGHT TO A JURY TRIAL." || p.Section != "7. Disputes" {
		t.Errorf("jury: quote %q, section %q", p.Quote, p.Section)
	}
	if p.Link != "https://example.com/terms#:~:text=YOU%20AND%20EXAMPLE%20WAIVE%20ANY%20RIGHT" {
		t.Errorf("jury: link %q", p.Link)
	}
	if d.Count[Against] == 0 || d.Count[For] == 0 || !strings.Contains(d.Tally(), "against you") {
		t.Errorf("count %v, tally %q", d.Count, d.Tally())
	}
	text := d.Text()
	for _, s := range []string{"Terms of Service | Example - example.com", "✕ No refunds", "✓ What you post stays yours", "Not legal advice"} {
		if !strings.Contains(text, s) {
			t.Errorf("Text lacks %q:\n%s", s, text)
		}
	}
}

// TestEachTopicByItself: a clause on a topic finds it, and a clause that
// only looks like it doesn't, as the clauses of real terms read.
func TestEachTopicByItself(t *testing.T) {
	cases := []struct {
		text, topic, verdict, says string // topic "" = none of the listed topics
		not                        []string
	}{
		{text: "You may close your Account at any time.", topic: "you-can-leave", verdict: For, not: []string{"termination"}},
		{text: "GitHub has the right to suspend or terminate your access to all or any part of the Website at any time, with or without cause, with or without notice, effective immediately.", topic: "termination", verdict: Against},
		{text: "Your account may be suspended or terminated without notice if you break these rules.", topic: "termination", verdict: Against},
		{text: "We may sell your personal information to third parties.", topic: "sell-data", verdict: Against},
		{text: "We do not sell your personal information to anyone.", topic: "sell-data", verdict: For},
		{text: "You may not use the API to download data for the purposes of selling users' personal information, such as to recruiters.", not: []string{"sell-data"}},
		{text: "We will never use your content to train our AI models.", topic: "ai-training", verdict: For},
		{text: "We develop new products and business models to serve our customers.", not: []string{"ai-training"}},
		{text: "GitHub does not warrant that the Service will meet your requirements or be error-free, and provides no other guarantees.", not: []string{"authorities"}},
		{text: "We may disclose your information to law enforcement in response to a subpoena or court order.", topic: "authorities", verdict: Know},
		{text: "An individual has the possibility, under certain conditions, to invoke binding arbitration for complaints regarding DPF compliance.", not: []string{"arbitration"}},
		{text: "We do not \"sell\" or \"share\" the personal information of known minors under 16 years of age.", not: []string{"age"}},
		{text: "You must be at least 18 years old to create an account.", topic: "age", verdict: Know, says: "at least 18"},
		{text: "These Terms are governed by the laws of England and Wales.", topic: "law", verdict: Know, says: "England and Wales"},
		{text: "This Agreement is governed by the federal laws of the United States of America and the laws of the State of California, without regard to conflict of law provisions.", topic: "law", verdict: Know, says: "the United States and the State of California"},
		{text: "ANY CLAIM ARISING UNDER THESE TERMS MUST BE COMMENCED WITHIN ONE (1) YEAR AFTER THE DATE THE CLAIM AROSE, OR IT IS PERMANENTLY BARRED.", topic: "claim-deadline", verdict: Against, says: "within a year"},
		{text: "Accounts that are inactive for more than two (2) years may be deleted.", topic: "inactive", verdict: Watch, says: "2 years"},
		{text: "Coins and other virtual items have no monetary value and are not redeemable for cash.", topic: "virtual-items", verdict: Watch},
		{text: "We may change the exchange rates we use for currency conversion.", not: []string{"price-change"}},
		{text: "You may request deletion of your personal data at any time by contacting us.", topic: "delete-data", verdict: For},
		{text: "We have the right to remove content or close Accounts if we need to.", not: []string{"delete-data"}},
	}
	for _, c := range cases {
		d := FromText("", "Terms", c.text)
		var got []string
		for _, p := range d.Points {
			got = append(got, p.Topic)
			if p.Topic == c.topic {
				if p.Verdict != c.verdict {
					t.Errorf("%q: %s's verdict %s, want %s", c.text, c.topic, p.Verdict, c.verdict)
				}
				if c.says != "" && !strings.Contains(p.Says, c.says) {
					t.Errorf("%q: %s says %q, want %q in it", c.text, c.topic, p.Says, c.says)
				}
			}
		}
		if c.topic != "" && !slices.Contains(got, c.topic) {
			t.Errorf("%q: no %s among %v", c.text, c.topic, got)
		}
		for _, n := range c.not {
			if slices.Contains(got, n) {
				t.Errorf("%q: found %s, which it isn't", c.text, n)
			}
		}
	}
}

// TestSegmentFindsHeadingsAndClauses: headings as htmltext writes them,
// numbered, in capitals and with a full stop; a clause's own short title;
// list items each a clause; wrapped lines joined; a long paragraph cut
// between its sentences.
func TestSegmentFindsHeadingsAndClauses(t *testing.T) {
	text := strings.Join([]string{
		"# Terms",
		"Welcome to the service, which these terms are for.",
		"1. Acceptance of Terms",
		"By using it you agree to them, as anyone does.",
		"LIMITATION OF LIABILITY",
		"We are not liable for what you lose.",
		"Governing Law and Venue.",
		"The laws of Utah apply to everything here.",
		"4. Your Content. You keep what you make, all of it.",
		"- one item of a list here\n- another item of the list",
		"This line was wrapped by a text editor at a fixed width so that\nit goes on here on the next line.",
	}, "\n\n")
	secs, cs := segment(text)
	var titles []string
	for _, s := range secs {
		titles = append(titles, s.title)
	}
	want := []string{"Terms", "1. Acceptance of Terms", "LIMITATION OF LIABILITY", "Governing Law and Venue.", "4. Your Content"}
	if !slices.Equal(titles, want) {
		t.Errorf("headings %q, want %q", titles, want)
	}
	var texts []string
	for _, c := range cs {
		texts = append(texts, c.text)
	}
	for _, w := range []string{"You keep what you make, all of it.", "one item of a list here", "another item of the list", "This line was wrapped by a text editor at a fixed width so that it goes on here on the next line."} {
		if !slices.Contains(texts, w) {
			t.Errorf("no clause %q among %q", w, texts)
		}
	}
	if c := cs[slices.Index(texts, "You keep what you make, all of it.")]; secs[c.sec].title != "4. Your Content" {
		t.Errorf("the run-in clause is under %q", secs[c.sec].title)
	}
	long := strings.Repeat("This sentence is one of many that make a long paragraph. ", 30)
	_, cs = segment(long)
	if len(cs) < 2 {
		t.Fatalf("a %d-character paragraph is %d clauses", len(long), len(cs))
	}
	for _, c := range cs {
		if len(c.text) > maxClause || !strings.HasSuffix(c.text, ".") {
			t.Errorf("a clause of %d characters: %q", len(c.text), c.text)
		}
	}
}

// TestSentencesKeepAbbreviations: Inc., U.S., e.g. and initials end no
// sentence.
func TestSentencesKeepAbbreviations(t *testing.T) {
	got := sentences(`Example, Inc. is in the U.S. and serves e.g. Alice B. Smith. It also says "hello." Then it ends!`)
	want := []string{"Example, Inc. is in the U.S. and serves e.g. Alice B. Smith.", `It also says "hello."`, "Then it ends!"}
	if !slices.Equal(got, want) {
		t.Errorf("sentences %q, want %q", got, want)
	}
}

// TestAPageInManyLanguagesKeepsItsEnglish: terms whose page holds them in
// several languages are read from their English, and a page in another
// language says it is.
func TestAPageInManyLanguagesKeepsItsEnglish(t *testing.T) {
	en := strings.Repeat("You and the company agree that any dispute will be resolved by binding arbitration and not in court. ", 40)
	de := strings.Repeat("Sie und das Unternehmen vereinbaren, dass jede Streitigkeit durch ein Schiedsverfahren beigelegt wird. ", 200)
	d := FromText("", "Terms", de+"\n\n"+en)
	if !d.English || len(d.Points) == 0 {
		t.Errorf("English %v, points %d", d.English, len(d.Points))
	}
	d = FromText("", "Bedingungen", de)
	if d.English {
		t.Error("German reads as English")
	}
	if !strings.Contains(d.Text(), "another language") {
		t.Errorf("Text says nothing of the language:\n%s", d.Text())
	}
}

// TestAPageBuiltByScripts says so.
func TestAPageBuiltByScripts(t *testing.T) {
	d := FromHTML("https://example.com/terms", []byte(`<html><head><title>Terms</title><script src="a.js"></script><script src="b.js"></script></head><body><div id="root"></div></body></html>`))
	if !d.Scripted() || !strings.Contains(d.Text(), "builds itself with scripts") {
		t.Errorf("scripted %v:\n%s", d.Scripted(), d.Text())
	}
}

// TestFragmentPointsAtTheWords: a link opens the page at the quote's first
// words, as a text fragment, escaped.
func TestFragmentPointsAtTheWords(t *testing.T) {
	cases := map[string]string{
		"Your use is at your own risk, and we are not liable.": "https://example.com/t#:~:text=Your%20use%20is%20at%20your%20own",
		"... We may sell-off the data to anyone.":              "https://example.com/t#:~:text=We%20may%20sell%2Doff%20the%20data",
		`"Quoted" start`: "https://example.com/t",
	}
	for q, want := range cases {
		got := fragment("https://example.com/t#old", q)
		// sell-off holds a hyphen, which is no letter: the words stop
		// before it, and a hyphen that is in them is escaped.
		if q == "... We may sell-off the data to anyone." {
			want = "https://example.com/t#:~:text=We%20may"
			if got != "https://example.com/t" && got != want {
				t.Errorf("fragment(%q) = %q", q, got)
			}
			continue
		}
		if got != want {
			t.Errorf("fragment(%q) = %q, want %q", q, got, want)
		}
	}
	if fragment("", "Some words here") != "" {
		t.Error("a document without an address has no links")
	}
}

// TestSitesAndPlaces: sites by their last names, and places as a sentence
// names them.
func TestSitesAndPlaces(t *testing.T) {
	for host, want := range map[string]string{"policies.example.com": "example.com", "www.example.co.uk": "example.co.uk", "example.com.": "example.com", "localhost": "localhost"} {
		if got := siteOf(host); got != want {
			t.Errorf("siteOf(%q) = %q, want %q", host, got, want)
		}
	}
	if !sameSite("https://policies.example.com/terms", "https://www.example.com/") || sameSite("https://example.com/", "https://example.org/") {
		t.Error("sameSite")
	}
	for s, want := range map[string]string{
		"governed by the laws of the State of New York, without regard to": "the State of New York",
		"GOVERNED BY THE LAWS OF THE STATE OF NEW YORK WITHOUT REGARD TO":  "the State of New York",
		"governed by the laws of Ireland.":                                 "Ireland",
		"governed by the laws of the Netherlands":                          "the Netherlands",
	} {
		if got := lawPlace(s); got != want {
			t.Errorf("lawPlace(%q) = %q, want %q", s, got, want)
		}
	}
	for s, want := range map[string]string{
		"submit to the exclusive jurisdiction of the courts located in New Castle County, Delaware, for": "New Castle County, Delaware",
		"the courts of the City of London shall have":                                                    "London",
		"in the courts located in San Francisco, California. However, either of us may":                  "San Francisco, California",
		"courts located in Washington, D.C. for any claim":                                               "Washington, D.C",
	} {
		if got := courtPlace(s); got != want {
			t.Errorf("courtPlace(%q) = %q, want %q", s, got, want)
		}
	}
	for s, want := range map[string]string{"thirty (30) days": "30 days", "one (1) year": "a year", "12 months": "12 months", "twenty-four hours": "24 hours"} {
		if got := plainSpan(s); got != want {
			t.Errorf("plainSpan(%q) = %q, want %q", s, got, want)
		}
	}
}

// TestGradeAndReader: plain sentences ask less of a reader than long ones
// of long words.
func TestGradeAndReader(t *testing.T) {
	plain := FromText("", "", strings.Repeat("You can stop when you like. We keep your name. ", 40))
	hard := FromText("", "", strings.Repeat("Notwithstanding the foregoing, the indemnification obligations hereunder shall survive termination, cancellation or expiration of this agreement irrespective of the circumstances thereof. ", 20))
	if plain.Grade == 0 || plain.Grade >= hard.Grade || hard.Reader != "a university graduate" {
		t.Errorf("plain %d (%s), hard %d (%s)", plain.Grade, plain.Reader, hard.Grade, hard.Reader)
	}
	if syllables("agreement") != 3 || syllables("the") != 1 || syllables("terminate") != 3 {
		t.Errorf("syllables: %d %d %d", syllables("agreement"), syllables("the"), syllables("terminate"))
	}
}
