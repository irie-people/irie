package bridge

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/mux"
	"github.com/irie-people/irie/internal/testutil"
)

func TestMain(m *testing.M) { testutil.Main(m) }

// fakeCore answers hello and echo on a socket of its own, as the core
// would, with a subscription that sends one frame.
type fakeCore struct{}

func (fakeCore) Call(op string, args json.RawMessage) (any, error) {
	switch op {
	case ipc.OpHello:
		return ipc.HelloResult{Version: "9.9.9", API: ipc.API}, nil
	case "echo":
		return args, nil
	}
	return nil, ipc.Errorf(ipc.CodeUnknownOp, "no op %s", op)
}

func (fakeCore) Subscribe(json.RawMessage) (<-chan ipc.Frame, func(), error) {
	ch := make(chan ipc.Frame, 1)
	ch <- ipc.Frame{T: ipc.FrameHistory, Undo: "park \"x\""}
	return ch, func() {}, nil
}

func startCore(t *testing.T, sock string) {
	t.Helper()
	l, err := ipc.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go ipc.Serve(l, fakeCore{})
}

// bridged serves the bridge over pipes and gives the app's end.
func bridged(t *testing.T, o Options) (*mux.Mux, mux.Hello) {
	t.Helper()
	upR, upW := io.Pipe()
	downR, downW := io.Pipe()
	go func() {
		Serve(upR, downW, o)
		downW.Close()
	}()
	m, h, err := mux.Client(downR, upW, upW)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	return m, h
}

func call(t *testing.T, m *mux.Mux, line string) string {
	t.Helper()
	s, err := m.Open(mux.Open{Kind: mux.StreamCore})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, err := io.WriteString(s, line+"\n"); err != nil {
		t.Fatal(err)
	}
	reply, err := bufio.NewReader(s).ReadString('\n')
	if err != nil {
		t.Fatalf("no reply to %s: %v", line, err)
	}
	return strings.TrimSpace(reply)
}

func TestCoreStreamsRelayTheSocket(t *testing.T) {
	sock := filepath.Join(testutil.ShortDir(t), "core.sock")
	startCore(t, sock)
	m, h := bridged(t, Options{Socket: sock, Version: "1.2.3"})
	if h.Core != "running" || h.Version != "1.2.3" || h.Problem != "" {
		t.Fatalf("hello = %+v", h)
	}
	got := call(t, m, `{"id":4,"op":"echo","args":{"a":"b"}}`)
	if got != `{"id":4,"ok":true,"result":{"a":"b"}}` {
		t.Fatalf("reply = %s", got)
	}
	// A subscription: its reply, then its frames, on the same stream.
	s, err := m.Open(mux.Open{Kind: mux.StreamCore})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.SetReadDeadline(time.Now().Add(10 * time.Second))
	io.WriteString(s, `{"id":1,"op":"subscribe","args":{"view":"app"}}`+"\n")
	r := bufio.NewReader(s)
	for _, want := range []string{`"ok":true`, `"t":"history"`} {
		line, err := r.ReadString('\n')
		if err != nil || !strings.Contains(line, want) {
			t.Fatalf("got %q (%v), want %s", line, err, want)
		}
	}
}

func TestTheCoreStartsWhenItDoesNotAnswer(t *testing.T) {
	sock := filepath.Join(testutil.ShortDir(t), "core.sock")
	started := false
	_, h := bridged(t, Options{Socket: sock, Up: func() error {
		started = true
		startCore(t, sock)
		return nil
	}})
	if !started || h.Core != "started" {
		t.Fatalf("hello = %+v, started %v", h, started)
	}
}

func TestHelloSaysWhyThereIsNoCore(t *testing.T) {
	sock := filepath.Join(testutil.ShortDir(t), "core.sock")
	m, h := bridged(t, Options{Socket: sock, Up: func() error { return errors.New("tmux is missing") }})
	if h.Core != "" || !strings.Contains(h.Problem, "tmux is missing") {
		t.Fatalf("hello = %+v", h)
	}
	// A stream to it says so, rather than hang.
	s, err := m.Open(mux.Open{Kind: mux.StreamCore})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(s); err != nil {
		t.Fatal(err)
	}
	if e := s.Ended(); e == nil || !strings.Contains(e.Error, "not answering") {
		t.Fatalf("ended = %+v", e)
	}
}

func TestTmuxStreamsAttachAndDetachLeavingTheSessionAsItWas(t *testing.T) {
	bin := testutil.NeedTmux(t)
	e := testutil.NewEnv(t)
	e.Tmux(t, "-f", "/dev/null", "new-session", "-d", "-s", "agent", "-x", "80", "-y", "24", "printf 'agent-ready\\n'; exec cat")
	sock := filepath.Join(e.TmuxTmpDir, fmt.Sprintf("tmux-%d", os.Getuid()), e.Socket)
	m, _ := bridged(t, Options{Socket: filepath.Join(testutil.ShortDir(t), "none.sock"), Env: os.Environ()})

	s, err := m.Open(mux.Open{Kind: mux.StreamTmux, Tmux: bin, Socket: sock, Target: "agent", Cols: 100, Rows: 30})
	if err != nil {
		t.Fatal(err)
	}
	out := make(chan string, 64)
	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := s.Read(buf)
			if n > 0 {
				out <- string(buf[:n])
			}
			if err != nil {
				close(out)
				return
			}
		}
	}()
	seen := ""
	waitFor := func(what string) {
		t.Helper()
		deadline := time.After(15 * time.Second)
		for !strings.Contains(seen, what) {
			select {
			case chunk, ok := <-out:
				if !ok {
					t.Fatalf("the terminal ended before %q; it showed %q (%+v)", what, seen, s.Ended())
				}
				seen += chunk
			case <-deadline:
				t.Fatalf("no %q in %q", what, seen)
			}
		}
	}
	waitFor("agent-ready")
	clients := func() string {
		out, _ := e.TryTmux("list-clients", "-F", "#{client_width}x#{client_height}")
		return out
	}
	testutil.Eventually(t, 10*time.Second, "the client takes the terminal's size", func() bool { return clients() == "100x30" })

	// A new size reaches tmux.
	s.Resize(120, 40)
	testutil.Eventually(t, 10*time.Second, "the client takes the new size", func() bool { return clients() == "120x40" })

	// Keys reach the program in the pane.
	io.WriteString(s, "hello-from-windows\r")
	waitFor("hello-from-windows")

	// The app ending the stream detaches; the session keeps running.
	s.Close()
	testutil.Eventually(t, 10*time.Second, "the client detaches", func() bool { return clients() == "" })
	if got := e.Tmux(t, "list-sessions", "-F", "#{session_name}"); got != "agent" {
		t.Fatalf("sessions after the detach = %q", got)
	}
}

func TestTmuxStreamsRefuseWhatIsNotAnAttach(t *testing.T) {
	for _, op := range []mux.Open{
		{Kind: mux.StreamTmux, Socket: "relative.sock", Target: "irie"},
		{Kind: mux.StreamTmux, Socket: "/tmp/x.sock", Target: "-t"},
		{Kind: mux.StreamTmux, Socket: "/tmp/x.sock", Target: "irie", Tmux: "tmux; rm -rf ~"},
	} {
		if _, err := tmuxArgs(op); err == nil {
			t.Errorf("%+v passed", op)
		}
	}
	got, err := tmuxArgs(mux.Open{Kind: mux.StreamTmux, Socket: "/run/irie/tmux.sock", Target: "irie"})
	if err != nil || strings.Join(got, " ") != "tmux -u -S /run/irie/tmux.sock attach-session -t irie" {
		t.Fatalf("args = %q, %v", got, err)
	}
	env := termEnv([]string{"HOME=/home/alice", "TMUX=/tmp/x,1,0", "TERM=dumb", "TMUX_PANE=%1"})
	if strings.Join(env, " ") != "HOME=/home/alice TERM=xterm-256color COLORTERM=truecolor" {
		t.Fatalf("env = %q", env)
	}
}
