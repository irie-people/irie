// Package winapp is Irie for Windows' own logic (docs/WINDOWS.md): the
// app's window draws a web page (package winapp/web) in WebView2, and this
// package carries what the page says to Irie inside WSL 2, and what Irie
// says back. Like the Mac app it adds no rules of its own (docs/CORE.md):
// the page sends the core's ops and draws its frames, and the terminal
// is a tmux client of the slot, as the Mac app's is.
//
// The way into WSL is one `irie bridge` (package bridge), run through
// wsl.exe, whose standard input and output carry every stream (package
// mux): a connection to the core for each call and subscription, and the
// terminal. Before that, while the computer lacks WSL, a Linux to run
// in or Irie inside it, the page shows the steps of `irie setup`
// (package wsl), each with its button.
//
// Nothing here needs Windows: cmd/windows-app gives it the window and
// wsl.exe, and the tests give it pipes, a fake core and a private tmux.
package winapp

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/mux"
	"github.com/irie-people/irie/internal/version"
	"github.com/irie-people/irie/internal/wsl"
)

// Host is the window the backend serves: cmd/windows-app's, or a test's.
type Host interface {
	// Post sends a message to the page. It may be called from any
	// goroutine and must not block for long.
	Post(msg []byte)
	// Do runs what the page asked of the window itself: open a link in
	// the browser, colour the title bar, title the window, notify.
	Do(req Request)
}

// Process is a running `irie bridge`: its standard output to read, its
// standard input to write, Close to end it, and Wait for how it ended.
type Process interface {
	io.Reader
	io.Writer
	Close() error
	// Wait waits for the process to exit, and gives its exit code and the
	// end of what it wrote to standard error.
	Wait() (code int, stderr string)
}

// Machine is the computer the app runs on: how far it is set up for Irie
// (package wsl), the fixes for what it lacks, and the bridge.
type Machine interface {
	// Look says how far the computer is set up; it may take seconds.
	Look() wsl.State
	// Fix runs a step's fix: in a console window of its own when it asks
	// the person something, such as Linux's user name and password.
	Fix(st wsl.State, step wsl.Step) error
	// Bridge starts `irie bridge` in the distribution.
	Bridge(distro string) (Process, error)
}

// Request is a message from the page that the window answers itself.
type Request struct {
	Q     string `json:"q"`               // open, look, title, notify, quit, devtools
	URL   string `json:"url,omitempty"`   // open
	Title string `json:"title,omitempty"` // title, notify
	Body  string `json:"body,omitempty"`  // notify
	// look: the title bar's colours, as "#RRGGBB", and whether the page is
	// dark.
	Dark    bool   `json:"dark,omitempty"`
	Caption string `json:"caption,omitempty"`
	Text    string `json:"text,omitempty"`
	Border  string `json:"border,omitempty"`
}

// Link states: how the app stands with Irie inside WSL, which the page
// shows until it is ready.
const (
	LinkLooking    = "looking"    // looking at how far the computer is set up
	LinkSetup      = "setup"      // something the app needs is missing: the setup steps
	LinkConnecting = "connecting" // starting the bridge, or starting it again
	LinkReady      = "ready"      // the core answers
	LinkDown       = "down"       // the bridge runs but the core does not answer, or the bridge fails
	LinkOld        = "old"        // Irie inside Linux is older than the app and has no bridge
)

// Link is the link message: the state, in words, and what came with it.
type Link struct {
	T       string `json:"t"` // "link"
	State   string `json:"state"`
	Text    string `json:"text,omitempty"`
	Distro  string `json:"distro,omitempty"`
	Version string `json:"version,omitempty"` // Irie's version inside Linux
	App     string `json:"app"`               // the app's own version
}

// in is a message from the page.
type in struct {
	Q       string          `json:"q"`
	ID      int64           `json:"id,omitempty"`
	Op      string          `json:"op,omitempty"`
	Args    json.RawMessage `json:"args,omitempty"`
	Timeout int             `json:"timeout,omitempty"` // a call's, in ms
	Cols    int             `json:"cols,omitempty"`
	Rows    int             `json:"rows,omitempty"`
	Data    string          `json:"data,omitempty"` // keys, as text
	Bin     string          `json:"bin,omitempty"`  // keys, as base64 bytes
	Step    string          `json:"step,omitempty"`
}

// Backend carries the page's messages to Irie and back.
type Backend struct {
	host Host
	mach Machine

	mu     sync.Mutex
	link   Link
	state  wsl.State
	m      *mux.Mux
	proc   Process
	ready  chan struct{} // closed while m is up; replaced when it goes
	kick   chan struct{} // starts the next look or connection at once
	subs   map[int64]*sub
	terms  map[int64]*term
	fixing string
	booted bool
}

// New makes a backend for host on mach; Start starts it.
func New(host Host, mach Machine) *Backend {
	return &Backend{
		host:  host,
		mach:  mach,
		link:  Link{T: "link", State: LinkLooking, App: version.String()},
		ready: make(chan struct{}),
		kick:  make(chan struct{}, 1),
		subs:  map[int64]*sub{},
		terms: map[int64]*term{},
	}
}

// Start looks at the computer and keeps the link to Irie up, for as long
// as the app runs.
func (b *Backend) Start() { go b.run() }

func (b *Backend) post(v any) {
	msg, err := json.Marshal(v)
	if err != nil {
		return
	}
	b.host.Post(msg)
}

func (b *Backend) setLink(state, text string) {
	b.mu.Lock()
	b.link.State, b.link.Text = state, text
	l := b.link
	b.mu.Unlock()
	b.post(l)
}

// poke starts the next try at once.
func (b *Backend) poke() {
	select {
	case b.kick <- struct{}{}:
	default:
	}
}

// wait sleeps d, or less when poked.
func (b *Backend) wait(d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-b.kick:
	}
}

// run looks at the computer until it is set up, then keeps a bridge
// running, starting it again whenever it ends.
func (b *Backend) run() {
	delay := time.Second
	for {
		b.setLink(LinkLooking, "Looking at how far this computer is set up…")
		st := b.mach.Look()
		b.mu.Lock()
		b.state = st
		b.link.Distro = st.Distro.Name
		b.mu.Unlock()
		if !wsl.Ready(st) {
			b.postSetup()
			b.setLink(LinkSetup, firstNeed(st))
			// A fix, or the person's retry, pokes; WSL may also be set up
			// from outside the app, so it looks again now and then.
			b.wait(30 * time.Second)
			continue
		}
		b.postSetup()
		state, text, steady := b.connect(st.Distro.Name)
		if steady {
			delay = time.Second
		}
		b.setLink(state, text)
		b.wait(delay)
		if !steady {
			delay = min(delay*2, 30*time.Second)
		}
	}
}

// connect starts the bridge and serves it until it ends. It gives the link
// state and words to show then, and whether it ran for a while, which
// starts the next try sooner.
func (b *Backend) connect(distro string) (state, text string, steady bool) {
	b.setLink(LinkConnecting, "Starting "+brand.Name+" in "+distro+"…")
	p, err := b.mach.Bridge(distro)
	if err != nil {
		return LinkDown, "wsl.exe did not start: " + err.Error(), false
	}
	// wsl.exe may take a while to start Linux the first time; a bridge
	// that says nothing for long is stuck.
	timer := time.AfterFunc(2*time.Minute, func() { p.Close() })
	m, hello, err := mux.Client(p, p, p)
	timer.Stop()
	if err != nil {
		p.Close()
		code, stderr := p.Wait()
		return whyNoBridge(code, stderr, err)
	}
	defer m.Close()
	b.mu.Lock()
	b.link.Version = hello.Version
	b.m, b.proc = m, p
	b.mu.Unlock()
	start := time.Now()
	if hello.Core == "" {
		b.setLink(LinkDown, hello.Problem)
		// The core may yet come up, as after a slow start.
		if !b.awaitCore(m) {
			b.unlink()
			return LinkConnecting, "The connection to " + brand.Name + " in Linux ended. Starting it again…", false
		}
	}
	b.setLink(LinkReady, "")
	b.mu.Lock()
	close(b.ready)
	b.mu.Unlock()
	<-m.Done()
	b.unlink()
	return LinkConnecting, "The connection to " + brand.Name + " in Linux ended. Starting it again…", time.Since(start) > 10*time.Second
}

// unlink forgets the bridge that ended.
func (b *Backend) unlink() {
	b.mu.Lock()
	if b.proc != nil {
		b.proc.Close()
	}
	b.m, b.proc = nil, nil
	select {
	case <-b.ready:
		b.ready = make(chan struct{})
	default:
	}
	b.mu.Unlock()
}

// awaitCore asks the core for hello now and then until it answers, or
// the bridge ends.
func (b *Backend) awaitCore(m *mux.Mux) bool {
	for {
		var r ipc.HelloResult
		if callOn(m, ipc.OpHello, ipc.HelloArgs{Client: "windows", API: ipc.API}, &r, 3*time.Second) == nil {
			return true
		}
		select {
		case <-m.Done():
			return false
		case <-b.kick:
		case <-time.After(5 * time.Second):
		}
	}
}

// whyNoBridge says, in the link's words, why a bridge gave no hello.
func whyNoBridge(code int, stderr string, err error) (state, text string, steady bool) {
	msg := strings.TrimSpace(stderr)
	switch {
	case strings.Contains(msg, `unknown command "bridge"`):
		return LinkOld, brand.Name + " inside Linux is older than this app, and can't talk to it. Update it to go on.", false
	case code == 127 || strings.Contains(msg, "not found"):
		return LinkSetup, brand.Name + " is not installed inside Linux.", false
	case msg != "":
		return LinkDown, lastLine(msg), false
	}
	return LinkDown, brand.Name + " inside Linux did not answer: " + err.Error(), false
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func firstNeed(st wsl.State) string {
	for _, s := range wsl.Plan(st) {
		if s.Blocks {
			return s.Need
		}
	}
	return ""
}

// Setup is the setup message: how far the computer is set up, and each
// step `irie setup` offers, in order.
type Setup struct {
	T      string      `json:"t"` // "setup"
	Ready  bool        `json:"ready"`
	Fixing string      `json:"fixing,omitempty"` // the step whose fix runs now
	Build  int         `json:"build,omitempty"`
	WSL    bool        `json:"wsl"`
	Distro string      `json:"distro,omitempty"`
	Has    bool        `json:"has_distro"`
	Irie   bool        `json:"installed"`
	Steps  []SetupStep `json:"steps"`
	Error  string      `json:"error,omitempty"` // what the last fix said when it failed
}

// SetupStep is one step of the plan.
type SetupStep struct {
	ID      string `json:"id"`
	Need    string `json:"need"`
	Ask     string `json:"ask,omitempty"`
	Blocks  bool   `json:"blocks,omitempty"`
	Restart bool   `json:"restart,omitempty"`
}

func (b *Backend) postSetup() { b.postSetupErr("") }

func (b *Backend) postSetupErr(problem string) {
	b.mu.Lock()
	st, fixing := b.state, b.fixing
	b.mu.Unlock()
	m := Setup{T: "setup", Ready: wsl.Ready(st), Fixing: fixing, Build: st.Build, WSL: st.WSL, Distro: st.Distro.Name,
		Has: st.HasDistro, Irie: st.Installed, Steps: []SetupStep{}, Error: problem}
	for _, s := range wsl.Plan(st) {
		m.Steps = append(m.Steps, SetupStep{ID: s.ID, Need: s.Need, Ask: s.Ask, Blocks: s.Blocks, Restart: s.Restart})
	}
	b.post(m)
}

// fix runs a step's fix, then looks again.
func (b *Backend) fix(id string) {
	b.mu.Lock()
	st := b.state
	if b.fixing != "" {
		b.mu.Unlock()
		return
	}
	var step *wsl.Step
	for _, s := range wsl.Plan(st) {
		if s.ID == id {
			step = &s
			break
		}
	}
	// "install" is also the update of an old Irie inside Linux, which the
	// plan does not list: it is installed, just too old.
	if step == nil && id == "install" {
		step = &wsl.Step{ID: "install"}
	}
	if step == nil {
		b.mu.Unlock()
		b.postSetup()
		return
	}
	b.fixing = id
	b.mu.Unlock()
	b.postSetup()
	go func() {
		err := b.mach.Fix(st, *step)
		st := b.mach.Look()
		b.mu.Lock()
		b.fixing = ""
		b.state = st
		b.mu.Unlock()
		problem := ""
		if err != nil {
			problem = "That did not work: " + err.Error()
		}
		b.postSetupErr(problem)
		b.poke()
	}()
}

// Receive takes a message from the page. The window calls it on its own
// thread, so it never waits on Irie: what takes time runs on a goroutine.
func (b *Backend) Receive(msg []byte) {
	var m in
	if json.Unmarshal(msg, &m) != nil {
		return
	}
	switch m.Q {
	case "hello":
		b.mu.Lock()
		l := b.link
		booted := b.booted
		b.booted = true
		// A page loaded again starts afresh: what the last one followed
		// is no one's now.
		subs, terms := b.subs, b.terms
		b.subs, b.terms = map[int64]*sub{}, map[int64]*term{}
		b.mu.Unlock()
		for _, s := range subs {
			s.stop()
		}
		for _, t := range terms {
			t.close()
		}
		b.post(l)
		b.postSetup()
		if !booted {
			b.Start()
		}
	case "call":
		go b.call(m)
	case "sub":
		b.subscribe(m)
	case "unsub":
		b.mu.Lock()
		s := b.subs[m.ID]
		delete(b.subs, m.ID)
		b.mu.Unlock()
		if s != nil {
			s.stop()
		}
	case "term":
		b.terminal(m)
	case "fix":
		b.fix(m.Step)
	case "retry":
		b.poke()
	default:
		var r Request
		if json.Unmarshal(msg, &r) == nil && r.Q != "" {
			b.host.Do(r)
		}
	}
}

// mux waits up to d for the bridge, and gives it; nil when it is not up
// by then, or done closes first.
func (b *Backend) waitMux(d time.Duration, done <-chan struct{}) *mux.Mux {
	b.mu.Lock()
	ready, m := b.ready, b.m
	b.mu.Unlock()
	select {
	case <-ready:
		return m
	default:
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ready:
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.m
	case <-t.C:
	case <-done:
	}
	return nil
}

// reply is the answer to a call: the core's, as it sent it.
type reply struct {
	T      string          `json:"t"` // "reply"
	ID     int64           `json:"id"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *ipc.Error      `json:"error,omitempty"`
}

func (b *Backend) call(m in) {
	timeout := time.Duration(m.Timeout) * time.Millisecond
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	r := reply{T: "reply", ID: m.ID}
	start := time.Now()
	mx := b.waitMux(min(timeout, 5*time.Second), nil)
	if mx == nil {
		r.Error = &ipc.Error{Code: "core_down", Msg: brand.Name + " is not connected yet."}
		b.post(r)
		return
	}
	res, err := rawCall(mx, m.Op, m.Args, timeout-time.Since(start))
	if err != nil {
		var e *ipc.Error
		if !errors.As(err, &e) {
			e = &ipc.Error{Code: "core_down", Msg: "The core did not answer (" + err.Error() + ")."}
		}
		r.Error = e
	} else {
		r.OK, r.Result = true, res
	}
	b.post(r)
}

// rawCall sends one request on a stream of its own, as ipc.Call does on a
// connection of its own, and gives the result as the core wrote it.
func rawCall(m *mux.Mux, op string, args json.RawMessage, timeout time.Duration) (json.RawMessage, error) {
	if timeout < time.Second {
		timeout = time.Second
	}
	s, err := m.Open(mux.Open{Kind: mux.StreamCore})
	if err != nil {
		return nil, err
	}
	defer s.Close()
	s.SetReadDeadline(time.Now().Add(timeout))
	req, err := json.Marshal(ipc.Request{ID: 1, Op: op, Args: args})
	if err != nil {
		return nil, err
	}
	if _, err := s.Write(append(req, '\n')); err != nil {
		return nil, err
	}
	r := bufio.NewReaderSize(s, 64<<10)
	for {
		line, err := readLine(r)
		if err != nil {
			if e := s.Ended(); e != nil && e.Error != "" {
				return nil, errors.New(e.Error)
			}
			return nil, err
		}
		var rep ipc.Reply
		if json.Unmarshal(line, &rep) != nil || rep.ID != 1 {
			continue
		}
		if !rep.OK {
			if rep.Error == nil {
				return nil, &ipc.Error{Code: ipc.CodeInternal, Msg: op + " failed"}
			}
			return nil, rep.Error
		}
		if len(rep.Result) == 0 {
			return json.RawMessage("{}"), nil
		}
		return rep.Result, nil
	}
}

// callOn is rawCall into a result.
func callOn(m *mux.Mux, op string, args, result any, timeout time.Duration) error {
	b, err := json.Marshal(args)
	if err != nil {
		return err
	}
	res, err := rawCall(m, op, b, timeout)
	if err != nil {
		return err
	}
	if result == nil {
		return nil
	}
	return json.Unmarshal(res, result)
}

// readLine reads one NDJSON line, however long.
func readLine(r *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		chunk, err := r.ReadSlice('\n')
		line = append(line, chunk...)
		switch {
		case err == nil:
			return bytes.TrimRight(line, "\r\n"), nil
		case err != bufio.ErrBufferFull:
			return nil, err
		case len(line) > ipc.MaxMessage:
			return nil, errors.New("a line from the core was too long")
		}
	}
}

// sub is one of the page's subscriptions, which reconnects by itself, as
// the phone app's do.
type sub struct {
	id   int64
	args json.RawMessage
	done chan struct{}
	once sync.Once
}

func (s *sub) stop() { s.once.Do(func() { close(s.done) }) }

// subState is a subscription's state message.
type subState struct {
	T     string     `json:"t"` // "sub"
	ID    int64      `json:"id"`
	State string     `json:"state"` // connecting, connected, refused
	Error *ipc.Error `json:"error,omitempty"`
}

// frame is one of a subscription's frames, as the core wrote it.
type frame struct {
	T  string          `json:"t"` // "frame"
	ID int64           `json:"id"`
	F  json.RawMessage `json:"f"`
}

func (b *Backend) subscribe(m in) {
	s := &sub{id: m.ID, args: m.Args, done: make(chan struct{})}
	b.mu.Lock()
	if old := b.subs[m.ID]; old != nil {
		old.stop()
	}
	b.subs[m.ID] = s
	b.mu.Unlock()
	go b.follow(s)
}

// follow keeps s subscribed: whenever its stream ends, as when the core
// restarts or the bridge does, it subscribes again.
func (b *Backend) follow(s *sub) {
	delay := 500 * time.Millisecond
	for {
		mx := b.waitMux(time.Hour, s.done)
		select {
		case <-s.done:
			return
		default:
		}
		if mx == nil {
			continue
		}
		start := time.Now()
		refused := b.stream(mx, s)
		select {
		case <-s.done:
			return
		default:
		}
		if refused {
			return
		}
		b.post(subState{T: "sub", ID: s.id, State: "connecting"})
		// One that keeps ending at once backs off; one that ran a while
		// comes back at once.
		if time.Since(start) > 3*time.Second {
			delay = 300 * time.Millisecond
		}
		select {
		case <-s.done:
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, 8*time.Second)
	}
}

// stream subscribes once and posts the frames until the stream ends; it
// says whether the core refused the subscription, which no retry mends.
func (b *Backend) stream(mx *mux.Mux, s *sub) (refused bool) {
	st, err := mx.Open(mux.Open{Kind: mux.StreamCore})
	if err != nil {
		return false
	}
	defer st.Close()
	over := make(chan struct{})
	defer close(over)
	go func() {
		select {
		case <-s.done:
			st.Close()
		case <-over:
		}
	}()
	req, _ := json.Marshal(ipc.Request{ID: 1, Op: ipc.OpSubscribe, Args: s.args})
	if _, err := st.Write(append(req, '\n')); err != nil {
		return false
	}
	r := bufio.NewReaderSize(st, 256<<10)
	st.SetReadDeadline(time.Now().Add(30 * time.Second))
	line, err := readLine(r)
	if err != nil {
		return false
	}
	st.SetReadDeadline(time.Time{})
	var rep ipc.Reply
	if json.Unmarshal(line, &rep) != nil {
		return false
	}
	if !rep.OK {
		b.post(subState{T: "sub", ID: s.id, State: "refused", Error: rep.Error})
		return true
	}
	b.post(subState{T: "sub", ID: s.id, State: "connected"})
	for {
		line, err := readLine(r)
		if err != nil {
			return false
		}
		if len(line) == 0 || !json.Valid(line) {
			continue
		}
		b.post(frame{T: "frame", ID: s.id, F: line})
	}
}

// term is the page's terminal: a tmux client of the slot, attached through
// the bridge, attached again whenever it detaches, as the Mac app's is.
type term struct {
	id   int64
	keys chan []byte
	done chan struct{}
	once sync.Once

	mu         sync.Mutex
	cols, rows int
	st         *mux.Stream
}

// close ends the terminal for good: it detaches, and attaches no more.
func (t *term) close() {
	t.once.Do(func() { close(t.done) })
	t.mu.Lock()
	st := t.st
	t.mu.Unlock()
	if st != nil {
		st.Close()
	}
}

// termOut is the terminal's output or state, to the page.
type termOut struct {
	T     string `json:"t"` // "term"
	ID    int64  `json:"id"`
	Out   string `json:"out,omitempty"`   // what tmux drew, as base64
	State string `json:"state,omitempty"` // attached, detached
	Text  string `json:"text,omitempty"`  // why it detached
}

func (b *Backend) terminal(m in) {
	b.mu.Lock()
	t := b.terms[m.ID]
	b.mu.Unlock()
	switch m.Op {
	case "open":
		if t != nil {
			return
		}
		t = &term{id: m.ID, keys: make(chan []byte, 256), done: make(chan struct{}), cols: m.Cols, rows: m.Rows}
		b.mu.Lock()
		b.terms[m.ID] = t
		b.mu.Unlock()
		go b.attach(t)
		go t.typeKeys()
	case "in":
		if t == nil {
			return
		}
		data := []byte(m.Data)
		if m.Bin != "" {
			d, err := base64.StdEncoding.DecodeString(m.Bin)
			if err != nil {
				return
			}
			data = d
		}
		select {
		case t.keys <- data:
		case <-t.done:
		default:
			// The terminal is gone or stuck; a key that can't go is dropped
			// rather than hold the window.
		}
	case "size":
		if t == nil || m.Cols <= 0 || m.Rows <= 0 {
			return
		}
		t.mu.Lock()
		t.cols, t.rows = m.Cols, m.Rows
		st := t.st
		t.mu.Unlock()
		if st != nil {
			go st.Resize(m.Cols, m.Rows)
		}
	case "close":
		if t == nil {
			return
		}
		b.mu.Lock()
		delete(b.terms, m.ID)
		b.mu.Unlock()
		t.close()
	}
}

// typeKeys writes the page's keys to the terminal, in order, off the
// window's thread.
func (t *term) typeKeys() {
	for {
		select {
		case <-t.done:
			return
		case k := <-t.keys:
			t.mu.Lock()
			st := t.st
			t.mu.Unlock()
			if st != nil {
				st.Write(k)
			}
		}
	}
}

// attach keeps t attached to the slot until the page closes it: again a
// second after it detaches, as the Mac app's terminal does, and less
// often while it keeps failing at once.
func (b *Backend) attach(t *term) {
	delay := time.Second
	for {
		mx := b.waitMux(time.Hour, t.done)
		select {
		case <-t.done:
			return
		default:
		}
		if mx == nil {
			continue
		}
		start := time.Now()
		why := b.attachOnce(mx, t)
		select {
		case <-t.done:
			return
		default:
		}
		b.post(termOut{T: "term", ID: t.id, State: "detached", Text: why})
		if time.Since(start) > 5*time.Second {
			delay = time.Second
		}
		select {
		case <-t.done:
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, 15*time.Second)
	}
}

// attachOnce attaches once, as the core's attach-info says, and posts what
// tmux draws until it detaches; it says why it ended.
func (b *Backend) attachOnce(mx *mux.Mux, t *term) string {
	var info ipc.AttachInfoResult
	if err := callOn(mx, ipc.OpAttachInfo, struct{}{}, &info, 10*time.Second); err != nil {
		return "The core did not say where its terminal is: " + err.Error()
	}
	t.mu.Lock()
	cols, rows := t.cols, t.rows
	t.mu.Unlock()
	st, err := mx.Open(mux.Open{Kind: mux.StreamTmux, Tmux: info.Tmux, Socket: info.Socket, Target: info.Target, Cols: cols, Rows: rows})
	if err != nil {
		return err.Error()
	}
	t.mu.Lock()
	t.st = st
	// A size that came while it attached.
	if t.cols != cols || t.rows != rows {
		go st.Resize(t.cols, t.rows)
	}
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		t.st = nil
		t.mu.Unlock()
		st.Close()
	}()
	b.post(termOut{T: "term", ID: t.id, State: "attached"})
	pump(st, func(p []byte) {
		b.post(termOut{T: "term", ID: t.id, Out: base64.StdEncoding.EncodeToString(p)})
	})
	if e := st.Ended(); e != nil {
		switch {
		case e.Error != "":
			return e.Error
		case e.Exit != nil && *e.Exit != 0:
			return fmt.Sprintf("tmux exited with %d", *e.Exit)
		}
	}
	return "The terminal detached."
}

// pump reads r until it ends, handing on what it read in batches: at once
// when the output is quiet, and at most every few milliseconds while a
// program draws a lot, so the page takes one message per frame it draws
// rather than one per read.
func pump(r io.Reader, out func([]byte)) {
	chunks := make(chan []byte, 64)
	go func() {
		defer close(chunks)
		buf := make([]byte, 32<<10)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				chunks <- append([]byte(nil), buf[:n]...)
			}
			if err != nil {
				return
			}
		}
	}()
	const most = 256 << 10
	for c := range chunks {
		batch := c
		deadline := time.After(4 * time.Millisecond)
	more:
		for len(batch) < most {
			select {
			case c, ok := <-chunks:
				if !ok {
					break more
				}
				batch = append(batch, c...)
			case <-deadline:
				break more
			}
		}
		out(batch)
	}
}
