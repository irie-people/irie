package ipc

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/irie-hq/irie/internal/event"
	"github.com/irie-hq/irie/internal/testutil"
)

type testHandler struct {
	frames    chan Frame
	cancelled chan struct{}
}

func (h *testHandler) Call(op string, args json.RawMessage) (any, error) {
	switch op {
	case "echo":
		var v map[string]any
		if err := json.Unmarshal(args, &v); err != nil {
			return nil, Errorf(CodeBadArgs, "%v", err)
		}
		return v, nil
	case "fail":
		return nil, errors.New("boom")
	}
	return nil, Errorf(CodeUnknownOp, "no op %q", op)
}

func (h *testHandler) Subscribe(args json.RawMessage) (<-chan Frame, func(), error) {
	return h.frames, func() { close(h.cancelled) }, nil
}

func serve(t *testing.T) (string, *testHandler) {
	t.Helper()
	sock := filepath.Join(testutil.ShortDir(t), "core.sock")
	l, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	h := &testHandler{frames: make(chan Frame, 4), cancelled: make(chan struct{})}
	go Serve(l, h)
	return sock, h
}

func TestSocketIsPrivate(t *testing.T) {
	sock, _ := serve(t)
	fi, err := os.Stat(sock)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("socket mode = %v, want 0600", perm)
	}
}

func TestCallRoundTrip(t *testing.T) {
	sock, _ := serve(t)
	c, err := Dial(sock, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	big := strings.Repeat("task line\n", 300_000) // 3 MB, well past any line buffer
	var got map[string]string
	if err := c.Call("echo", map[string]string{"task": big}, &got); err != nil {
		t.Fatal(err)
	}
	if got["task"] != big {
		t.Error("large payload did not survive the round trip")
	}

	// Several calls share one connection.
	if err := c.Call("echo", map[string]int{"n": 2}, nil); err != nil {
		t.Fatal(err)
	}

	err = c.Call("nope", nil, nil)
	var e *Error
	if !errors.As(err, &e) || e.Code != CodeUnknownOp {
		t.Errorf("unknown op gave %v", err)
	}
	err = c.Call("fail", nil, nil)
	if !errors.As(err, &e) || e.Code != CodeInternal || e.Msg != "boom" {
		t.Errorf("handler error gave %v", err)
	}
}

func TestSubscribeStreamsFrames(t *testing.T) {
	sock, h := serve(t)
	c, err := Dial(sock, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	frames, err := c.Subscribe(nil)
	if err != nil {
		t.Fatal(err)
	}
	h.frames <- Frame{T: FrameSnapshot, Sessions: []Session{{ID: "s1", Name: "one"}}, Header: &Header{Version: "v"}}
	h.frames <- Frame{T: FrameUpsert, Session: &Session{ID: "s2", Name: "two"}}
	h.frames <- Frame{T: FrameRemove, ID: "s1"}
	h.frames <- Frame{T: FrameEvent, Event: &event.Event{V: 1, Seq: 9, Session: "s0k3j9x2m4a7q", Kind: event.TurnEnded, Data: map[string]any{}}}

	f := <-frames
	if f.T != FrameSnapshot || len(f.Sessions) != 1 || f.Sessions[0].Name != "one" || f.Header == nil || f.Header.Version != "v" {
		t.Errorf("first frame = %+v", f)
	}
	if f = <-frames; f.T != FrameUpsert || f.Session == nil || f.Session.ID != "s2" {
		t.Errorf("second frame = %+v", f)
	}
	if f = <-frames; f.T != FrameRemove || f.ID != "s1" {
		t.Errorf("third frame = %+v", f)
	}
	if f = <-frames; f.T != FrameEvent || f.Event == nil || f.Event.Seq != 9 || f.Event.Kind != event.TurnEnded {
		t.Errorf("fourth frame = %+v", f)
	}

	c.Close()
	select {
	case <-h.cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("closing the client did not cancel the subscription")
	}
	if _, ok := <-frames; ok {
		t.Error("frame channel still open after close")
	}
}

func TestEmptySnapshotListsNoSessions(t *testing.T) {
	b, err := json.Marshal(Frame{T: FrameSnapshot, Header: &Header{}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"sessions":[]`) {
		t.Errorf("snapshot = %s", b)
	}
}

func TestEventFrameCarriesTheEnvelopeOnly(t *testing.T) {
	b, err := json.Marshal(Frame{T: FrameEvent, Event: &event.Event{V: 1, Seq: 3, Kind: event.ToolStarted, Data: map[string]any{"tool": "Bash"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), `{"t":"event","event":{"v":1,"seq":3,`) || strings.Contains(string(b), "sessions") {
		t.Errorf("event frame = %s", b)
	}
}

func TestDialFailsFastWithoutServer(t *testing.T) {
	sock := filepath.Join(testutil.ShortDir(t), "none.sock")
	start := time.Now()
	if _, err := Dial(sock, 200*time.Millisecond); err == nil {
		t.Fatal("dial succeeded")
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("dial took %v", d)
	}
}
