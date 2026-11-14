package rebuild

import (
	"strings"
)

// Rates are a model's list prices, in dollars a million tokens: its input,
// a write into the five minutes' cache and into the hour's, a read from the
// cache, and its output. Fast is its input price in fast mode, for a model
// that has one, 0 when it has none: the cache's prices scale with it.
type Rates struct {
	Input, Write5m, Write1h, Read, Output float64
	Fast                                  float64
}

// longFrom is where Claude Haiku 5.5's prices for long prompts start: a
// request whose prompt is over this many tokens pays them, cache reads and
// writes included.
const longFrom = 100_000

// prices are Anthropic's list prices on 2026-11-14, from its pricing page
// (PROVENANCE.md). Cache hits cost a tenth of the input price, but a
// twentieth on Opus 5.5 and Sonnet 5.5 and a fortieth on Fable 5.1 and 5.2;
// a write costs 1.25 times the input price into the five minutes' cache and
// twice it into the hour's. Fast mode's prices are those of the models that
// have it; the cache's multipliers stack on top of them.
var prices = []struct {
	name  string
	rates Rates
	long  *Rates // the prices for a prompt over longFrom tokens, where they differ
}{
	{"claude-fable-5-2", Rates{8, 10, 16, 0.20, 40, 16}, nil},
	{"claude-mythos-5-2", Rates{8, 10, 16, 0.20, 40, 16}, nil},
	{"claude-fable-5-1", Rates{10, 12.50, 20, 0.25, 50, 0}, nil},
	{"claude-mythos-5-1", Rates{10, 12.50, 20, 0.25, 50, 0}, nil},
	{"claude-fable-5", Rates{10, 12.50, 20, 1, 50, 0}, nil},
	{"claude-mythos-5", Rates{10, 12.50, 20, 1, 50, 0}, nil},
	{"claude-opus-5-5", Rates{4, 5, 8, 0.20, 20, 8}, nil},
	{"claude-opus-5", Rates{5, 6.25, 10, 0.50, 25, 10}, nil},
	{"claude-opus-4-8", Rates{5, 6.25, 10, 0.50, 25, 10}, nil},
	{"claude-opus-4-7", Rates{5, 6.25, 10, 0.50, 25, 0}, nil},
	{"claude-opus-4-6", Rates{5, 6.25, 10, 0.50, 25, 0}, nil},
	{"claude-opus-4-5", Rates{5, 6.25, 10, 0.50, 25, 0}, nil},
	{"claude-opus-4-1", Rates{15, 18.75, 30, 1.50, 75, 0}, nil},
	{"claude-opus-4", Rates{15, 18.75, 30, 1.50, 75, 0}, nil},
	{"claude-sonnet-5-5", Rates{2, 2.50, 4, 0.10, 10, 0}, nil},
	{"claude-sonnet-5", Rates{2, 2.50, 4, 0.20, 10, 0}, nil},
	{"claude-sonnet-4-6", Rates{3, 3.75, 6, 0.30, 15, 0}, nil},
	{"claude-sonnet-4-5", Rates{3, 3.75, 6, 0.30, 15, 0}, nil},
	{"claude-sonnet-4", Rates{3, 3.75, 6, 0.30, 15, 0}, nil},
	{"claude-haiku-5-5", Rates{0.10, 0.125, 0.20, 0.01, 0.50, 0}, &Rates{0.50, 0.625, 1, 0.05, 2.50, 0}},
	{"claude-haiku-4-5", Rates{1, 1.25, 2, 0.10, 5, 0}, nil},
}

// Price is model's list prices for a request whose prompt is prompt tokens
// long, and false for a model whose prices Irie does not know, such as a
// local one. A model ID names its model, then perhaps a date or a variant:
// claude-opus-4-5-20251101, claude-opus-5-5[1m], or a cloud's
// anthropic.claude-opus-4-5-20251101-v1:0.
func Price(model string, prompt int64) (Rates, bool) {
	m := strings.ToLower(strings.TrimSpace(model))
	if i := strings.IndexByte(m, '['); i >= 0 {
		m = m[:i]
	}
	if i := strings.Index(m, "claude-"); i > 0 {
		m = m[i:]
	}
	best := -1
	for i, p := range prices {
		rest, ok := strings.CutPrefix(m, p.name)
		if ok && variant(rest) && (best < 0 || len(p.name) > len(prices[best].name)) {
			best = i
		}
	}
	if best < 0 {
		return Rates{}, false
	}
	if p := prices[best]; p.long != nil && prompt > longFrom {
		return *p.long, true
	}
	return prices[best].rates, true
}

// variant reports whether rest may follow a model's name in its ID: nothing,
// a date of eight digits and what may follow that, or a cloud's version
// after @ or :. So claude-opus-5 is not a variant of claude-opus-5-5.
func variant(rest string) bool {
	if rest == "" || rest[0] == '@' || rest[0] == ':' {
		return true
	}
	if rest[0] != '-' {
		return false
	}
	n := 0
	for n < len(rest)-1 && rest[1+n] >= '0' && rest[1+n] <= '9' {
		n++
	}
	return n == 8 && (len(rest) == 9 || strings.ContainsRune("-@:", rune(rest[9])))
}

// scaled are r as a request paid them: in fast mode, every price scales
// with the fast input price, and with inference held in the US
// (inference_geo "us") every price is 1.1 times as much.
func (r Rates) scaled(speed, geo string) Rates {
	k := 1.0
	if speed == "fast" && r.Fast > 0 && r.Input > 0 {
		k = r.Fast / r.Input
	}
	if geo == "us" {
		k *= 1.1
	}
	return Rates{r.Input * k, r.Write5m * k, r.Write1h * k, r.Read * k, r.Output * k, r.Fast * k}
}
