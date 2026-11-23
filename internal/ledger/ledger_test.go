package ledger

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestPrices(t *testing.T) {
	for _, c := range []struct {
		model  string
		prompt int64
		write  float64 // the hour's write
		ok     bool
	}{
		{"claude-opus-5-5", 1000, 8, true},
		{"claude-opus-5", 1000, 10, true},
		{"claude-opus-4-5-20251101", 1000, 10, true},
		{"claude-opus-5-5[1m]", 1000, 8, true},
		{"anthropic.claude-opus-4-5-20251101-v1:0", 1000, 10, true},
		{"claude-opus-4-1", 1000, 30, true},
		{"claude-sonnet-5-5", 1000, 4, true},
		{"claude-haiku-5-5", 1000, 0.20, true},
		{"claude-haiku-5-5", 200_000, 1, true},
		{"claude-fable-5-2", 1000, 16, true},
		{"claude-opus-5-55", 1000, 0, false},
		{"qwen3.5:9b", 1000, 0, false},
		{"dory-4b", 1000, 0, false},
		{"<synthetic>", 1000, 0, false},
	} {
		p, ok := At(c.model, c.prompt, t0)
		if ok != c.ok || ok && !near(p.Write1h, c.write) {
			t.Errorf("At(%q, %d) = %+v, %v; want an hour's write of %v, %v", c.model, c.prompt, p, ok, c.write, c.ok)
		}
	}
	// Fast mode doubles Opus 5.5's prices, the cache's with them, and
	// inference held in the US costs a tenth more.
	p, _ := At("claude-opus-5-5", 1000, t0)
	if f := p.Scaled("fast", "global"); !near(f.Write1h, 16) || !near(f.Read, 0.40) || !near(f.Output, 40) {
		t.Errorf("fast %+v", f)
	}
	if u := p.Scaled("standard", "us"); !near(u.Write1h, 8.8) {
		t.Errorf("in the US %+v", u)
	}
	// A model without fast prices keeps its own in fast mode.
	s, _ := At("claude-sonnet-5-5", 1000, t0)
	if f := s.Scaled("fast", ""); !near(f.Write1h, 4) {
		t.Errorf("Sonnet in fast mode %+v", f)
	}
}

func TestAPriceChangeTakesEffectOnItsDay(t *testing.T) {
	day := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	table := []Price{
		{Model: "claude-sonnet-5", Rates: Rates{Input: 2, Output: 10}},
		{Model: "claude-sonnet-5", From: day, Rates: Rates{Input: 3, Output: 15}},
		{Model: "claude-sonnet-5-5", From: day, Rates: Rates{Input: 2, Output: 10}},
	}
	for _, c := range []struct {
		model string
		at    time.Time
		input float64
		ok    bool
	}{
		{"claude-sonnet-5", day.Add(-time.Second), 2, true}, // a call before the change keeps the old price
		{"claude-sonnet-5", day, 3, true},
		{"claude-sonnet-5-20270101", day.Add(time.Hour), 3, true},
		// A model whose only price starts later had none before it, and
		// never falls back to a shorter name's.
		{"claude-sonnet-5-5", day.Add(-time.Hour), 0, false},
		{"claude-sonnet-5-5", day, 2, true},
	} {
		p, ok := priceAt(table, c.model, c.at)
		if ok != c.ok || ok && !near(p.Rates.Input, c.input) {
			t.Errorf("%s at %s: %+v, %v; want input %v, %v", c.model, c.at, p.Rates, ok, c.input, c.ok)
		}
	}
	// No price in Irie's table has a date yet. One that gets one says so
	// in DESIGN §6.3, with a test of its day like the ones above.
	for _, p := range prices {
		if !p.From.IsZero() {
			t.Errorf("%s has a date: say when it changed in DESIGN §6.3 and test the change", p.Model)
		}
	}
}

func TestACallIsPricedByWhatItUsed(t *testing.T) {
	c := Call{At: t0, Purpose: Turn, Model: "claude-opus-5-5", Usage: Usage{Input: 1000, Write5m: 2000, Write1h: 3000, Read: 100_000, Output: 500}}
	want := (1000*4 + 2000*5 + 3000*8 + 100_000*0.20 + 500*20) / 1e6
	if usd, ok := c.Cost(); !ok || !near(usd, want) {
		t.Errorf("cost %v, %v; want %v", usd, ok, want)
	}
	// Web searches cost $10 a thousand on top, whatever the tokens.
	c.Usage.Searches = 3
	if usd, _ := c.Cost(); !near(usd, want+0.03) {
		t.Errorf("with 3 searches %v, want %v", usd, want+0.03)
	}
	// A long prompt on Haiku 5.5 pays its long prices on all of it.
	h := Call{At: t0, Model: "claude-haiku-5-5", Usage: Usage{Read: 150_000, Output: 1000}}
	if usd, _ := h.Cost(); !near(usd, (150_000*0.05+1000*2.50)/1e6) {
		t.Errorf("Haiku over 100k: %v", usd)
	}
	// A model with no price leaves the cost unknown, never $0; a free call
	// or one that used nothing costs nothing whatever its model.
	if _, ok := (Call{At: t0, Model: "dory-4b", Usage: Usage{Input: 10}}).Cost(); ok {
		t.Error("an unknown model has a price")
	}
	if usd, ok := (Call{At: t0, Model: "dory-4b", Usage: Usage{Input: 10}, Free: true}).Cost(); !ok || usd != 0 {
		t.Errorf("a free call costs %v, %v", usd, ok)
	}
	if usd, ok := (Call{At: t0, Model: "<synthetic>"}).Cost(); !ok || usd != 0 {
		t.Errorf("a call that used nothing costs %v, %v", usd, ok)
	}
}

func TestTotalsKeepUnpricedCallsApart(t *testing.T) {
	s := Sum{}
	opus := Call{At: t0, Purpose: Turn, Model: "claude-opus-5-5", Usage: Usage{Output: 100_000}} // $2
	s.Add(opus)
	s.Add(Call{At: t0, Purpose: Helper, Model: "claude-sonnet-5-5", Usage: Usage{Output: 50_000}}) // $0.50
	s.Add(Call{At: t0.Add(24 * time.Hour), Purpose: Turn, Model: "mystery-1", Usage: Usage{Input: 5}})
	s.Add(Call{At: t0, Purpose: Card, Model: "bonsai-2:9b", Usage: Usage{Input: 900, Output: 80}, Free: true})
	all := s.Total("")
	if all.Calls != 4 || !near(all.USD, 2.5) || all.Unpriced != 1 || all.Free != 1 || all.Known() {
		t.Errorf("total %+v", all)
	}
	if w := all.Words(); w != "$2.50, plus 1 call with no price" {
		t.Errorf("words %q", w)
	}
	// A day's total counts only its calls.
	if day := s.Total(Day(t0.Add(24 * time.Hour))); day.Calls != 1 || day.Words() != "no price" {
		t.Errorf("the second day %+v %q", day, day.Words())
	}
	byPurpose := s.By(func(k Key) string { return k.Purpose }, "")
	if r := Ranked(byPurpose); len(r) != 3 || r[0] != Turn || r[1] != Helper || r[2] != Card {
		t.Errorf("ranked %v", r)
	}
	if free := s.AllFree().Total(""); free.Free != 4 || free.USD != 0 || free.Unpriced != 0 || free.Words() != "free" {
		t.Errorf("all free %+v", free)
	}
	// A reply's later line brings its usage up to date: the call it
	// replaces comes out first.
	s.Remove(opus)
	opus.Usage.Output = 200_000
	s.Add(opus)
	if got := s.Total("").USD; !near(got, 4.5) {
		t.Errorf("after an update %v, want 4.5", got)
	}
	if (Tally{}).Words() != "nothing" {
		t.Error("no calls")
	}
}

func TestTotalsReadBackAsWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "costs.json")
	s := Sum{}
	s.Add(Call{At: t0, Purpose: Turn, Model: "claude-opus-5-5", Usage: Usage{Output: 1000, Searches: 2}})
	s.Add(Call{At: t0, Purpose: Compaction, Model: "claude-opus-5-5", Usage: Usage{Input: 180_000, Output: 3500}})
	if err := WriteTotals(path, s); err != nil {
		t.Fatal(err)
	}
	got, ok := ReadTotals(path)
	if !ok || len(got) != 2 || !near(got.Total("").USD, s.Total("").USD) || got.Total("").Usage.Searches != 2 {
		t.Fatalf("read %v, %v", got, ok)
	}
	// Totals priced another way are priced again.
	if err := os.WriteFile(path, []byte(`{"v":1,"rows":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadTotals(path); ok {
		t.Error("an old version reads")
	}
}

func TestOwnCallsAreAppendedAndTrimmed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	old := NewEntry(Call{At: t0.Add(-40 * 24 * time.Hour), Purpose: Route, Model: "embeddinggemma-2:270m", Usage: Usage{Input: 12}, Free: true}, "127.0.0.1", 80*time.Millisecond)
	card := NewEntry(Call{At: t0, Purpose: Card, Model: "bonsai-2:9b", Usage: Usage{Input: 900, Output: 80}, Free: true}, "127.0.0.1", 9*time.Second)
	cloud := NewEntry(Call{At: t0, Purpose: Card, Model: "gpt-oss:120b-cloud", Usage: Usage{Input: 900, Output: 80}}, "127.0.0.1", time.Second)
	for _, e := range []Entry{old, card, cloud} {
		if err := Append(path, e); err != nil {
			t.Fatal(err)
		}
	}
	if card.USD == nil || *card.USD != 0 || cloud.USD != nil {
		t.Errorf("a free call's price %v, a cloud model's %v", card.USD, cloud.USD)
	}
	// A line a crash cut short is skipped.
	f, _ := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	f.WriteString(`{"at":"2026-11-14T09:00:00Z","purp`)
	f.Close()
	got, err := ReadEntries(path, t0.Add(-30*24*time.Hour))
	if err != nil || len(got) != 2 || got[0].Millis != 9000 || got[0].Server != "127.0.0.1" {
		t.Fatalf("read %+v, %v", got, err)
	}
	if tot := SumOf(got).Total(""); tot.Free != 1 || tot.Unpriced != 1 {
		t.Errorf("own calls %+v", tot)
	}
	if err := Trim(path, t0.Add(-30*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	all, _ := ReadEntries(path, time.Time{})
	if len(all) != 2 {
		t.Errorf("after the trim %d entries, want 2", len(all))
	}
}

func TestCloudModels(t *testing.T) {
	for name, want := range map[string]bool{
		"gpt-oss:120b-cloud": true, "deepseek-v3.1:671b-cloud": true, "kimi-k2:cloud": true,
		"bonsai-2:9b": false, "embeddinggemma-2:270m": false, "cloudy": false, "qwen3.5": false,
	} {
		if IsCloudModel(name) != want {
			t.Errorf("IsCloudModel(%q) = %v", name, !want)
		}
	}
}
