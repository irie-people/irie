package ledger

import (
	"slices"
	"strings"
	"time"
)

// Rates are a model's list prices, in dollars a million tokens: its input,
// a write into the five minutes' cache and into the hour's, a read from the
// cache, and its output. Fast is its input price in fast mode, for a model
// that has one, 0 when it has none: the cache's prices scale with it.
type Rates struct {
	Input, Write5m, Write1h, Read, Output float64
	Fast                                  float64
}

// Price is a model's list prices from a day on (DESIGN §6.3).
type Price struct {
	Model string    // the name the model's IDs start with
	From  time.Time // when they took effect; zero for prices with no change before them
	Rates Rates
	Long  *Rates // the prices for a prompt over longFrom tokens, where they differ
}

// longFrom is where Claude Haiku 5.5's prices for long prompts start: a
// request whose prompt is over this many tokens pays them, cache reads and
// writes included.
const longFrom = 100_000

// prices are Anthropic's list prices, from its pricing page on 2026-11-14,
// checked again on 2026-11-23 (PROVENANCE.md). Cache hits cost a tenth of
// the input price, but a twentieth on Opus 5.5 and Sonnet 5.5 and a
// fortieth on Fable 5.1 and 5.2; a write costs 1.25 times the input price
// into the five minutes' cache and twice it into the hour's. Fast mode's
// prices are those of the models that have it; the cache's multipliers
// stack on top of them.
//
// A price change is a new row with the day it takes effect, beside the
// row before, which stays: a call is priced by the row in effect when it
// was made, so what it cost then stays what it cost, and a change
// announced ahead goes in before its day. No row has a date yet: every
// price here has held since its model came out (Sonnet 5's introductory
// price became its standard one, and the rise announced for 2026-09-01
// never came).
var prices = []Price{
	{Model: "claude-fable-5-2", Rates: Rates{8, 10, 16, 0.20, 40, 16}},
	{Model: "claude-mythos-5-2", Rates: Rates{8, 10, 16, 0.20, 40, 16}},
	{Model: "claude-fable-5-1", Rates: Rates{10, 12.50, 20, 0.25, 50, 0}},
	{Model: "claude-mythos-5-1", Rates: Rates{10, 12.50, 20, 0.25, 50, 0}},
	{Model: "claude-fable-5", Rates: Rates{10, 12.50, 20, 1, 50, 0}},
	{Model: "claude-mythos-5", Rates: Rates{10, 12.50, 20, 1, 50, 0}},
	{Model: "claude-opus-5-5", Rates: Rates{4, 5, 8, 0.20, 20, 8}},
	{Model: "claude-opus-5", Rates: Rates{5, 6.25, 10, 0.50, 25, 10}},
	{Model: "claude-opus-4-8", Rates: Rates{5, 6.25, 10, 0.50, 25, 10}},
	{Model: "claude-opus-4-7", Rates: Rates{5, 6.25, 10, 0.50, 25, 0}},
	{Model: "claude-opus-4-6", Rates: Rates{5, 6.25, 10, 0.50, 25, 0}},
	{Model: "claude-opus-4-5", Rates: Rates{5, 6.25, 10, 0.50, 25, 0}},
	{Model: "claude-opus-4-1", Rates: Rates{15, 18.75, 30, 1.50, 75, 0}},
	{Model: "claude-opus-4", Rates: Rates{15, 18.75, 30, 1.50, 75, 0}},
	{Model: "claude-sonnet-5-5", Rates: Rates{2, 2.50, 4, 0.10, 10, 0}},
	{Model: "claude-sonnet-5", Rates: Rates{2, 2.50, 4, 0.20, 10, 0}},
	{Model: "claude-sonnet-4-6", Rates: Rates{3, 3.75, 6, 0.30, 15, 0}},
	{Model: "claude-sonnet-4-5", Rates: Rates{3, 3.75, 6, 0.30, 15, 0}},
	{Model: "claude-sonnet-4", Rates: Rates{3, 3.75, 6, 0.30, 15, 0}},
	{Model: "claude-haiku-5-5", Rates: Rates{0.10, 0.125, 0.20, 0.01, 0.50, 0}, Long: &Rates{0.50, 0.625, 1, 0.05, 2.50, 0}},
	{Model: "claude-haiku-4-5", Rates: Rates{1, 1.25, 2, 0.10, 5, 0}},
}

// searchPrices are what a web search costs on the Claude API, on top of
// its tokens, by the day it took effect: $10 a thousand. A web fetch costs
// only its tokens.
var searchPrices = []struct {
	From time.Time
	USD  float64
}{
	{USD: 10.0 / 1000},
}

// billedRefusals are the refusal categories whose refusals are billed even
// before any output, by the day that took effect: those where Anthropic
// measures few false positives, as of September 2026. Any other category,
// or none, costs nothing before output; a refusal mid-stream bills its
// input and the output already streamed.
var billedRefusals = []struct {
	From       time.Time
	Categories []string
}{
	{Categories: []string{"bio", "frontier_llm", "reasoning_extraction"}},
}

// RefusalBilled reports whether a refusal of category, before any output,
// was billed at at.
func RefusalBilled(category string, at time.Time) bool {
	var cats []string
	from, found := time.Time{}, false
	for _, b := range billedRefusals {
		if !b.From.After(at) && (!found || b.From.After(from)) {
			cats, from, found = b.Categories, b.From, true
		}
	}
	return slices.Contains(cats, category)
}

// At is model's list prices for a request whose prompt is prompt tokens
// long, as they stood at at, and false for a model whose prices Irie does
// not know, such as a local one. A model ID names its model, then perhaps a
// date or a variant: claude-opus-4-5-20251101, claude-opus-5-5[1m], or a
// cloud's anthropic.claude-opus-4-5-20251101-v1:0.
func At(model string, prompt int64, at time.Time) (Rates, bool) {
	p, ok := priceAt(prices, model, at)
	if !ok {
		return Rates{}, false
	}
	if p.Long != nil && prompt > longFrom {
		return *p.Long, true
	}
	return p.Rates, true
}

// priceAt is the row of table in effect at at for model: of the rows of the
// longest name the model's ID starts with, the latest that took effect by
// then. A model whose rows all start later has no price then.
func priceAt(table []Price, model string, at time.Time) (Price, bool) {
	m := strings.ToLower(strings.TrimSpace(model))
	if i := strings.IndexByte(m, '['); i >= 0 {
		m = m[:i]
	}
	if i := strings.Index(m, "claude-"); i > 0 {
		m = m[i:]
	}
	name := ""
	for _, p := range table {
		rest, ok := strings.CutPrefix(m, p.Model)
		if ok && variant(rest) && len(p.Model) > len(name) {
			name = p.Model
		}
	}
	best, found := Price{}, false
	for _, p := range table {
		if p.Model == name && !p.From.After(at) && (!found || p.From.After(best.From)) {
			best, found = p, true
		}
	}
	return best, found && name != ""
}

// SearchAt is what one web search cost at at, in dollars.
func SearchAt(at time.Time) float64 {
	usd, from, found := 0.0, time.Time{}, false
	for _, s := range searchPrices {
		if !s.From.After(at) && (!found || s.From.After(from)) {
			usd, from, found = s.USD, s.From, true
		}
	}
	return usd
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

// Scaled are r as a request paid them: in fast mode, every price scales
// with the fast input price, and with inference held in the US
// (inference_geo "us") every price is 1.1 times as much.
func (r Rates) Scaled(speed, geo string) Rates {
	k := 1.0
	if speed == "fast" && r.Fast > 0 && r.Input > 0 {
		k = r.Fast / r.Input
	}
	if geo == "us" {
		k *= 1.1
	}
	return Rates{r.Input * k, r.Write5m * k, r.Write1h * k, r.Read * k, r.Output * k, r.Fast * k}
}
