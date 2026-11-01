package core

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/peer"
	"github.com/irie-people/irie/internal/testutil"
	"github.com/irie-people/irie/internal/testutil/peertest"
)

// newPeerCore is a core without tmux: enough for its list, and for its
// calls and subscriptions about the sessions of its peers.
func newPeerCore(t *testing.T) *Core {
	t.Helper()
	c := &Core{
		version: "test", paths: paths.Paths{State: t.TempDir()}, log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		sessions: map[string]*session{}, published: map[string]ipc.Session{}, subs: map[*subscriber]bool{}, hues: map[string]int{},
	}
	c.useConfig(config.Default())
	c.cfg().Remote.Name = "Mac"
	c.mach = &ipc.Machine{Name: "Mac"} // no hooks of its own
	t.Cleanup(func() {
		c.mu.Lock()
		for _, p := range c.peers {
			p.stop()
		}
		c.mu.Unlock()
	})
	return c
}

// pairWith pairs c with the gateway g as the peer called name.
func pairWith(t *testing.T, c *Core, g *peertest.Gateway, name string) {
	t.Helper()
	cl, err := peer.New(g.URL, "", g.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	token, err := cl.Pair(ctx, g.Code(t), "Mac (Irie)")
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.Save(c.paths.State, name, peer.Peer{URL: g.URL, Token: token, Fingerprint: g.Fingerprint}); err != nil {
		t.Fatal(err)
	}
	if err := c.loadPeers(); err != nil {
		t.Fatal(err)
	}
}

func (c *Core) ids() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, s := range c.list() {
		out = append(out, s.ID)
	}
	return out
}

func TestAPeersSessionsJoinTheListAndItsCallsGoThere(t *testing.T) {
	g := peertest.Start(t)
	g.Core.Start([]ipc.Session{
		{ID: "s2", Name: "write the docs", State: ipc.StateIdle, Order: 1},
		{ID: "s1", Name: "fix the build", State: ipc.StateWorking, Order: 0, Parent: "s0"},
	}, &ipc.Header{Version: "peer-v", Load: &ipc.Load{CPUs: 10, RunQueue: 0.5, PTYs: 8, PTYMax: 4096}, Machine: &ipc.Machine{Name: "Asahi", Claude: "ok"}})
	c := newPeerCore(t)
	pairWith(t, c, g, "Asahi")

	testutil.Eventually(t, 10*time.Second, "the peer's rows never joined the list", func() bool {
		return slices.Equal(c.ids(), []string{"Asahi/s1", "Asahi/s2"})
	})
	c.mu.Lock()
	rows, info := c.list(), c.peersInfo()
	c.mu.Unlock()
	if r := rows[0]; r.Host != "Asahi" || r.Parent != "Asahi/s0" || r.Order != 0 || r.HostDown != nil || rows[1].Order != 1 {
		t.Errorf("rows = %+v", rows)
	}
	if rows[0].Detail != "on Asahi · working" || rows[1].Detail != "on Asahi · idle" {
		t.Errorf("details = %q, %q", rows[0].Detail, rows[1].Detail)
	}
	if len(info) != 1 || !info[0].Up || info[0].Sessions != 2 || info[0].Version != "peer-v" || info[0].Load == nil || info[0].Load.CPUs != 10 || info[0].Refuses != "" {
		t.Errorf("peers = %+v", info)
	}

	// Changes come as they happen.
	g.Core.Send(ipc.Frame{T: ipc.FrameUpsert, Session: &ipc.Session{ID: "s3", Name: "new", State: ipc.StateStarting, Order: 2}})
	g.Core.Send(ipc.Frame{T: ipc.FrameRemove, ID: "s1"})
	testutil.Eventually(t, 10*time.Second, "the peer's changes never came", func() bool {
		return slices.Equal(c.ids(), []string{"Asahi/s2", "Asahi/s3"})
	})

	// A call about one of its rows goes there, with its own id, and what
	// comes back has this list's.
	if _, err := c.Call(ipc.OpSend, json.RawMessage(`{"session":"Asahi/s2","text":"hi"}`)); err != nil {
		t.Fatal(err)
	}
	if calls := g.Core.Calls(ipc.OpSend); len(calls) != 1 || calls[0].Args["session"] != "s2" || calls[0].Args["text"] != "hi" {
		t.Errorf("send there = %+v", calls)
	}
	g.Core.Set(ipc.OpRename, ipc.RenameResult{Session: ipc.Session{ID: "s2", Name: "docs"}})
	res, err := c.Call(ipc.OpRename, json.RawMessage(`{"session":"Asahi/s2","name":"docs"}`))
	if err != nil {
		t.Fatal(err)
	}
	var rr ipc.RenameResult
	if b, _ := json.Marshal(res); json.Unmarshal(b, &rr) != nil || rr.Session.ID != "Asahi/s2" || rr.Session.Host != "Asahi" {
		t.Errorf("rename = %s", b)
	}
	g.Core.Set(ipc.OpClose, ipc.Errorf(ipc.CodeBusy, "it is busy"))
	var e *ipc.Error
	if _, err := c.Call(ipc.OpClose, json.RawMessage(`{"session":"Asahi/s2"}`)); !errors.As(err, &e) || e.Code != ipc.CodeBusy {
		t.Errorf("close: %v; want the peer's own error", err)
	}
	if _, err := c.Call(ipc.OpReorder, json.RawMessage(`{"session":"Asahi/s2","before":"s9"}`)); err == nil {
		t.Error("a peer's row moved in this list")
	}

	// A subscription to one of its sessions follows it there.
	frames, cancel, err := c.Subscribe(json.RawMessage(`{"session":"Asahi/s2","conversation":true}`))
	if err != nil {
		t.Fatal(err)
	}
	f := <-frames
	cancel()
	if f.T != ipc.FrameSnapshot || len(f.Sessions) == 0 || !strings.HasPrefix(f.Sessions[0].ID, "Asahi/") {
		t.Errorf("first frame = %+v", f)
	}
	if subs := g.Core.Subscriptions(); !slices.ContainsFunc(subs, func(a ipc.SubscribeArgs) bool { return a.Session == "s2" && a.Conversation }) {
		t.Errorf("subscriptions there = %+v", subs)
	}

	// ^name starts a task there, which reads its own @ and + words.
	g.Core.Set(ipc.OpInput, ipc.InputResult{Did: "started", Op: ipc.OpSpawn, Text: "Started fix it", Session: &ipc.Session{ID: "s7", Name: "fix it"}})
	r, err := c.input(ipc.InputArgs{Text: "@irie ^asa fix it\nand test it", Session: "s1"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	in := g.Core.Calls(ipc.OpInput)
	if len(in) != 1 || in[0].Args["text"] != "@irie fix it\nand test it" || in[0].Args["session"] != nil {
		t.Errorf("input there = %+v", in)
	}
	if r.Session == nil || r.Session.ID != "Asahi/s7" || r.Session.Host != "Asahi" || !strings.HasPrefix(r.Text, "On Asahi: ") {
		t.Errorf("input result = %+v", r)
	}

	// The machine goes away: its rows stay, marked, and calls say so.
	g.Close()
	testutil.Eventually(t, 10*time.Second, "the peer's rows were never marked down", func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		rows := c.list()
		return len(rows) == 2 && rows[0].HostDown != nil && !c.peersInfo()[0].Up && strings.HasPrefix(rows[0].Detail, "Asahi isn't answering · ")
	})
	if _, err := c.Call(ipc.OpSend, json.RawMessage(`{"session":"Asahi/s2","text":"hi"}`)); !errors.As(err, &e) || e.Code != ipc.CodePeerDown {
		t.Errorf("send while down: %v", err)
	}
	if _, err := c.input(ipc.InputArgs{Text: "^asahi fix it"}, 0); !errors.As(err, &e) || e.Code != ipc.CodePeerDown {
		t.Errorf("^asahi while down: %v", err)
	}

	// Unpaired, its rows go.
	if err := peer.Remove(c.paths.State, "Asahi"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.peersOp(ipc.PeersArgs{Reload: true}); err != nil {
		t.Fatal(err)
	}
	if ids := c.ids(); len(ids) != 0 {
		t.Errorf("after unpairing the list has %v", ids)
	}
}

func TestACaretWordNamesAMachine(t *testing.T) {
	c := newPeerCore(t)
	c.peers = map[string]*peerState{"Asahi": {name: "Asahi", stop: func() {}}, "Atlas": {name: "Atlas", stop: func() {}}}
	for _, tc := range []struct {
		in, peer, rest string
		here           bool
	}{
		{"^asahi fix it", "Asahi", "fix it", false},
		{"fix it ^ATL", "Atlas", "fix it", false},
		{"^a fix it", "", "^a fix it", false}, // two machines start so
		{"^mac fix it", "", "fix it", true},
		{"why does ^C hang", "", "why does ^C hang", false},
		{"@irie ^asahi +sonnet do x\nsecond ^atlas line", "Asahi", "@irie +sonnet do x\nsecond ^atlas line", false},
	} {
		c.mu.Lock()
		p, rest, here := c.placeOn(tc.in)
		c.mu.Unlock()
		name := ""
		if p != nil {
			name = p.name
		}
		if name != tc.peer || rest != tc.rest || here != tc.here {
			t.Errorf("placeOn(%q) = %q, %q, %v; want %q, %q, %v", tc.in, name, rest, here, tc.peer, tc.rest, tc.here)
		}
	}
}

func TestABadFrameFromAPeerCannotStopThisCore(t *testing.T) {
	c := newPeerCore(t)
	p := &peerState{name: "Asahi", stop: func() {}, rows: map[string]ipc.Session{},
		fetchq: make(chan ipc.Session, 1), done: map[string]bool{}, fetched: map[string]fetched{}}
	c.peers = map[string]*peerState{"Asahi": p}
	for _, f := range []ipc.Frame{{T: ipc.FrameSnapshot}, {T: ipc.FrameHeader}, {T: ipc.FrameUpsert}, {T: ipc.FrameUpsert, Session: &ipc.Session{}}, {T: "nonsense"}} {
		if err := c.safePeerFrame(p, f); err != nil {
			t.Errorf("%+v: %v", f, err)
		}
	}
	c.mu.Lock()
	info := c.peersInfo()
	c.mu.Unlock()
	if len(info) != 1 || !info[0].Up {
		t.Errorf("peers = %+v", info)
	}
	// A frame that goes wrong ends the stream and lets the lock go.
	p.rows = nil
	if err := c.safePeerFrame(p, ipc.Frame{T: ipc.FrameUpsert, Session: &ipc.Session{ID: "s1"}}); err == nil {
		t.Error("a frame that went wrong passed")
	}
	if !c.mu.TryLock() {
		t.Fatal("the core's lock is still held")
	}
	c.mu.Unlock()
}
