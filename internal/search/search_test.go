package search

import (
	"slices"
	"testing"
)

func TestTokensDropCommonWordsAndStem(t *testing.T) {
	got := Tokens("The refunds were terminated, and they're refundable!", nil)
	if want := []string{"refund", "terminat", "refundabl"}; !slices.Equal(got, want) {
		t.Errorf("tokens %q, want %q", got, want)
	}
	// Words a caller finds too common go too.
	got = Tokens("Fix the sidebar's sidebar badge", map[string]bool{"sidebar": true})
	if want := []string{"fix", "badg"}; !slices.Equal(got, want) {
		t.Errorf("tokens %q, want %q", got, want)
	}
}

func index(texts ...string) *Index {
	ts := make([][]string, len(texts))
	for i, s := range texts {
		ts[i] = Tokens(s, nil)
	}
	return NewIndex(ts)
}

// A word few texts have says more of a text than one most of them have.
func TestARareWordOutweighsACommonOne(t *testing.T) {
	x := index(
		"fix the sidebar badge colours",
		"fix the parser test",
		"fix the release notes",
		"rename the parser package",
	)
	hits := x.Search(Tokens("fix parser", nil), -1)
	if len(hits) != 4 || hits[0].Doc != 1 {
		t.Fatalf("hits %+v, want the parser fix first and all four", hits)
	}
	if hits[1].Doc != 3 {
		t.Errorf("the second hit is %d, want the other parser text, as parser is rarer than fix", hits[1].Doc)
	}
	if got := x.Search(Tokens("vpn", nil), -1); len(got) != 0 {
		t.Errorf("a word no text has found %+v", got)
	}
	if got := x.Search(Tokens("fix", nil), 2); len(got) != 2 {
		t.Errorf("at most 2: %+v", got)
	}
	if x.Top(Tokens("fix parser", nil)) != hits[0].Score {
		t.Error("Top is not the best score")
	}
	q := Tokens("parser parser test", nil)
	if n := x.Has(1, q); n != 2 {
		t.Errorf("the parser test has %d of the words looked up, want 2", n)
	}
	if x.Score(1, q) != x.Score(1, Tokens("parser test", nil)) {
		t.Error("a word given twice counted twice")
	}
}

func TestFuseRewardsTextsFirstInEveryRanking(t *testing.T) {
	words := Ranking{Order: []int{2, 0}, Weight: 0.5}
	meaning := Ranking{Order: []int{0, 1, 2}, Weight: 1}
	got := Fuse(4, FuseK, words, meaning)
	if !(got[0] > got[2] && got[2] > got[1] && got[1] > got[3]) {
		t.Errorf("fused %v: want 0 (second by words, first by meaning), then 2, then 1, then 3, in neither", got)
	}
	if got[3] != 0 {
		t.Errorf("a text no ranking has scored %v", got[3])
	}
	if want := 0.5/(FuseK+2) + 1/(FuseK+1.0); got[0] != want {
		t.Errorf("fused %v, want %v", got[0], want)
	}
}
