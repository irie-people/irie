package core

// The prompt-cache meter (DESIGN §6.3) counts each session's rebuilds: the
// requests that wrote most of its context into the prompt cache again
// instead of reading it, read from its transcripts (internal/rebuild). It
// journals each one once, as cache.rebuilt, with what it cost at list price
// and, when Irie knows, what woke the session: your message after a long
// idle, a thaw, a reopen, the trod bot or the jot dog, work of its own in
// the background, a usage limit's reset or a long tool call. A row carries
// its session's count and cost today and this week, and once its cache has
// run out the price of waking it, which the trod bot and the jot dog weigh
// before they wake it (trod.go, dog.go) and the apps show where you wake it.

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/journal"
	"github.com/irie-people/irie/internal/rebuild"
)

// rebuildKeep is how long the meter keeps a rebuild: the seven days its
// week counts, and a day more.
const rebuildKeep = 8 * 24 * time.Hour

// maxWakes is how many of the things that woke it lately a session's state
// keeps, which reach back a few days for a busy session.
const maxWakes = 256

// longTool is how long a tool call must run for the meter to note it: as
// long as the shortest cache lives, less its slack (rebuild.ColdAfter).
var longTool = rebuild.ColdAfter(rebuild.DefaultTTL)

// wakeMark is one thing that woke a session, or that leaves it to wake:
// your message or your answer at a prompt, a message the trod bot typed, the
// jot dog's nudge, a thaw in the middle of its work, a reopen, the end of a
// turn that left work of its own in the background, a usage limit, Irie's
// carry-on after API errors, or a tool call that ran long. by is who
// thawed it, trod for the trod bot; agent is whose tool call it was, and
// dur how long it ran.
type wakeMark struct {
	at        time.Time
	cause, by string
	agent     string
	dur       time.Duration
}

// woke notes e as what woke the session, or will.
func (t *track) woke(e event.Event, cause, by string) {
	t.mark(e, wakeMark{cause: cause, by: by, agent: e.Agent})
}

// wokeAfter notes e, the end of a tool call that ran for dur, after which
// its agent went on.
func (t *track) wokeAfter(e event.Event, dur time.Duration) {
	t.mark(e, wakeMark{cause: ipc.RebuildTool, agent: e.Agent, dur: dur})
}

func (t *track) mark(e event.Event, m wakeMark) {
	if m.at = e.Time(); m.at.IsZero() {
		return
	}
	t.wakes = append(t.wakes, m)
	if n := len(t.wakes) - maxWakes; n > 0 {
		t.wakes = slices.Delete(t.wakes, 0, n)
	}
}

// causeOf is what woke the session for rebuild b, by what came after the
// request before b's and up to b's: a reopen, else a thaw, else the latest.
// A tool call counts when it ran about as long as the cache lives, and a
// helper's own conversation counts only those and its own tool calls: your
// messages and the bots' go to the session's agent. With nothing there, a
// request before that stopped to call a tool ran that tool all the time
// between. A rebuild that is not cold is no wake: the start of the prompt
// changed. "" when Irie cannot tell, as for a rebuild older than what the
// session's state keeps.
func (t *track) causeOf(b rebuild.Rebuild) (cause, by string) {
	if !b.Cold {
		return ipc.RebuildChanged, ""
	}
	from, to := b.Before.At, b.At
	agent := event.Main
	if b.Agent != "" {
		agent = event.Agent(b.Agent)
	}
	var latest, thaw *wakeMark
	for i := range t.wakes {
		m := &t.wakes[i]
		if !m.at.After(from) || m.at.After(to) {
			continue
		}
		switch m.cause {
		case ipc.RebuildReopen:
			return m.cause, m.by
		case ipc.RebuildThaw:
			thaw = m
		case ipc.RebuildTool:
			if m.agent != agent || m.dur < rebuild.ColdAfter(b.TTL) {
				continue
			}
		default:
			if agent != event.Main {
				continue
			}
		}
		latest = m
	}
	switch {
	case thaw != nil:
		return thaw.cause, thaw.by
	case latest != nil:
		return latest.cause, latest.by
	case b.Before.Tool && len(t.wakes) > 0 && !t.wakes[0].at.After(from):
		return ipc.RebuildTool, ""
	}
	return "", ""
}

// lastThaw is when a thaw last woke the session; zero if none did.
func (t *track) lastThaw() time.Time {
	for i := len(t.wakes) - 1; i >= 0; i-- {
		if t.wakes[i].cause == ipc.RebuildThaw {
			return t.wakes[i].at
		}
	}
	return time.Time{}
}

// conversation is the transcript of the conversation x's agent runs now:
// the one its last SessionStart named, else its newest own one.
func (x *session) conversation() string {
	if x.track.current != "" {
		return x.track.current
	}
	if n := len(x.track.own); n > 0 {
		return x.track.own[n-1]
	}
	return ""
}

// meter is the prompt-cache meter's record of the last rebuildKeep: every
// rebuild journalled, by session, the oldest first, and their message ids,
// so that a transcript read again records none twice. c.mu guards it.
type meter struct {
	by  map[string][]rebuilt
	ids map[string]bool
}

// rebuilt is one rebuild as the meter keeps it.
type rebuilt struct {
	id        string    // its request's message id
	at        time.Time // and when that was
	name      string    // the session's name when it was journalled
	tokens    int64
	usd       float64
	cause, by string
}

// rebuildTrodThaw is the report's cause for the trod bot's thaws, told
// apart from yours.
const rebuildTrodThaw = "trod-thaw"

func (m *meter) add(session string, r rebuilt) {
	if m.by == nil {
		m.by, m.ids = map[string][]rebuilt{}, map[string]bool{}
	}
	if r.id != "" {
		m.ids[r.id] = true
	}
	list := append(m.by[session], r)
	slices.SortStableFunc(list, func(a, b rebuilt) int { return a.at.Compare(b.at) })
	m.by[session] = list
}

// prune forgets the rebuilds older than rebuildKeep, their ids too: a
// transcript's rebuilds that old are never recorded again.
func (m *meter) prune(now time.Time) {
	cut := now.Add(-rebuildKeep)
	for session, list := range m.by {
		list = slices.DeleteFunc(list, func(r rebuilt) bool {
			if r.at.Before(cut) {
				delete(m.ids, r.id)
				return true
			}
			return false
		})
		if len(list) == 0 {
			delete(m.by, session)
		} else {
			m.by[session] = list
		}
	}
}

// rebuiltFrom is the meter's record of a cache.rebuilt event.
func rebuiltFrom(e event.Event) rebuilt {
	r := rebuilt{id: e.Str("message_id"), name: e.Str("name"), cause: e.Str("cause"), by: e.Str("by")}
	r.at, _ = time.Parse(time.RFC3339Nano, e.Str("at"))
	if r.at.IsZero() {
		r.at = e.Time()
	}
	if n, ok := e.Data["tokens"].(float64); ok {
		r.tokens = int64(n)
	} else if n, ok := e.Int("tokens"); ok {
		r.tokens = int64(n)
	}
	switch v := e.Data["usd"].(type) {
	case float64:
		r.usd = v
	case int:
		r.usd = float64(v)
	}
	return r
}

// loadMeter reads the rebuilds of the last week from the journal, as the
// core starts.
func (c *Core) loadMeter(now time.Time) {
	evs, err := journal.ReadKind(c.paths.Journal(), now.Add(-rebuildKeep-24*time.Hour), event.CacheRebuilt)
	if err != nil {
		c.log.Warn("journal", "err", err)
	}
	for _, e := range evs {
		c.meter.add(e.Session, rebuiltFrom(e))
	}
	c.meter.prune(now)
}

// noteRebuilds journals the rebuilds read from session x's transcripts
// since the last look, each once, with what woke it for each. A local
// model's session is not metered: its cache costs no money. The caller
// holds c.mu.
func (c *Core) noteRebuilds(x *session, now time.Time) {
	found := x.tally.rebuilds(x.track.transcripts)
	if len(found) == 0 || !c.cfg().Usage.Rebuilds || x.local {
		return
	}
	for _, b := range found {
		if b.ID == "" || c.meter.ids[b.ID] || now.Sub(b.At) > rebuildKeep {
			continue
		}
		cause, by := x.track.causeOf(b)
		data := map[string]any{
			"at": b.At.UTC().Format(time.RFC3339Nano), "message_id": b.ID, "name": x.name,
			"tokens": b.Tokens, "idle_ms": b.Idle.Milliseconds(), "ttl": ttlWord(b.TTL), "model": b.Model,
		}
		if b.Priced {
			data["usd"] = roundCents(b.USD)
		}
		if cause != "" {
			data["cause"] = cause
		}
		if by != "" {
			data["by"] = by
		}
		agent := event.Main
		if b.Agent != "" {
			agent = event.Agent(b.Agent)
		}
		c.record(x, event.Event{Source: event.SourceTranscript, Agent: agent, Kind: event.CacheRebuilt, Data: data})
		c.meter.add(x.id, rebuilt{id: b.ID, at: b.At, name: x.name, tokens: b.Tokens, usd: roundCents(b.USD), cause: cause, by: by})
		c.log.Info("prompt cache rebuilt", "session", x.id, "tokens", b.Tokens, "usd", roundCents(b.USD), "cause", cause, "idle", b.Idle.Round(time.Second))
	}
	c.meter.prune(now)
}

// roundCents keeps a price to a hundredth of a cent, as the journal keeps it.
func roundCents(v float64) float64 { return float64(int64(v*1e4+0.5)) / 1e4 }

// ttlWord is a cache's life as the journal says it.
func ttlWord(d time.Duration) string {
	if d >= time.Hour {
		return "1h"
	}
	return "5m"
}

// days are today's start and the start of the week that ends today, seven
// days with today, in the local time of now.
func days(now time.Time) (today, week time.Time) {
	y, m, d := now.Date()
	today = time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	return today, today.AddDate(0, 0, -6)
}

// rebuildsRow is what session id's row says of its rebuilds; nil when
// there were none this week. Its words, for its card, come once today's
// cost at least usage.rebuild_card. The caller holds c.mu.
func (c *Core) rebuildsRow(id string, now time.Time) *ipc.Rebuilds {
	list := c.meter.by[id]
	if len(list) == 0 || !c.cfg().Usage.Rebuilds {
		return nil
	}
	today, week := days(now)
	r := ipc.Rebuilds{}
	cold := true
	for _, b := range list {
		if b.at.Before(week) {
			continue
		}
		r.Week++
		r.WeekUSD += b.usd
		r.Last, r.Cause = b.at, b.cause
		if !b.at.Before(today) {
			r.Today++
			r.TodayUSD += b.usd
			cold = cold && b.cause != ipc.RebuildChanged
		}
	}
	if r.Week == 0 {
		return nil
	}
	r.WeekUSD, r.TodayUSD = roundCents(r.WeekUSD), roundCents(r.TodayUSD)
	if r.Today > 0 && r.TodayUSD > 0 && r.TodayUSD >= c.cfg().Usage.RebuildCard {
		r.Words = rebuildWords(r.Today, r.TodayUSD, cold)
	}
	return &r
}

// rebuildWords say on a card what a session's cache rebuilds cost today,
// and how often: "$4.10 today: woke cold 3 times". The price comes first,
// so a narrow card that cuts the line short still shows it.
func rebuildWords(n int, usd float64, cold bool) string {
	if cold {
		return fmt.Sprintf("%s today: woke cold %s", rebuild.Dollars(usd), rebuild.Times(n))
	}
	return fmt.Sprintf("%s today: rebuilt its cache %s", rebuild.Dollars(usd), rebuild.Times(n))
}

// wakePrice is what waking session x would cost now, once its prompt
// cache has run out: false while it is warm, for a local model, a context
// too small to count, or a model whose prices are not known. The caller
// holds c.mu.
func (c *Core) wakePrice(x *session, now time.Time) (rebuild.Wake, bool) {
	if x.local || !c.cfg().Usage.Rebuilds {
		return rebuild.Wake{}, false
	}
	w, ok := x.tally.wake(x.conversation())
	if !ok || !w.Priced || w.Tokens < rebuild.MinTokens || !w.IsCold(now) {
		return rebuild.Wake{}, false
	}
	return w, true
}

// coldOf says w for a row or a closed session.
func coldOf(w rebuild.Wake) *ipc.Cold {
	return &ipc.Cold{Since: w.Cold, Tokens: w.Tokens, USD: roundCents(w.USD), Words: "~" + rebuild.Dollars(w.USD) + " to wake"}
}

// coldRow is the price of waking session x now that its cache has run
// out, for its row; nil while it works or is warm, or when its agent has
// ended. The caller holds c.mu.
func (c *Core) coldRow(x *session, state string, now time.Time) *ipc.Cold {
	switch state {
	case ipc.StateWorking, ipc.StateStarting, ipc.StateExited, ipc.StateFailed:
		return nil
	}
	if w, ok := c.wakePrice(x, now); ok {
		return coldOf(w)
	}
	return nil
}

// coldWords is the price of waking x, for a note on a thaw or a send, as
// "its cache has run out: waking it writes 310k tokens again, ~$2.40"; ""
// while it is warm. The caller holds c.mu.
func (c *Core) coldWords(x *session, now time.Time) string {
	w, ok := c.wakePrice(x, now)
	if !ok {
		return ""
	}
	return wakeWords(w)
}

// reopenNote says what closed session r's next turn costs once reopened,
// for a question before its reopen: " Its prompt cache has run out, so its
// next turn writes 310k tokens into it again, ~$2.40."; "" while its cache
// is warm or when Irie cannot tell. The caller holds c.mu.
func (c *Core) reopenNote(r closedRecord, now time.Time) string {
	w := r.Wake
	if !c.cfg().Usage.Rebuilds || !w.Priced || w.Tokens < rebuild.MinTokens || !w.IsCold(now) {
		return ""
	}
	return fmt.Sprintf(" Its prompt cache has run out, so its next turn writes %s tokens into it again, ~%s.", rebuild.Tokens(w.Tokens), rebuild.Dollars(w.USD))
}

// wakeWords say what waking a cold conversation costs.
func wakeWords(w rebuild.Wake) string {
	return fmt.Sprintf("its cache has run out: waking it writes %s tokens again, ~%s", rebuild.Tokens(w.Tokens), rebuild.Dollars(w.USD))
}

// tooDear reports whether waking x would cost more than a nudge is worth
// (usage.wake_most), and says so, for the trod bot and the jot dog. The
// caller holds c.mu.
func (c *Core) tooDear(x *session, now time.Time) (string, bool) {
	most := c.cfg().Usage.WakeMost
	if most <= 0 {
		return "", false
	}
	w, ok := c.wakePrice(x, now)
	if !ok || w.USD <= most {
		return "", false
	}
	return fmt.Sprintf("left cold: waking it would write %s tokens into its cache again, ~%s, more than usage.wake_most (%s)",
		rebuild.Tokens(w.Tokens), rebuild.Dollars(w.USD), rebuild.Dollars(most)), true
}

// rebuildsOp is the meter's report (irie ctl rebuilds): today's rebuilds
// and the week's, by session and by cause, what cost most first.
func (c *Core) rebuildsOp() ipc.RebuildsResult {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.meter.prune(now)
	today, week := days(now)
	r := ipc.RebuildsResult{On: c.cfg().Usage.Rebuilds, Today: ipc.RebuildsSpan{From: today}, Week: ipc.RebuildsSpan{From: week}}
	for _, span := range []*ipc.RebuildsSpan{&r.Today, &r.Week} {
		bySession, byCause := map[string]*ipc.RebuildsPart{}, map[string]*ipc.RebuildsPart{}
		for id, list := range c.meter.by {
			for _, b := range list {
				if b.at.Before(span.From) {
					continue
				}
				span.Count++
				span.Tokens += b.tokens
				span.USD += b.usd
				s := bySession[id]
				if s == nil {
					s = &ipc.RebuildsPart{ID: id, Name: c.rebuildName(id, b.name)}
					s.Open = c.sessions[id] != nil
					bySession[id] = s
				}
				cause := b.cause
				if cause == ipc.RebuildThaw && b.by == trodBy {
					cause = rebuildTrodThaw
				}
				k := byCause[cause]
				if k == nil {
					k = &ipc.RebuildsPart{Name: cause, Words: causeWords(cause)}
					byCause[cause] = k
				}
				for _, p := range []*ipc.RebuildsPart{s, k} {
					p.Count++
					p.Tokens += b.tokens
					p.USD += b.usd
				}
			}
		}
		span.USD = roundCents(span.USD)
		span.Sessions, span.Causes = ranked(bySession), ranked(byCause)
	}
	return r
}

// rebuildName is session id's name now, or the one it had when its rebuild
// was journalled. The caller holds c.mu.
func (c *Core) rebuildName(id, then string) string {
	if x := c.sessions[id]; x != nil {
		return x.name
	}
	for _, r := range c.closes.list {
		if r.ID == id {
			return r.Name
		}
	}
	if then != "" {
		return then
	}
	return id
}

// ranked are parts, what cost most first, then the most rebuilds, then by
// name.
func ranked(parts map[string]*ipc.RebuildsPart) []ipc.RebuildsPart {
	out := make([]ipc.RebuildsPart, 0, len(parts))
	for _, p := range parts {
		p.USD = roundCents(p.USD)
		out = append(out, *p)
	}
	slices.SortFunc(out, func(a, b ipc.RebuildsPart) int {
		return cmp.Or(cmp.Compare(b.USD, a.USD), cmp.Compare(b.Count, a.Count), strings.Compare(a.Name, b.Name), strings.Compare(a.ID, b.ID))
	})
	return out
}

// causeWords say what woke a session for its rebuilds, for the report.
func causeWords(cause string) string {
	switch cause {
	case ipc.RebuildYou:
		return "your message after a long idle"
	case ipc.RebuildTrod:
		return "the trod bot's message"
	case ipc.RebuildDog:
		return "the jot dog's nudge"
	case ipc.RebuildThaw:
		return "a thaw"
	case rebuildTrodThaw:
		return "the trod bot's thaw"
	case ipc.RebuildReopen:
		return "a reopen"
	case ipc.RebuildBackground:
		return "its own work in the background"
	case ipc.RebuildLimit:
		return "a usage limit's reset"
	case ipc.RebuildBoost:
		return brand.Name + "'s carry-on after API errors"
	case ipc.RebuildTool:
		return "a long tool call"
	case ipc.RebuildChanged:
		return "a changed prompt, not a cold cache"
	}
	return "not known"
}
