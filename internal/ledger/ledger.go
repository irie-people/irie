// Package ledger is Irie's call-by-call record of what model calls cost
// (DESIGN §6.3): each call its agents and Irie itself make, priced at the
// list price in effect on the day it was made. A call on this computer or
// on a server of the owner's costs nothing. A call to a model whose price
// Irie does not know is counted apart, so a total reads "$0.42, plus 3
// calls with no price" and never takes the unknown for $0.
//
// The idea of a ledger of calls by purpose, with dated prices and unpriced
// calls kept visible, is Blitz's (spec_056 CA-120 to CA-126, spec_002
// CFG-13; PROVENANCE.md). The code is Irie's own.
package ledger

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

// Purposes: what a call was for.
const (
	Turn   = "turn"   // a session's own agent
	Helper = "helper" // one of its helpers, a subagent or a teammate
	// Compaction and Advisor are parts of a request that its top-level
	// usage leaves out and its usage.iterations holds: the API's summary
	// of a conversation, and the advisor tool's sub-inference, which runs
	// on a model of its own.
	Compaction = "compaction"
	Advisor    = "advisor"
	// Refused is an attempt a model declined: billed when it had begun its
	// output, or its refusal's category is billed (RefusalBilled); else it
	// used nothing that is paid for.
	Refused = "refused"
	Route   = "route" // route.embed: a task's class (§6.4)
	Find    = "find"  // irie ctl find: what sessions were about (§4.5)
	Card    = "card"  // a materialized card
)

// Usage is what a call used, each kind once.
type Usage struct {
	Input    int64 `json:"input,omitempty"`    // input tokens after the prompt's last cache breakpoint
	Write5m  int64 `json:"write_5m,omitempty"` // written into the five minutes' cache
	Write1h  int64 `json:"write_1h,omitempty"` // written into the hour's cache
	Read     int64 `json:"read,omitempty"`     // read from the cache
	Output   int64 `json:"output,omitempty"`
	Searches int64 `json:"searches,omitempty"` // web searches, priced each on top of the tokens
}

// Prompt is every token the call sent: what it read from the cache, what
// it wrote into the cache, and the rest.
func (u Usage) Prompt() int64 { return u.Input + u.Write5m + u.Write1h + u.Read }

// None reports whether the call used nothing, as a reply Claude Code
// writes itself (model "<synthetic>") does not.
func (u Usage) None() bool { return u == Usage{} }

func (u Usage) plus(v Usage, sign int64) Usage {
	return Usage{
		Input: u.Input + sign*v.Input, Write5m: u.Write5m + sign*v.Write5m, Write1h: u.Write1h + sign*v.Write1h,
		Read: u.Read + sign*v.Read, Output: u.Output + sign*v.Output, Searches: u.Searches + sign*v.Searches,
	}
}

// Call is one model call.
type Call struct {
	At      time.Time `json:"at"`
	Purpose string    `json:"purpose"`
	Model   string    `json:"model"`
	Usage   Usage     `json:"usage"`
	Speed   string    `json:"speed,omitempty"` // "fast" in fast mode
	Geo     string    `json:"geo,omitempty"`   // inference_geo: "us" costs a tenth more
	// Free says it ran on this computer or on a server of the owner's, so
	// it cost nothing, whatever its model.
	Free bool `json:"free,omitempty"`
}

// Cost is what c cost at the list prices in effect when it was made, and
// false when its model had no price then. A free call, and one that used
// nothing, cost $0 whatever its model.
func (c Call) Cost() (float64, bool) {
	u := c.Usage
	if c.Free || u.None() {
		return 0, true
	}
	usd := float64(u.Searches) * SearchAt(c.At)
	if u.Prompt()+u.Output == 0 {
		return usd, true
	}
	r, ok := At(c.Model, u.Prompt(), c.At)
	if !ok {
		return 0, false
	}
	r = r.Scaled(c.Speed, c.Geo)
	usd += (float64(u.Input)*r.Input + float64(u.Write5m)*r.Write5m + float64(u.Write1h)*r.Write1h +
		float64(u.Read)*r.Read + float64(u.Output)*r.Output) / 1e6
	return usd, true
}

// Tally is what calls came to.
type Tally struct {
	Calls    int     `json:"calls"`
	USD      float64 `json:"usd,omitempty"`      // what the priced calls cost
	Unpriced int     `json:"unpriced,omitempty"` // calls to a model with no price, whose cost is not known
	Free     int     `json:"free,omitempty"`     // calls on this computer or a server of the owner's
	Usage    Usage   `json:"usage"`
}

func (t *Tally) add(c Call, sign int) {
	t.Calls += sign
	t.Usage = t.Usage.plus(c.Usage, int64(sign))
	usd, ok := c.Cost()
	switch {
	case c.Free:
		t.Free += sign
	case !ok:
		t.Unpriced += sign
	default:
		t.USD += float64(sign) * usd
	}
}

// Merge adds o's calls to t's.
func (t *Tally) Merge(o Tally) {
	t.Calls += o.Calls
	t.USD += o.USD
	t.Unpriced += o.Unpriced
	t.Free += o.Free
	t.Usage = t.Usage.plus(o.Usage, 1)
}

// Known reports whether every call has a price, so USD is the whole cost.
func (t Tally) Known() bool { return t.Unpriced == 0 }

// Words say what the calls cost: "$12.40", "$12.40, plus 3 calls with no
// price", "free", "no price" when none has one, or "nothing" for no calls.
func (t Tally) Words() string {
	switch {
	case t.Calls == 0:
		return "nothing"
	case t.Unpriced == t.Calls:
		return "no price"
	case t.Free == t.Calls:
		return "free"
	}
	s := Dollars(t.USD)
	if t.Unpriced > 0 {
		s += fmt.Sprintf(", plus %d %s with no price", t.Unpriced, plural(t.Unpriced, "call", "calls"))
	}
	return s
}

// Dollars is v in dollars and cents, as "$4.10"; "<$0.01" for less than a
// cent that is more than nothing.
func Dollars(v float64) string {
	if v > 0 && v < 0.005 {
		return "<$0.01"
	}
	return fmt.Sprintf("$%.2f", v)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// Key is what a Sum adds calls up by.
type Key struct {
	Day     string `json:"day"` // the day it was made, in this computer's time zone: 2006-01-02
	Model   string `json:"model"`
	Purpose string `json:"purpose"`
}

// KeyOf is the key c adds up under.
func KeyOf(c Call) Key {
	return Key{Day: c.At.Local().Format(time.DateOnly), Model: c.Model, Purpose: c.Purpose}
}

// Day is the key day of t.
func Day(t time.Time) string { return t.Local().Format(time.DateOnly) }

// Sum is calls added up by day, model and purpose. Its totals are sums of
// the calls, so a call with no price leaves only its own cost unknown.
type Sum map[Key]*Tally

// Add adds c.
func (s Sum) Add(c Call) { s.change(c, 1) }

// Remove takes away a call added before, as when a reply's later line
// brings its usage up to date.
func (s Sum) Remove(c Call) { s.change(c, -1) }

func (s Sum) change(c Call, sign int) {
	k := KeyOf(c)
	t := s[k]
	if t == nil {
		t = &Tally{}
		s[k] = t
	}
	t.add(c, sign)
	if t.Calls <= 0 {
		delete(s, k)
	}
}

// Merge adds o's calls to s's.
func (s Sum) Merge(o Sum) {
	for k, t := range o {
		if s[k] == nil {
			s[k] = &Tally{}
		}
		s[k].Merge(*t)
	}
}

// Clone is a copy of s.
func (s Sum) Clone() Sum {
	out := make(Sum, len(s))
	for k, t := range s {
		c := *t
		out[k] = &c
	}
	return out
}

// AllFree is s with every call free, as a local profile's are: they ran
// on this computer or a server of the owner's, so none cost anything.
func (s Sum) AllFree() Sum {
	out := make(Sum, len(s))
	for k, t := range s {
		out[k] = &Tally{Calls: t.Calls, Free: t.Calls, Usage: t.Usage}
	}
	return out
}

// Total is what the calls of from's day and later came to; every call's
// when from is "".
func (s Sum) Total(from string) Tally {
	var out Tally
	for k, t := range s {
		if k.Day >= from {
			out.Merge(*t)
		}
	}
	return out
}

// By adds the calls of from's day and later up by what part names of
// their keys, such as the model or the purpose.
func (s Sum) By(part func(Key) string, from string) map[string]Tally {
	out := map[string]Tally{}
	for k, t := range s {
		if k.Day < from {
			continue
		}
		v := out[part(k)]
		v.Merge(*t)
		out[part(k)] = v
	}
	return out
}

// Ranked are the parts of by, what cost most first, then those with
// calls with no price, then the most calls, then by name.
func Ranked(by map[string]Tally) []string {
	names := slices.Collect(maps.Keys(by))
	slices.SortFunc(names, func(a, b string) int {
		ta, tb := by[a], by[b]
		switch {
		case ta.USD != tb.USD:
			if ta.USD > tb.USD {
				return -1
			}
			return 1
		case ta.Unpriced != tb.Unpriced:
			return tb.Unpriced - ta.Unpriced
		case ta.Calls != tb.Calls:
			return tb.Calls - ta.Calls
		}
		return strings.Compare(a, b)
	})
	return names
}

// IsCloudModel reports whether an Ollama model runs on Ollama's cloud,
// someone else's computer, at a price: its tag ends in cloud, as
// gpt-oss:120b-cloud's does.
func IsCloudModel(name string) bool {
	_, tag, ok := strings.Cut(strings.ToLower(name), ":")
	return ok && (tag == "cloud" || strings.HasSuffix(tag, "-cloud"))
}
