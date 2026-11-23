package core

// Finding a session by what it did (DESIGN §4.5). A person remembers what
// a session was about, not its name, so find looks through every session
// Irie kept, open or closed, by its name, its task and what its agent said
// at the end of its last turn. It ranks them by their words (BM25) and by
// their meaning, by route.embed's model, and fuses the two orders by
// reciprocal rank, the words at half the weight. Each session has two
// embeddings, of its task and of what its agent said, and the nearer
// counts. On 40 lookups written as one user asks for sessions, over 493 of
// that user's sessions, this put the right one first 90% of the time and in
// the first five 98%; the names history searches put it first 10% of the
// time, words alone 70% and meaning alone 88%. The model reads the sessions
// in the background, so a find answers at once, by words where it has yet
// to.

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/ledger"
	"github.com/irie-people/irie/internal/route"
	"github.com/irie-people/irie/internal/search"
)

const (
	// findNear is the least cosine at which a session is close to the
	// words looked up. With embeddinggemma-2 and its search prompts, the
	// lookups that meant a session found it at 0.72 or more, and those
	// that meant none came to 0.67 at most.
	findNear    = 0.70
	findClip    = 1000 // the runes of a task, or of what an agent said, the model reads
	findShow    = 5    // the sessions a find shows unless asked for more
	findMost    = 50   // the most it shows
	findNearest = 3    // the nearest it shows when none is close
	findByWords = 0.5  // what the order by words weighs, against meaning's 1
	findSaid    = 160  // the runes shown of what an agent said
	findTask    = 100  // the runes shown of a task
	unread      = -2.0 // the cosine of a session the model has yet to read

	// findWait is the longest a find waits for the meaning of the words
	// looked up, which someone asked for, unlike a spawn's class: the
	// model may have to load first, or the call queue behind a batch of
	// the background read. On a busy Mac a cold load took over 3 s.
	findWait = 15 * time.Second
	// findBatch is how many texts the background read sends in one call:
	// a few, so that a spawn's embedding (embedWait) and a find queue
	// behind little. A busy Mac read 2.6 a second.
	findBatch = 4
	findKeep  = 15 * time.Second // the background read keeps what it made at most this often, and at its end
	findRests = 3                // how many rests after another call's failure the read waits out, in a row
)

// Session states, as find shows them.
const (
	foundOpen   = "open"
	foundClosed = "closed"
	foundLost   = "lost" // closed with Irie's tmux server, as in a restart
	foundOld    = "old"  // closed past what reopen reaches
)

// findIndex is the embeddings of the sessions' texts by route.embed's
// model, by each text's key, kept in find-vectors.json.
type findIndex struct {
	mu      sync.Mutex
	loaded  bool
	model   string               // whose embeddings vecs holds
	vecs    map[string][]float32 // unit length
	filling bool                 // a fill runs
}

// findDoc is a session as find reads it.
type findDoc struct {
	found ipc.Found
	words string   // its name, its task and what its agent said
	parts []string // its task and what its agent said, as the model reads them
}

// find looks the sessions up by what they did (irie ctl find).
func (c *Core) find(a ipc.FindArgs) (ipc.FindResult, error) {
	q := strings.TrimSpace(a.Query)
	if q == "" {
		return ipc.FindResult{}, ipc.Errorf(ipc.CodeBadArgs, "find needs words: what the session was about")
	}
	limit := a.Limit
	if limit <= 0 {
		limit = findShow
	}
	limit = min(limit, findMost)
	res := ipc.FindResult{Found: []ipc.Found{}}
	docs := c.findDocs()
	if len(docs) == 0 {
		return res, nil
	}
	cos, reading, note := c.findMeaning(q, docs)
	res.Meaning, res.Reading, res.Note = cos != nil, reading, note
	// The session that asks is left out, since its own task has the words,
	// once the model has read it, so its embeddings stay kept.
	if i := slices.IndexFunc(docs, func(d findDoc) bool { return d.found.ID == a.From }); a.From != "" && i >= 0 {
		docs = slices.Delete(docs, i, i+1)
		if cos != nil {
			cos = slices.Delete(cos, i, i+1)
		}
	}
	if found, close := rankFound(q, docs, cos, limit); len(found) > 0 {
		res.Found, res.Close = found, close
	}
	return res, nil
}

// rankFound orders the sessions, given newest first, for the words looked
// up: by their words and, given each one's cosine (unread for one the
// model has yet to read), by their meaning too, fused. It returns the
// first limit of those close to the words, or when none is, the nearest
// few, and whether they are close.
func rankFound(q string, docs []findDoc, cos []float64, limit int) ([]ipc.Found, bool) {
	words := search.Tokens(q, nil)
	toks := make([][]string, len(docs))
	for i, d := range docs {
		toks[i] = search.Tokens(d.words, nil)
	}
	x := search.NewIndex(toks)
	var byWords []int
	for _, h := range x.Search(words, -1) {
		byWords = append(byWords, h.Doc)
	}
	rankings := []search.Ranking{{Order: byWords, Weight: findByWords}}
	known := func(i int) bool { return cos != nil && cos[i] > unread }
	if cos != nil {
		var byMeaning []int
		for i := range docs {
			if known(i) {
				byMeaning = append(byMeaning, i)
			}
		}
		slices.SortStableFunc(byMeaning, func(i, j int) int { return cmp.Compare(cos[j], cos[i]) })
		rankings = append(rankings, search.Ranking{Order: byMeaning, Weight: 1})
	}
	fused := search.Fuse(len(docs), search.FuseK, rankings...)
	var order []int
	for i, s := range fused {
		if s > 0 {
			order = append(order, i)
		}
	}
	// Ties keep their order: the newest first, as findDocs gives them.
	slices.SortStableFunc(order, func(i, j int) int { return cmp.Compare(fused[j], fused[i]) })
	all := len(slices.Compact(slices.Sorted(slices.Values(words))))
	var near, rest []ipc.Found
	for _, i := range order {
		f := docs[i].found
		f.Words = x.Has(i, words)
		if known(i) {
			f.Cosine = math.Round(cos[i]*1000) / 1000
		}
		f.Close = known(i) && cos[i] >= findNear || all > 0 && f.Words == all
		if f.Close && len(near) < limit {
			near = append(near, f)
		} else if !f.Close && len(rest) < findNearest {
			rest = append(rest, f)
		}
	}
	if len(near) > 0 {
		return near, true
	}
	return rest, false
}

// findDocs are the sessions Irie kept on this machine, newest first: each
// folder under sessions/ that holds a task or what an agent said. Ids grow
// with time.
func (c *Core) findDocs() []findDoc {
	ents, err := os.ReadDir(c.paths.Sessions())
	if err != nil {
		if !os.IsNotExist(err) {
			c.log.Warn("find", "err", err)
		}
		return nil
	}
	c.mu.Lock()
	known := make(map[string]ipc.Found, len(c.closes.past)+len(c.sessions))
	for id, p := range c.closes.past {
		known[id] = ipc.Found{Name: p.name, State: foundOld, Closed: p.closed}
	}
	for _, r := range c.closes.list {
		f := ipc.Found{Name: r.Name, State: foundClosed, Closed: r.Closed, Reopen: true}
		if r.Lost {
			f.State = foundLost
		}
		known[r.ID] = f
	}
	for id, x := range c.sessions {
		known[id] = ipc.Found{Name: x.name, State: foundOpen}
	}
	c.mu.Unlock()
	model := c.cfg().Route.Embed.Model
	var out []findDoc
	for _, e := range slices.Backward(ents) {
		id := e.Name()
		if !e.IsDir() {
			continue
		}
		task := readText(c.paths.TaskFile(id))
		said := readText(filepath.Join(c.paths.Session(id), resultFile))
		if task == "" && said == "" {
			continue
		}
		f := known[id]
		f.ID, f.State = id, cmp.Or(f.State, foundOld)
		f.Name = cmp.Or(f.Name, gist(strings.TrimLeft(task, "# ")), id)
		f.Task, f.Said = cutLine(task, findTask), saidStart(said)
		if said != "" {
			f.File = filepath.Join(c.paths.Session(id), resultFile)
		}
		d := findDoc{found: f, words: f.Name + "\n" + task + "\n" + said}
		for _, part := range []string{task, said} {
			if part != "" {
				d.parts = append(d.parts, findText(model, f.Name, part))
			}
		}
		out = append(out, d)
	}
	return out
}

// readText is a file's text, trimmed, or nothing.
func readText(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// mdMarks are the marks of Markdown left out of what an agent said, as
// shown.
var mdMarks = strings.NewReplacer("**", "", "__", "", "`", "", "[!ASK]", "", "[!NOTE]", "", "[!WARNING]", "")

// saidStart is the start of what an agent said, on one line, without
// Markdown's marks.
func saidStart(said string) string {
	var b strings.Builder
	for line := range strings.Lines(said) {
		line = strings.TrimLeft(strings.TrimSpace(line), "#> ")
		if line == "" {
			continue
		}
		b.WriteString(line)
		b.WriteByte(' ')
		if b.Len() > 8*findSaid {
			break
		}
	}
	return cutLine(mdMarks.Replace(b.String()), findSaid)
}

// gemmaPrompts says the model is EmbeddingGemma, which reads a search as
// it was trained to: a lookup and a document each with its prompt.
// Measured, its prompts ranked the right session first 82% of the time,
// and plain text 78%. Another model reads plain text.
func gemmaPrompts(model string) bool {
	return strings.HasPrefix(strings.ToLower(model), "embeddinggemma")
}

// findText is a session's task, or what its agent said, as the model reads
// it.
func findText(model, name, part string) string {
	part = cutLine(part, findClip)
	if gemmaPrompts(model) {
		return "title: " + cmp.Or(name, "none") + " | text: " + part
	}
	return name + "\n" + part
}

// findQuery is the words looked up, as the model reads them.
func findQuery(model, q string) string {
	if gemmaPrompts(model) {
		return "task: search result | query: " + q
	}
	return q
}

// findKey names a text the model read, in find-vectors.json.
func findKey(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:8])
}

// findMeaning is each session's cosine to the words looked up, by the
// nearer of its texts, or unread where the model has yet to read it, and
// how many it has yet to read; or nil, and why, when meaning can't rank
// them. It starts the model reading what it has not.
func (c *Core) findMeaning(q string, docs []findDoc) ([]float64, int, string) {
	e := c.cfg().Route.Embed
	if e.Model == "" {
		return nil, 0, "route.embed.model is empty"
	}
	f := &c.finds
	f.mu.Lock()
	c.loadFindVectors(e.Model)
	vecs := make([][][]float32, len(docs))
	current := map[string]bool{}
	var missing []string
	for i, d := range docs {
		for _, part := range d.parts {
			k := findKey(part)
			current[k] = true
			if v, ok := f.vecs[k]; ok {
				vecs[i] = append(vecs[i], v)
			} else {
				missing = append(missing, part)
			}
		}
	}
	// What no session holds now, such as an agent's answer before its
	// last turn, goes the next time the file is kept.
	maps.DeleteFunc(f.vecs, func(k string, _ []float32) bool { return !current[k] })
	f.mu.Unlock()
	if len(missing) > 0 {
		c.fillFind(e, missing)
	}
	ctx, cancel := context.WithTimeout(context.Background(), findWait)
	defer cancel()
	vs, err := c.embed(ctx, e, []string{findQuery(e.Model, q)}, ledger.Find)
	if err != nil {
		return nil, 0, fmt.Sprintf("%s at %s did not answer: %v", e.Model, e.URL, err)
	}
	qv := route.Unit(vs[0])
	cos := make([]float64, len(docs))
	reading := 0
	for i, d := range docs {
		cos[i] = unread
		for _, v := range vecs[i] {
			if len(v) == len(qv) {
				cos[i] = max(cos[i], dot(qv, v))
			}
		}
		if len(vecs[i]) < len(d.parts) {
			reading++
		}
	}
	return cos, reading, ""
}

// dot is two vectors' dot product: their cosine, when both are unit
// length.
func dot(a, b []float32) float64 {
	s := 0.0
	for i, x := range a {
		s += float64(x) * float64(b[i])
	}
	return s
}

// loadFindVectors reads find-vectors.json, once for each model. The caller
// holds c.finds.mu.
func (c *Core) loadFindVectors(model string) {
	f := &c.finds
	if f.loaded && f.model == model {
		return
	}
	f.loaded, f.model = true, model
	f.vecs = readVectors(c.paths.FindVectors(), model)
	if f.vecs == nil {
		f.vecs = map[string][]float32{}
	}
}

// fillFind has the model read, in the background, a few at a time, the
// sessions' texts it has not read, and keeps what it makes in
// find-vectors.json, so the next find, and a restarted core, need not
// make it again. One fill runs at a time: what a find meanwhile finds
// missing waits for the next. When another call to the server has just
// failed, as a spawn's can while the model loads, the read waits out the
// rest and goes on; when its own call fails, it stops.
func (c *Core) fillFind(e config.RouteEmbed, texts []string) {
	f := &c.finds
	f.mu.Lock()
	if f.filling {
		f.mu.Unlock()
		return
	}
	f.filling = true
	f.mu.Unlock()
	go func() {
		start, read, kept := time.Now(), 0, time.Now()
		keep := func() {
			f.mu.Lock()
			if f.model != e.Model {
				f.mu.Unlock()
				return
			}
			vs := maps.Clone(f.vecs)
			f.mu.Unlock()
			if err := writeVectors(c.paths.FindVectors(), e.Model, vs); err != nil {
				c.log.Warn("find", "err", err)
			}
			kept = time.Now()
		}
		defer func() {
			if read > 0 {
				keep()
				c.log.Info("find", "read", read, "left", len(texts), "took", time.Since(start).Round(time.Second))
			}
			f.mu.Lock()
			f.filling = false
			f.mu.Unlock()
		}()
		for rests := 0; len(texts) > 0; {
			batch := texts[:min(findBatch, len(texts))]
			ctx, cancel := context.WithTimeout(context.Background(), fillWait)
			vs, err := c.embed(ctx, e, batch, ledger.Find)
			cancel()
			if errors.Is(err, errEmbedRests) && rests < findRests {
				rests++
				time.Sleep(time.Until(c.embedRestsUntil()) + 100*time.Millisecond)
				continue
			}
			if err != nil {
				return
			}
			rests = 0
			texts = texts[len(batch):]
			f.mu.Lock()
			if f.model != e.Model {
				f.mu.Unlock()
				return
			}
			for i, t := range batch {
				f.vecs[findKey(t)] = route.Unit(vs[i])
			}
			f.mu.Unlock()
			read += len(batch)
			if time.Since(kept) >= findKeep {
				keep()
			}
		}
	}()
}

// embedRestsUntil is when the embedding server may be called again, after
// a call to it failed (classify.go's embed).
func (c *Core) embedRestsUntil() time.Time {
	l := &c.labels
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rest
}
