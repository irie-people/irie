package extension

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/terms"
)

// ServeWeb serves the side panel as a page of this computer's own, for a
// browser card of Irie's panes, where no extension runs, and any browser
// here: `irie terms serve`. GET gives the panel's files; POST /api takes a
// request as the extension sends one and answers with JSON lines, the
// pieces of an answer as they come, then the answer. It answers only under
// its own loopback address, never another name for it (a page that turns
// its own name into 127.0.0.1 is refused), and only its own pages: a
// request from another site is refused.
func ServeWeb(ctx context.Context, ln net.Listener, o Options) error {
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok || !addr.IP.IsLoopback() {
		return errors.New("the panel's page is served on this computer's loopback address only")
	}
	if o.Log == nil {
		o.Log = discard()
	}
	h := &host{o: o, docs: map[string]*terms.Doc{}}
	srv := &http.Server{Handler: h.web(addr.Port), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		srv.Close()
	}()
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// pageTypes are the panel's files' types.
var pageTypes = map[string]string{
	".html": "text/html; charset=utf-8",
	".js":   "text/javascript; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".json": "application/json",
	".png":  "image/png",
}

// webCSP lets the panel's page run only its own files and ask only Irie.
const webCSP = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'none'"

func (h *host) web(port int) http.Handler {
	p := strconv.Itoa(port)
	hosts := map[string]bool{"127.0.0.1:" + p: true, "localhost:" + p: true, "[::1]:" + p: true}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hosts[strings.ToLower(r.Host)] {
			http.Error(w, "This page answers only as 127.0.0.1:"+p+".", http.StatusMisdirectedRequest)
			return
		}
		hd := w.Header()
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "no-referrer")
		hd.Set("Cache-Control", "no-store")
		hd.Set("Cross-Origin-Resource-Policy", "same-origin")
		hd.Set("Content-Security-Policy", webCSP)
		if r.URL.Path == "/api" {
			h.api(w, r, hosts)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "panel.html"
		}
		b, ok := files()[name]
		if !ok || name == "manifest.json" || name == "background.js" {
			http.NotFound(w, r)
			return
		}
		hd.Set("Content-Type", pageTypes[path.Ext(name)])
		w.Write(b)
	})
}

// api answers one request. The page's own fetches send its origin, or say
// they are same-origin, and JSON, which a form of another site can't send
// without a preflight this never grants.
func (h *host) api(w http.ResponseWriter, r *http.Request, hosts map[string]bool) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	origin := r.Header.Get("Origin")
	own := strings.HasPrefix(origin, "http://") && hosts[strings.ToLower(strings.TrimPrefix(origin, "http://"))]
	if !own && !(origin == "" && r.Header.Get("Sec-Fetch-Site") == "same-origin") {
		http.Error(w, "Only the panel's own page asks here.", http.StatusForbidden)
		return
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		http.Error(w, "JSON only", http.StatusUnsupportedMediaType)
		return
	}
	var req Request
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxRequest)).Decode(&req); err != nil {
		http.Error(w, "A request that doesn't read: "+err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	flush, _ := w.(http.Flusher)
	var mu sync.Mutex
	write := func(x Reply) {
		b, err := json.Marshal(x)
		if err != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		w.Write(append(b, '\n'))
		if flush != nil {
			flush.Flush()
		}
	}
	write(h.handle(r.Context(), req, write))
}
