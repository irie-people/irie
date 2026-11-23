package founding

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
)

// The charters as a graph to explore, the Charters Graph page's (DESIGN
// §4.17, docs/proposals/founding-documents.md, "The explorer"): the texts,
// laid out by when and where they were made; their parts, the articles,
// sections, amendments and chapters the texts divide into; the rights and
// ideas they name (ideas.go); and the ties between them. Every tie can be
// checked against the texts: a part is a heading of ours over the text's
// own words, or a paragraph the text itself numbers; an idea is named
// where its words are in a passage, which the tie quotes; and a link's
// quotations, which the tests hold to the texts word for word, pin it to
// the passages it quotes.

// GraphFormat is the form of the graph the page reads, as the product
// names it.
const GraphFormat = brand.Slug + ".charters-graph/1"

// Graph is the charters as the page draws them.
type Graph struct {
	Format string `json:"format"`
	// Lanes are the timeline's rows, top to bottom; Families the groups
	// the ideas fall in.
	Lanes    []Lane      `json:"lanes"`
	Families []Family    `json:"families"`
	Nodes    []GraphNode `json:"nodes"`
	// Links are how the texts bear on one another, as links.go gives
	// them; ties of kind "quotes" name one by its index here.
	Links []ipc.FoundingLink `json:"links"`
	Ties  []GraphTie         `json:"ties"`
	// Docs are the texts whole, for the page to show the words.
	Docs []ipc.FoundingDoc `json:"docs"`
	// Spellings are the old spellings the search folds to today's, as
	// Find does, so the page's search finds what the op's would.
	Spellings map[string]string `json:"spellings"`
}

// Lane is a row of the page's timeline: where the texts in it were made.
type Lane struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Lanes are the timeline's rows, top to bottom: England first, where the
// oldest texts were made, then the colonies and states and the union they
// formed, then Europe, the United Nations and India.
var Lanes = []Lane{
	{ID: "england", Name: "England and the United Kingdom"},
	{ID: "colonies", Name: "American colonies and states"},
	{ID: "united-states", Name: "The United States"},
	{ID: "europe", Name: "Europe"},
	{ID: "united-nations", Name: "The United Nations"},
	{ID: "india", Name: "India"},
}

// laneOf says where each text was made, by its id.
var laneOf = map[string]string{
	"magna-carta": "england", "magna-carta-1297": "england", "petition": "england", "habeas": "england",
	"english-rights": "england", "uk-foia": "england",
	"mayflower": "colonies", "body-of-liberties": "colonies", "virginia": "colonies", "pennsylvania": "colonies",
	"massachusetts": "colonies", "virginia-statute": "colonies",
	"declaration": "united-states", "articles": "united-states", "northwest": "united-states",
	"constitution": "united-states", "rights": "united-states", "seneca": "united-states",
	"emancipation": "united-states", "gettysburg": "united-states", "civil-rights": "united-states",
	"voting-rights": "united-states", "foia": "united-states", "privacy": "united-states", "ada": "united-states",
	"sweden": "europe", "french": "europe", "echr": "europe",
	"un-charter": "united-nations", "udhr": "united-nations", "refugees": "united-nations", "cerd": "united-nations",
	"iccpr": "united-nations", "icescr": "united-nations", "cedaw": "united-nations", "crc": "united-nations",
	"crpd": "united-nations",
	"rti":  "india",
}

// GraphNode is a node of the graph: a text, a part of one, or an idea.
type GraphNode struct {
	ID   string `json:"id"`
	Kind string `json:"kind"` // text, part or idea
	Name string `json:"name"`
	// A text's lane; an idea's family.
	Lane   string `json:"lane,omitempty"`
	Family string `json:"family,omitempty"`
	// A part's text, and the part or text it is in.
	Doc string `json:"doc,omitempty"`
	Up  string `json:"up,omitempty"`
	// A part's blocks in its text: from the first, its heading or its
	// numbered paragraph, up to the second.
	Span []int `json:"span,omitempty"`
	// An idea's words: the phrases that name it, as fold writes them.
	Words []string `json:"words,omitempty"`
}

// GraphTie is an edge of the graph, other than the links between texts.
type GraphTie struct {
	From string `json:"from"`
	To   string `json:"to"`
	// How is "in", a part in the part or text above it; "names", a part
	// or text naming an idea, where the idea's words are; "quotes", a
	// part holding words that one of the links quotes, from the part to
	// the other text (or from the text to the part, when the part is in
	// the earlier one); or "same", a part with the very same words as a
	// part of an earlier text, as the Bill of Rights' ten amendments are
	// the Constitution's first ten.
	How string `json:"how"`
	// Link is the link a "quotes" tie pins, by its index in Links.
	Link *int `json:"link,omitempty"`
	// Exact says a "same" tie's words are the same to the letter; without
	// it, they are the same once spelling and punctuation are set aside.
	Exact bool `json:"exact,omitempty"`
	// Marks are where the words are: a block of From's text (or, for a
	// "quotes" tie from a text, of To's), and the words' start and end in
	// it, in UTF-16 code units, as the page's strings count.
	Marks [][3]int `json:"marks,omitempty"`
}

// partRule finds the paragraphs a text numbers itself, which become parts
// where no heading of ours divides it: Magna Carta's chapters, the
// Ordinance's sections and articles, the statutes' sections.
type partRule struct {
	re *regexp.Regexp
	// name names the part from the number the rule found and the heading
	// over it, as the text has it, "" where there is none.
	name func(n, over string) string
	// letters takes subsections lettered in order, (a), (b), (c), as the
	// United States Code letters them, and only in order, since (i) is
	// also a roman numeral one level down.
	letters bool
}

func named(prefix string) func(n, over string) string {
	return func(n, _ string) string { return prefix + n }
}

var (
	numbered  = regexp.MustCompile(`^(\d+[A-Z]?)\. `)
	usSection = regexp.MustCompile(`^SEC\. (\d+)\. `)
	lettered  = regexp.MustCompile(`^\(([a-z])\)`)
	nwSection = regexp.MustCompile(`^\*\*Sec(?:tion|\.)? ?(\d+)\.\*\*`)
	nwArticle = regexp.MustCompile(`^\*\*Art\. (\d+)\.\*\*`)
	partRules = map[string][]partRule{
		"magna-carta": {{re: numbered, name: named("Chapter ")}},
		// The Body of Liberties numbers its hundred liberties, and again
		// the capital laws of liberty 94 and the churches' liberties of 95.
		"body-of-liberties": {{re: numbered, name: func(n, over string) string {
			switch {
			case strings.HasPrefix(over, "94."):
				return "Capital law " + n
			case strings.HasPrefix(over, "95."):
				return "Church liberty " + n
			}
			return "Liberty " + n
		}}},
		"sweden":        {{re: numbered, name: named("§ ")}},
		"northwest":     {{re: nwSection, name: named("Section ")}, {re: nwArticle, name: named("Article ")}},
		"voting-rights": {{re: usSection, name: named("Section ")}},
		"civil-rights":  {{re: usSection, name: named("Section ")}},
		"ada":           {{re: usSection, name: named("Section ")}},
		// India's Act numbers its sections, and its schedules their items.
		"rti": {{re: numbered, name: func(n, over string) string {
			if strings.Contains(over, "Schedule") {
				return "Item " + n
			}
			return "Section " + n
		}}},
		"foia":    {{re: lettered, name: named("§ 552"), letters: true}},
		"privacy": {{re: lettered, name: named("§ 552a"), letters: true}},
	}
	// quiet are parts whose words name no idea: the UK Act's schedules,
	// which list the public bodies it covers and amend other Acts, so
	// that the Department of Health isn't a passage about health.
	quiet = []string{"uk-foia/schedule-"}
)

// Level of a part found by a rule: below every heading.
const ruleLevel = 5

// BuildGraph makes the graph from the texts, as Load gives them, the
// links and the ideas.
func BuildGraph(docs []ipc.FoundingDoc) (Graph, error) {
	g := Graph{Format: GraphFormat, Lanes: Lanes, Families: Families, Links: Links, Docs: docs, Spellings: oldSpellings}
	byID := map[string]ipc.FoundingDoc{}
	// same finds parts with the very same words in two texts: the words of
	// each part long enough to say something, folded, and how deep the
	// part lies, to the first part that has them, so a part is paired
	// with its like (a Part with a Part, an Article with an Article).
	same := map[string]GraphNode{}
	// partOf is, for each text, the part each block is in: the innermost
	// part, or the text itself before its first part.
	partOf := map[string][]string{}
	for _, d := range docs {
		lane, ok := laneOf[d.ID]
		if !ok {
			return g, fmt.Errorf("%s has no lane", d.ID)
		}
		byID[d.ID] = d
		g.Nodes = append(g.Nodes, GraphNode{ID: d.ID, Kind: "text", Name: d.Short, Lane: lane})
		parts, of := splitParts(d)
		partOf[d.ID] = of
		for _, p := range parts {
			g.Nodes = append(g.Nodes, p)
			g.Ties = append(g.Ties, GraphTie{From: p.ID, To: p.Up, How: "in"})
			w, raw := partWords(d, p)
			if len(w) < 80 {
				continue
			}
			key := fmt.Sprintf("%d|%s", strings.Count(p.ID, "/"), w)
			if first, ok := same[key]; ok && first.Doc != p.Doc {
				_, firstRaw := partWords(byID[first.Doc], first)
				g.Ties = append(g.Ties, GraphTie{From: p.ID, To: first.ID, How: "same", Exact: raw == firstRaw})
			} else if !ok {
				same[key] = p
			}
		}
	}
	for i, l := range Links {
		from, okFrom := byID[l.From]
		to, okTo := byID[l.To]
		if !okFrom || !okTo {
			return g, fmt.Errorf("the link %s → %s names a text the graph lacks", l.From, l.To)
		}
		g.Ties = append(g.Ties, quoteTies(i, l, from, to, partOf)...)
	}
	for _, idea := range Ideas {
		g.Nodes = append(g.Nodes, GraphNode{ID: idea.ID, Kind: "idea", Name: idea.Name, Family: idea.Family, Words: idea.folded()})
	}
	for _, d := range docs {
		g.Ties = append(g.Ties, ideaTies(d, partOf[d.ID])...)
	}
	return g, nil
}

// splitParts reads a text's parts from its headings, h2 to h4, and the
// paragraphs it numbers itself where partRules says so, each part running
// to the next heading or numbered paragraph at its own level or above. It
// gives the parts in order and, for each block, the part it is in.
func splitParts(d ipc.FoundingDoc) ([]GraphNode, []string) {
	type open struct {
		i     int // the part's index in parts
		level int
	}
	var (
		parts []GraphNode
		stack []open
		of    = make([]string, len(d.Blocks))
		used  = map[string]bool{}
		next  = byte('a') // the next letter a lettered rule takes
		over  string      // the innermost heading's name
	)
	closeTo := func(level, at int) {
		for len(stack) > 0 && stack[len(stack)-1].level >= level {
			parts[stack[len(stack)-1].i].Span[1] = at
			stack = stack[:len(stack)-1]
		}
	}
	start := func(name string, level, at int) {
		closeTo(level, at)
		up := d.ID
		if len(stack) > 0 {
			up = parts[stack[len(stack)-1].i].ID
		}
		id := up + "/" + slug(name)
		for n := 2; used[id]; n++ {
			id = fmt.Sprintf("%s/%s-%d", up, slug(name), n)
		}
		used[id] = true
		parts = append(parts, GraphNode{ID: id, Kind: "part", Name: name, Doc: d.ID, Up: up, Span: []int{at, at + 1}})
		stack = append(stack, open{i: len(parts) - 1, level: level})
	}
	for i, b := range d.Blocks {
		switch b.Kind {
		case "h2", "h3", "h4":
			over = strings.ReplaceAll(b.Text, "**", "")
			start(over, int(b.Kind[1]-'0'), i)
			next = 'a'
		case "text":
			for _, r := range partRules[d.ID] {
				m := r.re.FindStringSubmatch(b.Text)
				if m == nil || r.letters && m[1][0] != next {
					continue
				}
				if r.letters {
					next++
					m[1] = "(" + m[1] + ")"
				}
				start(r.name(m[1], over), ruleLevel, i)
				break
			}
		}
		if len(stack) > 0 {
			of[i] = parts[stack[len(stack)-1].i].ID
		} else {
			of[i] = d.ID
		}
	}
	closeTo(0, len(d.Blocks))
	return parts, of
}

// partWords are a part's own words, the document's: its passages of text,
// without our headings, notes or the lists of names, folded and as they
// are.
func partWords(d ipc.FoundingDoc, p GraphNode) (folded, raw string) {
	var f, r strings.Builder
	for _, bl := range d.Blocks[p.Span[0]:p.Span[1]] {
		if bl.Kind == "text" {
			f.WriteString(fold(bl.Text))
			f.WriteByte('\n')
			r.WriteString(strings.Join(strings.Fields(bl.Text), " "))
			r.WriteByte('\n')
		}
	}
	return f.String(), r.String()
}

// slug makes a part's name a piece of its id: its words in lower case,
// joined by hyphens, the first six of them, so "Article. I." is article-i
// and "Section. 8." section-8.
func slug(name string) string {
	words := strings.Fields(fold(name))
	if len(words) > 6 {
		words = words[:6]
	}
	if len(words) == 0 {
		return "part"
	}
	return strings.Join(words, "-")
}

// quoteTies pins link i to the passages it quotes: each part of the two
// texts that holds one of the link's quotations is tied to the other text.
func quoteTies(i int, l ipc.FoundingLink, from, to ipc.FoundingDoc, partOf map[string][]string) []GraphTie {
	type at struct{ doc, part string }
	found := map[at][][3]int{}
	var order []at
	for _, q := range quoted(l.Why) {
		for _, d := range []ipc.FoundingDoc{from, to} {
			for bi, b := range d.Blocks {
				if b.Kind != "text" {
					continue
				}
				off := strings.Index(b.Text, q)
				if off < 0 {
					continue
				}
				k := at{d.ID, partOf[d.ID][bi]}
				if _, seen := found[k]; !seen {
					order = append(order, k)
				}
				s := units(b.Text[:off])
				found[k] = append(found[k], [3]int{bi, s, s + units(q)})
			}
		}
	}
	var ties []GraphTie
	for _, k := range order {
		if k.part == k.doc {
			continue // the text's words before its first part: the link itself says it
		}
		t := GraphTie{How: "quotes", Link: &i, Marks: found[k]}
		if k.doc == from.ID {
			t.From, t.To = k.part, to.ID
		} else {
			t.From, t.To = from.ID, k.part
		}
		ties = append(ties, t)
	}
	return ties
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

// ideaTies ties each part of a text, or the text where no part holds the
// words, to each idea whose words are in its own passages: the document's
// words, never our headings, notes or the lists of names.
func ideaTies(d ipc.FoundingDoc, partOf []string) []GraphTie {
	type key struct{ from, idea string }
	marks := map[key][][3]int{}
	var order []key
	for bi, b := range d.Blocks {
		if b.Kind != "text" || slices.ContainsFunc(quiet, func(q string) bool { return strings.HasPrefix(partOf[bi], q) }) {
			continue
		}
		ws := spans(b.Text)
		for _, idea := range Ideas {
			for _, m := range idea.find(ws) {
				k := key{partOf[bi], idea.ID}
				if _, seen := marks[k]; !seen {
					order = append(order, k)
				}
				marks[k] = append(marks[k], [3]int{bi, m[0], m[1]})
			}
		}
	}
	ties := make([]GraphTie, 0, len(order))
	for _, k := range order {
		ties = append(ties, GraphTie{From: k.from, To: k.idea, How: "names", Marks: marks[k]})
	}
	return ties
}

// span is a word of a passage as fold gives it, with where it is in the
// passage, in UTF-16 code units.
type span struct {
	w    string
	s, e int
}

// spans splits a passage into its words as fold does, each with its place,
// so a match on folded words can light the passage's own: an old spelling
// is today's word at the old word's place, and one that is two words today
// ("anything" is any thing) gives both at it.
func spans(text string) []span {
	var (
		out   []span
		b     strings.Builder
		start = -1
		at    = 0 // the place, in UTF-16 code units, of the rune being read
	)
	flush := func(end int) {
		if b.Len() > 0 {
			w := b.String()
			if today, ok := oldSpellings[w]; ok {
				for _, t := range strings.Fields(today) {
					out = append(out, span{t, start, end})
				}
			} else {
				out = append(out, span{w, start, end})
			}
		}
		b.Reset()
		start = -1
	}
	for _, r := range text {
		n := utf16.RuneLen(r)
		switch lower := unicode.ToLower(r); {
		case r == '\'' || r == '’':
		case r == '&':
			flush(at)
			out = append(out, span{"and", at, at + n})
		case unicode.IsLetter(lower) || unicode.IsDigit(lower):
			if start < 0 {
				start = at
			}
			b.WriteString(strings.ToLower(string(r)))
		default:
			flush(at)
		}
		at += n
	}
	flush(at)
	return out
}

// units is how many UTF-16 code units s takes, as a page's string counts.
func units(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// Idea is a right or idea the texts name, found where its words are.
type Idea struct {
	ID     string
	Name   string
	Family string
	// Words are phrases that name it, each matched as whole words,
	// whatever their spelling or capitals, as Find matches.
	Words []string
	// Not are phrases in which its words name something else, such as
	// the Senate's "equal Suffrage": a match inside one of them is none.
	Not []string
}

// Family is a group of ideas.
type Family struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// folded gives the idea's words as fold writes them.
func (idea Idea) folded() []string {
	out := make([]string, 0, len(idea.Words))
	for _, w := range idea.Words {
		if f := fold(w); f != "" && !slices.Contains(out, f) {
			out = append(out, f)
		}
	}
	return out
}

// find gives where the idea's words are in a passage's words, each the
// start of its first word and the end of its last, in order, overlapping
// matches merged, leaving out those inside one of its Not phrases.
func (idea Idea) find(ws []span) [][2]int {
	var hits, not [][2]int
	for _, phrase := range idea.folded() {
		hits = append(hits, phraseAt(ws, phrase)...)
	}
	for _, phrase := range idea.Not {
		not = append(not, phraseAt(ws, fold(phrase))...)
	}
	hits = slices.DeleteFunc(hits, func(h [2]int) bool {
		return slices.ContainsFunc(not, func(n [2]int) bool { return n[0] <= h[0] && h[1] <= n[1] })
	})
	slices.SortFunc(hits, func(a, b [2]int) int { return a[0] - b[0] })
	var merged [][2]int
	for _, h := range hits {
		if n := len(merged); n > 0 && h[0] <= merged[n-1][1] {
			merged[n-1][1] = max(merged[n-1][1], h[1])
			continue
		}
		merged = append(merged, h)
	}
	return merged
}

// phraseAt gives where a folded phrase's words are in a passage's words,
// each the start of its first word and the end of its last.
func phraseAt(ws []span, phrase string) [][2]int {
	p := strings.Fields(phrase)
	if len(p) == 0 {
		return nil
	}
	var hits [][2]int
	for i := 0; i+len(p) <= len(ws); i++ {
		ok := true
		for j, w := range p {
			if ws[i+j].w != w {
				ok = false
				break
			}
		}
		if ok {
			hits = append(hits, [2]int{ws[i].s, ws[i+len(p)-1].e})
		}
	}
	return hits
}
