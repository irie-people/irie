// Command winpreview shows the Windows app's page (internal/winapp/web) in
// a browser, against a fake Irie (internal/winapp/fixture), so the page can
// be worked on, and drawn, on any computer:
//
//	go run ./tools/winpreview                 serve it at http://127.0.0.1:PORT/
//	go run ./tools/winpreview -scene setup    as on a Windows without WSL
//	go run ./tools/winpreview -scene old      with an Irie inside Linux too old for the app
//	go run ./tools/winpreview -bridge         be the bridge on standard input and output,
//	                                          for the app's IRIE_APP_BRIDGE (docs/WINDOWS.md §7)
//
// The page talks to the app through WebView2's window.chrome.webview; here
// a small script stands in for it, carrying the same messages over HTTP
// to the app's own backend (internal/winapp), which this program runs
// with the fixture in place of wsl.exe. Open the address in a browser of
// your own, or let tools/winpreview/snap.mjs draw it headless.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/winapp"
	"github.com/irie-people/irie/internal/winapp/fixture"
	"github.com/irie-people/irie/internal/winapp/web"
	"github.com/irie-people/irie/internal/wsl"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "where to serve the page")
	scene := flag.String("scene", "ready", "ready, setup or old")
	bridge := flag.Bool("bridge", false, "speak the bridge's side on standard input and output")
	dir := flag.String("dir", "", "serve the page from this checkout's files, read afresh on each load, rather than the ones built in")
	flag.Parse()
	if *bridge {
		if err := fixture.Serve(os.Stdin, os.Stdout); err != nil && err != io.EOF {
			log.Fatal(err)
		}
		return
	}
	h := newHost()
	b := winapp.New(h, &machine{scene: *scene})
	mux := http.NewServeMux()
	mux.HandleFunc("/preview/shim.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		io.WriteString(w, shim)
	})
	mux.HandleFunc("/preview/send", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		b.Receive(body)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/preview/events", h.events)
	mux.HandleFunc("/preview/did", h.did)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, typ, ok := web.File(r.URL.Path)
		if *dir != "" {
			body, typ, ok = fromDisk(*dir, r.URL.Path)
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		if typ == web.Type("index.html") {
			// The stand-in for window.chrome.webview, before any other script.
			body = bytes.Replace(body, []byte("<script "), []byte("<script src=\"preview/shim.js\"></script>\n<script "), 1)
		}
		w.Header().Set("Content-Type", typ)
		w.Header().Set("Cache-Control", "no-store")
		w.Write(body)
	})
	l, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("http://%s/\n", l.Addr())
	log.Fatal(http.Serve(l, mux))
}

// fromDisk is the page's file at urlPath in the checkout at root, as
// web.File has it built in: the page's own under internal/winapp/web, the
// phone app's shared modules under internal/remote/web/js.
func fromDisk(root, urlPath string) ([]byte, string, bool) {
	name := strings.TrimPrefix(path.Clean("/"+urlPath), "/")
	if name == "" {
		name = "index.html"
	}
	file := filepath.Join(root, "internal", "winapp", "web", filepath.FromSlash(name))
	if rest, ok := strings.CutPrefix(name, "shared/"); ok {
		file = filepath.Join(root, "internal", "remote", "web", "js", filepath.FromSlash(rest))
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, "", false
	}
	return b, web.Type(name), true
}

// shim is window.chrome.webview for a browser: what the page posts goes to
// /preview/send in order, and what the app posts comes back as
// server-sent events.
const shim = `(() => {
  const listeners = [];
  let open = false, queue = [], chain = Promise.resolve();
  const post = (s) => { chain = chain.then(() => fetch('preview/send', { method: 'POST', body: s }).catch(() => {})); };
  const es = new EventSource('preview/events');
  es.onopen = () => { open = true; for (const s of queue) post(s); queue = []; };
  es.onmessage = (e) => { for (const fn of listeners) fn({ data: e.data }); };
  window.chrome = window.chrome || {};
  window.chrome.webview = {
    postMessage: (s) => (open ? post(s) : queue.push(s)),
    addEventListener: (type, fn) => { if (type === 'message') listeners.push(fn); },
    removeEventListener: () => {},
  };
})();
`

// host is the window, for a browser: the page's messages as events, and
// what the page asks of the window kept for /preview/did.
type host struct {
	mu      sync.Mutex
	clients map[chan []byte]bool
	backlog [][]byte
	asked   []winapp.Request
}

func newHost() *host { return &host{clients: map[chan []byte]bool{}} }

func (h *host) Post(msg []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.clients) == 0 {
		h.backlog = append(h.backlog, msg)
		return
	}
	for c := range h.clients {
		select {
		case c <- msg:
		default:
		}
	}
}

func (h *host) Do(r winapp.Request) {
	h.mu.Lock()
	h.asked = append(h.asked, r)
	h.mu.Unlock()
	if r.Q != "look" {
		log.Printf("the page asks the window: %s %s%s", r.Q, r.URL, strings.TrimSpace(" "+r.Title))
	}
}

func (h *host) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no streaming", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	c := make(chan []byte, 4096)
	h.mu.Lock()
	for _, m := range h.backlog {
		c <- m
	}
	h.backlog = nil
	h.clients[c] = true
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.clients, c)
		h.mu.Unlock()
	}()
	io.WriteString(w, ": hello\n\n")
	fl.Flush()
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case m := <-c:
			fmt.Fprintf(w, "data: %s\n\n", m)
			fl.Flush()
		case <-tick.C:
			io.WriteString(w, ": ping\n\n")
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// did lists what the page asked of the window, for a script to check.
func (h *host) did(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	for _, a := range h.asked {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", a.Q, a.URL, a.Title, a.Caption)
	}
}

// machine is a Windows for the preview: set up, or not yet for the setup
// scene, with the fixture for a bridge.
type machine struct {
	mu    sync.Mutex
	scene string
	fixed []string
}

func (m *machine) Look() wsl.State {
	m.mu.Lock()
	defer m.mu.Unlock()
	ready := wsl.State{Build: 26100, WSL: true, Distro: wsl.Distro{Name: "Ubuntu", Version: 2}, HasDistro: true, Installed: true, KeepRunning: true, Shortcut: true, Startup: true}
	if m.scene != "setup" {
		return ready
	}
	// Each fix done brings the next step.
	st := wsl.State{Build: 26100, Distro: wsl.Distro{Name: "Ubuntu"}}
	for _, f := range m.fixed {
		switch f {
		case "wsl":
			st.WSL = true
		case "distro":
			st.HasDistro, st.Distro.Version = true, 2
		case "install":
			st.Installed = true
		case "keep":
			st.KeepRunning = true
		case "shortcut":
			st.Shortcut = true
		case "startup":
			st.Startup = true
		}
	}
	return st
}

func (m *machine) Fix(st wsl.State, step wsl.Step) error {
	time.Sleep(1500 * time.Millisecond)
	m.mu.Lock()
	m.fixed = append(m.fixed, step.ID)
	if step.ID == "install" && m.scene == "old" {
		m.scene = "ready"
	}
	m.mu.Unlock()
	return nil
}

func (m *machine) Bridge(string) (winapp.Process, error) {
	m.mu.Lock()
	old := m.scene == "old"
	m.mu.Unlock()
	if old {
		return &proc{r: strings.NewReader(""), w: io.Discard, code: 2, stderr: "irie: unknown command \"bridge\"\n"}, nil
	}
	upR, upW := io.Pipe()
	downR, downW := io.Pipe()
	p := &proc{r: downR, w: upW, closers: []io.Closer{upW, downR}, done: make(chan struct{})}
	go func() {
		fixture.Serve(upR, downW)
		downW.Close()
		close(p.done)
	}()
	return p, nil
}

type proc struct {
	r       io.Reader
	w       io.Writer
	closers []io.Closer
	done    chan struct{}
	code    int
	stderr  string
}

func (p *proc) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p *proc) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p *proc) Close() error {
	for _, c := range p.closers {
		c.Close()
	}
	return nil
}
func (p *proc) Wait() (int, string) {
	if p.done != nil {
		<-p.done
	}
	return p.code, p.stderr
}
