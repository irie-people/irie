package core

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/ledger"
	"github.com/irie-people/irie/internal/localmodel"
	"github.com/irie-people/irie/internal/route"
	"github.com/irie-people/irie/internal/testutil/ollamafake"
)

// usageLine is a transcript line of a reply with its usage, as Claude Code
// writes one; more goes into the line's top level, such as a helper's
// isSidechain and agentId.
func usageLine(id, model string, at time.Time, usage, more string) string {
	if more != "" {
		more = "," + more
	}
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q%s,"message":{"id":%q,"model":%q,"usage":%s}}`+"\n",
		at.UTC().Format(time.RFC3339Nano), more, id, model, usage)
}

func writeLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "")), 0o600); err != nil {
		t.Fatal(err)
	}
}

func nearUSD(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestTheTallyPricesEachCallOnce(t *testing.T) {
	began := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	main := filepath.Join(dir, "conv.jsonl")
	writeLines(t, main,
		// A fork's copy of the conversation it forked from is not its own.
		usageLine("m0", "claude-opus-5-5", began.Add(-time.Hour), `{"input_tokens":5,"output_tokens":1000000}`, ""),
		// A reply's first line, then its last with all of its output.
		usageLine("m1", "claude-opus-5-5", began.Add(time.Minute), `{"input_tokens":1000,"cache_read_input_tokens":100000,"cache_creation_input_tokens":3000,"cache_creation":{"ephemeral_1h_input_tokens":3000,"ephemeral_5m_input_tokens":0},"output_tokens":10}`, ""),
		usageLine("m1", "claude-opus-5-5", began.Add(time.Minute), `{"input_tokens":1000,"cache_read_input_tokens":100000,"cache_creation_input_tokens":3000,"cache_creation":{"ephemeral_1h_input_tokens":3000,"ephemeral_5m_input_tokens":0},"output_tokens":500}`, ""),
		// A compaction and the advisor's answer, which its top level leaves
		// out, with two web searches.
		usageLine("m2", "claude-sonnet-5-5", began.Add(2*time.Minute), `{"input_tokens":23000,"output_tokens":1000,"server_tool_use":{"web_search_requests":2},"iterations":[`+
			`{"type":"compaction","input_tokens":180000,"output_tokens":3500},`+
			`{"type":"advisor_message","model":"claude-opus-5-5","input_tokens":800,"output_tokens":1600},`+
			`{"type":"message","input_tokens":23000,"output_tokens":1000}]}`, ""),
		// A helper's part, written into the conversation's transcript.
		usageLine("h1", "claude-haiku-5-5", began.Add(3*time.Minute), `{"input_tokens":2000,"output_tokens":100}`, `"isSidechain":true,"agentId":"a1"`),
		// A reply Claude Code writes itself used nothing, and costs nothing.
		usageLine("s1", "<synthetic>", began.Add(4*time.Minute), `{"input_tokens":0,"output_tokens":0}`, ""),
	)
	tally := tokenTally{since: began}
	tally.update([]string{main})
	s := tally.spent([]string{main})
	by := s.By(func(k ledger.Key) string { return k.Purpose }, "")

	turn := (1000*4 + 100_000*0.20 + 3000*8 + 500*20 + 23000*2 + 1000*10) / 1e6
	turn += 2 * 0.01 // the searches
	if got := by[ledger.Turn]; got.Calls != 3 || !nearUSD(got.USD, turn) || got.Usage.Searches != 2 {
		t.Errorf("turns %+v, want 3 calls at $%v", got, turn)
	}
	if got := by[ledger.Compaction]; got.Calls != 1 || !nearUSD(got.USD, (180_000*2+3500*10)/1e6) {
		t.Errorf("compaction %+v", got)
	}
	if got := by[ledger.Advisor]; got.Calls != 1 || !nearUSD(got.USD, (800*4+1600*20)/1e6) {
		t.Errorf("advisor, at Opus's prices: %+v", got)
	}
	if got := by[ledger.Helper]; got.Calls != 1 || !nearUSD(got.USD, (2000*0.10+100*0.50)/1e6) {
		t.Errorf("helper %+v", got)
	}
	if total := s.Total(""); total.Calls != 6 || !total.Known() {
		t.Errorf("total %+v", total)
	}

	// A call to a model with no price leaves the session's cost unknown;
	// a local profile's calls are all free.
	sub := filepath.Join(dir, "agent-a2.jsonl")
	writeLines(t, sub, usageLine("d1", "dory-4b", began.Add(5*time.Minute), `{"input_tokens":12000,"output_tokens":300}`, `"isSidechain":true,"agentId":"a2"`))
	tally.update([]string{main, sub})
	x := &session{tally: tally, track: track{transcripts: []string{main, sub}}}
	if got := x.costUSD(); got != nil {
		t.Errorf("with an unpriced call the cost is %v, want unknown", *got)
	}
	if w := x.spent().Total("").Words(); !strings.HasSuffix(w, ", plus 1 call with no price") {
		t.Errorf("words %q", w)
	}
	x.local = true
	if got := x.costUSD(); got == nil || *got != 0 {
		t.Errorf("a local profile's cost %v, want free", got)
	}
	if free := x.spent().Total(""); free.Free != 7 || free.Words() != "free" {
		t.Errorf("a local profile's calls %+v", free)
	}
}

func TestTheRouterReadsTheLedgerNotTheStatusLine(t *testing.T) {
	c := bareCore(t, config.Default())
	status := func(id string, usd float64) {
		t.Helper()
		os.MkdirAll(c.paths.Session(id), 0o700)
		b, _ := json.Marshal(map[string]any{"model": "claude-opus-5-5", "cost_usd": usd})
		if err := os.WriteFile(c.paths.StatusFile(id), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Now().Add(-time.Hour)
	// Claude Code priced a local model's calls; the ledger never counts them.
	status("slocal", 0.47)
	if err := host.WriteSpec(c.paths.SpecFile("slocal"), host.Spec{Session: "slocal", Profile: "dory", Argv: []string{"claude"}, Local: true}); err != nil {
		t.Fatal(err)
	}
	if got := c.goneCost("slocal"); got != nil {
		t.Errorf("a local session's cost %v, want none", *got)
	}
	// A reopen started its status line again: the ledger has every call.
	status("sreopened", 6.43)
	whole := ledger.Sum{}
	whole.Add(ledger.Call{At: at, Purpose: ledger.Turn, Model: "claude-opus-5-5", Usage: ledger.Usage{Output: 2_289_000}}) // $45.78
	if err := ledger.WriteTotals(c.paths.CostsFile("sreopened"), whole); err != nil {
		t.Fatal(err)
	}
	if got := c.goneCost("sreopened"); got == nil || !nearUSD(*got, 45.78) {
		t.Errorf("a reopened session's cost %v, want $45.78", got)
	}
	// A call with no price makes it unknown.
	unknown := whole.Clone()
	unknown.Add(ledger.Call{At: at, Purpose: ledger.Turn, Model: "mystery-1", Usage: ledger.Usage{Input: 5}})
	ledger.WriteTotals(c.paths.CostsFile("sunknown"), unknown)
	if got := c.goneCost("sunknown"); got != nil {
		t.Errorf("an unpriced session's cost %v, want unknown", *got)
	}
	// A session the ledger never priced keeps its status line's figure.
	status("sold", 3.10)
	if got := c.goneCost("sold"); got == nil || *got != 3.10 {
		t.Errorf("an old session's cost %v, want $3.10", got)
	}

	// Three gone Dory sessions that Claude Code priced at $0.36 each leave
	// Dory free to the router, as the owner's rule has it.
	for i := range 3 {
		id := fmt.Sprintf("sdory%d", i)
		status(id, 0.36)
		host.WriteSpec(c.paths.SpecFile(id), host.Spec{Session: id, Profile: "dory", Argv: []string{"claude"}, Local: true})
		c.outcomes[id] = &outcome{profile: "dory", class: route.Small, ended: at, cost: c.goneCost(id)}
	}
	c.mu.Lock()
	stats := c.stats(time.Now())
	c.mu.Unlock()
	if n := stats.Counts("dory", route.Small); n.Good != 3 {
		t.Fatalf("dory's outcomes %+v", n)
	}
	if mean, ok := stats.MeanCost("dory", route.Small); ok {
		t.Errorf("dory's sessions cost $%v on average, want no cost", mean)
	}
}

func TestAClosedSessionKeepsItsLedger(t *testing.T) {
	c := bareCore(t, config.Default())
	began := time.Now().Add(-2 * time.Hour)
	dir := t.TempDir()
	conv := filepath.Join(dir, "conv.jsonl")
	writeLines(t, conv, usageLine("m1", "claude-opus-5-5", began.Add(time.Minute), `{"output_tokens":100000}`, "")) // $2
	os.MkdirAll(c.paths.Session("sgone"), 0o700)
	gone := map[string]*goneSession{"sgone": {began: began, transcripts: []string{conv}}, "snone": {began: began}}
	c.outcomes["sgone"] = &outcome{profile: "claude", class: route.Medium}
	if n := c.priceGone(gone); n != 1 {
		t.Fatalf("priced %d sessions, want 1", n)
	}
	s, ok := ledger.ReadTotals(c.paths.CostsFile("sgone"))
	if !ok || !nearUSD(s.Total("").USD, 2) {
		t.Fatalf("its totals %v, %v", s, ok)
	}
	if o := c.outcomes["sgone"]; o.cost == nil || !nearUSD(*o.cost, 2) {
		t.Errorf("its outcome's cost %v", o.cost)
	}
	// Priced once: nothing changed, so nothing is priced again, until its
	// agent writes after its totals were kept.
	if n := c.priceGone(gone); n != 0 {
		t.Errorf("priced %d again", n)
	}
	later := time.Now().Add(time.Minute)
	os.Chtimes(conv, later, later)
	if !c.needsPricing("sgone", []string{conv}) {
		t.Error("a transcript newer than its totals")
	}
}

func TestTheCostsReport(t *testing.T) {
	c := bareCore(t, config.Default())
	now := time.Now()
	dir := t.TempDir()
	conv := filepath.Join(dir, "conv.jsonl")
	writeLines(t, conv,
		usageLine("m1", "claude-opus-5-5", now, `{"output_tokens":50000}`, ""), // $1
		usageLine("m2", "mystery-1", now, `{"input_tokens":20}`, ""),
	)
	x := &session{id: "sopen", name: "open one", tally: tokenTally{since: now.Add(-time.Hour)}, track: track{transcripts: []string{conv}}}
	usd := 0.50
	x.usage.CostUSD = &usd
	x.tally.update(x.track.transcripts)
	c.sessions["sopen"] = x
	closed := ledger.Sum{}
	closed.Add(ledger.Call{At: now.Add(-3 * 24 * time.Hour), Purpose: ledger.Helper, Model: "claude-sonnet-5-5", Usage: ledger.Usage{Output: 300_000}}) // $3
	os.MkdirAll(c.paths.Session("sclosed"), 0o700)
	ledger.WriteTotals(c.paths.CostsFile("sclosed"), closed)

	// Irie's own call to a local model goes in the ledger, free.
	f := ollamafake.New(0, []ollamafake.Model{{Name: "embeddinggemma-2:270m", Size: 378 << 20, Embeds: true}})
	srv := httptest.NewServer(f.Handler())
	defer srv.Close()
	if _, err := (localmodel.Ollama{URL: srv.URL}).Embed(c.metered(context.Background(), ledger.Route), "embeddinggemma-2:270m", []string{"fix the typo"}, ""); err != nil {
		t.Fatal(err)
	}
	entries, err := ledger.ReadEntries(c.paths.Ledger(), time.Time{})
	if err != nil || len(entries) != 1 || entries[0].Purpose != ledger.Route || !entries[0].Free || entries[0].Usage.Input != 3 || entries[0].Server != "127.0.0.1" {
		t.Fatalf("Irie's own ledger %+v, %v", entries, err)
	}

	r := c.costsOp()
	if len(r.Spans) != 3 || r.Spans[0].Name != "today" || r.Spans[2].Name != "month" {
		t.Fatalf("spans %+v", r.Spans)
	}
	today, month := r.Spans[0], r.Spans[2]
	if today.Total.Calls != 3 || today.Total.USD != 1 || today.Total.Unpriced != 1 || today.Total.Free != 1 || today.Total.Words != "$1.00, plus 1 call with no price" {
		t.Errorf("today %+v", today.Total)
	}
	if month.Total.Calls != 4 || month.Total.USD != 4 {
		t.Errorf("the month %+v", month.Total)
	}
	if p := month.Purposes; len(p) != 3 || p[0].Name != ledger.Helper || p[1].Name != ledger.Turn || p[2].Name != ledger.Route || p[2].Words != "free" {
		t.Errorf("purposes %+v", p)
	}
	if len(r.Sessions) != 2 || r.Sessions[0].ID != "sclosed" || r.Sessions[0].Open || r.Sessions[1].ID != "sopen" || !r.Sessions[1].Open {
		t.Fatalf("sessions %+v", r.Sessions)
	}
	if s := r.Sessions[1]; s.Claude == nil || *s.Claude != 0.50 || s.Ledger.Unpriced != 1 {
		t.Errorf("the open session %+v", s)
	}
	// The report reads back as the CLI gets it.
	b, _ := json.Marshal(r)
	var back ipc.CostsResult
	if err := json.Unmarshal(b, &back); err != nil || len(back.Sessions) != 2 {
		t.Errorf("round trip %v", err)
	}
}
