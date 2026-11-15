package lock

import (
	"strconv"
	"strings"
	"unicode"
)

// Compare orders two versions of one package, as npm's and PyPI's
// releases sort: by their numbers in turn, a release after its own
// prereleases (2.0.0-rc.1, 2.0rc1) and after its .dev ones, a .post one
// after it. It returns -1, 0 or 1. Words it cannot read sort as text,
// after numbers.
func Compare(a, b string) int {
	pa, pb := parts(a), parts(b)
	for i := 0; i < max(len(pa), len(pb)); i++ {
		x, y := partAt(pa, i), partAt(pb, i)
		if c := x.compare(y); c != 0 {
			return c
		}
	}
	return 0
}

// part is one piece of a version: a number, or a word with its rank.
type part struct {
	num  int
	word string
	rank int // -2 dev, -1 a prerelease, 0 a number or the end, 1 post, 2 any other word
}

func (x part) compare(y part) int {
	if x.rank != y.rank {
		return cmpInt(x.rank, y.rank)
	}
	if x.rank == 0 || x.word == y.word {
		return cmpInt(x.num, y.num)
	}
	return strings.Compare(x.word, y.word)
}

// partAt is the i-th piece, or the end: 0, which a release's missing .0
// equals and a prerelease's word sorts before.
func partAt(ps []part, i int) part {
	if i < len(ps) {
		return ps[i]
	}
	return part{}
}

// parts splits a version into its numbers and words: 2.1.250 is 2, 1,
// 250; 1.2.0-rc.1 and 1.2.0rc1 are 1, 2, 0, rc, 1. Build metadata after
// a + and a leading v are left out.
func parts(v string) []part {
	v = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(v)), "v")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	var out []part
	for len(v) > 0 {
		r := rune(v[0])
		switch {
		case unicode.IsDigit(r):
			n := 0
			for n < len(v) && unicode.IsDigit(rune(v[n])) {
				n++
			}
			num, _ := strconv.Atoi(v[:n])
			out = append(out, part{num: num})
			v = v[n:]
		case unicode.IsLetter(r):
			n := 0
			for n < len(v) && unicode.IsLetter(rune(v[n])) {
				n++
			}
			w := v[:n]
			out = append(out, part{word: w, rank: wordRank(w)})
			// A word's number goes with it: rc1 is rc then 1, which a
			// missing number equals.
			v = v[n:]
		default:
			v = v[1:]
		}
	}
	return out
}

// wordRank places a word among the numbers: before a release (dev, then
// the prereleases), after it (post), or after everything (anything else).
func wordRank(w string) int {
	switch w {
	case "dev":
		return -2
	case "a", "alpha", "b", "beta", "c", "rc", "pre", "preview", "next", "canary":
		return -1
	case "post", "r", "rev":
		return 1
	}
	return 2
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
