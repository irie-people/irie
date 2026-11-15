// Package fixture is a stand-in for Irie inside WSL, for the Windows app's
// previews and snapshots: a bridge (package mux) in this process whose
// core is fake, with a few sessions in every state, and whose terminal
// draws a still screen and echoes keys. It needs no Linux, no tmux and no
// unix socket, so it runs on the Mac and on GitHub's Windows alike
// (tools/winpreview, and the app's IRIE_APP_BRIDGE). Nothing in it ships
// in the app.
package fixture

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/mux"
)

// Serve speaks the bridge's side of the mux on in and out until in ends,
// as `irie bridge` would, with a fake core.
func Serve(in io.Reader, out io.Writer) error {
	c := NewCore()
	l := newListener()
	go ipc.Serve(l, c)
	return mux.Serve(in, out, mux.Hello{Version: "1.4.0", Core: "running"}, func(op mux.Open, s *mux.Stream) {
		switch op.Kind {
		case mux.StreamCore:
			l.conns <- s
		case mux.StreamTmux:
			screen(s, op.Cols, op.Rows)
		default:
			s.CloseWith(mux.End{Error: "no such stream"})
		}
	})
}

// listener hands ipc.Serve each core stream as a connection.
type listener struct {
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func newListener() *listener { return &listener{conns: make(chan net.Conn), done: make(chan struct{})} }

func (l *listener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *listener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

func (l *listener) Addr() net.Addr { return fakeAddr{} }

type fakeAddr struct{}

func (fakeAddr) Network() string { return "fixture" }
func (fakeAddr) String() string  { return "fixture" }

// screen draws a still session in the terminal, as Claude Code would show
// one, at the terminal's width, and echoes what is typed.
func screen(s *mux.Stream, cols, rows int) {
	if cols <= 0 {
		cols = 100
	}
	bar := strings.Repeat("─", max(10, cols-2))
	lines := []string{
		"\x1b[2J\x1b[H",
		"\x1b[38;5;180m╭" + bar + "╮\x1b[0m",
		"\x1b[38;5;180m│\x1b[0m \x1b[1m✻ Welcome to Claude Code\x1b[0m",
		"\x1b[38;5;180m╰" + bar + "╯\x1b[0m",
		"",
		"\x1b[1m> Fix the flaky parser test\x1b[0m",
		"",
		"\x1b[38;5;108m⏺\x1b[0m I'll find where the parser test reads the clock.",
		"",
		"\x1b[38;5;108m⏺\x1b[0m \x1b[1mRead\x1b[0m(internal/parse/parse_test.go)",
		"  ⎿  Read 214 lines",
		"",
		"\x1b[38;5;108m⏺\x1b[0m \x1b[1mBash\x1b[0m(go test ./internal/parse/ -run TestDates -count=20)",
		"  ⎿  \x1b[32mok\x1b[0m   example.com/shop/internal/parse   0.412s",
		"",
		"\x1b[38;5;214m✶\x1b[0m Running the tests… \x1b[2m(esc to interrupt)\x1b[0m",
		"",
		"\x1b[38;5;180m╭" + bar + "╮\x1b[0m",
		"\x1b[38;5;180m│\x1b[0m > ",
		"\x1b[38;5;180m╰" + bar + "╯\x1b[0m",
		"  \x1b[2m⏵⏵ accept edits on\x1b[0m",
	}
	io.WriteString(s, strings.Join(lines, "\r\n"))
	// The cursor in the prompt.
	fmt.Fprintf(s, "\x1b[%d;5H", len(lines)-2)
	io.Copy(s, s)
	s.Close()
}

// Core is the fake core: a few sessions in every state, which the ops
// change as the real core's would, and the frames that say so.
type Core struct {
	mu      sync.Mutex
	rows    []ipc.Session
	header  ipc.Header
	outside []ipc.OutsideSession
	closed  []ipc.ClosedSession
	undo    string
	subs    map[chan ipc.Frame]bool
	next    int
}

// NewCore is the fake core with its sessions.
func NewCore() *Core {
	now := time.Now()
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	n := func(v float64) *float64 { return &v }
	c := &Core{subs: map[chan ipc.Frame]bool{}, next: 20}
	c.rows = []ipc.Session{
		{ID: "s1", Name: "Fix the flaky parser test", State: "working", Detail: "Running the tests", Command: "Bash · go test ./internal/parse/ -run TestDates -count=20",
			Created: ago(42 * time.Minute), Repo: "/home/alice/src/shop", Branch: brand.Slug + "/fix-the-flaky-parser", Profile: "claude", Model: "claude-opus-5-5", Hue: 1, Shown: true,
			Tokens: &ipc.Tokens{Input: 21000, Output: 9200, CacheRead: 160000}, CostUSD: n(1.84), Context: n(38),
			Subagents: []ipc.Subagent{{Agent: "sub:a1", Type: "Explore", Detail: "Reading parse_test.go", Started: ago(3 * time.Minute)}}},
		{ID: "s2", Name: "Docs: install on Windows", State: "working", Detail: "Editing docs/WINDOWS.md", Created: ago(2 * time.Hour), Repo: "/home/alice/src/" + brand.Slug,
			Branch: brand.Slug + "/docs-install-windows", Profile: "claude", Hue: 4, Pinned: true, Tokens: &ipc.Tokens{Input: 8000, Output: 4100, CacheRead: 52000}},
		{ID: "s3", Name: "Checkout page copy", State: "needs_you", Reason: "permission", Detail: "Asks: may it run npm install?", Created: ago(18 * time.Minute),
			Repo: "/home/alice/src/shop", Branch: brand.Slug + "/checkout-copy", Profile: "claude", Hue: 2, Group: "g1", Tokens: &ipc.Tokens{Input: 3100, Output: 900}},
		{ID: "s4", Name: "Upgrade the date library", State: "done", Created: ago(3 * time.Hour), Repo: "/home/alice/src/shop", Branch: brand.Slug + "/upgrade-dates",
			Profile: "sonnet", Hue: 3, Group: "g1", Loose: &ipc.Loose{Ahead: 2, Land: "main"}, Tokens: &ipc.Tokens{Input: 12000, Output: 5300, CacheRead: 98000}},
		{ID: "s5", Name: "Nightly report", State: "idle", Created: ago(26 * time.Hour), Cwd: "/home/alice/reports", Profile: "haiku", Hue: 5},
		{ID: "s6", Name: "Refactor the cart", State: "frozen", Created: ago(5 * time.Hour), Repo: "/home/alice/src/shop", Branch: brand.Slug + "/refactor-cart",
			Profile: "claude", Hue: 6, Frozen: &now},
		{ID: "s7", Name: "Migrate the tests", State: "stalled", Reason: "stalled", Detail: "No output for 12 minutes", Created: ago(50 * time.Minute),
			Repo: "/home/alice/src/" + brand.Slug, Branch: brand.Slug + "/migrate-tests", Profile: "local", Local: true, Hue: 1},
		{ID: "studio/s1", Name: "Benchmark the local model", State: "working", Detail: "on studio · Running the bench", Created: ago(9 * time.Minute),
			Host: "studio", Profile: "local", Local: true, Hue: 3},
		{ID: "s8", Name: "Old spike", State: "done", Created: ago(72 * time.Hour), Parked: true, Profile: "claude", Hue: 2},
	}
	for i := range c.rows {
		c.rows[i].Order = i
	}
	c.header = ipc.Header{Version: "1.4.0", Groups: []ipc.Group{{ID: "g1", Name: "Shop"}}}
	c.setTheme("rosewater")
	c.outside = []ipc.OutsideSession{{VendorID: "5c1d0b7e", Name: "scratch idea", Kind: "interactive", Status: "waiting", WaitingFor: "permission prompt", Cwd: "/home/alice/scratch", Movable: true}}
	c.closed = []ipc.ClosedSession{{ID: "c1", Name: "Rename the CLI flags", Closed: ago(2 * time.Hour), Auto: true}}
	return c
}

// setTheme names the palette, without its colours: the page then draws in
// its own, Evergreen. The palettes live in internal/ui, which a fixture
// that runs on Windows can't import.
func (c *Core) setTheme(name string) {
	c.header.Theme = &ipc.Theme{Name: name}
}

func (c *Core) publish(f ipc.Frame) {
	for ch := range c.subs {
		select {
		case ch <- f:
		default:
		}
	}
}

func (c *Core) find(id string) int {
	for i := range c.rows {
		if c.rows[i].ID == id {
			return i
		}
	}
	return -1
}

// change runs f on a session under the lock and publishes its upsert.
func (c *Core) change(id, undo string, f func(*ipc.Session)) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.find(id)
	if i < 0 {
		return ipc.Errorf(ipc.CodeNotFound, "There is no session %s.", id)
	}
	f(&c.rows[i])
	s := c.rows[i]
	c.publish(ipc.Frame{T: ipc.FrameUpsert, Session: &s})
	if undo != "" {
		c.undo = fmt.Sprintf("%s %q", undo, s.Name)
		c.publish(ipc.Frame{T: ipc.FrameHistory, Undo: c.undo})
	}
	return nil
}

// Call answers an op as the core would, or near enough for a picture.
func (c *Core) Call(op string, raw json.RawMessage) (any, error) {
	var a struct {
		Session string `json:"session"`
		Name    string `json:"name"`
		Text    string `json:"text"`
		Theme   string `json:"theme"`
		Before  string `json:"before"`
		Level   int    `json:"level"`
	}
	json.Unmarshal(raw, &a)
	switch op {
	case ipc.OpHello:
		return ipc.HelloResult{Version: "1.4.0", API: ipc.API, Features: []string{ipc.OpInput, ipc.OpArch, ipc.OpPlugins, ipc.OpReady}}, nil
	case ipc.OpAttachInfo:
		return ipc.AttachInfoResult{Tmux: "/usr/bin/tmux", Socket: "/tmp/" + brand.Slug + "-1000/tmux", Target: brand.Slug}, nil
	case ipc.OpShow:
		c.mu.Lock()
		for i := range c.rows {
			was := c.rows[i].Shown
			c.rows[i].Shown = c.rows[i].ID == a.Session
			if was != c.rows[i].Shown {
				s := c.rows[i]
				c.publish(ipc.Frame{T: ipc.FrameUpsert, Session: &s})
			}
		}
		c.mu.Unlock()
		return struct{}{}, nil
	case ipc.OpPark, ipc.OpUnpark:
		return struct{}{}, c.change(a.Session, op, func(s *ipc.Session) { s.Parked = op == ipc.OpPark })
	case ipc.OpPin, ipc.OpUnpin:
		return struct{}{}, c.change(a.Session, op, func(s *ipc.Session) { s.Pinned = op == ipc.OpPin })
	case ipc.OpFreeze, ipc.OpThaw:
		return ipc.FreezeResult{Sessions: []string{a.Session}}, c.change(a.Session, op, func(s *ipc.Session) {
			if op == ipc.OpFreeze {
				now := time.Now()
				s.Frozen, s.State = &now, "frozen"
			} else {
				s.Frozen, s.State = nil, "working"
			}
		})
	case ipc.OpPriority:
		return struct{}{}, c.change(a.Session, "boost", func(s *ipc.Session) { s.Priority = a.Level })
	case ipc.OpRename:
		return ipc.RenameResult{}, c.change(a.Session, "", func(s *ipc.Session) { s.Name = a.Name })
	case ipc.OpClose:
		c.mu.Lock()
		defer c.mu.Unlock()
		i := c.find(a.Session)
		if i < 0 {
			return nil, ipc.Errorf(ipc.CodeNotFound, "There is no session %s.", a.Session)
		}
		s := c.rows[i]
		c.rows = append(c.rows[:i], c.rows[i+1:]...)
		c.closed = append([]ipc.ClosedSession{{ID: s.ID, Name: s.Name, Closed: time.Now()}}, c.closed...)
		c.publish(ipc.Frame{T: ipc.FrameRemove, ID: s.ID})
		c.publish(ipc.Frame{T: ipc.FrameClosed, Closed: c.closed})
		return ipc.CloseResult{Sessions: []string{s.ID}}, nil
	case ipc.OpUndo, ipc.OpRedo:
		return ipc.UndoResult{Label: c.undo, Did: ipc.DidUndo}, nil
	case ipc.OpReorder:
		return struct{}{}, nil
	case ipc.OpTheme:
		c.mu.Lock()
		c.setTheme(a.Theme)
		h := c.header
		c.publish(ipc.Frame{T: ipc.FrameHeader, Header: &h})
		c.mu.Unlock()
		return struct{}{}, nil
	case ipc.OpComplete:
		if strings.HasPrefix(a.Text, "+") {
			return ipc.CompleteResult{Items: []string{"+claude", "+sonnet", "+haiku", "+local"}, Notes: []string{"Claude Code on Opus", "Claude Code on Sonnet", "Claude Code on Haiku", "A local model"}}, nil
		}
		return ipc.CompleteResult{Items: []string{"@shop", "@irie", "@site"}}, nil
	case ipc.OpInput:
		return c.input(a.Text)
	case ipc.OpConfig:
		return ipc.ConfigResult{Path: "/home/alice/.config/" + brand.Slug + "/config.toml", Config: map[string]any{
			"spawn": map[string]any{"default_profile": "claude", "close_when_done": false},
			"ui":    map[string]any{"theme": "rosewater", "red_on_done": false, "new_tabs": "last"},
		}}, nil
	case ipc.OpReady:
		return map[string]any{"items": []any{}}, nil
	case ipc.OpArch:
		return ipc.ArchResult{Agents: []ipc.ArchAgent{
			{Name: "mikael", Icon: "🛡️", Title: "Stands with you", About: "Weighs a call that could do harm before it runs.", On: true, Status: "Watching 7 sessions."},
			{Name: "gabriel", Icon: "🌿", Title: "Your focus first", About: "Gives the session you look at the machine first.", On: true, Status: "Nothing held back: the machine has room.",
				Actions: []ipc.ArchAction{{Label: "Boost for 1 hour", Op: "gabriel", Done: "Boosted for an hour."}}},
		}}, nil
	case ipc.OpPlugins:
		return ipc.PluginsResult{Plugins: []ipc.Plugin{{Name: "privacy", Title: "Privacy", Summary: "Research without being followed.", On: true,
			Parts: []ipc.PluginPart{{Name: "vpn", Title: "Mullvad VPN", Summary: "A VPN you drive from " + brand.Name + ".", Switch: "vpn.on", On: false, State: "off", Detail: "Off: turn it on to drive Mullvad from " + brand.Name + "."}}}}}, nil
	}
	return struct{}{}, nil
}

func (c *Core) input(text string) (any, error) {
	t := strings.TrimSpace(text)
	switch {
	case strings.HasPrefix(strings.ToLower(t), "hows"), strings.HasPrefix(strings.ToLower(t), "how's"):
		return ipc.InputResult{Did: "answered", Text: "Fix the flaky parser test is running the tests, 42 minutes in."}, nil
	case strings.HasPrefix(t, ":"):
		return ipc.InputResult{Did: "ran", Text: "Done."}, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	s := ipc.Session{ID: fmt.Sprintf("s%d", c.next), Name: firstLine(t), State: "starting", Created: time.Now(), Order: len(c.rows), Profile: "claude", Hue: c.next%6 + 1}
	c.rows = append(c.rows, s)
	c.publish(ipc.Frame{T: ipc.FrameUpsert, Session: &s})
	return ipc.InputResult{Did: "spawned", Op: ipc.OpSpawn, Session: &s, Text: "Started “" + s.Name + "”."}, nil
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

// Subscribe sends a snapshot and what came with it, then each change.
func (c *Core) Subscribe(json.RawMessage) (<-chan ipc.Frame, func(), error) {
	ch := make(chan ipc.Frame, 256)
	c.mu.Lock()
	rows := append([]ipc.Session(nil), c.rows...)
	h := c.header
	ch <- ipc.Frame{T: ipc.FrameSnapshot, Sessions: rows, Header: &h}
	ch <- ipc.Frame{T: ipc.FrameOutside, Outside: c.outside}
	ch <- ipc.Frame{T: ipc.FrameClosed, Closed: c.closed}
	ch <- ipc.Frame{T: ipc.FrameHistory, Undo: c.undo}
	c.subs[ch] = true
	c.mu.Unlock()
	return ch, func() {
		c.mu.Lock()
		delete(c.subs, ch)
		c.mu.Unlock()
	}, nil
}
