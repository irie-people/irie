package rebuild

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)

// reply is a transcript line of an assistant reply with its usage, as
// Claude Code writes one: w1h and w5m are what it wrote into the hour's
// cache and the five minutes'.
func reply(id string, at time.Time, in, w1h, w5m, read, out int64) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"id":%q,"model":"claude-opus-5-5","stop_reason":"end_turn","usage":{"input_tokens":%d,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d,"output_tokens":%d,"cache_creation":{"ephemeral_1h_input_tokens":%d,"ephemeral_5m_input_tokens":%d},"service_tier":"standard","inference_geo":"global","speed":"standard"}}}`,
		at.Format(time.RFC3339Nano), id, in, w1h+w5m, read, out, w1h, w5m)
}

// read feeds lines to a transcript, a reply's first line as its first, and
// returns the rebuilds it found.
func read(t *Transcript, lines ...string) []Rebuild {
	var out []Rebuild
	seen := map[string]bool{}
	for _, s := range lines {
		l, ok := ParseLine([]byte(s))
		if !ok {
			continue
		}
		first := !seen[l.ID]
		if l.Usage {
			seen[l.ID] = true
		}
		if b, ok := t.Read(l, first); ok {
			out = append(out, b)
		}
	}
	return out
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestTheResearchHolds(t *testing.T) {
	// A rebuild writes the context into the cache once; a warm turn reads
	// it. On Opus 5.5 a write into the hour's cache costs as much as 40
	// reads, and into the five minutes' cache 25.
	p, ok := Price("claude-opus-5-5", 50_000)
	if !ok {
		t.Fatal("no price for Opus 5.5")
	}
	if got := p.Write1h / p.Read; !near(got, 40) {
		t.Errorf("an hour's write is %v reads, want 40", got)
	}
	if got := p.Write5m / p.Read; !near(got, 25) {
		t.Errorf("five minutes' write is %v reads, want 25", got)
	}
}

func TestWarmTurnsAreNoRebuild(t *testing.T) {
	var tr Transcript
	got := read(&tr,
		reply("m1", t0, 3, 60_000, 0, 0, 500),                        // the first request writes everything
		reply("m2", t0.Add(2*time.Minute), 3, 2_000, 0, 60_000, 400), // reads the context, writes what is new
		reply("m3", t0.Add(50*time.Minute), 3, 1_500, 0, 62_000, 300),
	)
	if len(got) != 0 {
		t.Fatalf("rebuilds %+v, want none", got)
	}
}

func TestAColdWakeIsARebuild(t *testing.T) {
	var tr Transcript
	got := read(&tr,
		reply("m1", t0, 3, 300_000, 0, 0, 500),
		reply("m2", t0.Add(time.Minute), 3, 1_000, 0, 300_000, 800),
		// Two hours later the hour's cache has run out: everything is
		// written again but the system prompt, which other sessions keep.
		reply("m3", t0.Add(2*time.Hour), 3, 284_000, 0, 18_000, 200),
	)
	if len(got) != 1 {
		t.Fatalf("rebuilds %+v, want one", got)
	}
	b := got[0]
	// The context before was 301,003 tokens; 18,000 were read, so 283,003
	// were written again, at $8 a million.
	if b.ID != "m3" || b.Tokens != 283_003 || !b.Cold || b.TTL != time.Hour || b.Idle != 2*time.Hour-time.Minute {
		t.Errorf("rebuild %+v", b)
	}
	if want := 283_003 * 8 / 1e6; !b.Priced || !near(b.USD, want) {
		t.Errorf("cost %v (priced %v), want %v", b.USD, b.Priced, want)
	}
	if b.Before.ID != "m2" {
		t.Errorf("the request before is %q, want m2", b.Before.ID)
	}
}

func TestFiveMinutesCache(t *testing.T) {
	var tr Transcript
	got := read(&tr,
		reply("m1", t0, 3, 0, 80_000, 0, 500),
		reply("m2", t0.Add(10*time.Minute), 3, 0, 80_600, 0, 100),
	)
	if len(got) != 1 {
		t.Fatalf("rebuilds %+v, want one", got)
	}
	b := got[0]
	if !b.Cold || b.TTL != 5*time.Minute || b.Tokens != 80_003 {
		t.Errorf("rebuild %+v", b)
	}
	if want := 80_003 * 5 / 1e6; !near(b.USD, want) {
		t.Errorf("cost %v, want %v at $5 a million", b.USD, want)
	}
}

func TestColdAllowsForLongReplies(t *testing.T) {
	if got := ColdAfter(5 * time.Minute); got != 3*time.Minute {
		t.Errorf("five minutes' cache: cold after %v, want 3m", got)
	}
	if got := ColdAfter(time.Hour); got != 54*time.Minute {
		t.Errorf("an hour's cache: cold after %v, want 54m", got)
	}
	// A reply that took two minutes to write makes the idle read short.
	var tr Transcript
	got := read(&tr,
		reply("m1", t0, 3, 0, 80_000, 0, 500),
		reply("m2", t0.Add(3*time.Minute+30*time.Second), 3, 0, 80_600, 0, 100),
	)
	if len(got) != 1 || !got[0].Cold {
		t.Errorf("rebuilds %+v, want one that is cold", got)
	}
}

func TestAChangedPromptIsNotCold(t *testing.T) {
	var tr Transcript
	got := read(&tr,
		reply("m1", t0, 3, 100_000, 0, 0, 500),
		// A minute later the prompt's start changed, as for other tools.
		reply("m2", t0.Add(time.Minute), 3, 100_400, 0, 0, 100),
	)
	if len(got) != 1 || got[0].Cold {
		t.Fatalf("rebuilds %+v, want one that is not cold", got)
	}
}

func TestACompactionStartsAgain(t *testing.T) {
	var tr Transcript
	got := read(&tr,
		reply("m1", t0, 3, 400_000, 0, 0, 500),
		`{"type":"system","subtype":"compact_boundary","timestamp":"2026-11-14T09:30:00Z","content":"Conversation compacted"}`,
		reply("m2", t0.Add(31*time.Minute), 3, 30_000, 0, 0, 100),
	)
	if len(got) != 0 {
		t.Fatalf("rebuilds %+v after a compaction, want none", got)
	}
	if _, ok := tr.Wake(); !ok {
		t.Error("no wake after the request that followed the compaction")
	}
}

func TestSmallConversationsDoNotCount(t *testing.T) {
	var tr Transcript
	got := read(&tr,
		reply("m1", t0, 3, 15_000, 0, 0, 100),
		reply("m2", t0.Add(3*time.Hour), 3, 15_200, 0, 0, 100),
	)
	if len(got) != 0 {
		t.Fatalf("rebuilds %+v, want none under %d tokens", got, MinTokens)
	}
}

func TestAReplysLaterLinesCountOnce(t *testing.T) {
	var tr Transcript
	first := reply("m1", t0, 3, 50_000, 0, 0, 10)
	later := reply("m1", t0, 3, 50_000, 0, 0, 900)
	got := read(&tr, first, later, later)
	if len(got) != 0 {
		t.Fatalf("rebuilds %+v from one reply's lines", got)
	}
	w, ok := tr.Wake()
	if !ok || w.Tokens != 50_903 {
		t.Fatalf("wake %+v (%v), want the context and the reply's whole output", w, ok)
	}
	// The same reply's next line, even if called the first, is not a request.
	var again Transcript
	l, _ := ParseLine([]byte(first))
	again.Read(l, true)
	l, _ = ParseLine([]byte(later))
	if _, ok := again.Read(l, true); ok {
		t.Error("a reply's later line counted as a rebuild")
	}
}

func TestHelpersHaveChainsOfTheirOwn(t *testing.T) {
	side := func(agent, s string) string {
		return strings.Replace(s, `{"type":"assistant",`, `{"type":"assistant","isSidechain":true,`+agent, 1)
	}
	var tr Transcript
	got := read(&tr,
		reply("m1", t0, 3, 200_000, 0, 0, 100),
		// A helper's requests, written in the same transcript, start
		// small: none of them is the conversation's rebuild.
		side(`"agentId":"a1",`, reply("s1", t0.Add(time.Minute), 3, 25_000, 0, 0, 100)),
		side(`"agentId":"a1",`, reply("s2", t0.Add(2*time.Minute), 3, 500, 0, 25_000, 100)),
		side("", reply("s3", t0.Add(3*time.Minute), 3, 40_000, 0, 0, 100)),
		reply("m2", t0.Add(4*time.Minute), 3, 1_000, 0, 200_000, 100),
	)
	if len(got) != 0 {
		t.Fatalf("rebuilds %+v, want none", got)
	}
	got = read(&tr, side(`"agentId":"a1",`, reply("s4", t0.Add(3*time.Hour), 3, 25_600, 0, 0, 100)))
	if len(got) != 1 || got[0].Agent != "a1" {
		t.Fatalf("helper's rebuild %+v", got)
	}
}

func TestAnAPIErrorsReplyIsNoRequest(t *testing.T) {
	var tr Transcript
	got := read(&tr,
		reply("m1", t0, 3, 100_000, 0, 0, 100),
		`{"type":"assistant","timestamp":"2026-11-14T09:01:00Z","isApiErrorMessage":true,"message":{"id":"e1","model":"<synthetic>","usage":{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`,
		reply("m2", t0.Add(2*time.Minute), 3, 600, 0, 100_000, 100),
	)
	if len(got) != 0 {
		t.Fatalf("rebuilds %+v, want none", got)
	}
}

func TestWakeSaysWhenTheCacheRunsOutAndThePrice(t *testing.T) {
	var tr Transcript
	read(&tr, reply("m1", t0, 3, 249_000, 0, 0, 997))
	w, ok := tr.Wake()
	if !ok {
		t.Fatal("no wake")
	}
	if w.Tokens != 250_000 || w.TTL != time.Hour || !w.Cold.Equal(t0.Add(time.Hour)) || !w.Priced || !near(w.USD, 2) {
		t.Errorf("wake %+v, want 250k tokens cold at 10:00 for $2.00", w)
	}
	if w.IsCold(t0.Add(59*time.Minute)) || !w.IsCold(t0.Add(time.Hour)) {
		t.Error("cold at the wrong time")
	}
	var none Transcript
	if _, ok := none.Wake(); ok {
		t.Error("a wake before any request")
	}
}

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
		{"<synthetic>", 1000, 0, false},
	} {
		p, ok := Price(c.model, c.prompt)
		if ok != c.ok || ok && !near(p.Write1h, c.write) {
			t.Errorf("Price(%q, %d) = %+v, %v; want an hour's write of %v, %v", c.model, c.prompt, p, ok, c.write, c.ok)
		}
	}
	// Fast mode doubles Opus 5.5's prices, the cache's with them, and
	// inference held in the US costs a tenth more.
	p, _ := Price("claude-opus-5-5", 1000)
	if f := p.scaled("fast", "global"); !near(f.Write1h, 16) || !near(f.Read, 0.40) {
		t.Errorf("fast %+v", f)
	}
	if u := p.scaled("standard", "us"); !near(u.Write1h, 8.8) {
		t.Errorf("in the US %+v", u)
	}
	// A model without fast prices keeps its own in fast mode.
	s, _ := Price("claude-sonnet-5-5", 1000)
	if f := s.scaled("fast", ""); !near(f.Write1h, 4) {
		t.Errorf("Sonnet in fast mode %+v", f)
	}
}

func TestALongPromptOnHaikuPaysMore(t *testing.T) {
	r := Request{Model: "claude-haiku-5-5", Write: 150_000, Write1h: 150_000}
	usd, ok := r.writeCost(150_000)
	if !ok || !near(usd, 0.15) {
		t.Errorf("cost %v (%v), want $0.15 at $1 a million", usd, ok)
	}
}

func TestLastWakeReadsATranscriptsEnd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	var b strings.Builder
	// More than LastWake reads, so its first line is cut.
	for b.Len() < tailSize+1000 {
		b.WriteString(`{"type":"user","message":{"role":"user","content":"` + strings.Repeat("x", 900) + `"}}` + "\n")
	}
	b.WriteString(reply("m1", t0, 3, 120_000, 0, 0, 100) + "\n")
	b.WriteString(reply("m1", t0, 3, 120_000, 0, 0, 997) + "\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	w, ok := LastWake(path)
	if !ok || w.Tokens != 121_000 || !w.Last.Equal(t0) {
		t.Fatalf("wake %+v (%v)", w, ok)
	}
	if _, ok := LastWake(filepath.Join(t.TempDir(), "none.jsonl")); ok {
		t.Error("a wake from no transcript")
	}
}

func TestWords(t *testing.T) {
	for _, c := range []struct{ got, want string }{
		{Dollars(4.1), "$4.10"},
		{Dollars(0.004), "<$0.01"},
		{Dollars(0), "$0.00"},
		{Tokens(950), "950"},
		{Tokens(310_400), "310k"},
		{Tokens(999_600), "1.0M"},
		{Tokens(1_234_000), "1.2M"},
		{Times(1), "once"},
		{Times(2), "twice"},
		{Times(3), "3 times"},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}
