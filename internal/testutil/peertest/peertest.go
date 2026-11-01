// Package peertest runs another machine's Irie for tests of peers
// (docs/proposals/multi-host.md): the real gateway, internal/remote, over
// TLS on 127.0.0.1 with its own authority, in front of a fake core that the
// test drives.
package peertest

import (
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/remote"
)

// Gateway is the other machine's gateway.
type Gateway struct {
	URL         string // https://127.0.0.1:port
	Fingerprint string // its authority's SHA-256, hex
	Core        *Core
	store       *remote.Store
	ts          *httptest.Server
}

// Start runs a gateway in front of a new fake core until the test ends.
func Start(t testing.TB) *Gateway {
	t.Helper()
	dir := t.TempDir()
	// A unix socket's path must stay short.
	sockDir, err := os.MkdirTemp("/tmp", "irie-pt-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	sock := filepath.Join(sockDir, "core.sock")
	l, err := ipc.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	c := &Core{results: map[string]any{}, subs: map[chan ipc.Frame]bool{}}
	go ipc.Serve(l, c)
	srv, err := remote.New(remote.Options{Socket: sock, Dir: dir, Port: 7387, Name: "Peer", Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	ca, err := remote.LoadAuthority(dir)
	if err != nil {
		t.Fatal(err)
	}
	leaves := &remote.Leaves{CA: ca, Names: func() []string { return nil }, Addrs: func() []net.IP { return []net.IP{net.IPv4(127, 0, 0, 1)} }}
	// httptest would serve its own certificate to a client that names no
	// server, as one dialing an address does, so the gateway's is the
	// only one.
	cert, err := leaves.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewUnstartedServer(srv.App())
	ts.TLS = &tls.Config{Certificates: []tls.Certificate{*cert}, MinVersion: tls.VersionTLS12}
	ts.StartTLS()
	t.Cleanup(ts.Close)
	return &Gateway{URL: ts.URL, Fingerprint: ca.FingerprintHex(), Core: c, store: &remote.Store{Dir: dir}, ts: ts}
}

// Code opens a pairing code there.
func (g *Gateway) Code(t testing.TB) string {
	t.Helper()
	code, err := g.store.NewCode(10 * time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return code
}

// Close takes the machine away: its gateway stops answering.
func (g *Gateway) Close() {
	g.ts.CloseClientConnections()
	g.ts.Close()
}

// Call is one call the fake core got.
type Call struct {
	Op   string
	Args map[string]any
}

// Core is the other machine's core: it answers each op with what Set
// gave it, or with an empty result, and keeps every call. A subscription
// gets the snapshot Start gave it, then the frames Send sends.
type Core struct {
	mu       sync.Mutex
	calls    []Call
	results  map[string]any // by op
	snapshot []ipc.Session
	header   *ipc.Header
	subs     map[chan ipc.Frame]bool
	sub      []ipc.SubscribeArgs // every subscription's args
}

// Set makes the core answer op with result, or fail with it when it is an
// error.
func (c *Core) Set(op string, result any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.results[op] = result
}

// Start is what a subscription starts with.
func (c *Core) Start(rows []ipc.Session, h *ipc.Header) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.snapshot, c.header = rows, h
}

func (c *Core) Call(op string, raw json.RawMessage) (any, error) {
	var args map[string]any
	json.Unmarshal(raw, &args)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, Call{Op: op, Args: args})
	if op == ipc.OpHello {
		return ipc.HelloResult{Version: "test", API: ipc.API, Features: remote.Ops}, nil
	}
	if r, ok := c.results[op]; ok {
		if err, ok := r.(error); ok {
			return nil, err
		}
		return r, nil
	}
	return struct{}{}, nil
}

func (c *Core) Subscribe(raw json.RawMessage) (<-chan ipc.Frame, func(), error) {
	var a ipc.SubscribeArgs
	json.Unmarshal(raw, &a)
	ch := make(chan ipc.Frame, 64)
	c.mu.Lock()
	c.sub = append(c.sub, a)
	ch <- ipc.Frame{T: ipc.FrameSnapshot, Sessions: c.snapshot, Header: c.header}
	c.subs[ch] = true
	c.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			c.mu.Lock()
			delete(c.subs, ch)
			c.mu.Unlock()
		})
	}, nil
}

// Send sends a frame to every subscription.
func (c *Core) Send(f ipc.Frame) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for ch := range c.subs {
		ch <- f
	}
}

// Calls returns the calls of op so far.
func (c *Core) Calls(op string) []Call {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []Call
	for _, k := range c.calls {
		if k.Op == op {
			out = append(out, k)
		}
	}
	return out
}

// Subscriptions returns the args of every subscription so far.
func (c *Core) Subscriptions() []ipc.SubscribeArgs {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]ipc.SubscribeArgs(nil), c.sub...)
}
