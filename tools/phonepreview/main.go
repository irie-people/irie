// Command phonepreview shows the phone app (internal/remote/web) in a
// browser against a fake Irie, so the app can be worked on, and drawn, on
// any computer, with no phone, no core and no pairing:
//
//	go run ./tools/phonepreview                          serve it at http://127.0.0.1:7487/
//	go run ./tools/phonepreview -dir internal/remote/web  the checkout's files, read afresh on each load
//
// It runs the real gateway (internal/remote) over plain HTTP, against the
// Windows app's fake core (internal/winapp/fixture), with a conversation and
// a terminal for each session, and the page comes already paired with it.
// tools/phonepreview/snap.mjs draws the address it prints as an iPhone
// shows it, with no window; its ?standalone=1 is the app as opened from
// the Home Screen.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/remote"
	"github.com/irie-people/irie/internal/winapp/fixture"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:7487", "where to serve the app")
	dir := flag.String("dir", "", "serve the app's files from this folder, read afresh on each load, rather than the ones built in")
	flag.Parse()

	tmp, err := os.MkdirTemp("", "phonepreview")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmp)
	sock := filepath.Join(tmp, "core.sock")
	l, err := ipc.Listen(sock)
	if err != nil {
		log.Fatal(err)
	}
	go ipc.Serve(l, newCore())

	opt := remote.Options{Socket: sock, Dir: tmp, Name: "Mac", Version: "1.4.0"}
	if *dir != "" {
		opt.Web = os.DirFS(*dir)
	}
	gw, err := remote.New(opt)
	if err != nil {
		log.Fatal(err)
	}
	store := &remote.Store{Dir: tmp}
	code, err := store.NewCode(time.Hour)
	if err != nil {
		log.Fatal(err)
	}
	token, _, err := store.Pair(code, "Preview")
	if err != nil {
		log.Fatal(err)
	}
	id, err := remote.MachineID(tmp)
	if err != nil {
		log.Fatal(err)
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	origin := "http://" + ln.Addr().String()
	machines, _ := json.Marshal([]map[string]string{{"id": id, "name": "Mac", "url": origin, "token": token}})
	boot := fmt.Sprintf("// The preview pairs the page with its fake machine.\ntry { localStorage.setItem('irie.machines', %q); } catch (e) {}\n", machines)
	app := gw.App()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /preview/boot.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write([]byte(boot))
	})
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			app.ServeHTTP(w, r)
			return
		}
		// The page, with the pairing before the app's own script.
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, r)
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		body := bytes.Replace(rec.Body.Bytes(), []byte(`<script type="module"`), []byte("<script src=\"preview/boot.js\"></script>\n<script type=\"module\""), 1)
		w.Header().Del("Content-Length")
		w.WriteHeader(rec.Code)
		w.Write(body)
	}))
	fmt.Printf("The phone app, against a fake machine: %s/?standalone=1\n", origin)
	log.Fatal(http.Serve(ln, mux))
}

// core is the Windows app's fake core, with more sessions, and with what a
// phone reads of a session besides its row: its conversation and its
// terminal.
type core struct {
	*fixture.Core
	mu    sync.Mutex
	extra []ipc.Session
}

func newCore() *core {
	now := time.Now()
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	c := &core{Core: fixture.NewCore()}
	names := []struct {
		name, state, detail string
		hue                 int
	}{
		{"Grant finding plugin for the tracker", "working", "Reading the grant feeds", 2},
		{"Land the accounts page", "done", "", 5},
		{"Market graph tree traversal and the keyboard", "working", "Editing tree.go", 4},
		{"An interpolator for jot plus", "idle", "", 6},
		{"Dreams: tuning the night run with the bench's numbers from last week", "paused", "Held back while you work", 3},
		{"Written proposals", "needs_you", "Asks: which of the three openings?", 1},
		{"Item generation scaling", "working", "Running the generator", 5},
		{"SEO", "done", "", 2},
	}
	for i, n := range names {
		s := ipc.Session{ID: fmt.Sprintf("x%d", i+1), Name: n.name, State: n.state, Detail: n.detail, Created: ago(time.Duration(i+1) * 37 * time.Minute),
			Repo: "/home/alice/src/shop", Profile: "claude", Hue: n.hue, Order: 100 + i}
		if n.state == "needs_you" {
			s.Reason = "question"
		}
		if n.state == "paused" {
			s.Reason = "gabriel"
		}
		c.extra = append(c.extra, s)
	}
	return c
}

// row is a session's row as it is now: the fixture's, from the snapshot
// its subscription starts with, or one of the extra ones.
func (c *core) row(id string) (ipc.Session, bool) {
	if ch, stop, err := c.Core.Subscribe(nil); err == nil {
		f := <-ch
		stop()
		for _, s := range f.Sessions {
			if s.ID == id {
				return s, true
			}
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, s := range c.extra {
		if s.ID == id {
			return s, true
		}
	}
	return ipc.Session{ID: id, Name: id, State: "working"}, false
}

// Call answers what the fixture can't: hello names every op a phone may
// use, and a session's conversation, screen and keys are its own.
func (c *core) Call(op string, raw json.RawMessage) (any, error) {
	var a struct {
		Session string `json:"session"`
	}
	json.Unmarshal(raw, &a)
	switch op {
	case ipc.OpHello:
		res, err := c.Core.Call(op, raw)
		if h, ok := res.(ipc.HelloResult); ok {
			h.Features = append(append(h.Features, remote.Ops...), remote.Features...)
			return h, err
		}
		return res, err
	case ipc.OpConversation:
		s, _ := c.row(a.Session)
		return talk(s), nil
	case ipc.OpScreen:
		s, _ := c.row(a.Session)
		sc := screen(s)
		lines := make([]string, len(sc.Lines))
		for i, l := range sc.Lines {
			lines[i] = strings.TrimRight(sgr.ReplaceAllString(l, ""), " ")
		}
		return ipc.ScreenResult{Lines: lines, Options: sc.Options}, nil
	case ipc.OpKeys, ipc.OpSend:
		return struct{}{}, nil
	}
	return c.Core.Call(op, raw)
}

// Subscribe adds the extra sessions to the list's snapshot, and a
// session's conversation and terminal to a subscription to it.
func (c *core) Subscribe(raw json.RawMessage) (<-chan ipc.Frame, func(), error) {
	var a ipc.SubscribeArgs
	json.Unmarshal(raw, &a)
	in, stop, err := c.Core.Subscribe(raw)
	if err != nil {
		return nil, nil, err
	}
	out := make(chan ipc.Frame, 256)
	done := make(chan struct{})
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			close(done)
			stop()
		})
	}
	go func() {
		first := true
		for {
			select {
			case f := <-in:
				if f.T == ipc.FrameSnapshot {
					c.mu.Lock()
					f.Sessions = append(f.Sessions, c.extra...)
					c.mu.Unlock()
				}
				select {
				case out <- f:
				case <-done:
					return
				}
				if first && a.Session != "" {
					first = false
					s, _ := c.row(a.Session)
					if a.Conversation {
						cv := talk(s)
						cv.Reset = true
						out <- ipc.Frame{T: ipc.FrameConversation, Conversation: &cv}
					}
					if a.Screen {
						sc := screen(s)
						out <- ipc.Frame{T: ipc.FrameScreen, Screen: &sc}
					}
				}
			case <-done:
				return
			}
		}
	}()
	return out, cancel, nil
}
