package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/journal"
	"github.com/irie-people/irie/internal/rebuild"
)

// cachedReply is a transcript line of an Opus 5.5 reply, as Claude Code
// writes one: at its time, with what it wrote into the hour's cache and
// read from the cache. tool says it stopped to call a tool.
func cachedReply(id string, at time.Time, w1h, read, out int64, tool bool) string {
	stop := "end_turn"
	if tool {
		stop = "tool_use"
	}
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"id":%q,"model":"claude-opus-5-5","stop_reason":%q,"usage":{"input_tokens":3,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d,"output_tokens":%d,"cache_creation":{"ephemeral_1h_input_tokens":%d,"ephemeral_5m_input_tokens":0},"inference_geo":"global","speed":"standard"}}}`+"\n",
		at.UTC().Format(time.RFC3339Nano), id, stop, w1h, read, out, w1h)
}

// metered is a Claude Code session of c whose transcript a test writes:
// write appends lines to it, and ev folds an event into its state at a
// time.
type metered struct {
	x    *session
	path string
	t    *testing.T
}

func meteredSession(t *testing.T, c *Core, id string, born time.Time) *metered {
	t.Helper()
	m := &metered{x: &session{id: id, name: id, harness: "claude-code", observed: true}, path: filepath.Join(t.TempDir(), id+".jsonl"), t: t}
	m.ev(born, event.SpawnRequested, nil)
	m.ev(born, event.SessionStarted, map[string]any{"transcript": m.path})
	c.mu.Lock()
	c.sessions[id] = m.x
	c.mu.Unlock()
	return m
}

func (m *metered) write(lines ...string) {
	m.t.Helper()
	f, err := os.OpenFile(m.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		m.t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(strings.Join(lines, "")); err != nil {
		m.t.Fatal(err)
	}
}

func (m *metered) ev(at time.Time, kind string, data map[string]any) {
	m.x.track.apply(event.Event{TS: event.Stamp(at), Kind: kind, Agent: event.Main, Data: data})
}

// count is a tick's reading of the transcripts.
func count(c *Core) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.countTokens()
}

func rebuildEvents(t *testing.T, c *Core) []event.Event {
	t.Helper()
	evs, err := journal.ReadKind(c.paths.Journal(), time.Now().Add(-rebuildKeep), event.CacheRebuilt)
	if err != nil {
		t.Fatal(err)
	}
	return evs
}

func TestTheMeterJournalsEachRebuildOnceWithWhatWokeIt(t *testing.T) {
	c := historyCore(t)
	t0 := time.Now().Add(-4 * time.Hour).Truncate(time.Second)
	m := meteredSession(t, c, "s0000000000a1", t0.Add(-time.Minute))
	m.ev(t0.Add(-30*time.Second), event.PromptSubmitted, nil)
	m.write(
		cachedReply("m1", t0, 300_000, 0, 500, true),
		cachedReply("m2", t0.Add(time.Minute), 1_000, 300_003, 800, false),
	)
	count(c)
	m.ev(t0.Add(2*time.Minute), event.TurnEnded, map[string]any{})
	// Three hours later you write to it: the hour's cache has run out.
	at := t0.Add(3 * time.Hour)
	m.ev(at.Add(-20*time.Second), event.PromptSubmitted, nil)
	m.write(cachedReply("m3", at, 284_000, 18_000, 200, false))
	count(c)
	count(c)

	evs := rebuildEvents(t, c)
	if len(evs) != 1 {
		t.Fatalf("journalled %d rebuilds, want 1: %+v", len(evs), evs)
	}
	e := evs[0]
	tokens, _ := e.Int("tokens")
	usd, _ := e.Data["usd"].(float64)
	// The context before was 301,006 tokens, of which 18,000 were read:
	// 283,006 were written again, at $8 a million.
	if e.Session != "s0000000000a1" || e.Source != event.SourceTranscript || e.Str("cause") != ipc.RebuildYou || e.Str("message_id") != "m3" ||
		tokens != 283_006 || e.Str("ttl") != "1h" || e.Str("name") != "s0000000000a1" {
		t.Errorf("event %+v", e)
	}
	if want := 2.264; usd != want {
		t.Errorf("usd %v, want %v", usd, want)
	}

	// The row says it: once today's rebuilds cost a dollar, in words.
	c.mu.Lock()
	r := c.rebuildsRow("s0000000000a1", at.Add(time.Minute))
	c.mu.Unlock()
	if r == nil || r.Today != 1 || r.TodayUSD != 2.264 || r.Week != 1 || r.Cause != ipc.RebuildYou || r.Words != "$2.26 today: woke cold once" {
		t.Errorf("row %+v", r)
	}

	// A restarted core reads the transcript again from its start, but
	// journals nothing twice.
	c.mu.Lock()
	m.x.tally = tokenTally{}
	c.meter = meter{}
	c.loadMeter(time.Now())
	c.mu.Unlock()
	count(c)
	if evs := rebuildEvents(t, c); len(evs) != 1 {
		t.Errorf("after a restart, %d rebuilds journalled", len(evs))
	}
	c.mu.Lock()
	if n := len(c.meter.by["s0000000000a1"]); n != 1 {
		t.Errorf("the meter holds %d rebuilds after a restart", n)
	}
	c.mu.Unlock()
}

func TestALocalModelsCacheIsNotMetered(t *testing.T) {
	c := historyCore(t)
	t0 := time.Now().Add(-4 * time.Hour)
	m := meteredSession(t, c, "s0000000000a1", t0.Add(-time.Minute))
	m.x.local = true
	m.write(cachedReply("m1", t0, 300_000, 0, 500, false), cachedReply("m2", t0.Add(3*time.Hour), 300_600, 0, 500, false))
	count(c)
	if evs := rebuildEvents(t, c); len(evs) != 0 {
		t.Errorf("journalled %+v", evs)
	}
}

func TestWhatWokeASession(t *testing.T) {
	t0 := time.Date(2026, 11, 14, 1, 0, 0, 0, time.UTC)
	cold := func(before time.Time, tool bool, at time.Time) rebuild.Rebuild {
		return rebuild.Rebuild{Request: rebuild.Request{At: at}, Before: rebuild.Request{At: before, Tool: tool}, Cold: true, TTL: time.Hour}
	}
	base := func() *track {
		var t track
		t.apply(event.Event{TS: event.Stamp(t0.Add(-time.Hour)), Kind: event.SpawnRequested})
		t.apply(event.Event{TS: event.Stamp(t0.Add(-time.Hour)), Kind: event.PromptSubmitted})
		return &t
	}
	apply := func(t *track, at time.Time, kind string, data map[string]any) {
		t.apply(event.Event{TS: event.Stamp(at), Kind: kind, Agent: event.Main, Data: data})
	}
	cases := []struct {
		name      string
		set       func(t *track)
		tool      bool
		cause, by string
	}{
		{"your message after a long idle", func(t *track) {
			apply(t, t0.Add(time.Minute), event.TurnEnded, map[string]any{})
			apply(t, t0.Add(2*time.Hour), event.PromptSubmitted, nil)
		}, false, ipc.RebuildYou, ""},
		{"your answer to a permission prompt", func(t *track) {
			apply(t, t0.Add(time.Minute), event.InputNeeded, map[string]any{"reason": "permission"})
		}, true, ipc.RebuildYou, ""},
		{"a thaw in the middle of its work", func(t *track) {
			apply(t, t0.Add(time.Minute), event.FreezeOn, map[string]any{"working": true})
			apply(t, t0.Add(2*time.Hour), event.FreezeOff, map[string]any{})
		}, true, ipc.RebuildThaw, ""},
		{"the trod bot's thaw", func(t *track) {
			apply(t, t0.Add(time.Minute), event.FreezeOn, map[string]any{"working": true, "by": "trod"})
			apply(t, t0.Add(2*time.Hour), event.FreezeOff, map[string]any{"by": "trod"})
		}, true, ipc.RebuildThaw, "trod"},
		{"a thaw of a session that waited, then your message", func(t *track) {
			apply(t, t0.Add(time.Minute), event.TurnEnded, map[string]any{})
			apply(t, t0.Add(2*time.Minute), event.FreezeOn, map[string]any{"working": false})
			apply(t, t0.Add(2*time.Hour), event.FreezeOff, map[string]any{})
			apply(t, t0.Add(2*time.Hour+time.Minute), event.PromptSubmitted, nil)
		}, false, ipc.RebuildYou, ""},
		{"a reopen, then your message", func(t *track) {
			apply(t, t0.Add(time.Minute), event.TurnEnded, map[string]any{})
			apply(t, t0.Add(2*time.Hour), event.SpawnRequested, map[string]any{"resume": "v1"})
			apply(t, t0.Add(2*time.Hour), event.UIReopened, map[string]any{})
			apply(t, t0.Add(2*time.Hour+time.Minute), event.PromptSubmitted, nil)
		}, false, ipc.RebuildReopen, ""},
		{"a message the trod bot typed", func(t *track) {
			apply(t, t0.Add(time.Minute), event.TurnFailed, map[string]any{"error": "overloaded"})
			apply(t, t0.Add(2*time.Hour), event.TrodSent, map[string]any{"reason": "error", "how": "typed"})
			apply(t, t0.Add(2*time.Hour+time.Second), event.PromptSubmitted, nil)
		}, false, ipc.RebuildTrod, ""},
		{"its own work in the background", func(t *track) {
			apply(t, t0.Add(time.Minute), event.TurnEnded, map[string]any{"waiting": 1})
		}, false, ipc.RebuildBackground, ""},
		{"a usage limit's reset", func(t *track) {
			apply(t, t0.Add(time.Minute), event.TurnFailed, map[string]any{"error": "rate_limit"})
		}, false, ipc.RebuildLimit, ""},
		{"a long tool call", func(t *track) {}, true, ipc.RebuildTool, ""},
		{"a quick answer to a permission prompt, then a long tool call", func(t *track) {
			apply(t, t0.Add(time.Minute), event.ToolStarted, map[string]any{"tool": "Bash"})
			apply(t, t0.Add(time.Minute+time.Second), event.InputNeeded, map[string]any{"reason": "permission"})
			apply(t, t0.Add(2*time.Hour), event.ToolFinished, map[string]any{"tool": "Bash", "ms": (2*time.Hour - 2*time.Minute).Milliseconds()})
		}, true, ipc.RebuildTool, ""},
		{"a slow answer to a permission prompt, then a short tool call", func(t *track) {
			apply(t, t0.Add(time.Minute), event.InputNeeded, map[string]any{"reason": "permission"})
			apply(t, t0.Add(2*time.Hour), event.ToolFinished, map[string]any{"tool": "Bash", "ms": 4000})
		}, true, ipc.RebuildYou, ""},
		{"Irie's carry-on after API errors", func(t *track) {
			apply(t, t0.Add(time.Minute), event.TurnFailed, map[string]any{"error": "overloaded"})
			apply(t, t0.Add(2*time.Hour), event.BoostOn, map[string]any{"error": "overloaded"})
			apply(t, t0.Add(2*time.Hour+time.Second), event.PromptSubmitted, nil)
		}, false, ipc.RebuildBoost, ""},
		{"nothing Irie knows", func(t *track) {}, false, "", ""},
	}
	for _, k := range cases {
		tr := base()
		k.set(tr)
		b := cold(t0, k.tool, t0.Add(3*time.Hour))
		if c, w := tr.causeOf(b); c != k.cause || w != k.by {
			t.Errorf("%s: %q by %q, want %q by %q", k.name, c, w, k.cause, k.by)
		}
	}
	// A helper's conversation is woken by its own tool calls and a thaw,
	// not by your messages to the session's agent.
	tr := base()
	apply(tr, t0.Add(2*time.Hour), event.PromptSubmitted, nil)
	helper := cold(t0, true, t0.Add(3*time.Hour))
	helper.Agent = "h1"
	if c, _ := tr.causeOf(helper); c != ipc.RebuildTool {
		t.Errorf("a helper's rebuild after your message: %q, want its long tool call", c)
	}
	tr.apply(event.Event{TS: event.Stamp(t0.Add(2*time.Hour + time.Minute)), Kind: event.ToolFinished, Agent: "sub:h2", Data: map[string]any{"tool": "Bash", "ms": time.Hour.Milliseconds()}})
	tr.apply(event.Event{TS: event.Stamp(t0.Add(2*time.Hour + 2*time.Minute)), Kind: event.ToolFinished, Agent: "sub:h1", Data: map[string]any{"tool": "Bash", "ms": time.Hour.Milliseconds()}})
	if c, _ := tr.causeOf(helper); c != ipc.RebuildTool {
		t.Errorf("a helper's rebuild after its own long tool call: %q", c)
	}
	// A rebuild that is not cold is the prompt's start changing.
	if c, _ := base().causeOf(rebuild.Rebuild{Request: rebuild.Request{At: t0.Add(time.Minute)}, Before: rebuild.Request{At: t0}}); c != ipc.RebuildChanged {
		t.Errorf("not cold: %q", c)
	}
	// Older than what the session's state keeps, a long tool call cannot
	// be told from what woke it.
	var empty track
	if c, _ := empty.causeOf(cold(t0, true, t0.Add(3*time.Hour))); c != "" {
		t.Errorf("with no record: %q", c)
	}
}

func TestTheCardSaysItOnlyWhenItMatters(t *testing.T) {
	c := testCore(t)
	now := time.Date(2026, 11, 14, 18, 0, 0, 0, time.Local)
	add := func(id string, at time.Time, usd float64, cause string) {
		c.meter.add(id, rebuilt{id: fmt.Sprint(id, at.UnixNano()), at: at, usd: usd, tokens: 100_000, cause: cause})
	}
	add("cheap", now.Add(-time.Hour), 0.40, ipc.RebuildYou)
	add("dear", now.Add(-3*time.Hour), 1.10, ipc.RebuildThaw)
	add("dear", now.Add(-time.Hour), 3.00, ipc.RebuildYou)
	add("dear", now.Add(-2*24*time.Hour), 9.00, ipc.RebuildTrod) // this week, not today
	add("changed", now.Add(-time.Hour), 1.50, ipc.RebuildChanged)
	if r := c.rebuildsRow("cheap", now); r == nil || r.Words != "" || r.Today != 1 {
		t.Errorf("cheap: %+v", r)
	}
	if r := c.rebuildsRow("dear", now); r == nil || r.Words != "$4.10 today: woke cold twice" || r.Today != 2 || r.Week != 3 || r.WeekUSD != 13.10 {
		t.Errorf("dear: %+v", r)
	}
	if r := c.rebuildsRow("changed", now); r == nil || r.Words != "$1.50 today: rebuilt its cache once" {
		t.Errorf("changed: %+v", r)
	}
	if r := c.rebuildsRow("none", now); r != nil {
		t.Errorf("none: %+v", r)
	}
	cfg := config.Default()
	cfg.Usage.Rebuilds = false
	c.useConfig(cfg)
	if r := c.rebuildsRow("dear", now); r != nil {
		t.Errorf("with the meter off: %+v", r)
	}
}

func TestTheRowSaysWhatWakingAColdSessionCosts(t *testing.T) {
	c := historyCore(t)
	t0 := time.Now().Add(-2 * time.Hour)
	m := meteredSession(t, c, "s0000000000a1", t0.Add(-time.Minute))
	m.ev(t0.Add(-30*time.Second), event.PromptSubmitted, nil)
	m.write(cachedReply("m1", t0, 249_000, 0, 997, false))
	m.ev(t0.Add(time.Second), event.TurnEnded, map[string]any{})
	count(c)
	now := time.Now()
	c.mu.Lock()
	row := c.row(m.x, time.Time{})
	c.mu.Unlock()
	want := ipc.Cold{Since: t0.Add(time.Hour), Tokens: 250_000, USD: 2, Words: "~$2.00 to wake"}
	if row.Cold == nil || row.Cold.Tokens != want.Tokens || row.Cold.USD != want.USD || row.Cold.Words != want.Words || !row.Cold.Since.Equal(want.Since) {
		t.Fatalf("cold %+v, want %+v", row.Cold, want)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if w := c.coldWords(m.x, now); w != "its cache has run out: waking it writes 250k tokens again, ~$2.00" {
		t.Errorf("words %q", w)
	}
	// While its cache is warm, or while it works, the row says nothing.
	if cold := c.coldRow(m.x, ipc.StateDone, t0.Add(30*time.Minute)); cold != nil {
		t.Errorf("warm: %+v", cold)
	}
	if cold := c.coldRow(m.x, ipc.StateWorking, now); cold != nil {
		t.Errorf("working: %+v", cold)
	}
	if cold := c.coldRow(m.x, ipc.StateFrozen, now); cold == nil {
		t.Error("frozen: no price")
	}
	// A nudge is worth $4 by default, so a $2 wake goes ahead; at $1 it is
	// left be.
	if why, dear := c.tooDear(m.x, now); dear {
		t.Errorf("too dear at $4: %s", why)
	}
	cfg := config.Default()
	cfg.Usage.WakeMost = 1
	c.useConfig(cfg)
	if why, dear := c.tooDear(m.x, now); !dear || why != "left cold: waking it would write 250k tokens into its cache again, ~$2.00, more than usage.wake_most ($1.00)" {
		t.Errorf("at $1: %q, %v", why, dear)
	}
	cfg.Usage.WakeMost = 0
	c.useConfig(cfg)
	if _, dear := c.tooDear(m.x, now); dear {
		t.Error("too dear with no limit")
	}
}

func TestTheTrodBotLeavesADearColdSessionBe(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	now := time.Now()
	c := trodCore(t, "[usage]\nwake_most = 2.0\n"+trodSection("retry = \"10m\"\nnudges = 3\n"))
	s := c.shiftAt(now)
	asks := event.Event{Kind: event.TurnEnded, Data: map[string]any{"asks": true}}
	write := func(x *session, at time.Time, tokens int64) {
		path := filepath.Join(t.TempDir(), x.id+".jsonl")
		if err := os.WriteFile(path, []byte(cachedReply("m-"+x.id, at, tokens, 0, 0, false)), 0o600); err != nil {
			t.Fatal(err)
		}
		x.track.apply(event.Event{Kind: event.SessionStarted, TS: event.Stamp(at), Data: map[string]any{"transcript": path}})
		x.tally.update(x.track.transcripts)
	}
	// Its question came two hours ago: its cache has run out, and waking
	// it writes 400k tokens again, $3.20, more than the $2 a nudge is worth.
	dear := stopped("dear", now.Add(-2*time.Hour), asks)
	write(dear, now.Add(-2*time.Hour), 400_000)
	if d, work, cold := c.trodNext(dear, now, s, time.Time{}); d != nil || work || !strings.HasPrefix(cold, "left cold: waking it would write 400k tokens") {
		t.Errorf("dear: %+v, %v, %q", d, work, cold)
	}
	// A cheap one goes on, and so does a dear one whose cache is warm.
	cheap := stopped("cheap", now.Add(-2*time.Hour), asks)
	write(cheap, now.Add(-2*time.Hour), 100_000)
	if d, _, cold := c.trodNext(cheap, now, s, time.Time{}); d == nil || cold != "" {
		t.Errorf("cheap: %+v, %q", d, cold)
	}
	warm := stopped("warm", now.Add(-10*time.Minute), asks)
	write(warm, now.Add(-10*time.Minute), 400_000)
	if d, _, cold := c.trodNext(warm, now, s, time.Time{}); d == nil || cold != "" {
		t.Errorf("warm: %+v, %q", d, cold)
	}
	// Woken in turn, the warm one comes before the cold ones, and of
	// those the cheaper first.
	if c.wakeOrder(warm, cheap, now) >= 0 || c.wakeOrder(cheap, dear, now) >= 0 {
		t.Error("the warm and the cheap do not come first")
	}
	// The report says why it was left.
	dear.trodCold = "left cold: waking it would write 400k tokens into its cache again, ~$3.20, more than usage.wake_most ($2.00)"
	if left := c.trodLeft(dear, ipc.Session{State: ipc.StateDone}, s); left != dear.trodCold {
		t.Errorf("left %q", left)
	}
}

func TestTheReportRanksSessionsAndCauses(t *testing.T) {
	c := testCore(t)
	now := time.Now()
	today, _ := days(now)
	at := func(d time.Duration) time.Time {
		if t := today.Add(d); t.Before(now) {
			return t
		}
		return now.Add(-time.Minute)
	}
	c.meter.add("a", rebuilt{id: "1", at: at(time.Minute), name: "alpha", tokens: 300_000, usd: 2.40, cause: ipc.RebuildThaw, by: "trod"})
	c.meter.add("a", rebuilt{id: "2", at: at(2 * time.Minute), name: "alpha", tokens: 100_000, usd: 0.80, cause: ipc.RebuildYou})
	c.meter.add("b", rebuilt{id: "3", at: at(3 * time.Minute), name: "beta", tokens: 500_000, usd: 4.00, cause: ipc.RebuildYou})
	c.meter.add("b", rebuilt{id: "4", at: today.AddDate(0, 0, -3), name: "beta", tokens: 600_000, usd: 4.80, cause: ""})
	c.meter.add("old", rebuilt{id: "5", at: today.AddDate(0, 0, -9), name: "gone", tokens: 600_000, usd: 4.80, cause: ipc.RebuildYou})
	r := c.rebuildsOp()
	if !r.On || r.Today.Count != 3 || r.Today.USD != 7.20 || r.Week.Count != 4 || r.Week.USD != 12 {
		t.Fatalf("report %+v", r)
	}
	if s := r.Today.Sessions; len(s) != 2 || s[0].Name != "beta" || s[1].Name != "alpha" || s[1].Count != 2 || s[1].USD != 3.20 {
		t.Errorf("today's sessions %+v", s)
	}
	if k := r.Today.Causes; len(k) != 2 || k[0].Name != ipc.RebuildYou || k[0].USD != 4.80 || k[1].Name != rebuildTrodThaw || k[1].Words != "the trod bot's thaw" {
		t.Errorf("today's causes %+v", k)
	}
	// A tie in cost goes to the cause with more rebuilds.
	if k := r.Week.Causes; len(k) != 3 || k[0].Name != ipc.RebuildYou || k[1].Name != "" || k[1].Words != "not known" {
		t.Errorf("the week's causes %+v", k)
	}
	if _, ok := c.meter.by["old"]; ok {
		t.Error("a rebuild older than a week and a day was kept")
	}
}

func TestAClosedSessionSaysWhatReopeningCosts(t *testing.T) {
	c := testCore(t)
	now := time.Now()
	w := rebuild.Wake{Last: now.Add(-2 * time.Hour), TTL: time.Hour, Cold: now.Add(-time.Hour), Tokens: 310_000, Model: "claude-opus-5-5", USD: 2.48, Priced: true}
	data := map[string]any{"name": "alpha"}
	wakeData(data, w)
	e := event.Event{TS: event.Stamp(now), Session: "a", Kind: event.UIClosed, Source: event.SourceUser, Data: data}
	// As read back from the journal, numbers are float64s.
	e.Data["context"] = float64(310_000)
	r := closedFrom(e)
	if r.Wake.Tokens != 310_000 || !r.Wake.Priced || r.Wake.USD != 2.48 || r.Wake.TTL != time.Hour || !r.Wake.Cold.Equal(w.Cold.Truncate(time.Nanosecond)) {
		t.Fatalf("wake %+v, want %+v", r.Wake, w)
	}
	warm := closedRecord{ID: "b", Name: "beta", Wake: rebuild.Wake{Last: now, TTL: time.Hour, Cold: now.Add(time.Hour), Tokens: 310_000, USD: 2.48, Priced: true}}
	c.addClosed(warm)
	c.addClosed(r)
	list := c.closedList()
	if len(list) != 2 || list[0].Cold == nil || list[0].Cold.Words != "~$2.48 to wake" || list[1].Cold != nil {
		t.Fatalf("closed %+v", list)
	}
	// The list is told again once the warm one's cache runs out.
	if !c.closes.cold.Equal(warm.Wake.Cold) {
		t.Errorf("next cold at %v, want %v", c.closes.cold, warm.Wake.Cold)
	}
	if note := c.reopenNote(r, now); note != " Its prompt cache has run out, so its next turn writes 310k tokens into it again, ~$2.48." {
		t.Errorf("note %q", note)
	}
	if note := c.reopenNote(warm, now); note != "" {
		t.Errorf("warm note %q", note)
	}
}

func TestTheDogWeighsOnlyATurnThatEndsLate(t *testing.T) {
	c := historyCore(t)
	t0 := time.Now().Add(-3 * time.Hour)
	m := meteredSession(t, c, "s0000000000a2", t0.Add(-time.Minute))
	m.ev(t0.Add(-30*time.Second), event.PromptSubmitted, nil)
	// Its last request read so far, 3 hours ago, sent 600k tokens: $4.80
	// to write again, more than the $4 a nudge is worth.
	m.write(cachedReply("m1", t0, 599_000, 0, 997, true))
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	// A long last tool call: the turn's last reply, which Claude Code writes
	// after its Stop hooks, warmed the cache again.
	if c.dogDear(m.x, now) {
		t.Error("dear after a long tool call")
	}
	// Frozen at its turn's end and thawed now: its cache ran out.
	m.ev(t0.Add(time.Minute), event.FreezeOn, map[string]any{"working": true})
	m.ev(now.Add(-2*time.Second), event.FreezeOff, map[string]any{})
	if !c.dogDear(m.x, now) {
		t.Error("not dear after a thaw")
	}
	// Frozen in its last tool call instead: the call ends after the thaw,
	// and the turn's last request, not read yet, warmed the cache again.
	m.ev(now.Add(-time.Second), event.ToolFinished, map[string]any{"tool": "Bash", "ms": 1000})
	if c.dogDear(m.x, now) {
		t.Error("dear after a tool call that ended after the thaw")
	}
}

func TestThePriceIsTheConversationsItRunsNow(t *testing.T) {
	c := historyCore(t)
	t0 := time.Now().Add(-3 * time.Hour)
	m := meteredSession(t, c, "s0000000000a3", t0.Add(-time.Minute))
	m.write(cachedReply("m1", t0, 599_000, 0, 997, false))
	count(c)
	c.mu.Lock()
	if w, ok := c.wakePrice(m.x, time.Now()); !ok || w.Tokens != 600_000 {
		t.Errorf("before /clear: %+v, %v", w, ok)
	}
	c.mu.Unlock()
	// /clear starts a new conversation, which has sent nothing yet.
	cleared := filepath.Join(filepath.Dir(m.path), "cleared.jsonl")
	m.ev(time.Now(), event.SessionStarted, map[string]any{"transcript": cleared, "source": "clear"})
	count(c)
	c.mu.Lock()
	if w, ok := c.wakePrice(m.x, time.Now()); ok {
		t.Errorf("after /clear, the old conversation's price: %+v", w)
	}
	c.mu.Unlock()
	// /resume of the first conversation prices that one again.
	m.ev(time.Now(), event.SessionStarted, map[string]any{"transcript": m.path, "source": "resume"})
	c.mu.Lock()
	defer c.mu.Unlock()
	if w, ok := c.wakePrice(m.x, time.Now()); !ok || w.Tokens != 600_000 {
		t.Errorf("after /resume: %+v, %v", w, ok)
	}
}
