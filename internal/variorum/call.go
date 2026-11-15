package variorum

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/version"
)

// Variorum logs every call to its door, reads too, and keeps only its last
// 500 lines across all its clients: so Irie calls only when someone asks,
// never on a timer, and each helper here makes as few calls as it can.

// protocolVersion is the MCP version Irie speaks.
const protocolVersion = "2025-06-18"

// callTimeout bounds each exchange with the door: the initialize, and each
// call. Tests shorten it.
var callTimeout = 10 * time.Second

// closeWait is how long the door has to end once its input closes.
var closeWait = 2 * time.Second

// maxLine is the longest line Irie reads from the door.
const maxLine = 8 << 20

// A read follows next_cursor up to maxBlocks blocks or maxPages pages,
// whichever comes first, and then says there is more: each page is a call,
// and a line of Variorum's log. Tests lower them.
var (
	maxBlocks = 1000
	maxPages  = 10
)

// pageSize is the most blocks or notes a call asks for, the most Variorum
// gives.
const pageSize = 200

var (
	errTimeout    = errors.New("Variorum didn't answer within ten seconds.")
	errClosed     = errors.New("Variorum's door closed before it answered.")
	errLong       = errors.New("Variorum's answer ran past 8 MiB, more than " + brand.Name + " reads.")
	errUnreadable = errors.New(brand.Name + " couldn't read Variorum's answer.")
)

// Env is the whole environment the door's program gets: a home, a plain
// PATH and the key, and nothing else of Irie's or its caller's.
func Env(key string) []string {
	return []string{"HOME=" + home(), "PATH=/usr/bin:/bin", KeyVar + "=" + key}
}

func home() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	if u, err := user.Current(); err == nil && u.HomeDir != "" {
		return u.HomeDir
	}
	return "/"
}

// conn is one connection to the door: its program, started with the
// setup's key, spoken to as an MCP client named Irie's slug.
type conn struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *os.File
	lines  chan line
	quit   chan struct{} // closed when the connection closes
	exited chan struct{} // closed once the door has ended, and its stderr is read
	stderr *tail
	next   int // the last request's id
}

// line is one line the door wrote, or why there are no more.
type line struct {
	b   []byte
	err error
}

// open checks the setup's program, starts it and says initialize.
func (s Setup) open(ctx context.Context) (*conn, error) {
	if err := Check(s.Command); err != nil {
		return nil, err
	}
	cmd := exec.Command(s.Command)
	cmd.Env = Env(s.Key)
	cmd.WaitDelay = time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	// A pipe of its own for stdout, not StdoutPipe's, which Wait closes
	// while the door's last answer may still be in it.
	stdout, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	c := &conn{cmd: cmd, stdin: stdin, stdout: stdout, lines: make(chan line), quit: make(chan struct{}), exited: make(chan struct{}), stderr: &tail{}}
	cmd.Stdout, cmd.Stderr = w, c.stderr
	err = cmd.Start()
	w.Close()
	if err != nil {
		stdout.Close()
		return nil, fmt.Errorf("%s couldn't start Variorum's door: %v.", brand.Name, err)
	}
	go c.read()
	go func() { cmd.Wait(); close(c.exited) }()
	if _, err := c.request(ctx, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": brand.Slug, "version": version.Base},
	}); err != nil {
		c.close(false)
		return nil, err
	}
	if err := c.write(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}); err != nil {
		c.close(false)
		return nil, c.ended()
	}
	return c, nil
}

// read passes each line the door writes to c.lines, until it ends or c
// closes.
func (c *conn) read() {
	sc := bufio.NewScanner(c.stdout)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	send := func(l line) bool {
		select {
		case c.lines <- l:
			return true
		case <-c.quit:
			return false
		}
	}
	for sc.Scan() {
		if !send(line{b: bytes.Clone(sc.Bytes())}) {
			return
		}
	}
	err := sc.Err()
	if err == nil {
		err = io.EOF
	}
	send(line{err: err})
}

func (c *conn) write(msg any) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = c.stdin.Write(append(b, '\n'))
	return err
}

// request sends one request and waits for its answer, skipping the
// notifications and answers to anything else that come first. An error in
// answer is the door's own sentence.
func (c *conn) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.next++
	id := c.next
	if err := c.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, c.ended()
	}
	timer := time.NewTimer(callTimeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, errors.New(brand.Name + " stopped waiting for Variorum.")
		case <-timer.C:
			return nil, errTimeout
		case l := <-c.lines:
			switch {
			case errors.Is(l.err, bufio.ErrTooLong):
				return nil, errLong
			case l.err != nil:
				return nil, c.ended()
			}
			var m struct {
				ID     any             `json:"id"`
				Method string          `json:"method"`
				Result json.RawMessage `json:"result"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal(l.b, &m) != nil || m.Method != "" || m.ID != float64(id) {
				continue
			}
			if m.Error != nil {
				if m.Error.Message == "" {
					return nil, errors.New("Variorum refused, and didn't say why.")
				}
				return nil, errors.New(m.Error.Message)
			}
			return m.Result, nil
		}
	}
}

// ended is why the door stopped before it answered: its own last words on
// stderr, when it said any, as it does when Variorum ends the connection.
func (c *conn) ended() error {
	// The door may still be writing them as its output ends.
	select {
	case <-c.exited:
	case <-time.After(closeWait):
	}
	if w := c.stderr.last(); w != "" {
		return errors.New(w)
	}
	return errClosed
}

// call calls one of the door's tools and returns its answer's text, which
// is JSON, or with isError the sentence to show.
func (c *conn) call(ctx context.Context, tool string, args map[string]any) ([]byte, error) {
	res, err := c.request(ctx, "tools/call", map[string]any{"name": tool, "arguments": args})
	if err != nil {
		return nil, err
	}
	var r struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if json.Unmarshal(res, &r) != nil {
		return nil, errUnreadable
	}
	text, found := "", false
	for _, x := range r.Content {
		if x.Type == "text" {
			text, found = x.Text, true
			break
		}
	}
	switch {
	case r.IsError && text != "":
		return nil, errors.New(text)
	case r.IsError:
		return nil, errors.New("Variorum refused, and didn't say why.")
	case !found:
		return nil, errUnreadable
	}
	return []byte(text), nil
}

// close ends the connection: it closes the door's input, which ends it, and
// waits for it to go, or kills it at once when kind is false or after a
// while.
func (c *conn) close(kind bool) {
	c.stdin.Close()
	close(c.quit)
	ended := false
	if kind {
		select {
		case <-c.exited:
			ended = true
		case <-time.After(closeWait):
		}
	}
	if !ended {
		c.cmd.Process.Kill()
		<-c.exited
	}
	// Whatever else holds the door's output open, the read ends here.
	c.stdout.Close()
}

// session opens a connection, runs f on it and closes it.
func (s Setup) session(ctx context.Context, f func(*conn) error) error {
	c, err := s.open(ctx)
	if err != nil {
		return err
	}
	err = f(c)
	c.close(err == nil)
	return err
}

// Notes are the notes shared with this setup's client, most recently
// edited first: the first page, of up to 200.
func (s Setup) Notes(ctx context.Context) ([]ipc.VariorumNoteRef, error) {
	var out []ipc.VariorumNoteRef
	err := s.session(ctx, func(c *conn) error {
		text, err := c.call(ctx, "list_notes", map[string]any{"limit": pageSize})
		if err != nil {
			return err
		}
		var page struct {
			Notes []struct {
				ID     string          `json:"id"`
				Title  string          `json:"title"`
				Edited json.RawMessage `json:"edited"`
			} `json:"notes"`
		}
		if json.Unmarshal(text, &page) != nil {
			return errUnreadable
		}
		out = []ipc.VariorumNoteRef{}
		for _, n := range page.Notes {
			out = append(out, ipc.VariorumNoteRef{ID: n.ID, Title: n.Title, Edited: str(n.Edited)})
		}
		return nil
	})
	return out, err
}

// str is a JSON value as text: a string as it is, anything else as written.
func str(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	if v := strings.TrimSpace(string(raw)); v != "null" {
		return v
	}
	return ""
}

// readPage is one page of read_note's answer with with_ids.
type readPage struct {
	NoteID string `json:"note_id"`
	Title  string `json:"title"`
	Blocks []struct {
		BlockID string `json:"block_id"`
		Name    string `json:"name"`
		Shown   string `json:"shown_take_id"`
		Takes   []struct {
			TakeID   string `json:"take_id"`
			Label    string `json:"label"`
			Text     string `json:"text"`
			Chosen   bool   `json:"chosen"`
			Archived bool   `json:"archived"`
		} `json:"takes"`
		Comments []struct {
			CommentID string `json:"comment_id"`
			Text      string `json:"text"`
		} `json:"comments"`
	} `json:"blocks"`
	NextCursor string `json:"next_cursor"`
}

// Read reads the note noteID as Variorum holds it now: each block with its
// takes, those archived left out, and its comments. It follows next_cursor
// over one connection up to 1000 blocks or ten pages, and then says there
// is more.
func (s Setup) Read(ctx context.Context, noteID string) (*ipc.VariorumNote, error) {
	note := &ipc.VariorumNote{ID: noteID, Blocks: []ipc.VariorumBlock{}, Read: time.Now().UTC()}
	err := s.session(ctx, func(c *conn) error {
		cursor := ""
		for pages := 1; ; pages++ {
			args := map[string]any{"note_id": noteID, "with_ids": true, "limit": pageSize}
			if cursor != "" {
				args["cursor"] = cursor
			}
			text, err := c.call(ctx, "read_note", args)
			if err != nil {
				return err
			}
			var p readPage
			if json.Unmarshal(text, &p) != nil {
				return errUnreadable
			}
			if pages == 1 {
				note.Title = p.Title
				if p.NoteID != "" {
					note.ID = p.NoteID
				}
			}
			for _, b := range p.Blocks {
				blk := ipc.VariorumBlock{ID: b.BlockID, Name: b.Name, Shown: b.Shown, Takes: []ipc.VariorumTake{}}
				for _, t := range b.Takes {
					if !t.Archived {
						blk.Takes = append(blk.Takes, ipc.VariorumTake{ID: t.TakeID, Label: t.Label, Text: t.Text, Chosen: t.Chosen})
					}
				}
				for _, m := range b.Comments {
					blk.Comments = append(blk.Comments, ipc.VariorumComment{ID: m.CommentID, Text: m.Text})
				}
				note.Blocks = append(note.Blocks, blk)
			}
			cursor = p.NextCursor
			switch {
			case len(note.Blocks) > maxBlocks:
				note.Blocks, note.More = note.Blocks[:maxBlocks], true
				return nil
			case cursor == "":
				return nil
			case len(note.Blocks) == maxBlocks || pages == maxPages:
				note.More = true
				return nil
			}
		}
	})
	if err != nil {
		return nil, err
	}
	return note, nil
}

// What Show shows: a note, or a block's takes.
const (
	ShowNote  = "note"
	ShowTakes = "takes"
)

// Show asks Variorum to show the writer a note, at blockID when it is not
// empty, or with target takes a block's takes. Variorum offers it, or moves
// its view at once if the writer set it to; it never raises a window. It
// answers shown or offered.
func (s Setup) Show(ctx context.Context, target, noteID, blockID string) (string, error) {
	if target == "" {
		target = ShowNote
	}
	switch {
	case target != ShowNote && target != ShowTakes:
		return "", fmt.Errorf("%s shows a note or a paragraph's takes in Variorum, nothing else.", brand.Name)
	case noteID == "":
		return "", errors.New("Show in Variorum needs a note.")
	case target == ShowTakes && blockID == "":
		return "", errors.New("Showing a paragraph's takes needs the paragraph.")
	}
	args := map[string]any{"target": target, "note_id": noteID}
	if blockID != "" {
		args["block_id"] = blockID
	}
	var out string
	err := s.session(ctx, func(c *conn) error {
		text, err := c.call(ctx, "show_in_app", args)
		if err != nil {
			return err
		}
		var r struct {
			Shown   bool `json:"shown"`
			Offered bool `json:"offered"`
		}
		switch {
		case json.Unmarshal(text, &r) != nil:
			return errUnreadable
		case r.Shown:
			out = "shown"
		case r.Offered:
			out = "offered"
		default:
			return errors.New("Variorum didn't say whether it showed it.")
		}
		return nil
	})
	return out, err
}

// tail keeps the end of what the door writes on stderr.
type tail struct {
	mu sync.Mutex
	b  []byte
}

const tailMax = 4 << 10

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > tailMax {
		t.b = t.b[len(t.b)-tailMax:]
	}
	return len(p), nil
}

// last is its last line that says anything.
func (t *tail) last() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	lines := strings.Split(strings.TrimSpace(string(t.b)), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
