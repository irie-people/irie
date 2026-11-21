package main

import (
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/paths"
)

// sendFake is a core with two sessions that keeps what send and continue
// were asked.
type sendFake struct {
	mu   sync.Mutex
	sent []ipc.SendArgs
	cont []ipc.ContinueArgs
}

func (f *sendFake) Call(op string, raw json.RawMessage) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch op {
	case ipc.OpList:
		return ipc.ListResult{Sessions: []ipc.Session{{ID: "s01a", Name: "Night shift"}, {ID: "s02b", Name: "Warm Look"}}}, nil
	case ipc.OpSend:
		var a ipc.SendArgs
		json.Unmarshal(raw, &a)
		f.sent = append(f.sent, a)
		return nil, nil
	case ipc.OpContinue:
		var a ipc.ContinueArgs
		json.Unmarshal(raw, &a)
		f.cont = append(f.cont, a)
		return nil, nil
	}
	return nil, ipc.Errorf(ipc.CodeBadArgs, "no %s here", op)
}

func (f *sendFake) Subscribe(json.RawMessage) (<-chan ipc.Frame, func(), error) {
	return nil, nil, errors.New("no")
}

// TestCtlSendByName: ctl send and ctl continue take a session's name, in
// any case, as well as its id, as close and park do, so an agent can message
// a session it started by the name it gave it. A name that fits no session
// goes to the core as it is, which says so.
func TestCtlSendByName(t *testing.T) {
	bare(t)
	run, err := os.MkdirTemp("/tmp", "sd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(run) })
	t.Setenv("XDG_RUNTIME_DIR", run)
	p, err := paths.FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(p.Runtime, 0o700)
	l, err := ipc.Listen(p.Socket())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	fake := &sendFake{}
	go ipc.Serve(l, fake)

	for _, c := range []struct{ name, want string }{
		{"Night shift", "s01a"},
		{"night SHIFT", "s01a"},
		{"s02b", "s02b"},
		{"Nobody here", "Nobody here"},
	} {
		if err := ctlSend([]string{c.name, "The", "report", "is", "ready"}); err != nil {
			t.Fatalf("send %q: %v", c.name, err)
		}
		fake.mu.Lock()
		got := fake.sent[len(fake.sent)-1]
		fake.mu.Unlock()
		if got.Session != c.want || got.Text != "The report is ready" {
			t.Errorf("send %q asked %+v, want session %s", c.name, got, c.want)
		}
	}
	if err := ctlContinue([]string{"warm look"}); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.cont) != 1 || fake.cont[0].Session != "s02b" {
		t.Errorf("continue asked %+v, want s02b", fake.cont)
	}
}
