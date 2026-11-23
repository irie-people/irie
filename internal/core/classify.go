package core

// A task's class from the tasks given one before (DESIGN §6.4). Irie's
// words get a task's class right about half the time. The class of the
// labelled task nearest it, by a small embedding model, was right about
// seven times in ten on 153 of a user's tasks: as often as Haiku, and more
// often than any local chat model measured. When the words call a task
// hard, that stands: calling hard work easier is the costly mistake, and on
// the same tasks letting it stand was right on 72.5% and made that mistake
// 11 times, where the nearest alone made it 20. The model runs on the
// user's own Ollama (route.embed.url), since Irie's own server holds one
// model, which an embedding would push out. With too few labelled tasks, no
// server, or a task unlike any of them, the words decide.

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/ledger"
	"github.com/irie-people/irie/internal/localmodel"
	"github.com/irie-people/irie/internal/route"
)

const (
	minLabels  = 30              // fewer embedded labelled tasks than this, or a class with none, and the words decide
	embedWait  = 3 * time.Second // the longest a spawn waits for its task's embedding
	embedRest  = time.Minute     // after a failed call, how long the server is left alone
	embedKeep  = "30m"           // how long the server keeps the model loaded after a call
	embedBatch = 32              // labelled tasks embedded in one call
	fillWait   = time.Minute     // the longest one such call may take
)

// errEmbedRests is a call not made, since one failed not long ago.
var errEmbedRests = errors.New("the embedding server failed a minute ago")

// routeLabels are the tasks given a class, and their embeddings by one
// model.
type routeLabels struct {
	mu      sync.Mutex
	loaded  bool
	model   string           // whose embeddings list holds
	list    []route.Labelled // in the order given; a task given again takes its new class
	index   map[string]int   // list's places, by the task's fingerprint
	rest    time.Time        // no call to the server before this
	filling bool             // a fill runs
}

// loadLabels reads the labelled tasks, and their embeddings by model, once
// for each model. The caller holds c.labels.mu.
func (c *Core) loadLabels(model string) {
	l := &c.labels
	if l.loaded && l.model == model {
		return
	}
	l.loaded, l.model, l.list, l.index = true, model, nil, map[string]int{}
	f, err := os.Open(c.paths.RouteLabels())
	if err != nil {
		if !os.IsNotExist(err) {
			c.log.Warn("labels", "err", err)
		}
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var lb ipc.Label
		if json.Unmarshal(sc.Bytes(), &lb) == nil && checkLabel(lb) == nil {
			l.put(lb)
		}
	}
	if model == "" {
		return
	}
	vs := readVectors(c.paths.RouteVectors(), model)
	for i := range l.list {
		if v, ok := vs[route.Fingerprint(l.list[i].Text)]; ok {
			l.list[i].Vec = v
		}
	}
}

// checkLabel says what is wrong with a labelled task, if anything.
func checkLabel(lb ipc.Label) error {
	switch {
	case !slices.Contains(route.Classes, lb.Class):
		return ipc.Errorf(ipc.CodeBadArgs, "a class is %s, not %q", strings.Join(route.Classes, ", "), lb.Class)
	case strings.TrimSpace(lb.Text) == "":
		return ipc.Errorf(ipc.CodeBadArgs, "a labelled task needs its words")
	}
	return nil
}

// changes reports whether lb is new, or gives its task another class.
func (l *routeLabels) changes(lb ipc.Label) bool {
	i, ok := l.index[route.Fingerprint(lb.Text)]
	return !ok || l.list[i].Class != lb.Class
}

// put keeps lb; a task given again keeps its embedding and takes the new
// class.
func (l *routeLabels) put(lb ipc.Label) {
	k := route.Fingerprint(lb.Text)
	if i, ok := l.index[k]; ok {
		l.list[i].Class = lb.Class
		return
	}
	l.index[k] = len(l.list)
	l.list = append(l.list, route.Labelled{Text: lb.Text, Class: lb.Class})
}

// ready returns the labelled tasks with embeddings when there are enough to
// sort tasks by, else why not.
func (l *routeLabels) ready() ([]route.Labelled, string) {
	var out []route.Labelled
	classes := map[string]bool{}
	for _, x := range l.list {
		if x.Vec != nil {
			out = append(out, x)
			classes[x.Class] = true
		}
	}
	switch {
	case len(out) < minLabels:
		return nil, fmt.Sprintf("it sorts by them once %d have embeddings", minLabels)
	case len(classes) < len(route.Classes):
		return nil, "it sorts by them once every class has a labelled task"
	}
	return out, ""
}

// unembedded counts the labelled tasks without an embedding.
func (l *routeLabels) unembedded() int {
	n := 0
	for _, x := range l.list {
		if x.Vec == nil {
			n++
		}
	}
	return n
}

// classify gives a task its class, and why: hard when its words say so,
// else that of the nearest labelled task when there are enough of them and
// one is near enough, else the words'. The caller holds no lock: it may
// wait embedWait for the embedding.
func (c *Core) classify(task string) (string, string) {
	class, why := route.Classify(task, c.cfg().Route.Words)
	if class == route.Hard {
		return class, why
	}
	if near, nwhy, ok := c.nearestClass(task); ok {
		return near, nwhy
	}
	return class, why
}

func (c *Core) nearestClass(task string) (string, string, bool) {
	e := c.cfg().Route.Embed
	if e.Model == "" {
		return "", "", false
	}
	l := &c.labels
	l.mu.Lock()
	c.loadLabels(e.Model)
	ls, _ := l.ready()
	missing := l.unembedded()
	l.mu.Unlock()
	if missing > 0 {
		c.fillLabels()
	}
	if ls == nil {
		return "", "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), embedWait)
	defer cancel()
	vs, err := c.embed(ctx, e, []string{task}, ledger.Route)
	if err != nil {
		return "", "", false
	}
	best, sim, ok := route.Nearest(route.Unit(vs[0]), ls)
	if !ok || sim < e.Near {
		return "", "", false
	}
	return best.Class, fmt.Sprintf("like “%s”, %.2f", gist(best.Text), sim), true
}

// gist is a task's words on one line, cut to fit a why.
func gist(text string) string {
	s := strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(s) <= 48 {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:47])) + "…"
}

// embed embeds texts on route.embed's server, for purpose (ledger.Route or
// ledger.Find), unless a call to it failed in the last embedRest. Each call
// goes into the ledger as one of Irie's own.
func (c *Core) embed(ctx context.Context, e config.RouteEmbed, texts []string, purpose string) ([][]float64, error) {
	l := &c.labels
	l.mu.Lock()
	rest := l.rest
	l.mu.Unlock()
	if time.Now().Before(rest) {
		return nil, errEmbedRests
	}
	vs, err := localmodel.Ollama{URL: e.URL, HTTP: c.http}.Embed(c.metered(ctx, purpose), e.Model, texts, embedKeep)
	if err != nil {
		l.mu.Lock()
		l.rest = time.Now().Add(embedRest)
		l.mu.Unlock()
		c.log.Info("embedding", "url", e.URL, "model", e.Model, "err", err)
	}
	return vs, err
}

// fillLabels embeds, in the background, the labelled tasks that have no
// embedding yet, and keeps what it makes, so a restarted core need not
// embed them again. One fill runs at a time.
func (c *Core) fillLabels() {
	l := &c.labels
	l.mu.Lock()
	if l.filling {
		l.mu.Unlock()
		return
	}
	l.filling = true
	l.mu.Unlock()
	go func() {
		defer func() {
			l.mu.Lock()
			l.filling = false
			l.mu.Unlock()
		}()
		e := c.cfg().Route.Embed
		for {
			l.mu.Lock()
			if l.model != e.Model {
				l.mu.Unlock()
				return
			}
			var texts []string
			for _, x := range l.list {
				if x.Vec == nil && len(texts) < embedBatch {
					texts = append(texts, x.Text)
				}
			}
			l.mu.Unlock()
			if len(texts) == 0 {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), fillWait)
			vs, err := c.embed(ctx, e, texts, ledger.Route)
			cancel()
			if err != nil {
				return
			}
			l.mu.Lock()
			if l.model == e.Model {
				for i, t := range texts {
					if j, ok := l.index[route.Fingerprint(t)]; ok {
						l.list[j].Vec = route.Unit(vs[i])
					}
				}
				if err := writeVectors(c.paths.RouteVectors(), l.model, l.vectors()); err != nil {
					c.log.Warn("labels", "err", err)
				}
			}
			l.mu.Unlock()
		}
	}()
}

// label gives tasks their classes (irie ctl route --label and --import),
// which the router sorts new tasks by.
func (c *Core) label(a ipc.LabelArgs) (ipc.LabelResult, error) {
	for _, lb := range a.Labels {
		if err := checkLabel(lb); err != nil {
			return ipc.LabelResult{}, err
		}
	}
	e := c.cfg().Route.Embed
	l := &c.labels
	l.mu.Lock()
	c.loadLabels(e.Model)
	// A task given twice in one call takes its last class.
	last := map[string]ipc.Label{}
	var order []string
	for _, lb := range a.Labels {
		lb.Text = strings.TrimSpace(lb.Text)
		k := route.Fingerprint(lb.Text)
		if _, ok := last[k]; !ok {
			order = append(order, k)
		}
		last[k] = lb
	}
	var add []ipc.Label
	for _, k := range order {
		if lb := last[k]; l.changes(lb) {
			add = append(add, lb)
		}
	}
	if err := appendLabels(c.paths.RouteLabels(), add); err != nil {
		l.mu.Unlock()
		return ipc.LabelResult{}, err
	}
	for _, lb := range add {
		l.put(lb)
	}
	res := ipc.LabelResult{Added: len(add), Labels: len(l.list), Model: e.Model}
	res.Embedded = len(l.list) - l.unembedded()
	ls, note := l.ready()
	res.Sorting = ls != nil
	if e.Model == "" {
		res.Embedded, res.Sorting, note = 0, false, "route.embed.model is empty, so the words give every class"
	}
	if !res.Sorting {
		res.Note = note
	}
	missing := l.unembedded()
	l.mu.Unlock()
	if e.Model != "" && missing > 0 {
		c.fillLabels()
	}
	return res, nil
}

// appendLabels adds labelled tasks to the file, one JSON object a line; a
// task given again later wins when it is read back.
func appendLabels(path string, ls []ipc.Label) error {
	if len(ls) == 0 {
		return nil
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	for _, lb := range ls {
		if err := enc.Encode(lb); err != nil {
			f.Close()
			return err
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// vectorFile keeps embeddings by one model, by their texts' keys, as the
// labelled tasks' fingerprints: little-endian float32s, in base64.
type vectorFile struct {
	Model   string            `json:"model"`
	Vectors map[string]string `json:"vectors"`
}

// readVectors reads the embeddings model made, if the file has them.
func readVectors(path, model string) map[string][]float32 {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var f vectorFile
	if json.Unmarshal(b, &f) != nil || f.Model != model {
		return nil
	}
	out := make(map[string][]float32, len(f.Vectors))
	for k, s := range f.Vectors {
		raw, err := base64.StdEncoding.DecodeString(s)
		if err != nil || len(raw)%4 != 0 || len(raw) == 0 {
			continue
		}
		v := make([]float32, len(raw)/4)
		for i := range v {
			v[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[4*i:]))
		}
		out[k] = v
	}
	return out
}

// vectors are the labelled tasks' embeddings, by the task's fingerprint.
func (l *routeLabels) vectors() map[string][]float32 {
	out := map[string][]float32{}
	for _, x := range l.list {
		if x.Vec != nil {
			out[route.Fingerprint(x.Text)] = x.Vec
		}
	}
	return out
}

// writeVectors keeps embeddings by model, by their texts' keys.
func writeVectors(path, model string, vs map[string][]float32) error {
	f := vectorFile{Model: model, Vectors: make(map[string]string, len(vs))}
	for k, vec := range vs {
		raw := make([]byte, 4*len(vec))
		for i, v := range vec {
			binary.LittleEndian.PutUint32(raw[4*i:], math.Float32bits(v))
		}
		f.Vectors[k] = base64.StdEncoding.EncodeToString(raw)
	}
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	return filetx.WriteFile(path, b, 0o600)
}
