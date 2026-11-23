package main

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/localmodel"
	"github.com/irie-people/irie/internal/paths"
)

// lostFake is a core with three sessions the last restart lost, one of
// them at work, that keeps what reopen was asked.
type lostFake struct {
	mu     sync.Mutex
	reopen []ipc.ReopenArgs
}

func (f *lostFake) Call(op string, raw json.RawMessage) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch op {
	case ipc.OpList:
		at := time.Date(2026, 11, 23, 12, 30, 0, 0, time.UTC)
		return ipc.ListResult{Closed: []ipc.ClosedSession{
			{ID: "s03w", Name: "Tests", Closed: at.Add(2 * time.Second), Lost: true, Working: true},
			{ID: "s02i", Name: "Docs", Closed: at.Add(time.Second), Lost: true},
			{ID: "s01i", Name: "Notes", Closed: at, Lost: true},
		}}, nil
	case ipc.OpReopen:
		var a ipc.ReopenArgs
		json.Unmarshal(raw, &a)
		f.reopen = append(f.reopen, a)
		state := ipc.StateStarting
		if a.Rest {
			state = ipc.StateResting
		}
		return ipc.ReopenResult{Session: ipc.Session{ID: a.Session, State: state}}, nil
	}
	return nil, ipc.Errorf(ipc.CodeBadArgs, "no %s here", op)
}

func (f *lostFake) Subscribe(json.RawMessage) (<-chan ipc.Frame, func(), error) {
	return nil, nil, errors.New("no")
}

// TestReopenLostRestsWhatWasNotAtWork: a restore starts the agents of the
// sessions that were at work when they were lost, and brings the rest back
// resting (DESIGN §4.5); --awake starts them all, as before.
func TestReopenLostRestsWhatWasNotAtWork(t *testing.T) {
	bare(t)
	run, err := os.MkdirTemp("/tmp", "rl")
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
	read, gap := readMemory, lostGap
	t.Cleanup(func() { readMemory, lostGap = read, gap })
	lostGap = 0
	readMemory = func() (localmodel.Machine, error) { return localmodel.Machine{Total: 100, Available: 60}, nil }
	fake := &lostFake{}
	go ipc.Serve(l, fake)

	if err := ctlReopen([]string{"--lost"}); err != nil {
		t.Fatal(err)
	}
	want := []ipc.ReopenArgs{{Session: "s01i", Rest: true}, {Session: "s02i", Rest: true}, {Session: "s03w"}}
	if !slices.Equal(fake.reopen, want) {
		t.Errorf("reopened %+v, want %+v", fake.reopen, want)
	}
	fake.reopen = nil
	if err := ctlReopen([]string{"--lost", "--awake"}); err != nil {
		t.Fatal(err)
	}
	if want := []ipc.ReopenArgs{{Session: "s01i"}, {Session: "s02i"}, {Session: "s03w"}}; !slices.Equal(fake.reopen, want) {
		t.Errorf("reopened awake %+v, want %+v", fake.reopen, want)
	}
	if err := ctlReopen([]string{"--awake", "s01i"}); err == nil {
		t.Error("--awake took a session")
	}
}
