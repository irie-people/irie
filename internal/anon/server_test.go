package anon

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/mcp"
)

// call runs one tool of s and returns its text and whether it failed.
func call(t *testing.T, s *server, name string, args map[string]any) (string, bool) {
	t.Helper()
	raw, _ := json.Marshal(args)
	for _, tl := range s.tools() {
		if tl.Name == name {
			r := mcp.Run(context.Background(), tl, raw)
			return r.Content[0].Text, r.IsError
		}
	}
	t.Fatalf("no tool %s", name)
	return "", true
}

func TestToolsAreWellFormed(t *testing.T) {
	s := &server{c: Compartment{Name: "acme", Route: Mullvad, Country: "Sweden"}}
	seen := map[string]bool{}
	for _, tl := range s.tools() {
		if seen[tl.Name] || tl.Description == "" || tl.Call == nil || tl.InputSchema["type"] != "object" {
			t.Errorf("tool %q is not well formed", tl.Name)
		}
		seen[tl.Name] = true
	}
	in := s.instructions()
	if n := len(in); n > 2000 || !strings.Contains(in, `"acme"`) || !strings.Contains(in, "untrusted") {
		t.Errorf("instructions (%d characters): %s", n, in)
	}
}

func TestSearchReadsSearXNG(t *testing.T) {
	var asked string
	sx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.RawQuery
		io.WriteString(w, `{"query": "acme", "results": [
			{"title": "Acme Corp fined", "url": "https://news.example.com/acme", "content": "Acme   was fined\nfor fraud.", "engines": ["brave", "duckduckgo"], "publishedDate": "2026-10-01"},
			{"title": "Acme", "url": "https://example.com/", "content": "", "engines": ["wikipedia"]}],
			"answers": [{"answer": "Acme is a company."}], "suggestions": ["acme fraud"], "corrections": [],
			"infoboxes": [{"infobox": "Acme", "content": "A company."}],
			"unresponsive_engines": [["google", "CAPTCHA"]]}`)
	}))
	defer sx.Close()
	s := &server{o: ServerOptions{Search: sx.URL}, c: Compartment{Name: "acme", Route: Mullvad}}
	text, isErr := call(t, s, "search", map[string]any{"query": "acme corp", "time_range": "month", "page": 2})
	if isErr {
		t.Fatal(text)
	}
	for _, want := range []string{
		"1. Acme Corp fined\n   https://news.example.com/acme\n   Acme was fined for fraud.\n   (2026-10-01 · brave, duckduckgo)",
		"2. Acme\n   https://example.com/", "Answer: Acme is a company.", "Infobox: Acme: A company.",
		"Related searches: acme fraud", "Engines that did not answer: google (CAPTCHA)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("search lacks %q:\n%s", want, text)
		}
	}
	for _, want := range []string{"format=json", "q=acme+corp", "time_range=month", "pageno=2"} {
		if !strings.Contains(asked, want) {
			t.Errorf("SearXNG was asked %q, without %s", asked, want)
		}
	}
	off := &server{c: Compartment{Name: "acme"}}
	if text, isErr := call(t, off, "search", map[string]any{"query": "x"}); !isErr || !strings.Contains(text, "irie setup anon") {
		t.Errorf("search with no SearXNG: %q", text)
	}
}

func TestFetchReadsAPageThroughTheExit(t *testing.T) {
	page := `<html><head><title>Acme &amp; Co</title><script>var x = "</div>secret";</script></head>
<body><nav>Home</nav><main><h1>Filings</h1><p>Acme filed ` + strings.Repeat("many ", 3000) + `papers.</p>
<a href="/doc.pdf">The filing</a></main></body></html>`
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, page)
	}))
	defer site.Close()
	proxy := newSocks5(t, strings.TrimPrefix(site.URL, "http://"))
	through(t, proxy)
	dir := t.TempDir()
	s := &server{o: ServerOptions{Evidence: filepath.Join(dir, "evidence")}, c: Compartment{Name: "acme", Route: Mullvad}}

	text, isErr := call(t, s, "fetch", map[string]any{"url": "http://acme.example/filings", "save": true})
	if isErr {
		t.Fatal(text)
	}
	if !strings.Contains(text, "# Acme & Co") || !strings.Contains(text, "Filings") || strings.Contains(text, "secret") {
		t.Errorf("fetch read:\n%.400s", text)
	}
	if !strings.Contains(text, "characters left: fetch again with offset") {
		t.Errorf("a long page should say what is left:\n%.300s", text)
	}
	if h := proxy.hosts(); len(h) != 1 || h[0] != "acme.example:80" {
		t.Errorf("the exit was asked for %v: the name should be looked up there", h)
	}
	// Kept as evidence: the body as it came, and what it was.
	files, _ := filepath.Glob(filepath.Join(dir, "evidence", "*"))
	if len(files) != 2 {
		t.Fatalf("evidence = %v", files)
	}
	var meta map[string]any
	for _, f := range files {
		b, _ := os.ReadFile(f)
		if strings.HasSuffix(f, ".html") && string(b) != page {
			t.Error("the evidence is not the response as it came")
		}
		if strings.HasSuffix(f, ".json") {
			json.Unmarshal(b, &meta)
		}
	}
	if meta["url"] != "http://acme.example/filings" || meta["compartment"] != "acme" || len(meta["sha256"].(string)) != 64 || !strings.HasSuffix(meta["fetched_at"].(string), "Z") {
		t.Errorf("evidence meta = %v", meta)
	}
	// Reading on reads the page fetched before: the site sees no more.
	more, isErr := call(t, s, "fetch", map[string]any{"url": "http://acme.example/filings", "offset": 10000})
	if isErr || strings.Contains(more, "# Acme & Co") || !strings.Contains(more, "papers.") {
		t.Errorf("offset read:\n%.300s", more)
	}
	if h := proxy.hosts(); len(h) != 1 {
		t.Errorf("reading on fetched again: %v", h)
	}
	if !strings.Contains(more, "/doc.pdf") {
		t.Error("the page's links should come after its text")
	}
}

func TestFetchRefusesWhatTheExitCannotReach(t *testing.T) {
	s := &server{c: Compartment{Name: "acme", Route: Mullvad}}
	for address, want := range map[string]string{
		"ftp://example.com/":          "not an http or https address",
		"http://localhost:8888/":      "this machine",
		"http://127.0.0.1/":           "private address",
		"http://192.168.1.1/":         "private address",
		"http://[::1]/":               "private address",
		"http://printer.local/":       "this machine",
		"http://exampleonion.onion/x": "Tor compartment",
		"not a url":                   "not an http or https address",
	} {
		if text, isErr := call(t, s, "fetch", map[string]any{"url": address}); !isErr || !strings.Contains(text, want) {
			t.Errorf("fetch %s: %q, want an error saying %q", address, text, want)
		}
	}
}

func TestFetchSaysWhatAFileIs(t *testing.T) {
	f := Fetched{Type: "application/pdf", Body: []byte("%PDF-1.7")}
	if r := f.Read(false); !strings.Contains(r, "application/pdf, not text") {
		t.Errorf("a PDF: %q", r)
	}
	j := Fetched{Type: "application/json", Body: []byte(`{"a": 1}`)}
	if r := j.Read(false); r != `{"a": 1}` {
		t.Errorf("JSON: %q", r)
	}
	if extension("application/pdf") != ".pdf" || extension("text/html; charset=utf-8") != ".html" {
		t.Error("extensions")
	}
}
