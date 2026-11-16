package extension

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/terms"
	"github.com/irie-people/irie/internal/version"
)

// The native messaging protocol (Chrome's and Firefox's alike): each
// message is UTF-8 JSON after its length, four bytes in this computer's
// own byte order. The browser takes at most 1 MB from the host, and gives
// it far more; the host takes at most MaxRequest.

// MaxRequest is the most a message from the extension may hold: Chrome's
// own limit for what it sends a host, 64 MiB.
const MaxRequest = 64 << 20

// maxReply is the most the browser takes in one message from the host.
const maxReply = 1 << 20

// ReadMessage reads one message.
func ReadMessage(r io.Reader) ([]byte, error) {
	var n uint32
	if err := binary.Read(r, binary.NativeEndian, &n); err != nil {
		return nil, err
	}
	if n > MaxRequest {
		return nil, fmt.Errorf("a message of %d bytes, more than the %d a host takes", n, MaxRequest)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return nil, err
	}
	return b, nil
}

// WriteMessage writes one message.
func WriteMessage(w io.Writer, b []byte) error {
	if len(b) > maxReply {
		return fmt.Errorf("a reply of %d bytes, more than the %d a browser takes", len(b), maxReply)
	}
	var head [4]byte
	binary.NativeEndian.PutUint32(head[:], uint32(len(b)))
	if _, err := w.Write(append(head[:], b...)); err != nil {
		return err
	}
	return nil
}

// Request is a message from the extension.
type Request struct {
	ID int    `json:"id"`
	Op string `json:"op"`
	// hello's: the browser the extension runs in, and its version.
	Browser string `json:"browser,omitempty"`
	Version string `json:"version,omitempty"`
	// terms.read's: the page, as the browser shows it, or only its
	// address, which the host then fetches; Want, "privacy", asks for the
	// site's privacy policy rather than its terms.
	URL   string `json:"url,omitempty"`
	Title string `json:"title,omitempty"`
	HTML  string `json:"html,omitempty"`
	Text  string `json:"text,omitempty"`
	Want  string `json:"want,omitempty"`
	// terms.ask's.
	Doc      string `json:"doc,omitempty"`
	Question string `json:"question,omitempty"`
	Model    bool   `json:"model,omitempty"`
}

// Reply is a message to the extension: the answer to a request, or a piece
// of one as it comes (Part), then the whole with Done.
type Reply struct {
	ID    int    `json:"id"`
	Done  bool   `json:"done"`
	Part  string `json:"part,omitempty"`
	Error string `json:"error,omitempty"`
	// Code says what kind of error: off (the plugin is), notfound (no
	// terms found), fetch, gone (a document the host no longer holds),
	// model (no local model), bad (a request it can't read).
	Code   string        `json:"code,omitempty"`
	Hello  *Hello        `json:"hello,omitempty"`
	Doc    *terms.Doc    `json:"doc,omitempty"`
	Ask    []string      `json:"ask,omitempty"`
	Answer *terms.Answer `json:"answer,omitempty"`
	Others []terms.Link  `json:"others,omitempty"`
	Model  string        `json:"model,omitempty"`
}

// Hello is what the host says first: who it is, and what it can do.
type Hello struct {
	Name    string   `json:"name"`
	Irie    string   `json:"irie"`
	Version string   `json:"version"` // the extension's version this build writes
	On      bool     `json:"on"`
	Why     string   `json:"why,omitempty"`
	Tools   []string `json:"tools"`
	// Updated says the host wrote this build's extension into its
	// folder, which the browser takes when the extension reloads.
	Updated bool `json:"updated,omitempty"`
}

// Options are what the host needs from Irie.
type Options struct {
	Paths paths.Paths
	// Load reads the config, which may change while the host runs.
	Load func() (config.Config, error)
	// Why is what a part of the plugin says when it can't run, or "" when
	// it can (plugins.Why).
	Why func(cfg config.Config, part string) string
	// Servers are the Ollama servers a local model may word answers on.
	Servers func(cfg config.Config) []string
	// Fetcher fetches pages; the zero value is the real one.
	Fetcher terms.Fetcher
	Log     *slog.Logger
}

// host is one extension's connection: the documents it has read.
type host struct {
	o     Options
	out   io.Writer
	wmu   sync.Mutex
	mu    sync.Mutex
	docs  map[string]*terms.Doc
	order []string
	model *terms.Model
}

// maxDocs is how many documents a host keeps for questions.
const maxDocs = 8

// Serve answers the extension's requests from in until it closes, writing
// the replies to out.
func Serve(ctx context.Context, in io.Reader, out io.Writer, o Options) error {
	if o.Log == nil {
		o.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	h := &host{o: o, out: out, docs: map[string]*terms.Doc{}}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		b, err := ReadMessage(in)
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			// The browser closed the port: the panel went.
			cancel()
			return nil
		}
		if err != nil {
			return err
		}
		var r Request
		if err := json.Unmarshal(b, &r); err != nil {
			h.send(Reply{Done: true, Code: "bad", Error: "a request that isn't JSON: " + err.Error()})
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if p := recover(); p != nil {
					o.Log.Error("panic", "op", r.Op, "panic", p)
					h.send(Reply{ID: r.ID, Done: true, Code: "bad", Error: fmt.Sprintf("%s's host failed: %v", brand.Name, p)})
				}
			}()
			h.send(h.handle(ctx, r))
		}()
	}
}

func (h *host) send(r Reply) {
	b, err := json.Marshal(r)
	if err == nil && len(b) > maxReply {
		// Too big for the browser: a document of thousands of points
		// can't be, but say so rather than fail without a word.
		b, err = json.Marshal(Reply{ID: r.ID, Done: true, Code: "bad", Error: "the answer is too big to show"})
	}
	if err != nil {
		h.o.Log.Error("reply", "err", err)
		return
	}
	h.wmu.Lock()
	defer h.wmu.Unlock()
	if err := WriteMessage(h.out, b); err != nil {
		h.o.Log.Error("write", "err", err)
	}
}

func (h *host) handle(ctx context.Context, r Request) Reply {
	cfg, err := h.o.Load()
	if err != nil {
		return Reply{ID: r.ID, Done: true, Code: "bad", Error: err.Error()}
	}
	why := h.o.Why(cfg, "terms")
	if r.Op == "hello" {
		return Reply{ID: r.ID, Done: true, Hello: h.hello(r, why)}
	}
	if why != "" {
		return Reply{ID: r.ID, Done: true, Code: "off", Error: why}
	}
	switch r.Op {
	case "model":
		m, err := h.findModel(ctx, cfg)
		if err != nil {
			return Reply{ID: r.ID, Done: true, Code: "model", Error: err.Error()}
		}
		return Reply{ID: r.ID, Done: true, Model: m.Name}
	case "terms.read":
		return h.read(ctx, r)
	case "terms.ask":
		return h.ask(ctx, cfg, r)
	}
	return Reply{ID: r.ID, Done: true, Code: "bad", Error: fmt.Sprintf("no op %q", r.Op)}
}

// hello notes the browser that connected, and writes this build's
// extension into its folder when the one there is older.
func (h *host) hello(r Request, why string) *Hello {
	x := &Hello{Name: brand.Name, Irie: version.String(), Version: Version(), On: why == "", Why: why, Tools: []string{"terms"}}
	folder := Folder(h.o.Paths)
	if _, err := os.Stat(filepath.Join(folder, "manifest.json")); err == nil && r.Version != Version() {
		if changed, err := Write(folder); err != nil {
			h.o.Log.Warn("write", "err", err)
		} else {
			x.Updated = changed || r.Version != Version()
		}
	}
	h.seen(r.Browser)
	return x
}

// seen notes when a browser's extension last talked to Irie, for the
// plugin's check, which reads only files.
func (h *host) seen(browser string) {
	name := strings.TrimSpace(browser)
	if name == "" || len(name) > 40 {
		name = "a browser"
	}
	p := seenFile(h.o.Paths)
	seen := map[string]time.Time{}
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &seen)
	}
	seen[name] = time.Now().UTC().Truncate(time.Second)
	// A few browsers at most: drop the oldest past that.
	for len(seen) > 8 {
		oldest := ""
		for k, t := range seen {
			if oldest == "" || t.Before(seen[oldest]) {
				oldest = k
			}
		}
		delete(seen, oldest)
	}
	b, _ := json.MarshalIndent(seen, "", "  ")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err == nil {
		_ = filetx.WriteFile(p, b, 0o600)
	}
}

func (h *host) read(ctx context.Context, r Request) Reply {
	f := h.o.Fetcher
	f.Local = false
	var page *terms.Page
	switch {
	case r.HTML != "":
		page = &terms.Page{URL: r.URL, Body: []byte(r.HTML), HTML: true, Title: r.Title}
	case r.Text != "":
		page = &terms.Page{URL: r.URL, Body: []byte(r.Text), Title: r.Title}
	case r.URL == "":
		return Reply{ID: r.ID, Done: true, Code: "bad", Error: "nothing to read: no page and no address"}
	}
	want := ""
	if r.Want == terms.KindPrivacy {
		want = terms.KindPrivacy
	}
	d, err := terms.Locate(ctx, f, r.URL, page, want)
	if err != nil {
		var nf *terms.NotFoundError
		if errors.As(err, &nf) {
			return Reply{ID: r.ID, Done: true, Code: "notfound", Error: "No terms of service found on this page.", Others: nf.Others}
		}
		return Reply{ID: r.ID, Done: true, Code: "fetch", Error: err.Error()}
	}
	h.keep(d)
	return Reply{ID: r.ID, Done: true, Doc: d, Ask: d.Suggestions()}
}

func (h *host) keep(d *terms.Doc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.docs[d.ID]; !ok {
		h.order = append(h.order, d.ID)
	}
	h.docs[d.ID] = d
	for len(h.order) > maxDocs {
		delete(h.docs, h.order[0])
		h.order = h.order[1:]
	}
}

func (h *host) ask(ctx context.Context, cfg config.Config, r Request) Reply {
	h.mu.Lock()
	d := h.docs[r.Doc]
	h.mu.Unlock()
	if d == nil {
		return Reply{ID: r.ID, Done: true, Code: "gone", Error: "These terms need reading again."}
	}
	if !r.Model {
		a := d.Ask(r.Question)
		return Reply{ID: r.ID, Done: true, Answer: &a}
	}
	m, err := h.findModel(ctx, cfg)
	if err != nil {
		a := d.Ask(r.Question)
		return Reply{ID: r.ID, Done: true, Answer: &a, Code: "model", Error: err.Error()}
	}
	a, err := d.AskModel(ctx, m, r.Question, func(s string) { h.send(Reply{ID: r.ID, Part: s}) })
	if err != nil {
		h.mu.Lock()
		h.model = nil
		h.mu.Unlock()
		return Reply{ID: r.ID, Done: true, Answer: &a, Code: "model", Error: m.Name + " couldn't answer, so " + brand.Name + "'s code did: " + err.Error()}
	}
	return Reply{ID: r.ID, Done: true, Answer: &a}
}

// findModel finds the local model that words answers, once a connection.
func (h *host) findModel(ctx context.Context, cfg config.Config) (terms.Model, error) {
	h.mu.Lock()
	if h.model != nil {
		m := *h.model
		h.mu.Unlock()
		return m, nil
	}
	h.mu.Unlock()
	m, err := terms.FindModel(ctx, h.o.Servers(cfg), cfg.Extension.Model)
	if err != nil {
		return terms.Model{}, err
	}
	h.mu.Lock()
	h.model = &m
	h.mu.Unlock()
	return m, nil
}

// Seen are the browsers that last talked to Irie, newest first, as a
// sentence's words: "Chrome 2 minutes ago".
func Seen(s Status, now time.Time) string {
	if len(s.Seen) == 0 {
		return ""
	}
	names := slicesSortedByTime(s.Seen)
	var parts []string
	for _, n := range names {
		parts = append(parts, n+" "+ago(now.Sub(s.Seen[n])))
	}
	return strings.Join(parts, ", ")
}

func slicesSortedByTime(m map[string]time.Time) []string {
	names := make([]string, 0, len(m))
	for n := range maps.Keys(m) {
		names = append(names, n)
	}
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && m[names[j]].After(m[names[j-1]]); j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	return names
}

// ago says how long ago d was, roughly.
func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		n := int(d / time.Minute)
		return fmt.Sprintf("%d minute%s ago", n, plural(n))
	case d < 48*time.Hour:
		n := int(d / time.Hour)
		return fmt.Sprintf("%d hour%s ago", n, plural(n))
	}
	n := int(d / (24 * time.Hour))
	return fmt.Sprintf("%d days ago", n)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
