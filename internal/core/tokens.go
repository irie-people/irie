package core

import (
	"bytes"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/ledger"
	"github.com/irie-people/irie/internal/rebuild"
)

// tokenTally counts a session's model usage from its Claude Code
// transcripts (JSONL). A reply spans one line per content block and each
// line repeats the reply's usage, so each message id counts once. Each file
// is read on from where the last read stopped. The same reads catch the
// titles Claude Code gives the session (title.go), and price each call for
// the ledger (DESIGN §6.3).
type tokenTally struct {
	files map[string]*transcriptCount
	// since is when the session began: a call before it, as in a fork's
	// copy of the conversation it forked from, is not its own.
	since time.Time
}

type transcriptCount struct {
	follower
	byID  map[string]ipc.Tokens // usage per message id
	sum   ipc.Tokens
	title string // the newest title read and not yet taken
	// The prompt-cache meter's reading of it (rebuilds.go), and the
	// rebuilds read since the meter last took them.
	meter rebuild.Transcript
	found []rebuild.Rebuild
	// The ledger's calls by message id: each request's own, then each
	// part of it its top-level usage leaves out; and what they came to.
	calls map[string][]ledger.Call
	spent ledger.Sum
	since time.Time
	// refusals are the replies that are, or came after, a refusal, by
	// message id: a reply's lines do not each say so.
	refusals map[string]refusal
}

// refusal is what a reply's lines said of a refusal: that it is one, and
// its category.
type refusal struct {
	refused  bool
	category string
}

// sinceSlack is how far before a session's start a call may be dated and
// still be its own: a transcript dates a reply when it was written.
const sinceSlack = time.Minute

// maxRead caps one read, so a long transcript is caught up over a few ticks.
const maxRead = 8 << 20

// follower reads a growing JSONL file, such as a transcript, a line at a
// time from where its last read stopped. The token tally and the
// conversations read transcripts this way.
type follower struct {
	offset  int64
	partial []byte // an unfinished last line
}

// read calls line for each complete line the file gained since the last
// read, reading at most maxRead bytes. A file that shrank was replaced:
// reset is called first, and the file is read from its start.
func (f *follower) read(path string, reset func(), line func([]byte)) {
	fh, err := os.Open(path)
	if err != nil {
		return // not written yet, or gone
	}
	defer fh.Close()
	st, err := fh.Stat()
	if err != nil {
		return
	}
	if st.Size() < f.offset {
		*f = follower{}
		reset()
	}
	if st.Size() == f.offset {
		return
	}
	buf := make([]byte, min(st.Size()-f.offset, maxRead))
	n, _ := fh.ReadAt(buf, f.offset)
	if n == 0 {
		return
	}
	f.offset += int64(n)
	data := append(f.partial, buf[:n]...)
	end := bytes.LastIndexByte(data, '\n')
	if end < 0 {
		f.partial = data
		return
	}
	f.partial = append([]byte(nil), data[end+1:]...)
	for _, l := range bytes.Split(data[:end], []byte{'\n'}) {
		line(l)
	}
}

// update reads what the transcripts gained since the last call and returns
// the totals, or false while none of them has any usage.
func (t *tokenTally) update(paths []string) (ipc.Tokens, bool) {
	if t.files == nil {
		t.files = map[string]*transcriptCount{}
	}
	var total ipc.Tokens
	seen := false
	for _, p := range paths {
		f := t.files[p]
		if f == nil {
			f = &transcriptCount{byID: map[string]ipc.Tokens{}, calls: map[string][]ledger.Call{}, spent: ledger.Sum{}}
			if !t.since.IsZero() {
				f.since = t.since.Add(-sinceSlack)
			}
			t.files[p] = f
		}
		f.read(p)
		if len(f.byID) > 0 {
			seen = true
			total = addTokens(total, f.sum, 1)
		}
	}
	return total, seen
}

// spent is what the calls read from the transcripts at paths came to, by
// day, model and purpose: a copy.
func (t *tokenTally) spent(paths []string) ledger.Sum {
	out := ledger.Sum{}
	for _, p := range paths {
		if f := t.files[p]; f != nil {
			out.Merge(f.spent)
		}
	}
	return out
}

// readAll reads the transcripts at paths to their ends, as for a session
// no longer open, whose files no longer grow.
func (t *tokenTally) readAll(paths []string) {
	for {
		before := t.offsets(paths)
		t.update(paths)
		if t.offsets(paths) == before {
			return
		}
	}
}

// offsets is how far the transcripts at paths have been read, in all.
func (t *tokenTally) offsets(paths []string) int64 {
	var n int64
	for _, p := range paths {
		if f := t.files[p]; f != nil {
			n += f.offset
		}
	}
	return n
}

// rebuilds takes the rebuilds of the prompt cache read from the
// transcripts since the last call, the transcripts' in the order of paths.
func (t *tokenTally) rebuilds(paths []string) []rebuild.Rebuild {
	var out []rebuild.Rebuild
	for _, p := range paths {
		if f := t.files[p]; f != nil && len(f.found) > 0 {
			out = append(out, f.found...)
			f.found = nil
		}
	}
	return out
}

// wake is what the next request of the conversation in the transcript at
// path would cost in its cache; false while it has made no request, as
// just after /clear.
func (t *tokenTally) wake(path string) (rebuild.Wake, bool) {
	if f := t.files[path]; f != nil {
		return f.meter.Wake()
	}
	return rebuild.Wake{}, false
}

// title returns the newest title that the transcripts at paths gained since
// the last call, if any.
func (t *tokenTally) title(paths []string) (string, bool) {
	title, ok := "", false
	for _, p := range paths {
		if f := t.files[p]; f != nil && f.title != "" {
			title, ok, f.title = f.title, true, ""
		}
	}
	return title, ok
}

func (f *transcriptCount) read(path string) {
	// Replaced or cut short: count it again from the start.
	f.follower.read(path, func() {
		f.byID, f.sum, f.title = map[string]ipc.Tokens{}, ipc.Tokens{}, ""
		f.meter, f.found = rebuild.Transcript{}, nil
		f.calls, f.spent, f.refusals = map[string][]ledger.Call{}, ledger.Sum{}, nil
	}, f.line)
}

func (f *transcriptCount) line(b []byte) {
	if t, ok := titleLine(b); ok {
		f.title = t
		return
	}
	l, ok := rebuild.ParseLine(b)
	if !ok {
		return
	}
	if !l.Usage {
		f.meter.Read(l, false) // a compaction
		return
	}
	now := ipc.Tokens{Input: l.Input, Output: l.Output, CacheRead: l.Read, CacheWrite: l.Write}
	old, seen := f.byID[l.ID]
	if seen {
		f.sum = addTokens(f.sum, old, -1)
	}
	f.byID[l.ID] = now
	f.sum = addTokens(f.sum, now, 1)
	f.price(l)
	if b, ok := f.meter.Read(l, !seen); ok {
		f.found = append(f.found, b)
	}
}

// price puts the calls of a reply's line in the ledger, in place of what
// an earlier line of the same reply put there: each line repeats the
// reply's usage, its output and iterations as far as they had come. A
// call from before the session began is not its own.
func (f *transcriptCount) price(l rebuild.Line) {
	r := f.refusals[l.ID]
	if l.Refused || l.Refusal != "" {
		r.refused = r.refused || l.Refused
		if l.Refusal != "" {
			r.category = l.Refusal
		}
		if f.refusals == nil {
			f.refusals = map[string]refusal{}
		}
		f.refusals[l.ID] = r
	}
	calls := lineCalls(l, r)
	if !f.since.IsZero() && !l.At.IsZero() && l.At.Before(f.since) {
		calls = nil
	}
	for _, c := range f.calls[l.ID] {
		f.spent.Remove(c)
	}
	if prev := f.calls[l.ID]; len(prev) > 0 && len(calls) > 0 && prev[0].Usage.Output > calls[0].Usage.Output {
		calls[0].Usage.Output = prev[0].Usage.Output
	}
	f.calls[l.ID] = calls
	for _, c := range calls {
		f.spent.Add(c)
	}
}

// synthetic is the model of a reply Claude Code writes itself, as for an
// API error, with no call behind it.
const synthetic = "<synthetic>"

// lineCalls are the ledger's calls in a reply's line, r being what its
// lines said of a refusal: the request's own, its web searches with it,
// then each part of it that its top-level usage leaves out, at its own
// model or else the request's. A reply Claude Code wrote itself is no
// call.
//
// After a server-side fallback the top level is the attempt that
// answered, and each "message" in usage.iterations an attempt a model
// declined; otherwise they are what the top level sums. A refusal is
// billed only once its output began or when its category is billed, so
// one that is not is a call that used nothing paid for.
func lineCalls(l rebuild.Line, r refusal) []ledger.Call {
	if l.Model == synthetic {
		return nil
	}
	purpose := ledger.Turn
	if l.Agent != "" {
		purpose = ledger.Helper
	}
	fellBack := slices.ContainsFunc(l.Parts, func(p rebuild.Part) bool { return p.Type == "fallback_message" })
	billed := ledger.RefusalBilled(r.category, l.At)
	top := ledger.Call{
		At: l.At, Purpose: purpose, Model: l.Model, Speed: l.Speed, Geo: l.Geo,
		Usage: ledger.Usage{Input: l.Input, Write5m: l.Write - min(l.Write1h, l.Write), Write1h: min(l.Write1h, l.Write), Read: l.Read, Output: l.Output, Searches: l.Searches},
	}
	if r.refused && !fellBack && l.Output == 0 && !billed {
		top.Purpose, top.Usage = ledger.Refused, ledger.Usage{}
	}
	calls := []ledger.Call{top}
	for _, p := range l.Parts {
		model, purpose := p.Model, partPurpose(p.Type)
		switch {
		case p.Type == "fallback_message", p.Type == "message" && !fellBack:
			continue // the top level holds it
		case p.Type == "message" && p.Output == 0 && !billed:
			continue // declined before any output, unbilled
		case p.Type == "message":
			purpose = ledger.Refused
		}
		if model == "" {
			model = l.Model
		}
		calls = append(calls, ledger.Call{
			At: l.At, Purpose: purpose, Model: model, Speed: l.Speed, Geo: l.Geo,
			Usage: ledger.Usage{Input: p.Input, Write5m: p.Write - min(p.Write1h, p.Write), Write1h: min(p.Write1h, p.Write), Read: p.Read, Output: p.Output},
		})
	}
	return calls
}

// partPurpose is the ledger's purpose for a part of a request: compaction,
// advisor for the advisor tool's advisor_message, else the part's own type.
func partPurpose(typ string) string {
	switch typ {
	case "compaction":
		return ledger.Compaction
	case "advisor_message":
		return ledger.Advisor
	}
	return strings.TrimSuffix(typ, "_message")
}

// addTokens returns a plus sign times b.
func addTokens(a, b ipc.Tokens, sign int64) ipc.Tokens {
	return ipc.Tokens{
		Input:      a.Input + sign*b.Input,
		Output:     a.Output + sign*b.Output,
		CacheRead:  a.CacheRead + sign*b.CacheRead,
		CacheWrite: a.CacheWrite + sign*b.CacheWrite,
	}
}
