// Package ipc is Irie's socket protocol (DESIGN §2.4): NDJSON over a unix
// stream socket, one object per line.
//
//	request: {"id":1,"op":"spawn","args":{…}}
//	reply:   {"id":1,"ok":true,"result":{…}} or {"id":1,"ok":false,"error":{"code":"…","msg":"…"}}
//
// After a subscribe reply the connection carries frames until the client
// closes it.
package ipc

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

// MaxMessage is the longest line either side accepts. Tasks have no length
// cap of their own, so this is generous.
const MaxMessage = 64 << 20

type Request struct {
	ID   int64           `json:"id"`
	Op   string          `json:"op"`
	Args json.RawMessage `json:"args,omitempty"`
}

type Reply struct {
	ID     int64           `json:"id"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

// Error is a failed request.
type Error struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
}

func (e *Error) Error() string { return e.Msg }

// Errorf returns an *Error with the given code.
func Errorf(code, format string, a ...any) *Error {
	return &Error{Code: code, Msg: fmt.Sprintf(format, a...)}
}

func asError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{Code: CodeInternal, Msg: err.Error()}
}

var errTooLong = errors.New("ipc: message too long")

func readLine(r *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		chunk, err := r.ReadSlice('\n')
		line = append(line, chunk...)
		switch {
		case err == nil:
			return line, nil
		case err != bufio.ErrBufferFull:
			return nil, err
		case len(line) > MaxMessage:
			return nil, errTooLong
		}
	}
}

// lineWriter writes one JSON value per line; it is safe for concurrent use.
type lineWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (lw *lineWriter) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	lw.mu.Lock()
	defer lw.mu.Unlock()
	_, err = lw.w.Write(append(b, '\n'))
	return err
}

// Listen creates the unix socket at path with mode 0600.
func Listen(path string) (net.Listener, error) {
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		l.Close()
		return nil, err
	}
	return l, nil
}

// Handler answers requests.
type Handler interface {
	// Call answers one request. A returned *Error goes back as it is; any
	// other error becomes an internal error.
	Call(op string, args json.RawMessage) (any, error)
	// Subscribe starts a stream of frames. cancel is called once the client
	// has gone; after that frames is no longer read.
	Subscribe(args json.RawMessage) (frames <-chan Frame, cancel func(), err error)
}

// Serve accepts connections on l until l is closed.
func Serve(l net.Listener, h Handler) error {
	for {
		c, err := l.Accept()
		if errors.Is(err, net.ErrClosed) {
			return nil
		}
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		go serveConn(c, h)
	}
}

func serveConn(c net.Conn, h Handler) {
	defer c.Close()
	r := bufio.NewReader(c)
	w := &lineWriter{w: c}
	for {
		line, err := readLine(r)
		if err != nil {
			return
		}
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			w.write(Reply{Error: Errorf(CodeBadRequest, "%v", err)})
			continue
		}
		if req.Op == OpSubscribe {
			frames, cancel, err := h.Subscribe(req.Args)
			if err != nil {
				w.write(Reply{ID: req.ID, Error: asError(err)})
				continue
			}
			if w.write(Reply{ID: req.ID, OK: true, Result: json.RawMessage(`{}`)}) == nil {
				stream(r, w, frames)
			}
			cancel()
			return
		}
		res, err := h.Call(req.Op, req.Args)
		if err != nil {
			w.write(Reply{ID: req.ID, Error: asError(err)})
			continue
		}
		if res == nil {
			res = struct{}{}
		}
		b, err := json.Marshal(res)
		if err != nil {
			w.write(Reply{ID: req.ID, Error: asError(err)})
			continue
		}
		if w.write(Reply{ID: req.ID, OK: true, Result: b}) != nil {
			return
		}
	}
}

func stream(r *bufio.Reader, w *lineWriter, frames <-chan Frame) {
	gone := make(chan struct{})
	go func() {
		io.Copy(io.Discard, r)
		close(gone)
	}()
	for {
		select {
		case f, ok := <-frames:
			if !ok || w.write(f) != nil {
				return
			}
		case <-gone:
			return
		}
	}
}

// Client is one connection to the core.
type Client struct {
	conn    net.Conn
	r       *bufio.Reader
	w       *lineWriter
	mu      sync.Mutex
	next    int64
	done    chan struct{}
	closing sync.Once

	// Timeout bounds each Call.
	Timeout time.Duration
}

// Dial connects to the socket at path.
func Dial(path string, timeout time.Duration) (*Client, error) {
	c, err := net.DialTimeout("unix", path, timeout)
	if err != nil {
		return nil, err
	}
	return &Client{conn: c, r: bufio.NewReader(c), w: &lineWriter{w: c}, done: make(chan struct{}), Timeout: 30 * time.Second}, nil
}

// Close closes the connection and ends a subscription.
func (c *Client) Close() error {
	c.closing.Do(func() { close(c.done) })
	return c.conn.Close()
}

// Call sends one request and decodes the reply's result into result, which
// may be nil.
func (c *Client) Call(op string, args, result any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	req := Request{ID: c.next, Op: op}
	if args != nil {
		b, err := json.Marshal(args)
		if err != nil {
			return err
		}
		req.Args = b
	}
	c.conn.SetDeadline(time.Now().Add(c.Timeout))
	defer c.conn.SetDeadline(time.Time{})
	if err := c.w.write(req); err != nil {
		return err
	}
	for {
		line, err := readLine(c.r)
		if err != nil {
			return err
		}
		var rep Reply
		if err := json.Unmarshal(line, &rep); err != nil {
			return err
		}
		if rep.ID != req.ID {
			continue
		}
		if !rep.OK {
			if rep.Error == nil {
				return Errorf(CodeInternal, "%s failed", op)
			}
			return rep.Error
		}
		if result == nil || len(rep.Result) == 0 {
			return nil
		}
		return json.Unmarshal(rep.Result, result)
	}
}

// Subscribe asks for a stream of frames. The connection then carries only
// frames; the channel closes when the connection does.
func (c *Client) Subscribe(args any) (<-chan Frame, error) {
	if err := c.Call(OpSubscribe, args, nil); err != nil {
		return nil, err
	}
	ch := make(chan Frame, 64)
	go func() {
		defer close(ch)
		for {
			line, err := readLine(c.r)
			if err != nil {
				return
			}
			var f Frame
			if json.Unmarshal(line, &f) != nil {
				continue
			}
			select {
			case ch <- f:
			case <-c.done:
				return
			}
		}
	}()
	return ch, nil
}

// Call dials path, sends one request and closes the connection. timeout
// bounds both the dial and the call.
func Call(path string, timeout time.Duration, op string, args, result any) error {
	c, err := Dial(path, timeout)
	if err != nil {
		return err
	}
	defer c.Close()
	c.Timeout = timeout
	return c.Call(op, args, result)
}
