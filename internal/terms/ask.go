package terms

import (
	"slices"
	"strings"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/search"
)

// Answer is what the terms say to a question.
type Answer struct {
	Question string  `json:"question"`
	Says     string  `json:"says"`
	Quotes   []Quote `json:"quotes"`
	// Topics are the topics the question is about, most likely first.
	Topics []string `json:"topics,omitempty"`
	// Found says whether the terms answer it, rather than only coming
	// near.
	Found bool `json:"found"`
	// By is who worded it: "code", or the local model's name.
	By string `json:"by"`
}

// Quote is a passage of the document.
type Quote struct {
	Text    string `json:"text"`
	Section string `json:"section,omitempty"`
	Link    string `json:"link,omitempty"`

	clause int
}

// Code is what an Answer's By says when Irie's own code worded it.
const Code = "code"

// maxQuotes is the most passages an answer quotes.
const maxQuotes = 3

var (
	askWorst   = re(`\b(?:worst|bad (?:parts?|things?|bits?)|red flags?|watch out|concern\w*|dangerous|risks?|warning|should i worry|catch|gotchas?|sketchy|shady|scary)\b`)
	askBest    = re(`\b(?:good (?:parts?|things?|bits?)|best|in my favou?r|positives?|pros|protect\w* me|my rights)\b`)
	askSummary = re(`\b(?:summar\w*|tl;?dr|overview|main points?|key points?|short version|in short|in a nutshell|explain|what (?:do|does) (?:it|this|these) (?:say|mean)|what am i agreeing)\b`)
	askWho     = re(`\b(?:who (?:is|are|am i)|which company|company name|agreement with)\b`)
	askWhen    = re(`\b(?:when (?:was|were|did)|last updated|how old (?:is|are) (?:this|these|it)|how recent|date)\b`)
	askLong    = re(`\b(?:how long (?:is|are) (?:it|this|these)|how many words|reading time|minutes to read)\b`)
)

// Ask answers a question from the document, by its own code: the points
// on the topic the question is about, else the passages whose words come
// closest to it.
func (d *Doc) Ask(question string) Answer {
	q := squash(normalise(question))
	a := Answer{Question: q, By: Code}
	switch {
	case q == "":
		a.Says = "Ask anything about these terms, such as whether you can get a refund or who owns what you post."
		return a
	case askSummary.MatchString(q) || askWorst.MatchString(q) || askBest.MatchString(q):
		return d.overview(a, q)
	case askWho.MatchString(q) && d.Company != "":
		a.Found = true
		a.Says = "These terms are an agreement between you and " + d.Company + "."
		return d.withLaw(a)
	case askWhen.MatchString(q) && d.Updated != "":
		a.Found = true
		a.Says = "They say they were last updated on " + d.Updated + "."
		return a
	case askLong.MatchString(q):
		a.Found = true
		a.Says = "They run to " + thousands(d.Words) + " words, about " + plainMinutes(d.Minutes) + " to read."
		return a
	}

	topics := intents(q)
	for _, t := range topics {
		a.Topics = append(a.Topics, t.id)
	}
	// The points on the topics asked about, as the summary says them.
	var says []string
	for _, t := range topics {
		for _, p := range d.Points {
			if p.Topic != t.id || len(a.Quotes) >= maxQuotes {
				continue
			}
			says = append(says, p.Says)
			a.Quotes = append(a.Quotes, d.quoteOf(p.clause, p.Quote))
		}
		if len(says) >= 2 {
			break
		}
	}
	if len(says) > 0 {
		a.Found = true
		a.Says = strings.Join(says, " ")
		// More passages on the words asked, after the points.
		for _, h := range d.index.Search(expand(q, topics), maxQuotes) {
			if len(a.Quotes) >= maxQuotes {
				break
			}
			if !slices.ContainsFunc(a.Quotes, func(x Quote) bool { return x.clause == h.Doc }) && h.Score >= 0.5*d.index.Top(expand(q, topics)) {
				a.Quotes = append(a.Quotes, d.quoteOf(h.Doc, ""))
			}
		}
		return a
	}

	hits := d.index.Search(expand(q, topics), maxQuotes)
	if len(hits) > 0 {
		for _, h := range hits {
			if h.Score < 0.45*hits[0].Score {
				break
			}
			a.Quotes = append(a.Quotes, d.quoteOf(h.Doc, ""))
		}
	}
	switch {
	case len(topics) > 0 && len(a.Quotes) > 0:
		a.Says = "These terms don't say anything clear about " + topics[0].about + ". The passages closest to your question:"
	case len(topics) > 0:
		a.Says = "These terms don't say anything about " + topics[0].about + " that " + brand.Name + " can find."
	case len(a.Quotes) > 0:
		a.Says = "Here is what the terms say closest to your question:"
	default:
		a.Says = brand.Name + " can't find anything in these terms about that. Try other words, such as refund, cancel, data, content or arbitration."
	}
	return a
}

// overview answers "what's the catch?" and the like with the points
// themselves.
func (d *Doc) overview(a Answer, q string) Answer {
	want := []string{Against, Watch}
	lead := "The points that matter most:"
	switch {
	case askBest.MatchString(q) && !askWorst.MatchString(q):
		want, lead = []string{For}, "In your favour:"
	case askWorst.MatchString(q):
		lead = "What to watch out for:"
	}
	var says []string
	for _, p := range d.Points {
		if !slices.Contains(want, p.Verdict) || len(says) >= 4 {
			continue
		}
		says = append(says, p.Says)
		if len(a.Quotes) < maxQuotes {
			a.Quotes = append(a.Quotes, d.quoteOf(p.clause, p.Quote))
		}
	}
	if len(says) == 0 {
		if slices.Contains(want, For) {
			a.Says = brand.Name + " finds nothing in these terms that's particularly in your favour."
		} else {
			a.Says = brand.Name + " finds nothing in these terms that takes much from you; " + d.Tally() + "."
		}
		return a
	}
	a.Found = true
	a.Says = lead + " " + strings.Join(says, " ")
	return a
}

// withLaw adds whose law applies to an answer.
func (d *Doc) withLaw(a Answer) Answer {
	for _, p := range d.Points {
		if p.Topic == "law" {
			a.Says += " " + p.Says
			a.Quotes = append(a.Quotes, d.quoteOf(p.clause, p.Quote))
		}
	}
	return a
}

// quoteOf is clause i as a quote, with text in place of its words when
// given.
func (d *Doc) quoteOf(i int, text string) Quote {
	c := d.clauses[i]
	if text == "" {
		text = c.text
		if len(text) > maxQuote {
			text = clip(text, maxQuote)
		}
	}
	q := Quote{Text: text, clause: i, Link: fragment(d.URL, text)}
	if c.sec >= 0 {
		q.Section = d.secs[c.sec].title
	}
	return q
}

// Passages are the passages of the document closest to a question, for a
// model to answer from: those of the points on its topics, then the
// closest by their words, at most n.
func (d *Doc) Passages(question string, n int) []Quote {
	q := squash(normalise(question))
	topics := intents(q)
	var out []Quote
	has := func(i int) bool { return slices.ContainsFunc(out, func(x Quote) bool { return x.clause == i }) }
	for _, t := range topics {
		for _, p := range d.Points {
			if p.Topic == t.id && !has(p.clause) && len(out) < n {
				out = append(out, d.quoteOf(p.clause, d.clauses[p.clause].text))
			}
		}
	}
	for _, h := range d.index.Search(expand(q, topics), n) {
		if !has(h.Doc) && len(out) < n {
			out = append(out, d.quoteOf(h.Doc, d.clauses[h.Doc].text))
		}
	}
	return out
}

// intents are the topics a question is about, the likeliest first: those
// whose question words it holds, by how many.
func intents(q string) []*topic {
	type hit struct {
		t *topic
		n int
	}
	var hits []hit
	for _, t := range catalog {
		if t.ask == nil {
			continue
		}
		if n := len(t.ask.FindAllStringIndex(q, -1)); n > 0 {
			if t.lead != nil && t.lead.MatchString(q) {
				n += 10
			}
			hits = append(hits, hit{t, n})
		}
	}
	slices.SortStableFunc(hits, func(a, b hit) int { return b.n - a.n })
	var out []*topic
	for _, h := range hits {
		out = append(out, h.t)
	}
	return out
}

// synonyms are the words terms use for what people ask in their own.
var synonyms = map[string]string{
	"sue":      "arbitration court dispute claim action",
	"suing":    "arbitration court dispute claim action",
	"lawsuit":  "arbitration court dispute claim action",
	"cancel":   "cancel terminate subscription close account",
	"quit":     "terminate close account",
	"ban":      "suspend terminate account",
	"banned":   "suspend terminate account",
	"kick":     "suspend terminate account",
	"money":    "refund payment fee charge",
	"refund":   "refund refundable payment",
	"charge":   "charge fee payment billing",
	"cost":     "fee price payment",
	"photos":   "content upload post license",
	"pictures": "content upload post license",
	"videos":   "content upload post license",
	"posts":    "content post license",
	"own":      "own ownership license rights content",
	"data":     "data information personal",
	"info":     "information personal data",
	"sell":     "sell sale share data",
	"spy":      "monitor collect track",
	"track":    "track cookies advertising",
	"ads":      "advertising ads advertisers",
	"kids":     "children minor age",
	"child":    "children minor age",
	"old":      "age years",
	"hacked":   "security account unauthorized liability",
	"breach":   "security liability damages",
	"ai":       "artificial intelligence machine learning train model",
	"delete":   "delete deletion erase account data",
	"change":   "change modify update terms",
	"notify":   "notice notify email",
}

// expand adds to a question the words terms use for it and its topics'
// own words.
func expand(q string, topics []*topic) []string {
	ws := tokens(q)
	var more []string
	for _, w := range ws {
		if s, ok := synonyms[w]; ok {
			more = append(more, tokens(s)...)
		}
	}
	for i, t := range topics {
		if i == 2 {
			break
		}
		more = append(more, tokens(t.title)...)
	}
	return append(ws, more...)
}

// commonInTerms are words too common in terms to search them by.
var commonInTerms = map[string]bool{"terms": true, "service": true, "services": true, "agreement": true, "company": true}

// tokens are a text's words for search: lower case, stemmed, without the
// common ones.
func tokens(s string) []string { return search.Tokens(s, commonInTerms) }

func plainMinutes(n int) string {
	if n == 1 {
		return "a minute"
	}
	return itoa(n) + " minutes"
}

func itoa(n int) string { return strings.TrimSpace(thousands(n)) }

// Suggestions are questions worth asking of the document, its own points
// first.
func (d *Doc) Suggestions() []string {
	byTopic := map[string]string{
		"arbitration":   "Can I sue them?",
		"no-refunds":    "Can I get a refund?",
		"refunds":       "Can I get a refund?",
		"auto-renew":    "How do I stop paying?",
		"license":       "What can they do with what I post?",
		"sell-data":     "Do they sell my data?",
		"share-data":    "Who gets my data?",
		"termination":   "Can they close my account?",
		"ai-training":   "Will my content train AI?",
		"changes":       "Can they change the terms?",
		"liability-cap": "What if they lose my data?",
		"collect":       "What do they collect about me?",
		"delete-data":   "Can I get my data deleted?",
	}
	var out []string
	add := func(s string) {
		if s != "" && !slices.Contains(out, s) && len(out) < 4 {
			out = append(out, s)
		}
	}
	add("What's the catch?")
	for _, p := range d.Points {
		add(byTopic[p.Topic])
	}
	for _, s := range []string{"Can I get a refund?", "Do they sell my data?", "Can I sue them?"} {
		add(s)
	}
	return out
}
