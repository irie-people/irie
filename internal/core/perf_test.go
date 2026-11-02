package core

// Benchmarks of the core's busiest paths with 40 sessions, part of the
// optimisation plan's yardstick (docs/proposals/optimisation.md, iteration
// 1): the diff of every row, the list, a status update, and encoding a
// frame. Run them with
//
//	go test ./internal/core -run '^$' -bench . -benchmem

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/statusline"
	"github.com/irie-people/irie/internal/tmux"
)

// benchSessions is how many sessions the benchmarks hold: about what the
// owner's machine ran when the plan was measured.
const benchSessions = 40

// benchCore is a core with no tmux behind it and n sessions that look like
// working agents: a prompt, a tool call, cost, context and tokens.
func benchCore(tb testing.TB, n int) *Core {
	tb.Helper()
	dir := tb.TempDir()
	env := map[string]string{"HOME": "/home/alice", "XDG_STATE_HOME": filepath.Join(dir, "state"), "XDG_RUNTIME_DIR": filepath.Join(dir, "run")}
	p, err := paths.New(func(k string) string { return env[k] }, 1000)
	if err != nil {
		tb.Fatal(err)
	}
	c := newCore(Options{Config: config.Default(), Paths: p, Version: "bench", Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, &tmux.Runner{})
	at := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	cost, ctx := 1.23, 41.0
	for i := range n {
		id := fmt.Sprintf("s%012d", i)
		if err := os.MkdirAll(p.Session(id), 0o700); err != nil {
			tb.Fatal(err)
		}
		x := &session{
			id: id, name: fmt.Sprintf("Fix the parser's error messages %d", i), agent: event.Main,
			created: at.Add(time.Duration(i) * time.Minute), cwd: "/home/alice/src/irie-worktrees/fix-" + id,
			repo: "/home/alice/src/irie", worktree: "/home/alice/src/irie-worktrees/fix-" + id, branch: "irie/fix-" + id,
			profile: "claude", harness: "claude-code", observed: true, pane: fmt.Sprintf("%%%d", i+1), command: "claude",
			vendorID: fmt.Sprintf("0a1b2c3d-0000-4000-8000-%012d", i), model: "claude-opus-5-5", hue: 1 + i%8,
			usage:  statusline.Usage{Model: "claude-opus-5-5", ModelName: "Opus 5.5", CostUSD: &cost, Context: &ctx, ContextSize: 200000},
			tokens: &ipc.Tokens{Input: 12000, Output: 34000, CacheRead: 400000, CacheWrite: 36000},
		}
		for k, e := range []event.Event{
			{Source: event.SourceHook, Kind: event.SessionStarted, Vendor: &event.Vendor{SessionID: x.vendorID}, Data: map[string]any{"source": "startup"}},
			{Source: event.SourceHook, Kind: event.PromptSubmitted, Data: map[string]any{}},
			{Source: event.SourceHook, Kind: event.ToolStarted, Agent: event.Main, Vendor: &event.Vendor{ToolUseID: "toolu_" + id},
				Data: map[string]any{"tool": "Bash", "summary": "go test ./internal/parser/...", "phrase": "Running the parser's tests"}},
		} {
			e.Session, e.TS = id, at.Add(time.Duration(i)*time.Minute+time.Duration(k)*time.Second).Format("2006-01-02T15:04:05.000Z")
			x.track.apply(e)
		}
		c.sessions[id] = x
	}
	return c
}

// BenchmarkChanges is the diff every tick, hook and status update runs:
// each row worked out again and compared with what subscribers last heard.
func BenchmarkChanges(b *testing.B) {
	c := benchCore(b, benchSessions)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.changes()
	b.ResetTimer()
	for range b.N {
		if f := c.changes(); len(f) != 0 {
			b.Fatalf("%d frames when nothing changed", len(f))
		}
	}
}

// BenchmarkList is every row worked out, in the list's order.
func BenchmarkList(b *testing.B) {
	c := benchCore(b, benchSessions)
	c.mu.Lock()
	defer c.mu.Unlock()
	b.ResetTimer()
	for range b.N {
		if rows := c.list(); len(rows) != benchSessions {
			b.Fatalf("%d rows", len(rows))
		}
	}
}

// statusPayload is a status line update as Claude Code sends it, with the
// cost given.
func statusPayload(cost float64) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"model":{"id":"claude-opus-5-5","display_name":"Opus 5.5"},"cost":{"total_cost_usd":%g},`+
		`"context_window":{"used_percentage":41,"context_window_size":200000},`+
		`"rate_limits":{"five_hour":{"used_percentage":37,"resets_at":1793653800},"seven_day":{"used_percentage":66,"resets_at":1794045600}}}`, cost))
}

// BenchmarkStatusUnchanged is a status update that changed nothing, as an
// idle session's line sends every few seconds.
func BenchmarkStatusUnchanged(b *testing.B) {
	c := benchCore(b, benchSessions)
	a := ipc.StatusArgs{Session: "s000000000007", Payload: statusPayload(1.23)}
	if _, err := c.status(a); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for range b.N {
		if _, err := c.status(a); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkStatusChanged is a status update whose cost moved, as a working
// session's line sends.
func BenchmarkStatusChanged(b *testing.B) {
	c := benchCore(b, benchSessions)
	payloads := []json.RawMessage{statusPayload(1.23), statusPayload(1.24)}
	b.ResetTimer()
	for i := range b.N {
		if _, err := c.status(ipc.StatusArgs{Session: "s000000000007", Payload: payloads[i%2]}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEncodeSnapshot is a snapshot of every row as it goes on the
// wire.
func BenchmarkEncodeSnapshot(b *testing.B) {
	c := benchCore(b, benchSessions)
	c.mu.Lock()
	f := ipc.Frame{T: ipc.FrameSnapshot, Sessions: c.list(), Header: new(c.headerNow())}
	c.mu.Unlock()
	b.ResetTimer()
	for range b.N {
		buf, err := json.Marshal(f)
		if err != nil {
			b.Fatal(err)
		}
		b.SetBytes(int64(len(buf)))
	}
}

// BenchmarkEncodeUpsert is one row as it goes on the wire.
func BenchmarkEncodeUpsert(b *testing.B) {
	c := benchCore(b, benchSessions)
	c.mu.Lock()
	rows := c.list()
	c.mu.Unlock()
	f := ipc.Frame{T: ipc.FrameUpsert, Session: &rows[7]}
	b.ResetTimer()
	for range b.N {
		buf, err := json.Marshal(f)
		if err != nil {
			b.Fatal(err)
		}
		b.SetBytes(int64(len(buf)))
	}
}

// TestBenchCoreLooksReal keeps the benchmarks honest: its rows are working
// agents with a tool call each, of about the size measured on the wire.
func TestBenchCoreLooksReal(t *testing.T) {
	c := benchCore(t, benchSessions)
	c.mu.Lock()
	rows := c.list()
	c.mu.Unlock()
	if len(rows) != benchSessions {
		t.Fatalf("%d rows", len(rows))
	}
	r := rows[3]
	if r.State != ipc.StateWorking || r.Command == "" || r.CostUSD == nil {
		t.Errorf("row %+v is not a working agent with a tool call and a cost", r)
	}
	b, err := json.Marshal(ipc.Frame{T: ipc.FrameUpsert, Session: &r})
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 600 || len(b) > 2000 {
		t.Errorf("an upsert is %d bytes; the wire carried about 1,083", len(b))
	}
}
