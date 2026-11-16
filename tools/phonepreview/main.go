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
	"slices"
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
	cc := newCore()
	go ipc.Serve(l, cc)

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
	// As the Mac shows a card: curl -X POST 'http://ADDR/preview/show?session=x4'
	// takes the machine's screen to that card's space, which the phone follows.
	mux.HandleFunc("POST /preview/show", func(w http.ResponseWriter, r *http.Request) {
		if err := cc.show(r.URL.Query().Get("session")); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
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

// core is the Windows app's fake core, with more sessions in two more
// spaces, groups inside groups, and what a phone reads of a session besides
// its row: its conversation and its terminal. Show, which the Mac app sends
// for the card it shows, moves the machine's screen to that card's space.
type core struct {
	*fixture.Core
	mu    sync.Mutex
	extra []ipc.Session
	subs  map[chan ipc.Frame]bool
}

// The spaces: the fixture's sessions are in the first, Shop.
var (
	spaces = []ipc.Space{{ID: "pshop", Name: "Shop"}, {ID: "pgames", Name: "Games"}, {ID: "presearch", Name: "Research"}}
	groups = []ipc.Group{{ID: "gdreams", Name: "Dreams"}, {ID: "gnight", Name: "Night runs", Parent: "gdreams"}}
)

func newCore() *core {
	now := time.Now()
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	c := &core{Core: fixture.NewCore(), subs: map[chan ipc.Frame]bool{}}
	names := []struct {
		name, state, detail, space, group string
		hue                               int
		pinned                            bool
	}{
		{"Grant finding plugin for the tracker", "working", "Reading the grant feeds", "presearch", "", 2, true},
		{"Land the accounts page", "done", "", "", "", 5, false},
		{"Market graph tree traversal and the keyboard", "working", "Editing tree.go", "presearch", "", 4, false},
		{"An interpolator for jot plus", "idle", "", "pgames", "", 6, false},
		{"Dreams: tuning the night run with the bench's numbers from last week", "paused", "Held back while you work", "pgames", "gnight", 3, false},
		{"Written proposals", "needs_you", "Asks: which of the three openings?", "presearch", "", 1, false},
		{"Item generation scaling", "working", "Running the generator", "pgames", "gdreams", 5, false},
		{"SEO", "done", "", "presearch", "", 2, false},
	}
	for i, n := range names {
		s := ipc.Session{ID: fmt.Sprintf("x%d", i+1), Name: n.name, State: n.state, Detail: n.detail, Created: ago(time.Duration(i+1) * 37 * time.Minute),
			Repo: "/home/alice/src/shop", Profile: "claude", Hue: n.hue, Order: 100 + i, Space: n.space, Group: n.group, Pinned: n.pinned}
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

// show marks the session the machine's screen shows, as the Mac app's show
// does, among the fixture's rows and the extra ones alike.
func (c *core) show(id string) error {
	raw, _ := json.Marshal(map[string]string{"session": id})
	if _, err := c.Core.Call(ipc.OpShow, raw); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.extra {
		if was := c.extra[i].Shown; was != (c.extra[i].ID == id) {
			c.extra[i].Shown = !was
			s := c.extra[i]
			for ch := range c.subs {
				select {
				case ch <- ipc.Frame{T: ipc.FrameUpsert, Session: &s}:
				default:
				}
			}
		}
	}
	return nil
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
	case ipc.OpInput:
		// A task starts in the space the client shows, as the core's does.
		var in ipc.InputArgs
		json.Unmarshal(raw, &in)
		t := strings.TrimSpace(in.Text)
		if t == "" || strings.HasPrefix(t, ":") || strings.HasPrefix(strings.ToLower(t), "how") {
			return c.Core.Call(op, raw)
		}
		name, _, _ := strings.Cut(t, "\n")
		c.mu.Lock()
		defer c.mu.Unlock()
		s := ipc.Session{ID: fmt.Sprintf("x%d", len(c.extra)+1), Name: name, State: "starting", Created: time.Now(), Order: 100 + len(c.extra),
			Profile: "claude", Hue: len(c.extra)%6 + 1, Space: in.Space}
		c.extra = append(c.extra, s)
		for ch := range c.subs {
			select {
			case ch <- ipc.Frame{T: ipc.FrameUpsert, Session: &s}:
			default:
			}
		}
		return ipc.InputResult{Did: "spawned", Op: ipc.OpSpawn, Session: &s, Text: "Started “" + s.Name + "”."}, nil
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
	c.mu.Lock()
	c.subs[out] = true
	c.mu.Unlock()
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			c.mu.Lock()
			delete(c.subs, out)
			c.mu.Unlock()
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
					if f.Header != nil {
						h := *f.Header
						h.Spaces = spaces
						h.Groups = append(slices.Clone(h.Groups), groups...)
						f.Header = &h
					}
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
