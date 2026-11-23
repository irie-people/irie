package winapp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/bridge"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/testutil"
	"github.com/irie-people/irie/internal/wsl"
)

func TestMain(m *testing.M) { testutil.Main(m) }

// page is the window's web page, as the backend sees it: the messages
// posted to it, and a way to wait for one.
type page struct {
	mu   sync.Mutex
	msgs []map[string]any
	did  []Request
	cond *sync.Cond
}

func newPage() *page {
	p := &page{}
	p.cond = sync.NewCond(&p.mu)
	return p
}

func (p *page) Post(msg []byte) {
	var m map[string]any
	if json.Unmarshal(msg, &m) != nil {
		panic("the backend posted what is not JSON: " + string(msg))
	}
	p.mu.Lock()
	p.msgs = append(p.msgs, m)
	p.cond.Broadcast()
	p.mu.Unlock()
}

func (p *page) Do(r Request) {
	p.mu.Lock()
	p.did = append(p.did, r)
	p.mu.Unlock()
}

// await waits for a message that match takes, and gives it.
func (p *page) await(t *testing.T, what string, match func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	timer := time.AfterFunc(20*time.Second, func() {
		p.mu.Lock()
		p.cond.Broadcast()
		p.mu.Unlock()
	})
	defer timer.Stop()
	p.mu.Lock()
	defer p.mu.Unlock()
	seen := 0
	for {
		for ; seen < len(p.msgs); seen++ {
			if match(p.msgs[seen]) {
				m := p.msgs[seen]
				// Each message answers one await; the others wait for theirs,
				// whatever order they came in.
				p.msgs = append(p.msgs[:seen:seen], p.msgs[seen+1:]...)
				return m
			}
		}
		if time.Now().After(deadline) {
			var last []string
			for _, m := range p.msgs {
				b, _ := json.Marshal(m)
				last = append(last, string(b))
			}
			t.Fatalf("no %s; the page heard:\n%s", what, strings.Join(last, "\n"))
		}
		p.cond.Wait()
	}
}

func is(t, key, val string) func(map[string]any) bool {
	return func(m map[string]any) bool { return m["t"] == t && (key == "" || fmt.Sprint(m[key]) == val) }
}

// core is a fake core: hello, echo, attach-info for a private tmux, and a
// subscription that sends a snapshot.
type core struct {
	tmux, socket string
}

func (c core) Call(op string, args json.RawMessage) (any, error) {
	switch op {
	case ipc.OpHello:
		return ipc.HelloResult{Version: "9.9.9", API: ipc.API, Features: []string{"input"}}, nil
	case "echo":
		return args, nil
	case ipc.OpAttachInfo:
		return ipc.AttachInfoResult{Tmux: c.tmux, Socket: c.socket, Target: "irie"}, nil
	}
	return nil, ipc.Errorf(ipc.CodeUnknownOp, "There is no op %s.", op)
}

func (core) Subscribe(json.RawMessage) (<-chan ipc.Frame, func(), error) {
	ch := make(chan ipc.Frame, 1)
	ch <- ipc.Frame{T: ipc.FrameSnapshot, Sessions: []ipc.Session{{ID: "s1", Name: "api fix", State: "working"}}, Header: &ipc.Header{Version: "9.9.9"}}
	return ch, func() {}, nil
}

// machine is a fake Windows: its setup state, the fixes run, and bridges
// served in this process against the fake core's socket.
type machine struct {
	mu      sync.Mutex
	state   wsl.State
	fixed   []string
	afterFx func()
	socket  string
	procs   []*proc
	bridge  func() (Process, error) // stands in for the real bridge when set
}

func ready() wsl.State {
	return wsl.State{Build: 26100, WSL: true, Distro: wsl.Distro{Name: "Ubuntu", Version: 2}, HasDistro: true, Installed: true,
		KeepRunning: true, Shortcut: true, Startup: true}
}

func (m *machine) Look() wsl.State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

func (m *machine) Fix(st wsl.State, step wsl.Step) error {
	m.mu.Lock()
	m.fixed = append(m.fixed, step.ID)
	f := m.afterFx
	m.mu.Unlock()
	if f != nil {
		f()
	}
	return nil
}

func (m *machine) Bridge(distro string) (Process, error) {
	m.mu.Lock()
	f := m.bridge
	m.mu.Unlock()
	if f != nil {
		return f()
	}
	upR, upW := io.Pipe()
	downR, downW := io.Pipe()
	p := &proc{r: downR, w: upW, closers: []io.Closer{upW, downR, upR, downW}, exited: make(chan struct{})}
	go func() {
		bridge.Serve(upR, downW, bridge.Options{Socket: m.socket, Version: "9.9.9", Env: os.Environ()})
		downW.Close()
		close(p.exited)
	}()
	m.mu.Lock()
	m.procs = append(m.procs, p)
	m.mu.Unlock()
	return p, nil
}

// kill ends the bridges running now, as when wsl.exe dies.
func (m *machine) kill() {
	m.mu.Lock()
	procs := m.procs
	m.procs = nil
	m.mu.Unlock()
	for _, p := range procs {
		p.Close()
	}
}

type proc struct {
	r       io.Reader
	w       io.Writer
	closers []io.Closer
	exited  chan struct{}
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
	<-p.exited
	return p.code, p.stderr
}

func startCore(t *testing.T, c core) string {
	t.Helper()
	sock := filepath.Join(testutil.ShortDir(t), "core.sock")
	l, err := ipc.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go ipc.Serve(l, c)
	return sock
}

func hello(b *Backend) { b.Receive([]byte(`{"q":"hello"}`)) }

func TestCallsAndSubscriptionsGoToTheCore(t *testing.T) {
	m := &machine{state: ready(), socket: startCore(t, core{})}
	p := newPage()
	b := New(p, m)
	hello(b)
	p.await(t, "the link up", is("link", "state", LinkReady))

	b.Receive([]byte(`{"q":"call","id":7,"op":"echo","args":{"text":"hows the parser?"}}`))
	r := p.await(t, "the echo's reply", is("reply", "id", "7"))
	if r["ok"] != true || r["result"].(map[string]any)["text"] != "hows the parser?" {
		t.Fatalf("reply = %v", r)
	}
	b.Receive([]byte(`{"q":"call","id":8,"op":"nope"}`))
	r = p.await(t, "an unknown op's reply", is("reply", "id", "8"))
	if e := r["error"].(map[string]any); r["ok"] != false || e["code"] != ipc.CodeUnknownOp || e["msg"] != "There is no op nope." {
		t.Fatalf("reply = %v", r)
	}

	b.Receive([]byte(`{"q":"sub","id":3,"args":{"view":"app"}}`))
	p.await(t, "the subscription", is("sub", "state", "connected"))
	f := p.await(t, "the snapshot", is("frame", "id", "3"))["f"].(map[string]any)
	if f["t"] != "snapshot" || f["sessions"].([]any)[0].(map[string]any)["name"] != "api fix" {
		t.Fatalf("frame = %v", f)
	}

	// The bridge dies, as wsl.exe may: the link comes back, and the
	// subscription with it.
	m.kill()
	p.await(t, "the link down", is("link", "state", LinkConnecting))
	p.await(t, "the subscription reconnecting", is("sub", "state", "connecting"))
	p.await(t, "the link back", is("link", "state", LinkReady))
	p.await(t, "the subscription back", is("sub", "state", "connected"))
	p.await(t, "a new snapshot", is("frame", "id", "3"))

	// What the window answers itself goes to the host.
	b.Receive([]byte(`{"q":"open","url":"https://example.com/"}`))
	p.mu.Lock()
	did := p.did
	p.mu.Unlock()
	if len(did) != 1 || did[0].Q != "open" || did[0].URL != "https://example.com/" {
		t.Fatalf("did = %+v", did)
	}
}

func TestTheTerminalAttachesToTheSlot(t *testing.T) {
	bin := testutil.NeedTmux(t)
	e := testutil.NewEnv(t)
	e.Tmux(t, "-f", "/dev/null", "new-session", "-d", "-s", "irie", "-x", "80", "-y", "24", "printf 'slot-ready\\n'; exec cat")
	tsock := filepath.Join(e.TmuxTmpDir, fmt.Sprintf("tmux-%d", os.Getuid()), e.Socket)
	m := &machine{state: ready(), socket: startCore(t, core{tmux: bin, socket: tsock})}
	p := newPage()
	b := New(p, m)
	hello(b)
	p.await(t, "the link up", is("link", "state", LinkReady))

	b.Receive([]byte(`{"q":"term","id":1,"op":"open","cols":100,"rows":30}`))
	p.await(t, "the terminal attached", is("term", "state", "attached"))
	shown := ""
	drawn := func(what string) {
		t.Helper()
		p.await(t, what+" on the terminal", func(m map[string]any) bool {
			if m["t"] != "term" || m["out"] == nil {
				return false
			}
			b, _ := base64.StdEncoding.DecodeString(m["out"].(string))
			shown += string(b)
			return strings.Contains(shown, what)
		})
	}
	drawn("slot-ready")
	clients := func() string {
		out, _ := e.TryTmux("list-clients", "-F", "#{client_width}x#{client_height}")
		return out
	}
	testutil.Eventually(t, 10*time.Second, "the client at the page's size", func() bool { return clients() == "100x30" })

	b.Receive([]byte(`{"q":"term","id":1,"op":"in","data":"typed on windows\r"}`))
	drawn("typed on windows")
	b.Receive([]byte(`{"q":"term","id":1,"op":"size","cols":132,"rows":43}`))
	testutil.Eventually(t, 10*time.Second, "the client at the new size", func() bool { return clients() == "132x43" })

	b.Receive([]byte(`{"q":"term","id":1,"op":"close"}`))
	testutil.Eventually(t, 10*time.Second, "the client gone", func() bool { return clients() == "" })
	if got := e.Tmux(t, "list-sessions", "-F", "#{session_name}"); got != "irie" {
		t.Fatalf("sessions = %q", got)
	}
}

func TestSetupStepsUntilTheComputerIsReady(t *testing.T) {
	m := &machine{state: wsl.State{Build: 26100, Distro: wsl.Distro{Name: "Ubuntu"}}}
	m.socket = startCore(t, core{})
	m.afterFx = func() {
		m.mu.Lock()
		m.state = ready()
		m.mu.Unlock()
	}
	p := newPage()
	b := New(p, m)
	hello(b)
	p.await(t, "the link at setup", is("link", "state", LinkSetup))
	s := p.await(t, "the steps", func(m map[string]any) bool { return m["t"] == "setup" && len(m["steps"].([]any)) > 0 })
	first := s["steps"].([]any)[0].(map[string]any)
	if first["id"] != "wsl" || first["blocks"] != true || first["restart"] != true {
		t.Fatalf("first step = %v", first)
	}
	b.Receive([]byte(`{"q":"fix","step":"wsl"}`))
	p.await(t, "the fix running", is("setup", "fixing", "wsl"))
	p.await(t, "the link up once set up", is("link", "state", LinkReady))
	m.mu.Lock()
	fixed := m.fixed
	m.mu.Unlock()
	if len(fixed) != 1 || fixed[0] != "wsl" {
		t.Fatalf("fixed = %v", fixed)
	}
}

func TestAnOldIrieInsideLinuxAsksForTheUpdate(t *testing.T) {
	m := &machine{state: ready()}
	m.bridge = func() (Process, error) {
		r, w := io.Pipe()
		w.Close()
		ex := make(chan struct{})
		close(ex)
		return &proc{r: r, w: io.Discard, exited: ex, code: 2, stderr: "irie: unknown command \"bridge\"\n\nirie runs your agents…"}, nil
	}
	p := newPage()
	b := New(p, m)
	hello(b)
	l := p.await(t, "the link asking for the update", is("link", "state", LinkOld))
	if !strings.Contains(l["text"].(string), "older than this app") {
		t.Fatalf("link = %v", l)
	}
	// Its update is the install step, though the plan has none.
	b.Receive([]byte(`{"q":"fix","step":"install"}`))
	p.await(t, "the update running", is("setup", "fixing", "install"))
}

// TestABridgeThatEndsSaysWhatItWroteLast: a bridge that ends with no hello
// leaves the window its last words, the reason and the lines before it,
// for Try again, Repair and Copy details.
func TestABridgeThatEndsSaysWhatItWroteLast(t *testing.T) {
	m := &machine{state: ready()}
	m.bridge = func() (Process, error) {
		r, w := io.Pipe()
		w.Close()
		ex := make(chan struct{})
		close(ex)
		return &proc{r: r, w: io.Discard, exited: ex, code: 1, stderr: "first\r\nsecond\nirie: up.lock: busy\n"}, nil
	}
	p := newPage()
	b := New(p, m)
	hello(b)
	l := p.await(t, "the link down", is("link", "state", LinkDown))
	if l["text"] != "irie: up.lock: busy" || l["detail"] != "first\nsecond\nirie: up.lock: busy" {
		t.Fatalf("link = %v", l)
	}
	// Its repair is the install step, though the plan has none.
	b.Receive([]byte(`{"q":"fix","step":"install"}`))
	p.await(t, "the repair running", is("setup", "fixing", "install"))
}

func TestLastLinesKeepsTheEndAndCutsLongLines(t *testing.T) {
	long := strings.Repeat("x", 400)
	if got := lastLines("a\nb\nc\n", 2); got != "b\nc" {
		t.Errorf("lastLines = %q", got)
	}
	if got := lastLines(long, 3); got != strings.Repeat("x", 300)+"…" {
		t.Errorf("lastLines of a long line = %d runes", len([]rune(got)))
	}
}

func TestPumpBatchesABurst(t *testing.T) {
	r, w := io.Pipe()
	var got [][]byte
	done := make(chan struct{})
	go func() {
		pump(r, func(b []byte) { got = append(got, b) })
		close(done)
	}()
	for i := 0; i < 50; i++ {
		w.Write([]byte("0123456789"))
	}
	w.Close()
	<-done
	total := 0
	for _, b := range got {
		total += len(b)
	}
	if total != 500 || len(got) > 10 {
		t.Fatalf("%d bytes in %d batches", total, len(got))
	}
}
