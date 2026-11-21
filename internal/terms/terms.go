// Package terms reads a website's terms of service, or its privacy policy,
// and says in plain words what they mean for the person agreeing to them:
// the points that matter, each with the clause it comes from, and answers
// to their questions about them (docs/proposals/browser-extension.md).
//
// It is code, not a model: a catalog of the clauses such documents hold
// (topics.go), each found by patterns and said in words written for it,
// and a search of the document's own words for questions. Nothing is sent
// anywhere and no tokens are spent. A local model, when the person asks
// for one, only words an answer from the passages the code found
// (model.go).
//
// What it reads is untrusted: a page may be hostile. Every pattern runs in
// time linear in its input (Go's regexp), the document is cut to a size,
// and nothing in it is followed but the address of a page the person asked
// for.
package terms

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/irie-people/irie/internal/brand"
	"math"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/irie-people/irie/internal/htmltext"
)

// The kinds of document.
const (
	KindTerms   = "terms"   // terms of service, of use, a user agreement
	KindPrivacy = "privacy" // a privacy policy or notice
	KindOther   = "other"   // some other page
)

// MaxText is the most text a document is read to: about 150,000 words,
// far more than the longest terms.
const MaxText = 1 << 20

// Doc is a document read: what it is, how long, and its points.
type Doc struct {
	ID      string `json:"id"`
	URL     string `json:"url,omitempty"`
	Title   string `json:"title"`
	Site    string `json:"site,omitempty"`
	Kind    string `json:"kind"`
	Company string `json:"company,omitempty"`
	Updated string `json:"updated,omitempty"`
	// English is false when the document reads as another language, which
	// the patterns don't know.
	English bool `json:"english"`
	Words   int  `json:"words"`
	Minutes int  `json:"minutes"`
	// Grade is the years of school its sentences ask of a reader
	// (Flesch-Kincaid), and Reader says who that is.
	Grade  int     `json:"grade"`
	Reader string  `json:"reader,omitempty"`
	Points []Point `json:"points"`
	// Missing are things such documents usually say that this one
	// doesn't.
	Missing []Gap `json:"missing,omitempty"`
	// Privacy is the address of the privacy policy the terms link to.
	Privacy string `json:"privacy,omitempty"`
	// Count is how many points there are of each verdict.
	Count map[string]int `json:"count"`

	secs    []section
	clauses []clause
	index   *index
	links   []Link
	// scripted is Scripted's answer.
	scripted bool
	// head is the start of the page's whole text, menus and all, where
	// its date and its company may be.
	head string
}

// Scripted says whether a page built itself with scripts, so its own
// HTML holds next to nothing to read: a browser shows what it builds,
// and the extension reads that.
func (d *Doc) Scripted() bool { return d.scripted }

// Point is one thing the document means for you.
type Point struct {
	Topic   string `json:"topic"`
	Group   string `json:"group"`
	Title   string `json:"title"`
	Verdict string `json:"verdict"`
	Says    string `json:"says"`
	// Quote is the clause it comes from, Section its heading, and Link the
	// document's address pointing at the words.
	Quote   string `json:"quote"`
	Section string `json:"section,omitempty"`
	Link    string `json:"link,omitempty"`
	// Also is how many more passages are on the same topic.
	Also int `json:"also,omitempty"`

	clause int
}

// Gap is something the document doesn't say.
type Gap struct {
	Topic string `json:"topic"`
	Says  string `json:"says"`
}

// FromHTML reads a page of terms: its address, which may be "", and its
// HTML.
func FromHTML(address string, page []byte) *Doc {
	var base *url.URL
	if u, err := url.Parse(address); err == nil && u.IsAbs() {
		base = u
	}
	p := htmltext.Read(page, base)
	text := p.Text
	// htmltext puts a page's main part first, then a rule, then the rest:
	// menus and footers, which are no part of the terms.
	full := text
	if main, _, ok := strings.Cut(text, "\n\n---\n\n"); ok && words(main) >= 300 {
		text = main
	}
	d := read(address, p.Title, text)
	d.scripted = words(full) < 80 && strings.Count(strings.ToLower(string(page[:min(len(page), 1<<20)])), "<script") >= 2
	if len(full) > 40000 {
		full = full[:40000]
	}
	d.head = normalise(full)
	d.links = linksOf(p.Links)
	for _, l := range d.links {
		if privacyLink.MatchString(l.Text) && sameSite(l.URL, address) {
			d.Privacy = l.URL
			break
		}
	}
	d.analyse()
	return d
}

// FromText reads terms written as plain text or Markdown.
func FromText(address, title, text string) *Doc {
	d := read(address, title, text)
	d.analyse()
	return d
}

func read(address, title, text string) *Doc {
	if len(text) > MaxText {
		text = text[:MaxText]
	}
	text = normalise(text)
	d := &Doc{URL: address, Title: squash(normalise(title)), Count: map[string]int{}}
	if u, err := url.Parse(address); err == nil {
		d.Site = strings.TrimPrefix(u.Hostname(), "www.")
	}
	d.secs, d.clauses = segment(text)
	d.English = english(text)
	if !d.English {
		// A page may hold the terms in many languages, as tabs: keep the
		// English, which the patterns know, when there is enough of it.
		var en []clause
		n := 0
		for _, c := range d.clauses {
			if english(c.text) {
				en = append(en, c)
				n += words(c.text)
			}
		}
		if n >= 300 {
			d.clauses, d.English = en, true
		}
	}
	d.head = text[:min(len(text), 40000)]
	sum := sha256.Sum256([]byte(address + "\x00" + text))
	d.ID = hex.EncodeToString(sum[:8])
	return d
}

// analyse finds the document's kind, its numbers and its points.
func (d *Doc) analyse() {
	var all strings.Builder
	for _, c := range d.clauses {
		all.WriteString(c.text)
		all.WriteByte(' ')
	}
	body := all.String()
	d.Words = words(body)
	d.Minutes = max(1, int(math.Ceil(float64(d.Words)/readingSpeed)))
	d.Grade = grade(d.clauses)
	d.Reader = reader(d.Grade)
	d.Kind = d.kind(body)
	if d.Title == "" && len(d.secs) > 0 {
		d.Title = d.secs[0].title
	}
	d.Updated = cmp.Or(updated(d.head), updated(body))
	d.Company = company(d.clauses, body)
	d.index = newIndex(d.clauses)
	d.Points = []Point{}

	for _, t := range catalog {
		if p, ok := d.point(t); ok {
			d.Points = append(d.Points, p)
		} else if t.expect != nil && t.expect(d) && !d.covered(t) {
			d.Missing = append(d.Missing, Gap{Topic: t.id, Says: t.missing})
		}
	}
	order := map[string]int{}
	for i, t := range catalog {
		order[t.id] = i
	}
	slices.SortStableFunc(d.Points, func(a, b Point) int {
		if r := rank[a.Verdict] - rank[b.Verdict]; r != 0 {
			return r
		}
		return order[a.Topic] - order[b.Topic]
	})
	for _, p := range d.Points {
		d.Count[p.Verdict]++
	}
}

// covered says whether a topic's opposite or a near topic is in the
// points, so its absence isn't worth a line: no refunds covers refunds.
func (d *Doc) covered(t *topic) bool {
	near := map[string][]string{
		"no-refunds":     {"refunds"},
		"refunds":        {"no-refunds"},
		"you-can-leave":  {"termination"},
		"privacy-policy": {},
	}
	for _, p := range d.Points {
		if p.Topic == t.id || slices.Contains(near[t.id], p.Topic) {
			return true
		}
	}
	return false
}

// scored is a clause and how well it shows a topic.
type scored struct {
	i     int
	score float64
}

// point finds what the document says on a topic, if anything.
func (d *Doc) point(t *topic) (Point, bool) {
	var hits, negs []scored
	for i, c := range d.clauses {
		s := score(t, c.text)
		if s == 0 {
			continue
		}
		if t.head != nil && c.sec >= 0 && t.head.MatchString(d.secs[c.sec].title) {
			s++
		}
		if s < t.need {
			continue
		}
		if t.neg != nil && t.neg.MatchString(c.text) {
			negs = append(negs, scored{i, s})
			continue
		}
		hits = append(hits, scored{i, s})
	}
	best := func(xs []scored) []scored {
		slices.SortStableFunc(xs, func(a, b scored) int {
			switch {
			case a.score > b.score:
				return -1
			case a.score < b.score:
				return 1
			}
			return a.i - b.i
		})
		return xs
	}
	hits, negs = best(hits), best(negs)
	title, verdict, says := t.title, t.verdict, t.says
	use := hits
	switch {
	case len(hits) == 0 && len(negs) == 0:
		return Point{}, false
	case len(hits) == 0:
		// It only says the opposite: we don't sell your data.
		title, verdict, says = t.flip.title, t.flip.verdict, t.flip.says
		use = negs
	case len(negs) > 0 && t.flip != nil:
		// It says both, such as "we share your data with partners" and
		// "we don't share it with advertisers": what it may do comes
		// first, and the other is noted.
		m := d.match(hits, t.verdict)
		p := d.newPoint(t, hits, t.title, Watch, m)
		p.Says = says(m) + " Another passage limits that in some way: read both."
		p.Verdict = Watch
		return p, true
	}
	m := d.match(use, verdict)
	p := d.newPoint(t, use, title, verdict, m)
	p.Says = says(m)
	p.Verdict = m.verdict
	return p, true
}

func (d *Doc) match(xs []scored, verdict string) *match {
	m := &match{verdict: verdict, doc: d}
	for _, x := range xs {
		m.texts = append(m.texts, d.clauses[x.i].text)
	}
	m.text = m.texts[0]
	return m
}

func (d *Doc) newPoint(t *topic, xs []scored, title, verdict string, m *match) Point {
	c := d.clauses[xs[0].i]
	p := Point{Topic: t.id, Group: t.group, Title: title, Verdict: verdict, clause: xs[0].i, Also: len(xs) - 1}
	p.Quote = quote(t, c.text)
	if c.sec >= 0 {
		p.Section = d.secs[c.sec].title
	}
	p.Link = fragment(d.URL, p.Quote)
	return p
}

// score is how well text shows the topic: the weights of the patterns it
// matches, or 0 when a pattern of what it isn't matches.
func score(t *topic, text string) float64 {
	if t.not != nil && t.not.MatchString(text) {
		return 0
	}
	s := 0.0
	for _, f := range t.find {
		if f.matches(text) {
			s += f.w
		}
	}
	// A row of a table, such as a summary's, says less than the clause
	// it sums up, and a line of a few words less than a clause: the clause
	// wins a tie.
	if s > 0 && strings.Contains(text, " | ") {
		s -= 0.5
	}
	if s > 0 && words(text) >= 20 {
		s += 0.25
	}
	return s
}

// maxQuote is the most characters of a clause a point quotes.
const maxQuote = 420

// quote is the part of a clause a point quotes: the clause, or the
// sentences of it where the topic's strongest pattern matched.
func quote(t *topic, text string) string {
	if len(text) <= maxQuote {
		return text
	}
	sents := sentences(text)
	bestAt, bestW := 0, -1.0
	for i, s := range sents {
		w := 0.0
		for _, f := range t.find {
			if f.matches(s) {
				w += f.w
			}
		}
		if w > bestW {
			bestAt, bestW = i, w
		}
	}
	q := sents[bestAt]
	// The sentence after, while it fits: a clause's point often runs on.
	if bestAt+1 < len(sents) && len(q)+1+len(sents[bestAt+1]) <= maxQuote {
		q += " " + sents[bestAt+1]
	}
	if len(q) > maxQuote {
		q = clip(q, maxQuote)
	}
	if bestAt > 0 {
		q = "... " + q
	}
	return q
}

// clip cuts s to at most n bytes at a word's end, marking the cut.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := strings.LastIndexByte(s[:n], ' ')
	if cut < n/2 {
		cut = n
	}
	return strings.ToValidUTF8(s[:cut], "") + " ..."
}

// fragment is the address that opens a page at the words of a quote and
// marks them (a text fragment, which Chrome, Edge, Safari and Firefox
// follow), or the page's address when the quote has no words to point at.
func fragment(address, quote string) string {
	if address == "" {
		return ""
	}
	// A page routed by its fragment, #/terms, keeps its route: the text
	// directive goes after it.
	base, frag, _ := strings.Cut(address, "#")
	if frag, _, _ = strings.Cut(frag, ":~:"); frag != "" {
		base += "#" + frag
	}
	// Only words of letters and digits: they read the same on the page
	// as in the quote, whatever quotes and dashes the page draws.
	var ws []string
	for _, f := range strings.Fields(strings.TrimPrefix(quote, "... ")) {
		if strings.IndexFunc(f, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) >= 0 {
			break
		}
		ws = append(ws, f)
		if len(ws) == 6 {
			break
		}
	}
	if len(ws) < 3 {
		return base
	}
	sep := "#:~:text="
	if strings.Contains(base, "#") {
		sep = ":~:text="
	}
	return base + sep + strings.ReplaceAll(url.PathEscape(strings.Join(ws, " ")), "-", "%2D")
}

// readingSpeed is how many words a minute an adult reads, silently, in
// English non-fiction (Brysbaert 2019).
const readingSpeed = 238

// grade is the years of school a text's sentences ask of a reader, by the
// Flesch-Kincaid grade level: 0.39 words a sentence + 11.8 syllables a word
// - 15.59.
func grade(cs []clause) int {
	var w, s, syl int
	for _, c := range cs {
		for _, sent := range sentences(c.text) {
			n := 0
			for _, f := range strings.Fields(sent) {
				if words(f) == 0 {
					continue
				}
				n++
				syl += syllables(f)
			}
			if n > 0 {
				w += n
				s++
			}
		}
	}
	if w < 100 || s == 0 {
		return 0
	}
	g := 0.39*float64(w)/float64(s) + 11.8*float64(syl)/float64(w) - 15.59
	return int(math.Round(math.Max(1, math.Min(g, 22))))
}

// syllables counts a word's syllables by its groups of vowels, the usual
// way for English.
func syllables(word string) int {
	w := strings.ToLower(word)
	n := 0
	prev := false
	letters := 0
	for _, r := range w {
		if !unicode.IsLetter(r) {
			continue
		}
		letters++
		v := strings.ContainsRune("aeiouy", r)
		if v && !prev {
			n++
		}
		prev = v
	}
	if letters == 0 {
		return 0
	}
	if strings.HasSuffix(w, "e") && !strings.HasSuffix(w, "le") && n > 1 {
		n--
	}
	return max(n, 1)
}

// reader says who can read text of a grade.
func reader(g int) string {
	switch {
	case g == 0:
		return ""
	case g <= 6:
		return "a child of 11"
	case g <= 9:
		return "a 14-year-old"
	case g <= 12:
		return "someone leaving school"
	case g <= 16:
		return "a university student"
	}
	return "a university graduate"
}

// commonWords are words that fill English text, of which a page in
// another language has few.
var commonWords = map[string]bool{"the": true, "and": true, "of": true, "to": true, "you": true, "in": true, "or": true, "your": true, "we": true, "is": true, "any": true, "that": true, "for": true, "be": true, "by": true, "with": true, "not": true, "may": true, "this": true, "are": true, "our": true, "on": true}

// english says whether text reads as English.
func english(text string) bool {
	n, common := 0, 0
	for _, f := range strings.Fields(strings.ToLower(text)) {
		f = strings.Trim(f, ".,;:()\"'!?")
		if f == "" {
			continue
		}
		n++
		if commonWords[f] {
			common++
		}
		if n >= 4000 {
			break
		}
	}
	if n < 12 {
		return true
	}
	if n < 50 {
		return common*100 >= n*8
	}
	return common*100 >= n*15
}

var (
	termsWords   = re(`terms|conditions|user agreement|eula|licen[cs]e agreement|agreement|terms of (?:service|use)`)
	privacyWords = re(`privacy|personal (?:data|information)|data (?:policy|protection)|cookie`)
	privacyLink  = re(`^(?:our |your )?privacy(?: (?:policy|notice|statement|center|centre))?$|^privacy & (?:terms|cookies)$|^privacy policy$`)
	legalWords   = re(`\b(?:agree|agreement|terms|liab\w*|warrant\w*|indemn\w*|arbitrat\w*|governing law|jurisdiction|licen[cs]e|terminat\w*|shall|hereby|herein|thereof|parties|personal (?:data|information)|privacy|consent)\b`)
)

// kind says what kind of document it is, by its title, address and words.
func (d *Doc) kind(body string) string {
	name := d.Title + " " + d.URL
	if len(d.secs) > 0 {
		name += " " + d.secs[0].title
	}
	legal := len(legalWords.FindAllStringIndex(body, -1))
	dense := d.Words > 0 && legal*1000/d.Words >= 8
	switch {
	case privacyWords.MatchString(name) && !termsWords.MatchString(d.Title):
		return KindPrivacy
	case termsWords.MatchString(name) && (dense || d.Words >= 400):
		return KindTerms
	case dense && d.Words >= 800:
		if strings.Count(strings.ToLower(body), "personal") > strings.Count(strings.ToLower(body), "liab") {
			return KindPrivacy
		}
		return KindTerms
	}
	return KindOther
}

// paid says whether the terms take payments.
func (d *Doc) paid() bool {
	n := 0
	for _, c := range d.clauses {
		if paidWords.MatchString(c.text) {
			n++
		}
	}
	return n >= 2
}

var paidWords = re(`\b(?:subscriptions?|payments?|fees|purchases?|billing|charges?|prices?)\b`)

var updatedRE = re(`(?:last (?:updated|modified|revised|changed)|effective(?: date| as of| from| on)?|updated(?: on)?|revised(?: on)?|version date|date of last revision)\s*(?:on|as of)?\s*[:\-]?\s*((?:January|February|March|April|May|June|July|August|September|October|November|December|Jan|Feb|Mar|Apr|Jun|Jul|Aug|Sept?|Oct|Nov|Dec)\.?\s+\d{1,2}(?:st|nd|rd|th)?,?\s+\d{4}|\d{1,2}(?:st|nd|rd|th)?\s+(?:January|February|March|April|May|June|July|August|September|October|November|December)\s+\d{4}|\d{4}-\d{2}-\d{2}|\d{1,2}[/.]\d{1,2}[/.]\d{2,4})`)

// updated is when the document says it last changed.
func updated(body string) string {
	head := body
	if len(head) > 20000 {
		head = head[:20000]
	}
	if m := updatedRE.FindStringSubmatch(head); m != nil {
		return squash(m[1])
	}
	return ""
}

var (
	// defined is a company naming itself: Example, Inc. ("Example", "we").
	defined = regexp.MustCompile(`([A-Z][A-Za-z0-9&'-]*(?:[ ,]+(?:[A-Z][A-Za-z0-9&.'-]*|of|and|&)){0,5})\s*\([^)]{0,120}?["']\s*(?:we|We|us|Us|our|Our|Company|the Company|Service Provider|Provider|Operator)\s*[,"']`)
	// between is "an agreement between you and Example".
	between = regexp.MustCompile(`(?:between you and|agreement with|contract with|provided by|operated by)\s+([A-Z][A-Za-z0-9&'-]*(?:[ ,]+(?:[A-Z][A-Za-z0-9&.'-]*|of|and|&)){0,5})`)
	// entity is the end of a company's legal name.
	entity = regexp.MustCompile(`(?:,?\s+(?:Inc|Incorporated|LLC|L\.L\.C|Ltd|Limited|GmbH|Corp|Corporation|Company|Co|S\.A|SAS|B\.V|N\.V|AG|AB|plc|PLC|Pty|ULC|LLP|LP)\.?)$`)
)

// company is who the terms are with, when they say so plainly: a company
// that names itself ("we"), else one an agreement is with whose legal name
// ends as a company's does, or whose name the terms keep using.
func company(cs []clause, body string) string {
	tidy := func(name string) string {
		name = strings.TrimRight(squash(name), ", ")
		for _, w := range []string{" and", " of", " &"} {
			name = strings.TrimSuffix(name, w)
		}
		// A full stop ends a sentence, unless it ends Inc. or Co.
		if strings.HasSuffix(name, ".") && !entity.MatchString(name) {
			name = strings.TrimSuffix(name, ".")
		}
		return name
	}
	limit := min(len(cs), 120)
	for _, c := range cs[:limit] {
		if m := defined.FindStringSubmatch(c.text); m != nil {
			if n := tidy(m[1]); len(n) >= 2 && len(n) <= 80 {
				return n
			}
		}
	}
	for _, c := range cs[:limit] {
		m := between.FindStringSubmatch(c.text)
		if m == nil {
			continue
		}
		n := tidy(m[1])
		first := strings.Fields(n)[0]
		if entity.MatchString(n) || (len(first) >= 3 && strings.Count(body, first) >= 5) {
			if !entity.MatchString(n) {
				n = strings.TrimRight(first, ",.")
			}
			if len(n) >= 2 && len(n) <= 80 {
				return n
			}
		}
	}
	return ""
}

// sameSite says whether two addresses are on the same site: one host,
// or hosts under one name, such as policies.example.com and example.com.
func sameSite(a, b string) bool {
	ua, err1 := url.Parse(a)
	ub, err2 := url.Parse(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return siteOf(ua.Hostname()) != "" && siteOf(ua.Hostname()) == siteOf(ub.Hostname())
}

// twoPart are second-level names under which a site's own name sits one
// further in: example.co.uk.
var twoPart = map[string]bool{"co": true, "com": true, "org": true, "net": true, "ac": true, "gov": true, "edu": true, "ne": true, "or": true, "go": true}

// siteOf is a host's site: its last two names, or three under a country's
// second-level name such as co.uk.
func siteOf(host string) string {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	labels := strings.Split(host, ".")
	n := len(labels)
	switch {
	case n < 2:
		return host
	case n >= 3 && len(labels[n-1]) == 2 && twoPart[labels[n-2]]:
		return strings.Join(labels[n-3:], ".")
	}
	return strings.Join(labels[n-2:], ".")
}

// Text is the document's points in words, as irie terms prints them.
func (d *Doc) Text() string {
	var b strings.Builder
	name := d.Title
	if name == "" {
		name = "Terms"
	}
	if d.Site != "" {
		name += " - " + d.Site
	}
	fmt.Fprintln(&b, name)
	if d.URL != "" {
		fmt.Fprintln(&b, d.URL)
	}
	facts := []string{fmt.Sprintf("%s words, about %d minute%s to read", thousands(d.Words), d.Minutes, plural(d.Minutes))}
	if d.Reader != "" {
		facts = append(facts, "written for "+d.Reader)
	}
	if d.Updated != "" {
		facts = append(facts, "last updated "+d.Updated)
	}
	if d.Company != "" {
		facts = append(facts, "with "+d.Company)
	}
	fmt.Fprintln(&b, strings.Join(facts, "; ")+".")
	switch {
	case d.scripted:
		fmt.Fprintln(&b, "\nThis page builds itself with scripts, so its HTML holds next to nothing to read. Open it in your browser and press "+brand.Name+"'s button there: the extension reads the page as the browser shows it.")
	case !d.English:
		fmt.Fprintln(&b, "\nThis reads as another language than English, which "+brand.Name+"'s reading doesn't know yet, so it may miss what matters.")
	case d.Kind == KindOther:
		fmt.Fprintln(&b, "\nThis doesn't read as terms of service or a privacy policy.")
	}
	if len(d.Points) == 0 {
		fmt.Fprintln(&b, "\nNothing found that the reading knows.")
	} else {
		fmt.Fprintf(&b, "\n%s\n", d.Tally())
	}
	for _, p := range d.Points {
		fmt.Fprintf(&b, "\n%s %s\n  %s\n", Mark(p.Verdict), p.Title, p.Says)
		q := "  \"" + p.Quote + "\""
		if p.Section != "" {
			q += " (" + p.Section + ")"
		}
		fmt.Fprintln(&b, q)
	}
	if len(d.Missing) > 0 {
		fmt.Fprintln(&b, "\nNot in these terms:")
		for _, g := range d.Missing {
			fmt.Fprintf(&b, "  ? %s\n", g.Says)
		}
	}
	fmt.Fprintln(&b, "\nNot legal advice: a reading of the text by "+brand.Name+"'s own code, on this computer.")
	return b.String()
}

// Tally is how many points of each verdict, in a line.
func (d *Doc) Tally() string {
	var parts []string
	for _, v := range []string{Against, Watch, Know, For} {
		if n := d.Count[v]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, VerdictWords(v)))
		}
	}
	return strings.Join(parts, " · ")
}

// VerdictWords is a verdict as a person reads it.
func VerdictWords(v string) string {
	switch v {
	case Against:
		return "against you"
	case Watch:
		return "to watch"
	case Know:
		return "good to know"
	case For:
		return "for you"
	}
	return v
}

// Mark is a verdict's glyph, so colour never carries it alone.
func Mark(v string) string {
	switch v {
	case Against:
		return "✕"
	case Watch:
		return "!"
	case For:
		return "✓"
	}
	return "·"
}

func thousands(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
