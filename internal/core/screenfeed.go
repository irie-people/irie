package core

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/ipc"
)

// screenEvery is how often the terminal of a session that a client watches
// live is read (ipc.SubscribeArgs.Screen), for the phone app's Term view.
// Tests may shorten it.
var screenEvery = 250 * time.Millisecond

// pokeWait is how long a read waits after a key, for the agent to draw
// what the key did.
const pokeWait = 40 * time.Millisecond

// screenFormat asks tmux for a pane's size and its cursor, ahead of its
// lines.
const screenFormat = "#{pane_width} #{pane_height} #{cursor_x} #{cursor_y} #{cursor_flag}"

// screenFeed reads one session's terminal while clients watch it live.
// Each read that differs from the one before is a new version, which every
// watching subscriber sends on as the lines that changed for its client.
// Its fields are guarded by Core.mu.
type screenFeed struct {
	subs int           // the subscriptions that watch it
	ver  uint64        // counts the reads that changed what it shows
	cur  ipc.Screen    // the newest read: Rows lines, never changed in place
	poke chan struct{} // reads soon, as after a key
	stop chan struct{} // closed once the last subscription ends
}

// watchScreen counts one more subscription that watches x's terminal, and
// starts reading it for the first. The caller holds c.mu.
func (c *Core) watchScreen(x *session) *screenFeed {
	f := x.screen
	if f == nil {
		f = &screenFeed{poke: make(chan struct{}, 1), stop: make(chan struct{})}
		x.screen = f
		go c.readScreens(x.id, f)
	}
	f.subs++
	return f
}

// unwatchScreen counts one subscription less, and stops reading once it
// was the last. The caller holds c.mu.
func (c *Core) unwatchScreen(x *session, f *screenFeed) {
	if x.screen != f {
		return
	}
	if f.subs--; f.subs > 0 {
		return
	}
	close(f.stop)
	x.screen = nil
}

// pokeScreen has x's terminal read soon for those who watch it, after
// something pressed a key or typed there. The caller holds c.mu.
func pokeScreen(x *session) {
	if x.screen == nil {
		return
	}
	select {
	case x.screen.poke <- struct{}{}:
	default:
	}
}

// readScreens reads a watched terminal, one read at a time, until its last
// watcher leaves or its session closes. A read that tmux is slow to answer
// only puts the next one off.
func (c *Core) readScreens(id string, f *screenFeed) {
	t := time.NewTimer(0)
	defer t.Stop()
	for {
		select {
		case <-f.stop:
			return
		case <-f.poke:
			select {
			case <-time.After(pokeWait):
			case <-f.stop:
				return
			}
		case <-t.C:
		}
		if !c.readScreen(id, f) {
			return
		}
		t.Reset(screenEvery)
	}
}

// readScreen reads id's terminal once and, when it shows something new,
// tells the subscriptions that watch it. It is false once there is no
// terminal left to read for f.
func (c *Core) readScreen(id string, f *screenFeed) bool {
	c.mu.Lock()
	x := c.sessions[id]
	if x == nil || x.screen != f {
		c.mu.Unlock()
		return false
	}
	pane, asks := x.pane, x.wire().State == ipc.StateNeedsYou
	c.mu.Unlock()
	// One command list, so the size and the lines are of the same moment.
	// -N keeps the spaces a line was written with, so a background drawn
	// to the edge, as Claude Code draws your message, stays as wide.
	out, err := c.tmux.RunList([][]string{
		{"display-message", "-p", "-t", pane, screenFormat},
		{"capture-pane", "-p", "-e", "-N", "-t", pane},
	})
	c.perf.Add("screen.read", 1)
	if err != nil {
		return true // a busy tmux: the next read tries again
	}
	scr, ok := parseScreen(out)
	if !ok {
		return true
	}
	scr.Options = []ipc.ScreenOption{}
	if asks {
		scr.Options = promptOptions(screenLines(plainText(strings.Join(scr.Lines, "\n"))))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sessions[id] != x || x.screen != f {
		return false
	}
	if f.ver > 0 && sameScreen(f.cur, scr) {
		return true
	}
	f.cur = scr
	f.ver++
	for s := range c.subs {
		if s.feed == f {
			s.noteScreen()
		}
	}
	return true
}

// parseScreen reads what readScreen's command list printed: the pane's
// size and cursor on the first line, then its lines, made exactly as many
// as it has rows.
func parseScreen(out string) (ipc.Screen, bool) {
	head, body, _ := strings.Cut(out, "\n")
	fields := strings.Fields(head)
	if len(fields) != 5 {
		return ipc.Screen{}, false
	}
	var n [5]int
	for i, s := range fields {
		v, err := strconv.Atoi(s)
		if err != nil || v < 0 {
			return ipc.Screen{}, false
		}
		n[i] = v
	}
	cols, rows := n[0], n[1]
	if cols == 0 || rows == 0 {
		return ipc.Screen{}, false
	}
	// capture-pane ends the last line with a newline too.
	lines := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	if len(lines) > rows {
		lines = lines[:rows]
	}
	for len(lines) < rows {
		lines = append(lines, "")
	}
	scr := ipc.Screen{Cols: cols, Rows: rows, Lines: lines}
	if n[4] != 0 {
		scr.Cursor = &ipc.ScreenCursor{X: n[2], Y: n[3]}
	}
	return scr, true
}

// escape matches what a line of capture-pane -e may hold besides text: a
// control sequence, such as SGR, an operating system command, such as a
// hyperlink, a character set's designation, or another escape of two
// characters.
var escape = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\)?|[()*+][ -~]|[@-Z\\-_])`)

// plainText is s without its escapes: what a person reads on the terminal.
func plainText(s string) string {
	if !strings.Contains(s, "\x1b") {
		return s
	}
	return escape.ReplaceAllString(s, "")
}

// sameScreen says whether two reads show the same.
func sameScreen(a, b ipc.Screen) bool {
	if a.Cols != b.Cols || a.Rows != b.Rows || (a.Cursor == nil) != (b.Cursor == nil) {
		return false
	}
	if a.Cursor != nil && *a.Cursor != *b.Cursor {
		return false
	}
	return slices.Equal(a.Lines, b.Lines) && slices.Equal(a.Options, b.Options)
}
