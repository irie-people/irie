// Package search ranks texts for what someone looks up: by their words,
// with BM25 (Robertson and Zaragoza, "The Probabilistic Relevance
// Framework: BM25 and Beyond", 2009), and by more than one ranking at once,
// fused by reciprocal rank (Cormack, Clarke and Büttcher, "Reciprocal Rank
// Fusion outperforms Condorcet and individual Rank Learning Methods",
// SIGIR 2009). The terms reader and irie ctl find (DESIGN §4.5) search with
// it.
package search

import (
	"math"
	"slices"
	"strings"
	"unicode"
)

// StopWords are words too common to search by.
var StopWords = map[string]bool{
	"a": true, "an": true, "the": true, "and": true, "or": true, "of": true, "to": true, "in": true,
	"on": true, "for": true, "by": true, "with": true, "at": true, "from": true, "as": true, "is": true,
	"are": true, "be": true, "been": true, "was": true, "were": true, "it": true, "its": true, "this": true,
	"that": true, "these": true, "those": true, "i": true, "me": true, "my": true, "we": true, "us": true,
	"our": true, "you": true, "your": true, "they": true, "them": true, "their": true, "he": true,
	"she": true, "do": true, "does": true, "did": true, "can": true, "could": true, "will": true,
	"would": true, "shall": true, "should": true, "may": true, "might": true, "must": true, "have": true,
	"has": true, "had": true, "what": true, "which": true, "who": true, "whom": true, "how": true,
	"when": true, "where": true, "why": true, "if": true, "then": true, "than": true, "so": true,
	"not": true, "no": true, "any": true, "all": true, "such": true, "there": true, "here": true,
	"about": true, "into": true, "out": true, "up": true, "get": true, "also": true, "other": true,
}

// Tokens are a text's words for search: lower case, stemmed, without the
// common ones, those under three letters, or those in more.
func Tokens(s string, more map[string]bool) []string {
	var out []string
	for _, f := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if StopWords[f] || more[f] || len(f) < 3 {
			continue
		}
		out = append(out, Stem(f))
	}
	return out
}

var stemRules = []struct{ suffix, with string }{
	{"ational", "ate"}, {"ization", "ize"}, {"isation", "ize"}, {"fulness", "ful"},
	{"iveness", "ive"}, {"ically", "ic"}, {"ations", "ate"}, {"ation", "ate"},
	{"ments", ""}, {"ment", ""}, {"ities", "ity"}, {"ingly", ""}, {"edly", ""},
	{"ies", "y"}, {"ied", "y"}, {"ing", ""}, {"ers", "er"}, {"ed", ""}, {"es", ""},
	{"ly", ""}, {"s", ""},
}

// Stem cuts a word to its stem, roughly, so refunds finds refund and
// terminated finds terminate.
func Stem(w string) string {
	for _, r := range stemRules {
		if strings.HasSuffix(w, r.suffix) && len(w)-len(r.suffix)+len(r.with) >= 4 {
			w = w[:len(w)-len(r.suffix)] + r.with
			break
		}
	}
	return strings.TrimSuffix(w, "e")
}

// Index is a BM25 index of texts, each given as its tokens.
type Index struct {
	docs []map[string]int
	lens []int
	avg  float64
	df   map[string]int
}

// BM25's usual constants.
const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

// NewIndex indexes texts, each given as its tokens.
func NewIndex(docs [][]string) *Index {
	x := &Index{df: map[string]int{}}
	total := 0
	for _, ts := range docs {
		tf := map[string]int{}
		for _, t := range ts {
			tf[t]++
		}
		for t := range tf {
			x.df[t]++
		}
		x.docs = append(x.docs, tf)
		x.lens = append(x.lens, len(ts))
		total += len(ts)
	}
	if len(docs) > 0 {
		x.avg = float64(total) / float64(len(docs))
	}
	return x
}

// Hit is a text and how well it scores.
type Hit struct {
	Doc   int // its place among the texts indexed
	Score float64
}

// Search is the n texts that score best for the query's tokens, best
// first, each above nothing; n < 0 is every one.
func (x *Index) Search(query []string, n int) []Hit {
	var out []Hit
	for i := range x.docs {
		if s := x.Score(i, query); s > 0 {
			out = append(out, Hit{i, s})
		}
	}
	slices.SortStableFunc(out, func(a, b Hit) int {
		switch {
		case a.Score > b.Score:
			return -1
		case a.Score < b.Score:
			return 1
		}
		return a.Doc - b.Doc
	})
	if n >= 0 && len(out) > n {
		out = out[:n]
	}
	return out
}

// Top is the best score any text has for the query.
func (x *Index) Top(query []string) float64 {
	if hits := x.Search(query, 1); len(hits) > 0 {
		return hits[0].Score
	}
	return 0
}

// Score is how well text i matches the query's tokens, a token given twice
// counting once.
func (x *Index) Score(i int, query []string) float64 {
	n := float64(len(x.docs))
	s := 0.0
	seen := map[string]bool{}
	for _, t := range query {
		if seen[t] {
			continue
		}
		seen[t] = true
		tf := float64(x.docs[i][t])
		if tf == 0 {
			continue
		}
		df := float64(x.df[t])
		idf := math.Log(1 + (n-df+0.5)/(df+0.5))
		s += idf * tf * (bm25K1 + 1) / (tf + bm25K1*(1-bm25B+bm25B*float64(x.lens[i])/math.Max(x.avg, 1)))
	}
	return s
}

// Has counts the query's tokens that text i has, each once.
func (x *Index) Has(i int, query []string) int {
	n := 0
	seen := map[string]bool{}
	for _, t := range query {
		if !seen[t] && x.docs[i][t] > 0 {
			n++
		}
		seen[t] = true
	}
	return n
}

// Ranking is one order of texts, the best first, and what it weighs when
// fused with others. A text it leaves out gets nothing from it.
type Ranking struct {
	Order  []int
	Weight float64
}

// FuseK is reciprocal rank fusion's usual constant, which keeps one
// ranking's first few from outweighing the rest.
const FuseK = 60

// Fuse scores n texts by reciprocal rank: the sum, over the rankings, of
// weight / (k + rank), the first rank being 1.
func Fuse(n int, k float64, rs ...Ranking) []float64 {
	out := make([]float64, n)
	for _, r := range rs {
		for rank, i := range r.Order {
			if i >= 0 && i < n {
				out[i] += r.Weight / (k + float64(rank+1))
			}
		}
	}
	return out
}
