package browser

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"maps"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/mcp"
)

// The shared server (DESIGN §4.15): `irie browser serve`, one browser
// server for every Claude Code session, where each session once ran an
// `irie browser` of its own. It speaks MCP's Streamable HTTP on 127.0.0.1,
// and each session's requests carry a token of its own (Token), which
// names the session: its browser, its profile in its own folder and its
// tabs are its alone, as they were. It starts a session's browser on the
// session's first call that needs one, holds it stopped while the session
// is frozen, and closes it after browser.idle without a call or once the
// session's agent has exited. It runs in a window of its own, so a restart
// of the core leaves it and its browsers running.

// Endpoint is the shared server's MCP endpoint.
const Endpoint = "/mcp"

// CallTimeout is the most one tool call of the shared server's may take,
// in milliseconds, which each session's config gives Claude Code: it stops
// waiting on an HTTP server's call that sends nothing for five minutes,
// where a stdio server's gets thirty, and a call that starts a browser on
// a busy machine may wait five minutes for it (startTimeout) before it
// loads its page.
const CallTimeout = 10 * 60 * 1000

// FirstPort is the first port the shared server tries, below the range
// systems hand to outgoing connections, so the port it had is still free
// when it starts again: the sessions it serves have it in their configs.
const FirstPort = 7440

// Files in the shared server's folder.
const (
	KeyFile    = "key"         // the key of its tokens, 0600
	StatusFile = "server.json" // where it listens, and the browsers it runs
)

// SharedOptions configure the shared server.
type SharedOptions struct {
	Options         // every session's browser's, but Profile: each session's is in its own folder
	Sessions string // the folder of the sessions' folders
	Dir      string // the server's own folder, of its key and its status
	Port     int    // its port on 127.0.0.1: 0 = the port it had, else the first free from FirstPort; -1 = any
}

// Status is the shared server's status file, which it writes as it starts
// and as a browser starts or closes: where it listens, and the browsers it
// runs, which the server after it stops if this one was killed before it
// could close them.
type Status struct {
	PID     int      `json:"pid"`
	Addr    string   `json:"addr"` // 127.0.0.1:port
	Version string   `json:"version,omitempty"`
	Open    []Opened `json:"open,omitempty"`
}

// Opened is a browser the shared server runs.
type Opened struct {
	Session string `json:"session"`
	PID     int    `json:"pid"`
	Profile string `json:"profile"`
}

// ReadStatus reads the shared server's status in dir.
func ReadStatus(dir string) (Status, error) {
	var st Status
	b, err := os.ReadFile(filepath.Join(dir, StatusFile))
	if err != nil {
		return st, err
	}
	err = json.Unmarshal(b, &st)
	return st, err
}

// LoadKey returns the key in dir, and makes one when there is none. The
// core makes each session's token with it and the shared server checks
// them, so neither tells the other of a session.
func LoadKey(dir string) ([]byte, error) {
	path := filepath.Join(dir, KeyFile)
	for range 3 {
		b, err := os.ReadFile(path)
		if err == nil {
			key, err := hex.DecodeString(strings.TrimSpace(string(b)))
			if err != nil || len(key) < 32 {
				return nil, fmt.Errorf("%s holds no key", path)
			}
			return key, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		// Written whole beside it, then linked into place, which fails
		// when another made it first: then theirs is read.
		tmp := path + ".new-" + randHex()
		if err := os.WriteFile(tmp, []byte(hex.EncodeToString(key)+"\n"), 0o600); err != nil {
			return nil, err
		}
		err = os.Link(tmp, path)
		os.Remove(tmp)
		if err == nil {
			return key, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("%s could not be read or made", path)
}

// Token is the bearer token of session id's requests: the id and an
// HMAC-SHA256 of it under key, which only a holder of the key can make, so
// one session's token reaches no other session's browser.
func Token(key []byte, id string) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(id))
	return id + "." + hex.EncodeToString(m.Sum(nil))
}

// sessionOf is the session a token names, when it is one of key's.
func sessionOf(key []byte, token string) (string, bool) {
	id, _, ok := strings.Cut(token, ".")
	if !ok || !sessionID(id) {
		return "", false
	}
	return id, hmac.Equal([]byte(token), []byte(Token(key, id)))
}

// sessionID reports whether id can be a session's id, which names its
// folder: letters, digits, - and _.
func sessionID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// Shared is the shared server's state: a browser for each session that
// called it.
type Shared struct {
	so    SharedOptions
	key   []byte
	addr  string
	dirty chan struct{}

	retiring atomic.Bool  // asked to exit once nothing is open (Retire)
	inflight atomic.Int64 // requests being answered
	quit     func()       // ends ServeShared

	mu sync.Mutex
	by map[string]*entry
}

type entry struct {
	b       *Browser
	server  mcp.Server
	checked time.Time   // when the tick last looked at its session
	closing atomic.Bool // its session ended, and its browser is closing
}

// NewShared returns the shared server's state, with its key, before it
// listens.
func NewShared(so SharedOptions) (*Shared, error) {
	if so.Search == "" {
		so.Search = DefaultSearch
	}
	if so.Sessions == "" || so.Dir == "" {
		return nil, errors.New("the shared server needs its sessions' folder and its own")
	}
	key, err := LoadKey(so.Dir)
	if err != nil {
		return nil, err
	}
	return &Shared{so: so, key: key, dirty: make(chan struct{}, 1), by: map[string]*entry{}}, nil
}

// ServeShared runs the shared server until ctx is done, or until it has
// retired, then closes every browser it started. SIGUSR1 retires it
// (Retire). ready, when set, is called once it listens.
func ServeShared(ctx context.Context, so SharedOptions, ready func(*Shared)) error {
	s, err := NewShared(so)
	if err != nil {
		return err
	}
	ctx, quit := context.WithCancel(ctx)
	defer quit()
	s.quit = quit
	usr1 := make(chan os.Signal, 1)
	signal.Notify(usr1, syscall.SIGUSR1)
	defer signal.Stop(usr1)
	go func() {
		select {
		case <-usr1:
			s.Retire()
		case <-ctx.Done():
		}
	}()
	prev, _ := ReadStatus(so.Dir)
	if err := waitGone(ctx, prev.PID); err != nil {
		return err
	}
	ln, err := s.listen(prev)
	if err != nil {
		return err
	}
	s.addr = ln.Addr().String()
	sweep(prev)
	s.save()
	srv := &http.Server{
		Handler:           s.Handler(ctx),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          log.New(discard{}, "", 0),
	}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	if ready != nil {
		ready(s)
	}
	logf(so.Options, "serving every session's browser at http://%s%s", s.addr, Endpoint)
	wctx, stop := context.WithCancel(ctx)
	defer stop()
	go s.watch(wctx)
	select {
	case <-ctx.Done():
	case err = <-served:
	}
	sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	srv.Shutdown(sctx)
	cancel()
	stop()
	s.CloseAll()
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	return err
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

// waitGone waits for the server before, when it is still closing its
// browsers, as after `respawn-pane -k`: two would hold one session's
// profile, and the port.
func waitGone(ctx context.Context, pid int) error {
	if pid <= 1 || pid == os.Getpid() {
		return nil
	}
	for deadline := time.Now().Add(30 * time.Second); syscall.Kill(pid, 0) == nil && sharedServer(pid); {
		if time.Now().After(deadline) {
			return fmt.Errorf("another shared browser server, pid %d, still runs", pid)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return nil
}

// sharedServer reports whether pid is a shared browser server, since a
// pid is reused.
func sharedServer(pid int) bool {
	out, err := psCommand(pid)
	return err == nil && strings.Contains(out, " browser serve")
}

// psCommand is a process's command line; tests replace it.
var psCommand = func(pid int) (string, error) {
	out, err := exec.Command("ps", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
	return string(out), err
}

// listen listens on 127.0.0.1: at so.Port when it is set; else at the
// port of the server before, which its sessions have in their configs,
// waiting a little for it when another holds it for a moment; else at the
// first free port from FirstPort.
func (s *Shared) listen(prev Status) (net.Listener, error) {
	at := func(port int) (net.Listener, error) {
		return net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	}
	switch {
	case s.so.Port > 0:
		return at(s.so.Port)
	case s.so.Port < 0:
		return at(0)
	}
	if _, p, err := net.SplitHostPort(prev.Addr); err == nil {
		if port, err := strconv.Atoi(p); err == nil && port > 0 {
			for range 20 {
				if ln, err := at(port); err == nil {
					return ln, nil
				}
				time.Sleep(250 * time.Millisecond)
			}
			logf(s.so.Options, "port %d is taken: the sessions that had it lose their browser", port)
		}
	}
	for port := FirstPort; port < FirstPort+100; port++ {
		if ln, err := at(port); err == nil {
			return ln, nil
		}
	}
	return at(0)
}

// sweep stops the browsers a server before this one left running, when it
// was killed before it could close them: each by the pid its profile
// holds, and only while that process names the profile (killStale).
func sweep(prev Status) {
	for _, o := range prev.Open {
		if o.Profile != "" {
			killStale(o.Profile)
			os.RemoveAll(o.Profile)
		}
	}
}

// Handler is the shared server's HTTP handler: MCP at Endpoint, each
// call under ctx.
func (s *Shared) Handler(ctx context.Context) http.Handler {
	h := mcp.HTTP{Pick: s.pick, Context: ctx}
	mux := http.NewServeMux()
	mux.HandleFunc(Endpoint, func(w http.ResponseWriter, r *http.Request) {
		s.inflight.Add(1)
		defer s.inflight.Add(-1)
		h.ServeHTTP(w, r)
	})
	return mux
}

// Addr is where the server listens, 127.0.0.1:port.
func (s *Shared) Addr() string { return s.addr }

// Retire has the server exit once no browser of its is open and no call is
// under way: the core asks it to after an install or a change of
// [browser], and starts the new one, so no agent loses its tabs.
func (s *Shared) Retire() { s.retiring.Store(true) }

// idle reports whether no browser of the server's is open.
func (s *Shared) idle() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.by {
		if e.b.pid.Load() != 0 {
			return false
		}
	}
	return true
}

// pick is the server a request reaches: its session's, by its token. A
// request without a good token is refused, and one of a session whose
// folder is gone is not found. A session's server is made on its first
// request; its browser starts only on a call that needs a page.
func (s *Shared) pick(r *http.Request) (mcp.Server, int) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return mcp.Server{}, http.StatusForbidden
	}
	id, ok := sessionOf(s.key, strings.TrimSpace(token))
	if !ok {
		return mcp.Server{}, http.StatusForbidden
	}
	if _, err := os.Stat(filepath.Join(s.so.Sessions, id)); err != nil {
		return mcp.Server{}, http.StatusNotFound
	}
	return s.entry(id).server, 0
}

// entry is session id's browser and server, made on its first request.
func (s *Shared) entry(id string) *entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.by[id]; e != nil {
		return e
	}
	o := s.so.Options
	o.Profile = filepath.Join(s.so.Sessions, id, "browser")
	o.group = true
	b := New(o)
	b.changed = s.touch
	e := &entry{b: b, server: mcp.Server{Name: ServerName, Version: o.Version, Instructions: b.instructions(), Tools: b.tools()}, checked: time.Now()}
	s.by[id] = e
	return e
}

// touch has the status written again, soon.
func (s *Shared) touch() {
	select {
	case s.dirty <- struct{}{}:
	default:
	}
}

// watch writes the status when it changes, and ticks each second.
func (s *Shared) watch(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.dirty:
			s.save()
		case now := <-t.C:
			s.Tick(ctx, now)
		}
	}
}

// Tick follows each session the server has a browser for: a browser whose
// session ended closes, and its profile goes with it; one whose session is
// frozen stops until the thaw; and one idle for browser.idle closes, as a
// session's own server closed it. A session whose browser runs is looked
// at each tick, the rest each minute. An ended session's entry goes once
// its browser has closed and no request is under way, so a call that came
// meanwhile, as from a session woken from rest, waits for the close on the
// same entry and starts afresh, rather than run a second browser on the
// profile.
func (s *Shared) Tick(ctx context.Context, now time.Time) {
	s.mu.Lock()
	all := maps.Clone(s.by)
	s.mu.Unlock()
	for id, e := range all {
		dir := filepath.Join(s.so.Sessions, id)
		running := e.b.pid.Load() != 0
		if running || now.Sub(e.checked) >= time.Minute {
			e.checked = now
			if Ended(dir) {
				switch {
				case running:
					if e.closing.CompareAndSwap(false, true) {
						logf(s.so.Options, "session %s ended: closing its browser", id)
						go func() {
							e.b.Close()
							e.closing.Store(false)
						}()
					}
				case !e.closing.Load():
					s.mu.Lock()
					if s.inflight.Load() == 0 && e.b.pid.Load() == 0 {
						delete(s.by, id)
					}
					s.mu.Unlock()
				}
				continue
			}
		}
		if running {
			e.b.hold(host.Frozen(dir))
		}
		if !e.b.held.Load() {
			e.b.idleTick(ctx)
		}
	}
	if s.retiring.Load() && s.quit != nil && s.inflight.Load() == 0 && s.idle() {
		logf(s.so.Options, "retiring: no browser is open")
		s.quit()
	}
}

// Ended reports whether the session whose folder is dir has ended: its
// folder is gone, or its agent exited since its host last started, by its
// exit file or by its pid.
func Ended(dir string) bool {
	hi, err := os.Stat(filepath.Join(dir, host.InfoFile))
	if err != nil {
		_, err := os.Stat(dir)
		return errors.Is(err, fs.ErrNotExist)
	}
	if ex, err := os.Stat(filepath.Join(dir, host.ExitFile)); err == nil && !ex.ModTime().Before(hi.ModTime()) {
		return true
	}
	info, err := host.ReadInfo(filepath.Join(dir, host.InfoFile))
	return err == nil && info.Agent > 1 && errors.Is(syscall.Kill(info.Agent, 0), syscall.ESRCH)
}

// save writes the status: where the server listens and the browsers it
// runs.
func (s *Shared) save() {
	st := Status{PID: os.Getpid(), Addr: s.addr, Version: s.so.Version}
	s.mu.Lock()
	for _, id := range slices.Sorted(maps.Keys(s.by)) {
		e := s.by[id]
		if pid := e.b.pid.Load(); pid != 0 {
			st.Open = append(st.Open, Opened{Session: id, PID: int(pid), Profile: e.b.o.Profile})
		}
	}
	s.mu.Unlock()
	b, _ := json.MarshalIndent(st, "", "  ")
	if err := filetx.WriteFile(filepath.Join(s.so.Dir, StatusFile), append(b, '\n'), 0o600); err != nil {
		logf(s.so.Options, "writing the status: %v", err)
	}
}

// CloseAll closes every session's browser, each at once.
func (s *Shared) CloseAll() {
	s.mu.Lock()
	all := slices.Collect(maps.Values(s.by))
	s.by = map[string]*entry{}
	s.mu.Unlock()
	var wg sync.WaitGroup
	for _, e := range all {
		wg.Go(e.b.Close)
	}
	wg.Wait()
	s.save()
}
