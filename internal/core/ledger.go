package core

// The ledger (DESIGN §6.3): what each model call cost at the list price of
// its day. A session's calls are read from the transcripts its token tally
// follows (tokens.go), each request once, with the parts its top-level
// usage leaves out; Irie's own calls to local models go into ledger.jsonl
// as they are answered. A local profile's calls ran on this computer or a
// server of the owner's, so they cost nothing. A call to a model with no
// price leaves its session's cost unknown, never $0.
//
// The router's cost per attempt (§6.4) is a session's ledger, not Claude
// Code's status line: over a month of one user's sessions the status line
// read $0 or a part for 32 sessions after a reopen started it again ($698,
// 7.8% of the month), and priced 30 calls to local models at $0.80
// (docs/proposals/cost-ledger.md).

import (
	"cmp"
	"context"
	"encoding/json"
	"maps"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/journal"
	"github.com/irie-people/irie/internal/ledger"
	"github.com/irie-people/irie/internal/localmodel"
	"github.com/irie-people/irie/internal/statusline"
)

const (
	// ledgerKeep is how far back the costs report and Irie's own ledger
	// reach, as the router's outcomes do.
	ledgerKeep = 30 * 24 * time.Hour
	// ledgerSweepFirst is how long after the core starts the sweep begins
	// pricing the closed sessions the ledger lacks.
	ledgerSweepFirst = 2 * time.Minute
	// ledgerSweepPause is its pause between two sessions, and
	// ledgerSweepHeld its pause while Gabriel holds work back for you.
	ledgerSweepPause = 200 * time.Millisecond
	ledgerSweepHeld  = 5 * time.Second
)

// ownCalls are Irie's own calls to models in the last ledgerKeep, added up
// by day, model and purpose; ledger.jsonl keeps them call by call. Its own
// lock, since a call is answered wherever its caller runs, c.mu held or not.
type ownCalls struct {
	mu  sync.Mutex
	sum ledger.Sum
}

// spent is what session x's calls came to so far, read from its
// transcripts: its ledger. A local profile's calls are all free. The
// caller holds c.mu.
func (x *session) spent() ledger.Sum {
	s := x.tally.spent(x.track.transcripts)
	if x.local {
		return s.AllFree()
	}
	return s
}

// costUSD is what session x has cost at list price, for the router and
// the + list: its ledger's total once it has calls, if every one has a
// price, free ones at $0. Until the ledger has a call, its status line's
// figure, which never counts for a local profile. nil when neither says.
// The caller holds c.mu.
func (x *session) costUSD() *float64 {
	if t := x.spent().Total(""); t.Calls > 0 {
		return knownCost(t)
	}
	if x.local {
		return nil
	}
	return x.usage.CostUSD
}

// knownCost is t's dollars when every call has a price, else nil: a
// session's cost with an unpriced call in it is not known.
func knownCost(t ledger.Tally) *float64 {
	if !t.Known() {
		return nil
	}
	usd := t.USD
	return &usd
}

// goneCost is what session id cost, for the router, when it is not open:
// the total its costs.json keeps, if every call has a price. A session the
// ledger has not priced has its last status line's figure instead, never
// a local profile's. nil when neither says.
func (c *Core) goneCost(id string) *float64 {
	if s, ok := ledger.ReadTotals(c.paths.CostsFile(id)); ok {
		return knownCost(s.Total(""))
	}
	if spec, err := host.ReadSpec(c.paths.SpecFile(id)); err == nil && spec.Local {
		return nil
	}
	var u statusline.Usage
	if b, err := os.ReadFile(c.paths.StatusFile(id)); err == nil && json.Unmarshal(b, &u) == nil {
		return u.CostUSD
	}
	return nil
}

// keepSpent writes a closed session's ledger into its costs.json, once its
// transcripts are read to their ends. It owns tally: the session is out of
// the registry.
func (c *Core) keepSpent(id string, tally *tokenTally, transcripts []string, local bool) {
	if !anyExists(transcripts) {
		return
	}
	tally.readAll(transcripts)
	s := tally.spent(transcripts)
	if local {
		s = s.AllFree()
	}
	if err := ledger.WriteTotals(c.paths.CostsFile(id), s); err != nil {
		c.log.Warn("ledger", "session", id, "err", err)
	}
}

// anyExists reports whether any of paths is a file there now.
func anyExists(paths []string) bool {
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// goneSession is what the journal says of a session the sweep may price:
// when it began, and its transcripts.
type goneSession struct {
	began       time.Time
	transcripts []string
}

// ledgerSweep prices, once, each session of the last ledgerKeep that is
// not open and whose costs.json is missing, priced another way, or older
// than one of its transcripts: one closed before the ledger, lost with its
// tmux server, or whose agent wrote after its close.
func (c *Core) ledgerSweep() {
	time.Sleep(ledgerSweepFirst)
	start := time.Now()
	if n := c.priceGone(c.goneSessions(start)); n > 0 {
		c.log.Info("ledger: priced closed sessions", "sessions", n, "took", time.Since(start).Round(time.Second))
	}
}

// goneSessions are the sessions of the last ledgerKeep, as the journal
// tells them: when each began and its transcripts.
func (c *Core) goneSessions(now time.Time) map[string]*goneSession {
	evs, err := journal.ReadKinds(c.paths.Journal(), now.Add(-ledgerKeep), event.SpawnRequested, event.SessionStarted, event.SubagentFinished)
	if err != nil {
		c.log.Warn("ledger", "journal", err)
	}
	gone := map[string]*goneSession{}
	for _, e := range evs {
		g := gone[e.Session]
		if g == nil {
			g = &goneSession{}
			gone[e.Session] = g
		}
		switch e.Kind {
		case event.SpawnRequested:
			if g.began.IsZero() {
				g.began = e.Time()
			}
		default:
			if p := e.Str("transcript"); p != "" && !slices.Contains(g.transcripts, p) {
				g.transcripts = append(g.transcripts, p)
			}
		}
	}
	return gone
}

// priceGone prices those of gone that are not open and need it, as the
// token tally would have, and returns how many it priced. One session at a
// time, with a pause between, and a longer one while Gabriel holds work
// back for you, so it never holds the machine.
func (c *Core) priceGone(gone map[string]*goneSession) int {
	priced := 0
	for _, id := range slices.Sorted(maps.Keys(gone)) {
		g := gone[id]
		c.mu.Lock()
		_, open := c.sessions[id]
		held := c.gab.holding
		c.mu.Unlock()
		if open || len(g.transcripts) == 0 || !c.needsPricing(id, g.transcripts) {
			continue
		}
		spec, _ := host.ReadSpec(c.paths.SpecFile(id))
		c.keepSpent(id, &tokenTally{since: g.began}, g.transcripts, spec.Local)
		s, ok := ledger.ReadTotals(c.paths.CostsFile(id))
		if !ok {
			continue
		}
		priced++
		c.mu.Lock()
		if o := c.outcomes[id]; o != nil && c.sessions[id] == nil {
			o.cost = knownCost(s.Total(""))
		}
		c.mu.Unlock()
		pause := ledgerSweepPause
		if held {
			pause = ledgerSweepHeld
		}
		time.Sleep(pause)
	}
	return priced
}

// needsPricing reports whether session id's costs.json is missing, priced
// another way, or older than one of its transcripts.
func (c *Core) needsPricing(id string, transcripts []string) bool {
	path := c.paths.CostsFile(id)
	if _, ok := ledger.ReadTotals(path); !ok {
		return true
	}
	kept, err := os.Stat(path)
	if err != nil {
		return true
	}
	for _, p := range transcripts {
		if st, err := os.Stat(p); err == nil && st.ModTime().After(kept.ModTime()) {
			return true
		}
	}
	return false
}

// loadLedger trims Irie's own ledger to the last ledgerKeep and adds up
// what is left, as the core starts, before it makes any call.
func (c *Core) loadLedger(now time.Time) {
	from := now.Add(-ledgerKeep)
	if err := ledger.Trim(c.paths.Ledger(), from); err != nil {
		c.log.Warn("ledger", "trim", err)
	}
	entries, err := ledger.ReadEntries(c.paths.Ledger(), from)
	if err != nil {
		c.log.Warn("ledger", "err", err)
	}
	c.own.mu.Lock()
	c.own.sum = ledger.SumOf(entries)
	c.own.mu.Unlock()
}

// metered is ctx with Irie's own calls to models under it put in the
// ledger, for purpose: free, since they run on this computer or a server
// of the owner's, unless the model is one of Ollama's cloud models, which
// has no price Irie knows.
func (c *Core) metered(ctx context.Context, purpose string) context.Context {
	return localmodel.WithMeter(ctx, func(u localmodel.Use) {
		call := ledger.Call{
			At: time.Now().UTC(), Purpose: purpose, Model: u.Model,
			Usage: ledger.Usage{Input: u.Prompt, Output: u.Output}, Free: !ledger.IsCloudModel(u.Model),
		}
		host := ""
		if p, err := url.Parse(u.URL); err == nil {
			host = p.Hostname()
		}
		c.own.mu.Lock()
		defer c.own.mu.Unlock()
		if c.own.sum == nil {
			c.own.sum = ledger.Sum{}
		}
		c.own.sum.Add(call)
		if err := ledger.Append(c.paths.Ledger(), ledger.NewEntry(call, host, u.Took)); err != nil {
			c.log.Warn("ledger", "err", err)
		}
	})
}

// costsOp is the ledger's report (irie ctl costs): today's calls, the
// week's and the last 30 days', by purpose and model, the sessions' and
// Irie's own, and the sessions with calls in that time, what they cost
// first, with Claude Code's own figure beside each.
func (c *Core) costsOp() ipc.CostsResult {
	now := time.Now()
	month := ledger.Day(now.Add(-29 * 24 * time.Hour))
	type known struct {
		x     ipc.CostsSession
		spent ledger.Sum
	}
	all := ledger.Sum{}
	var sessions []known
	seen := map[string]bool{}
	c.mu.Lock()
	for id, x := range c.sessions {
		seen[id] = true
		s := x.spent()
		row := ipc.CostsSession{ID: id, Name: x.name, Open: true, Profile: x.profile, Claude: x.usage.CostUSD}
		if o := c.outcomes[id]; o != nil {
			row.Class = o.class
		}
		sessions = append(sessions, known{row, s})
	}
	names := map[string]string{}
	for _, r := range c.closes.list {
		names[r.ID] = r.Name
	}
	classes, profiles := map[string]string{}, map[string]string{}
	for id, o := range c.outcomes {
		classes[id], profiles[id] = o.class, o.profile
	}
	c.mu.Unlock()
	// The closed sessions' totals, as they kept them.
	if dirs, err := os.ReadDir(c.paths.Sessions()); err == nil {
		for _, d := range dirs {
			id := d.Name()
			if seen[id] || !d.IsDir() {
				continue
			}
			s, ok := ledger.ReadTotals(c.paths.CostsFile(id))
			if !ok {
				continue
			}
			row := ipc.CostsSession{ID: id, Name: cmp.Or(names[id], id), Profile: profiles[id], Class: classes[id]}
			var u statusline.Usage
			if b, err := os.ReadFile(c.paths.StatusFile(id)); err == nil && json.Unmarshal(b, &u) == nil {
				row.Claude = u.CostUSD
			}
			sessions = append(sessions, known{row, s})
		}
	}
	var out ipc.CostsResult
	for _, k := range sessions {
		if k.spent.Total(month).Calls == 0 {
			continue
		}
		all.Merge(k.spent)
		k.x.Ledger = costsTally(k.spent.Total(""))
		out.Sessions = append(out.Sessions, k.x)
	}
	c.own.mu.Lock()
	all.Merge(c.own.sum)
	c.own.mu.Unlock()
	slices.SortFunc(out.Sessions, func(a, b ipc.CostsSession) int {
		return cmp.Or(cmp.Compare(b.Ledger.USD, a.Ledger.USD), cmp.Compare(b.Ledger.Unpriced, a.Ledger.Unpriced), strings.Compare(a.ID, b.ID))
	})
	for _, span := range []struct{ name, from string }{
		{"today", ledger.Day(now)}, {"week", ledger.Day(now.Add(-6 * 24 * time.Hour))}, {"month", month},
	} {
		sp := ipc.CostsSpan{Name: span.name, From: span.from, Total: costsTally(all.Total(span.from))}
		sp.Purposes = costsParts(all.By(func(k ledger.Key) string { return k.Purpose }, span.from))
		sp.Models = costsParts(all.By(func(k ledger.Key) string { return k.Model }, span.from))
		out.Spans = append(out.Spans, sp)
	}
	return out
}

// costsTally is t as the costs report says it.
func costsTally(t ledger.Tally) ipc.CostsTally {
	return ipc.CostsTally{Calls: t.Calls, USD: roundCents(t.USD), Unpriced: t.Unpriced, Free: t.Free, Searches: t.Usage.Searches, Words: t.Words()}
}

// costsParts are by's parts, what cost most first.
func costsParts(by map[string]ledger.Tally) []ipc.CostsPart {
	out := []ipc.CostsPart{}
	for _, name := range ledger.Ranked(by) {
		out = append(out, ipc.CostsPart{Name: name, CostsTally: costsTally(by[name])})
	}
	return out
}
