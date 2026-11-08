package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// A WebDriver BiDi client (W3C WebDriver BiDi: its WebSocket transport and
// its messages): commands go out as {id, method, params} and each reply
// carries its command's id, with type "success" or "error". Events carry no
// id and are passed over: polling the page is enough for what the tools do.

type bidi struct {
	ws *wsConn

	mu      sync.Mutex
	next    int64
	pending map[int64]chan bidiReply
	err     error // why the connection ended; nil while it is open
	done    chan struct{}
}

type bidiReply struct {
	Type    string          `json:"type"` // success | error | event
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   string          `json:"error"`
	Message string          `json:"message"`
}

// bidiError is an error the browser returned for a command, such as
// "no such frame" or "unknown error".
type bidiError struct {
	Method, Code, Message string
}

func (e *bidiError) Error() string {
	if e.Message == "" {
		return e.Method + ": " + e.Code
	}
	return e.Method + ": " + e.Code + ": " + e.Message
}

// errClosed is the error of a call on a connection that has ended.
var errClosed = errors.New("the browser's connection closed")

// connect opens a BiDi connection to the Remote Agent at addr (host:port).
// A session still has to be made with session.new.
func connect(addr string, timeout time.Duration) (*bidi, error) {
	ws, err := dialWS(addr, "/session", timeout)
	if err != nil {
		return nil, err
	}
	b := &bidi{ws: ws, pending: map[int64]chan bidiReply{}, done: make(chan struct{})}
	go b.readLoop()
	return b, nil
}

func (b *bidi) readLoop() {
	for {
		msg, err := b.ws.ReadMessage()
		if err != nil {
			if err == io.EOF {
				err = errClosed
			}
			b.end(err)
			return
		}
		var r bidiReply
		if json.Unmarshal(msg, &r) != nil || r.ID == 0 {
			continue // an event, or nothing we asked for
		}
		b.mu.Lock()
		ch := b.pending[r.ID]
		delete(b.pending, r.ID)
		b.mu.Unlock()
		if ch != nil {
			ch <- r
		}
	}
}

// end records why the connection ended and wakes every waiting call.
func (b *bidi) end(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.err != nil {
		return
	}
	b.err = fmt.Errorf("%w: %v", errClosed, err)
	if errors.Is(err, errClosed) {
		b.err = errClosed
	}
	close(b.done)
}

// alive reports whether the connection is still open.
func (b *bidi) alive() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.err == nil
}

// call sends one command and waits for its reply, which it decodes into
// result when result is not nil. It gives up when ctx ends; a reply that
// comes later is dropped.
func (b *bidi) call(ctx context.Context, method string, params, result any) error {
	if params == nil {
		params = struct{}{}
	}
	b.mu.Lock()
	if b.err != nil {
		err := b.err
		b.mu.Unlock()
		return err
	}
	b.next++
	id := b.next
	ch := make(chan bidiReply, 1)
	b.pending[id] = ch
	b.mu.Unlock()
	forget := func() {
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
	}

	msg, err := json.Marshal(struct {
		ID     int64  `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params"`
	}{id, method, params})
	if err == nil {
		err = b.ws.WriteText(msg)
	}
	if err != nil {
		forget()
		return err
	}
	select {
	case r := <-ch:
		if r.Type == "error" {
			return &bidiError{Method: method, Code: r.Error, Message: r.Message}
		}
		if result != nil {
			return json.Unmarshal(r.Result, result)
		}
		return nil
	case <-b.done:
		b.mu.Lock()
		err := b.err
		b.mu.Unlock()
		return err
	case <-ctx.Done():
		forget()
		return fmt.Errorf("%s: %w", method, ctx.Err())
	}
}

// close ends the connection.
func (b *bidi) close() {
	b.ws.Close()
	b.end(errClosed)
}

// remoteValue is a value the browser serialised (WebDriver BiDi's
// script.RemoteValue).
type remoteValue struct {
	Type     string          `json:"type"`
	Value    json.RawMessage `json:"value"`
	SharedID string          `json:"sharedId"`
}

// evalResult is the reply to script.evaluate and script.callFunction.
type evalResult struct {
	Type             string      `json:"type"` // success | exception
	Result           remoteValue `json:"result"`
	ExceptionDetails struct {
		Text      string      `json:"text"`
		Exception remoteValue `json:"exception"`
	} `json:"exceptionDetails"`
}

// errorText is what an exception says.
func (r evalResult) errorText() string {
	if r.ExceptionDetails.Text != "" {
		return r.ExceptionDetails.Text
	}
	return "the script threw an exception"
}

// plain turns a remote value into Go values that encode as JSON, for the
// agent to read: objects become maps, arrays and sets lists, and values that
// JSON has no word for, such as undefined or a node, a short description.
func plain(v remoteValue) any {
	switch v.Type {
	case "undefined":
		return "undefined"
	case "null":
		return nil
	case "string", "boolean":
		var x any
		json.Unmarshal(v.Value, &x)
		return x
	case "number":
		// NaN, -0, Infinity and -Infinity come as strings.
		var x any
		json.Unmarshal(v.Value, &x)
		return x
	case "bigint":
		var s string
		json.Unmarshal(v.Value, &s)
		return s + "n"
	case "array", "set", "nodelist", "htmlcollection":
		var items []remoteValue
		json.Unmarshal(v.Value, &items)
		out := make([]any, len(items))
		for i, it := range items {
			out[i] = plain(it)
		}
		return out
	case "object", "map":
		// A list of [key, value] pairs, where a key is a string or a value.
		var pairs [][2]json.RawMessage
		json.Unmarshal(v.Value, &pairs)
		out := map[string]any{}
		for _, p := range pairs {
			var key string
			if json.Unmarshal(p[0], &key) != nil {
				var k remoteValue
				json.Unmarshal(p[0], &k)
				key = fmt.Sprint(plain(k))
			}
			var val remoteValue
			json.Unmarshal(p[1], &val)
			out[key] = plain(val)
		}
		return out
	case "date", "regexp":
		var x any
		json.Unmarshal(v.Value, &x)
		return x
	case "node":
		var n struct {
			NodeName  string `json:"nodeName"`
			LocalName string `json:"localName"`
		}
		json.Unmarshal(v.Value, &n)
		if n.LocalName != "" {
			return "<" + n.LocalName + "> element"
		}
		return n.NodeName + " node"
	default:
		return v.Type
	}
}
