// Package rebuild is the prompt-cache meter's reading of transcripts
// (DESIGN §6.3): it finds the requests that wrote most of a conversation's
// context into the cache again instead of reading it, and prices them.
//
// Claude Code sends a conversation's whole context with each request.
// Anthropic's prompt cache keeps what a request sent for five minutes or an
// hour from the request that last read or wrote it. A request within that
// time reads the context at a tenth of the input price or less; one after
// it, as when a session wakes from a long idle, a freeze or a reopen, writes
// it all into the cache again at 1.25 or 2 times the input price. On Claude
// Opus 5.5 such a rebuild into the hour's cache costs as much as 40 reads of
// it.
package rebuild

import (
	"bytes"
	"encoding/json"
	"time"
)

// MinTokens is the least a request must write again of its conversation's
// context to count as a rebuild. Claude Code's system prompt and tools take
// about this much, so a smaller loss is a conversation that has barely
// begun.
const MinTokens = 20_000

// DefaultTTL is how long a cache lives when its writes do not say: the
// API's default of five minutes.
const DefaultTTL = 5 * time.Minute

// Request is one model call of a conversation, from the first transcript
// line of its reply, whose usage says what the call sent.
type Request struct {
	ID      string    // the reply's message id
	At      time.Time // when the reply was written
	Model   string
	Input   int64  // input_tokens: what came after the prompt's last cache breakpoint
	Write   int64  // cache_creation_input_tokens: what it wrote into the cache
	Write1h int64  // of which into the hour's cache (cache_creation.ephemeral_1h_input_tokens)
	Write5m int64  // and into the five minutes' (ephemeral_5m_input_tokens)
	Read    int64  // cache_read_input_tokens
	Output  int64  // output_tokens, which a reply's later lines bring up to date
	Speed   string // usage.speed: "fast" in fast mode
	Geo     string // usage.inference_geo: "us" costs 1.1 times as much
	Tool    bool   // its reply stopped to call a tool (stop_reason tool_use)
}

// Prompt is everything the request sent: what it read from the cache, what
// it wrote into the cache, and the rest.
func (r Request) Prompt() int64 { return r.Input + r.Write + r.Read }

// TTL is how long what the request wrote into the cache lives: an hour when
// any of it went into the hour's cache, else five minutes; 0 when it wrote
// nothing.
func (r Request) TTL() time.Duration {
	switch {
	case r.Write1h > 0:
		return time.Hour
	case r.Write > 0:
		return DefaultTTL
	}
	return 0
}

// rates are the prices the request paid, and false for a model whose
// prices are not known.
func (r Request) rates() (Rates, bool) {
	p, ok := Price(r.Model, r.Prompt())
	if !ok {
		return Rates{}, false
	}
	return p.scaled(r.Speed, r.Geo), true
}

// writeCost is what tokens of the request's writes into the cache cost, at
// the share of its writes that went into the hour's cache and the five
// minutes'. A write that does not say which cache it went into went into
// the five minutes', the API's default.
func (r Request) writeCost(tokens int64) (float64, bool) {
	p, ok := r.rates()
	if !ok {
		return 0, false
	}
	rate := p.Write5m
	if split := r.Write1h + r.Write5m; split > 0 {
		rate = (float64(r.Write1h)*p.Write1h + float64(r.Write5m)*p.Write5m) / float64(split)
	}
	return float64(tokens) * rate / 1e6, true
}

// Rebuild is a request that wrote most of the context its conversation's
// request before had sent into the cache again, rather than reading it.
type Rebuild struct {
	Request
	Agent  string        // the helper whose conversation it is; "" for the session's own
	Tokens int64         // what it wrote again of the context before
	Idle   time.Duration // since the request before
	TTL    time.Duration // how long the cache it found lived
	// Cold: the cache had run out, since it had been idle about as long as
	// the cache lives or longer. Else the start of its prompt changed, as
	// for another model, other tools or other settings.
	Cold bool
	// Before is the request before, which the cache was written for.
	Before Request
	USD    float64 // what writing Tokens again cost at list price
	Priced bool    // false for a model whose prices are not known
}

// coldShare is how much of its cache's life a conversation must have been
// idle for a rebuild to count as cold. A cache's life runs from the start
// of the request that last read it, and a transcript dates a reply when it
// was written, after the request's start, so the idle time read is a
// little short.
const coldShare = 0.9

// Chain is one conversation's requests in order, as a transcript holds
// them.
type Chain struct {
	last      Request
	has       bool
	ttl       time.Duration // how long the cache its latest write made lives
	compacted bool          // compacted since its last request: the next starts a new context
}

// Add takes the conversation's next request and reports whether it rebuilt
// the cache: it read less than half of what the request before had sent,
// and wrote at least MinTokens of that again. A request after a compaction
// starts a new context, which is no rebuild. A request that sent nothing,
// such as the reply Claude Code writes for an API error, is not one.
func (c *Chain) Add(r Request) (Rebuild, bool) {
	if r.Prompt() == 0 {
		return Rebuild{}, false
	}
	if c.has && r.ID != "" && r.ID == c.last.ID {
		// The same reply's next line.
		c.Grow(r.ID, r.Output)
		return Rebuild{}, false
	}
	prev, had, life, compacted := c.last, c.has, c.life(), c.compacted
	c.last, c.has, c.compacted = r, true, false
	if t := r.TTL(); t > 0 {
		c.ttl = t
	}
	if !had || compacted {
		return Rebuild{}, false
	}
	lost := prev.Prompt() - r.Read
	if lost < MinTokens || 2*lost < prev.Prompt() || r.Write < MinTokens {
		return Rebuild{}, false
	}
	b := Rebuild{Request: r, Tokens: min(r.Write, lost), Idle: r.At.Sub(prev.At), TTL: life, Before: prev}
	b.Cold = float64(b.Idle) >= coldShare*float64(life)
	b.USD, b.Priced = r.writeCost(b.Tokens)
	return b, true
}

// Grow brings the latest request's output up to date from a later line of
// its reply.
func (c *Chain) Grow(id string, output int64) {
	if c.has && c.last.ID == id && output > c.last.Output {
		c.last.Output = output
	}
}

// Compact notes a compaction: the next request starts a new context.
func (c *Chain) Compact() { c.compacted = true }

// life is how long the conversation's cache lives.
func (c *Chain) life() time.Duration {
	if c.ttl > 0 {
		return c.ttl
	}
	return DefaultTTL
}

// Wake is what a conversation's next request would cost in its cache: once
// the cache has run out, it writes the whole context again.
type Wake struct {
	Last   time.Time     // its latest request
	TTL    time.Duration // how long its cache lives
	Cold   time.Time     // when its cache runs out, or ran out
	Tokens int64         // its context: what its latest request sent, and the reply
	Model  string
	USD    float64 // what writing Tokens into its cache costs at list price
	Priced bool    // false for a model whose prices are not known
}

// IsCold reports whether the cache has run out by now.
func (w Wake) IsCold(now time.Time) bool { return !w.Last.IsZero() && !now.Before(w.Cold) }

// Wake is what the conversation's next request would cost in its cache, and
// false before its first request or after a compaction, when its context is
// not known yet.
func (c *Chain) Wake() (Wake, bool) {
	if !c.has || c.compacted {
		return Wake{}, false
	}
	r, life := c.last, c.life()
	w := Wake{Last: r.At, TTL: life, Cold: r.At.Add(life), Tokens: r.Prompt() + r.Output, Model: r.Model}
	if p, ok := Price(r.Model, w.Tokens); ok {
		p = p.scaled(r.Speed, r.Geo)
		rate := p.Write5m
		if life >= time.Hour {
			rate = p.Write1h
		}
		w.USD, w.Priced = float64(w.Tokens)*rate/1e6, true
	}
	return w, true
}

// Transcript is the meter's reading of one transcript: the conversation's
// own chain of requests, and each helper's whose part is written there too,
// as older Claude Code wrote its subagents' (isSidechain, agentId).
type Transcript struct {
	chains map[string]*Chain
}

// Line is what one transcript line tells the meter.
type Line struct {
	Request
	Usage   bool   // an assistant reply's line, with its usage
	Agent   string // the helper whose part of the transcript it is in; "" for the conversation's own
	Compact bool   // a compaction boundary: what follows starts a new context
}

// sidechain stands for a helper's part of a transcript that names no agent,
// whose requests cannot be told from another helper's.
const sidechain = "\x00sidechain"

// ParseLine reads one transcript line, and reports whether it says anything
// the meter reads.
func ParseLine(b []byte) (Line, bool) {
	compact := bytes.Contains(b, []byte(`"compact_boundary"`))
	if !compact && !bytes.Contains(b, []byte(`"usage"`)) {
		return Line{}, false
	}
	var rec struct {
		Type      string `json:"type"`
		Subtype   string `json:"subtype"`
		Timestamp string `json:"timestamp"`
		Sidechain bool   `json:"isSidechain"`
		AgentID   string `json:"agentId"`
		Message   struct {
			ID         string `json:"id"`
			Model      string `json:"model"`
			StopReason string `json:"stop_reason"`
			Usage      *struct {
				Input         int64 `json:"input_tokens"`
				Output        int64 `json:"output_tokens"`
				CacheWrite    int64 `json:"cache_creation_input_tokens"`
				CacheRead     int64 `json:"cache_read_input_tokens"`
				CacheCreation *struct {
					W5m int64 `json:"ephemeral_5m_input_tokens"`
					W1h int64 `json:"ephemeral_1h_input_tokens"`
				} `json:"cache_creation"`
				Speed string `json:"speed"`
				Geo   string `json:"inference_geo"`
			} `json:"usage"`
		} `json:"message"`
	}
	if json.Unmarshal(b, &rec) != nil {
		return Line{}, false
	}
	l := Line{}
	if rec.Sidechain {
		l.Agent = rec.AgentID
		if l.Agent == "" {
			l.Agent = sidechain
		}
	}
	if rec.Type == "system" && rec.Subtype == "compact_boundary" {
		l.Compact = true
		return l, true
	}
	u := rec.Message.Usage
	if u == nil || rec.Message.ID == "" {
		return Line{}, false
	}
	at, _ := time.Parse(time.RFC3339Nano, rec.Timestamp)
	l.Usage = true
	l.Request = Request{
		ID: rec.Message.ID, At: at, Model: rec.Message.Model,
		Input: u.Input, Write: u.CacheWrite, Read: u.CacheRead, Output: u.Output,
		Speed: u.Speed, Geo: u.Geo, Tool: rec.Message.StopReason == "tool_use",
	}
	if cc := u.CacheCreation; cc != nil {
		l.Write1h, l.Write5m = cc.W1h, cc.W5m
	}
	return l, true
}

// Read takes a line of the transcript and reports whether it rebuilt its
// conversation's cache. first says whether the line is its reply's first:
// a reply spans a line for each of its content blocks, each with its
// usage, so only the first is a request, and the later ones only bring its
// output up to date. A helper's part that names no agent is left out.
func (t *Transcript) Read(l Line, first bool) (Rebuild, bool) {
	if l.Agent == sidechain {
		return Rebuild{}, false
	}
	if t.chains == nil {
		t.chains = map[string]*Chain{}
	}
	c := t.chains[l.Agent]
	if c == nil {
		c = &Chain{}
		t.chains[l.Agent] = c
	}
	switch {
	case l.Compact:
		c.Compact()
	case l.Usage && first:
		b, ok := c.Add(l.Request)
		b.Agent = l.Agent
		return b, ok
	case l.Usage:
		c.Grow(l.ID, l.Output)
	}
	return Rebuild{}, false
}

// Wake is what the next request of the transcript's own conversation would
// cost in its cache, and false when that is not known.
func (t *Transcript) Wake() (Wake, bool) {
	if c := t.chains[""]; c != nil {
		return c.Wake()
	}
	return Wake{}, false
}
