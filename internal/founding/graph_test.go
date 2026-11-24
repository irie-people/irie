package founding

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/skills"
)

func graph(t *testing.T) (Graph, map[string]GraphNode) {
	t.Helper()
	all, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	g, err := BuildGraph(all)
	if err != nil {
		t.Fatal(err)
	}
	nodes := map[string]GraphNode{}
	for _, n := range g.Nodes {
		if _, twice := nodes[n.ID]; twice {
			t.Errorf("two nodes are %s", n.ID)
		}
		nodes[n.ID] = n
	}
	return g, nodes
}

// cut gives the words a mark lights: a passage's UTF-16 places from s to e.
func cut(text string, s, e int) string {
	u := utf16.Encode([]rune(text))
	if s < 0 || e > len(u) || s >= e {
		return fmt.Sprintf("<bad mark %d–%d of %d>", s, e, len(u))
	}
	return string(utf16.Decode(u[s:e]))
}

// ties gives the ties of a kind, as from → to.
func ties(g Graph, how string) map[string]bool {
	out := map[string]bool{}
	for _, t := range g.Ties {
		if t.How == how {
			out[t.From+" → "+t.To] = true
		}
	}
	return out
}

// TestTheGraphHoldsTogether: every text has its lane and every idea its
// family; every tie joins two nodes; every part lies inside the part or
// text above it, its blocks inside its text's and inside its parent's;
// and no id is both a text's and an idea's.
func TestTheGraphHoldsTogether(t *testing.T) {
	g, nodes := graph(t)
	lanes := map[string]bool{}
	for _, l := range Lanes {
		lanes[l.ID] = true
	}
	fams := map[string]bool{}
	for _, f := range Families {
		fams[f.ID] = true
	}
	used := map[string]bool{}
	var texts, parts, ideas int
	for _, n := range g.Nodes {
		switch n.Kind {
		case "text":
			texts++
			if !lanes[n.Lane] {
				t.Errorf("%s: lane %q", n.ID, n.Lane)
			}
			used[n.Lane] = true
			if strings.HasPrefix(n.ID, "idea") {
				t.Errorf("a text's id, %s, looks like an idea's", n.ID)
			}
		case "part":
			parts++
			up, ok := nodes[n.Up]
			if !ok || !strings.HasPrefix(n.ID, n.Up+"/") {
				t.Errorf("%s: up %q", n.ID, n.Up)
				continue
			}
			if len(n.Span) != 2 || n.Span[0] >= n.Span[1] {
				t.Errorf("%s: span %v", n.ID, n.Span)
				continue
			}
			if up.Kind == "part" && (n.Span[0] < up.Span[0] || n.Span[1] > up.Span[1]) {
				t.Errorf("%s: span %v outside %s's %v", n.ID, n.Span, up.ID, up.Span)
			}
		case "idea":
			ideas++
			if !strings.HasPrefix(n.ID, "idea/") || !fams[n.Family] {
				t.Errorf("%s: family %q", n.ID, n.Family)
			}
		default:
			t.Errorf("%s: kind %q", n.ID, n.Kind)
		}
	}
	if texts != len(Documents) || ideas != len(Ideas) || parts < 1000 {
		t.Errorf("%d texts, %d parts, %d ideas", texts, parts, ideas)
	}
	for _, l := range Lanes {
		if !used[l.ID] {
			t.Errorf("no text in the lane %s", l.ID)
		}
	}
	for _, tie := range g.Ties {
		if _, ok := nodes[tie.From]; !ok {
			t.Errorf("a %s tie from %s, no node", tie.How, tie.From)
		}
		if _, ok := nodes[tie.To]; !ok {
			t.Errorf("a %s tie to %s, no node", tie.How, tie.To)
		}
		if tie.How == "quotes" && (tie.Link == nil || *tie.Link < 0 || *tie.Link >= len(Links)) {
			t.Errorf("a quotes tie %s → %s names no link", tie.From, tie.To)
		}
	}
}

// TestSpansAreFold: the page lights the words its search found where fold
// found them, so spans splits every passage into fold's words, each at the
// passage's own word.
func TestSpansAreFold(t *testing.T) {
	for _, d := range docs(t) {
		for i, b := range d.Blocks {
			text := b.Text
			if b.Kind == "list" {
				text = strings.Join(b.Items, "\n")
			}
			ws := spans(text)
			got := make([]string, len(ws))
			for j, w := range ws {
				got[j] = w.w
				// A word is found at its own place: the place folds to it
				// (or, for one old word that is two today, to both).
				if f := fold(cut(text, w.s, w.e)); !strings.Contains(" "+f+" ", " "+w.w+" ") {
					t.Fatalf("%s block %d: %q at %d–%d folds to %q", d.ID, i, w.w, w.s, w.e, f)
				}
			}
			if strings.Join(got, " ") != fold(text) {
				t.Fatalf("%s block %d: spans give %q, fold %q", d.ID, i, strings.Join(got, " "), fold(text))
			}
		}
	}
}

// TestTheTextsDivideIntoTheirParts: the paragraphs a text numbers itself
// become its parts, named as the text counts them.
func TestTheTextsDivideIntoTheirParts(t *testing.T) {
	_, nodes := graph(t)
	for id, name := range map[string]string{
		"magna-carta/chapter-1":   "Chapter 1",
		"magna-carta/chapter-39":  "Chapter 39",
		"magna-carta/chapter-63":  "Chapter 63",
		"northwest/section-14":    "Section 14",
		"northwest/article-6":     "Article 6",
		"voting-rights/section-2": "Section 2",
		"civil-rights/title-vii-equal-employment-opportunity/discrimination-because-of-race-color-religion/section-703": "Section 703",
		"foia/552-public-information-agency-rules-opinions/552-a":                                                       "§ 552(a)",
		"privacy/552a-records-maintained-on-individuals/552a-b":                                                         "§ 552a(b)",
		"body-of-liberties/liberty-1":                                                                                   "Liberty 1",
		"body-of-liberties/94-capitall-laws/capital-law-1":                                                              "Capital law 1",
		"rti/chapter-ii-right-to-information-and/section-6":                                                             "Section 6",
		"rti/the-second-schedule/item-1":                                                                                "Item 1",
		"sweden/2":                                                                                                      "§ 2",
		"constitution/article-i/section-9":                                                                              "Section. 9.",
		"rights/amendments/amendment-viii":                                                                              "Amendment VIII",
	} {
		if n, ok := nodes[id]; !ok || n.Name != name {
			t.Errorf("%s: %q, %v", id, n.Name, ok)
		}
	}
	// The Code's subsections go in order, so a roman (i) a level down is
	// no subsection (i).
	var foia []string
	for _, n := range nodes {
		if n.Kind == "part" && n.Doc == "foia" && strings.HasPrefix(n.Name, "§ 552(") {
			foia = append(foia, n.Name)
		}
	}
	slices.Sort(foia)
	if want := "§ 552(a) § 552(b) § 552(c) § 552(d) § 552(e) § 552(f) § 552(g) § 552(h) § 552(i) § 552(j) § 552(k) § 552(l) § 552(m)"; strings.Join(foia, " ") != want {
		t.Errorf("FOIA's subsections: %v", foia)
	}
}

// TestEveryIdeaIsNamed: each idea is named in its own words in two texts
// or more; each tie lights the words that name it; it finds the passages
// it must, and not those where its words mean something else.
func TestEveryIdeaIsNamed(t *testing.T) {
	g, nodes := graph(t)
	byDoc := map[string]ipc.FoundingDoc{}
	for _, d := range g.Docs {
		byDoc[d.ID] = d
	}
	texts := map[string]map[string]bool{}
	for _, tie := range g.Ties {
		if tie.How != "names" {
			continue
		}
		from, idea := nodes[tie.From], nodes[tie.To]
		doc := from.ID
		if from.Kind == "part" {
			doc = from.Doc
		}
		if texts[idea.ID] == nil {
			texts[idea.ID] = map[string]bool{}
		}
		texts[idea.ID][doc] = true
		if len(tie.Marks) == 0 {
			t.Errorf("%s names %s with no words", tie.From, tie.To)
		}
		for _, m := range tie.Marks {
			b := byDoc[doc].Blocks[m[0]]
			if b.Kind != "text" || (from.Kind == "part" && (m[0] < from.Span[0] || m[0] >= from.Span[1])) {
				t.Errorf("%s names %s in block %d, not its own", tie.From, tie.To, m[0])
				continue
			}
			lit := " " + fold(cut(b.Text, m[1], m[2])) + " "
			if !slices.ContainsFunc(idea.Words, func(w string) bool { return strings.Contains(lit, " "+w+" ") }) {
				t.Errorf("%s names %s with %q, none of its words", tie.From, tie.To, cut(b.Text, m[1], m[2]))
			}
		}
	}
	for _, idea := range Ideas {
		if len(texts[idea.ID]) < 2 {
			t.Errorf("%s is named in %d texts", idea.ID, len(texts[idea.ID]))
		}
	}
	names := ties(g, "names")
	for _, want := range []string{
		"rights/amendments/amendment-i → idea/speech",
		"english-rights/freedom-of-speech → idea/speech",
		"rights/amendments/amendment-i → idea/religion",
		"virginia/section-16 → idea/religion",
		"constitution/article-i/section-9 → idea/habeas",
		"northwest/article-2 → idea/habeas",
		"rights/amendments/amendment-vii → idea/jury",
		"magna-carta/chapter-39 → idea/due-process",
		"rights/amendments/amendment-v → idea/due-process",
		"english-rights/excessive-bail-2 → idea/bail",
		"rights/amendments/amendment-viii → idea/punishment",
		"udhr/article-5 → idea/punishment",
		"northwest/article-6 → idea/slavery",
		"constitution/amendments/amendment-xiii/section-1 → idea/slavery",
		"declaration → idea/equality",
		"declaration → idea/consent",
		"declaration → idea/revolution",
		"constitution/amendments/amendment-xix → idea/vote",
		"udhr/article-19 → idea/information",
		"sweden/10 → idea/information",
		"rights/amendments/amendment-ii → idea/arms",
		"rights/amendments/amendment-iii → idea/quartering",
		"rights/amendments/amendment-iv → idea/searches",
		"virginia/section-5 → idea/separation",
	} {
		if !names[want] {
			t.Errorf("no tie %s", want)
		}
	}
	for _, not := range []string{
		"constitution/article-vi → idea/due-process",          // the supreme Law of the Land
		"constitution/article-v → idea/vote",                  // the States' equal Suffrage in the Senate
		"constitution/article-ii/section-1 → idea/disability", // until the Disability be removed
		"crpd/article-44-regional-integration-organizations → idea/vote",
		"foia/552-public-information-agency-rules-opinions/552-a → idea/separation",
	} {
		if names[not] {
			t.Errorf("a tie %s, where the words mean something else", not)
		}
	}
}

// TestTheQuotesPinTheLinks: a quotes tie lights a quotation of its link,
// word for word, in the part it comes from or goes to.
func TestTheQuotesPinTheLinks(t *testing.T) {
	g, nodes := graph(t)
	byDoc := map[string]ipc.FoundingDoc{}
	for _, d := range g.Docs {
		byDoc[d.ID] = d
	}
	for _, tie := range g.Ties {
		if tie.How != "quotes" {
			continue
		}
		part := nodes[tie.From]
		if part.Kind != "part" {
			part = nodes[tie.To]
		}
		qs := quoted(Links[*tie.Link].Why)
		for _, m := range tie.Marks {
			if m[0] < part.Span[0] || m[0] >= part.Span[1] {
				t.Errorf("%s → %s: block %d is outside %s", tie.From, tie.To, m[0], part.ID)
				continue
			}
			if w := cut(byDoc[part.Doc].Blocks[m[0]].Text, m[1], m[2]); !slices.Contains(qs, w) {
				t.Errorf("%s → %s lights %q, which link %d doesn't quote", tie.From, tie.To, w, *tie.Link)
			}
		}
	}
	quotes := ties(g, "quotes")
	for _, want := range []string{
		"virginia/section-9 → english-rights",
		"rights/amendments/amendment-viii → english-rights",
		"rights/amendments/amendment-v → magna-carta",
		"rights → magna-carta/chapter-39",
		"constitution/article-i/section-9 → habeas",
		"constitution/amendments/amendment-xiii/section-1 → northwest",
		"constitution → northwest/article-6",
	} {
		if !quotes[want] {
			t.Errorf("no tie %s", want)
		}
	}
}

// TestTheSameWords: parts of two texts with the very same words are tied,
// to the letter where they are, and spelling aside where they are not.
func TestTheSameWords(t *testing.T) {
	g, _ := graph(t)
	exact := map[string]bool{}
	for _, tie := range g.Ties {
		if tie.How == "same" {
			exact[tie.From+" → "+tie.To] = tie.Exact
		}
	}
	for _, n := range []string{"i", "ii", "iii", "iv", "v", "vi", "vii", "viii", "ix", "x"} {
		if e, ok := exact["rights/amendments/amendment-"+n+" → constitution/amendments/amendment-"+n]; !ok || !e {
			t.Errorf("Amendment %s: tied %v, to the letter %v", strings.ToUpper(n), ok, e)
		}
	}
	// James Madison's second article of 1789, ratified in 1992.
	if _, ok := exact["rights/not-ratified-in-1791/article-the-second → constitution/amendments/amendment-xxvii"]; !ok {
		t.Error("the Article the second isn't the Twenty-seventh Amendment")
	}
	// Virginia's bail is the English Act's, in Virginia's spelling.
	if e, ok := exact["virginia/section-9 → english-rights/excessive-bail-2"]; !ok || e {
		t.Errorf("Virginia's Section 9: tied %v, to the letter %v", ok, e)
	}
}

// TestThePage: the page holds the graph and its own style and script, and
// loads nothing from elsewhere.
func TestThePage(t *testing.T) {
	b, err := Page()
	if err != nil {
		t.Fatal(err)
	}
	page := string(b)
	for _, mark := range []string{"{{style}}", "{{data}}", "{{script}}"} {
		if strings.Contains(page, mark) {
			t.Errorf("the page still has %s", mark)
		}
	}
	for _, want := range []string{`<meta http-equiv="Content-Security-Policy" content="default-src 'none';`, `"format":"` + GraphFormat + `"`, "chartersGraph", "--sidebar:"} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	for _, out := range []string{"<script src", "<link ", "@import", "url(http", "fetch(", "XMLHttpRequest", "WebSocket"} {
		if strings.Contains(page, out) {
			t.Errorf("the page has %q: it must load nothing", out)
		}
	}
	if len(b) > 4<<20 {
		t.Errorf("the page is %d bytes", len(b))
	}
}

// TestMostLinksQuoteBoth: the page says most links quote both texts, so
// more than half of them hold a quotation from each of their two texts.
func TestMostLinksQuoteBoth(t *testing.T) {
	var files map[string][]byte
	for _, s := range skills.Bundled() {
		if s.Name == Skill {
			files = s.Files
		}
	}
	both := 0
	for _, l := range Links {
		from, _ := ByID(l.From)
		to, _ := ByID(l.To)
		var inFrom, inTo bool
		for _, q := range quoted(l.Why) {
			inFrom = inFrom || strings.Contains(string(files[from.File]), q)
			inTo = inTo || strings.Contains(string(files[to.File]), q)
		}
		if inFrom && inTo {
			both++
		}
	}
	if both*2 <= len(Links) {
		t.Errorf("%d of %d links quote both texts: the page's \"most\" is untrue", both, len(Links))
	}
	t.Logf("%d of %d links quote both texts", both, len(Links))
}
