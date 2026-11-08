package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// fakeRemote answers BiDi commands as Firefox's Remote Agent does, with an
// event before each reply, which the client must pass over.
func fakeRemote(t *testing.T, answer func(method string, params json.RawMessage) (result any, errCode string)) *wsServer {
	return newWSServer(t, func(c *testConn) {
		for {
			op, p, err := c.read()
			if err != nil || op == opClose {
				return
			}
			var cmd struct {
				ID     int64           `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if err := json.Unmarshal(p, &cmd); err != nil {
				t.Errorf("not a command: %q", p)
				return
			}
			ev, _ := json.Marshal(map[string]any{"type": "event", "method": "log.entryAdded", "params": map[string]any{}})
			c.frame(true, opText, ev)
			if cmd.Method == "hang" {
				continue
			}
			result, code := answer(cmd.Method, cmd.Params)
			var reply []byte
			if code != "" {
				reply, _ = json.Marshal(map[string]any{"type": "error", "id": cmd.ID, "error": code, "message": "it went wrong"})
			} else {
				reply, _ = json.Marshal(map[string]any{"type": "success", "id": cmd.ID, "result": result})
			}
			c.frame(true, opText, reply)
		}
	})
}

func TestBiDiCallsGetTheirOwnReplies(t *testing.T) {
	s := fakeRemote(t, func(method string, params json.RawMessage) (any, string) {
		switch method {
		case "session.new":
			return map[string]any{"capabilities": map[string]any{"browserName": "mullvadbrowser", "browserVersion": "153.6.0"}}, ""
		case "browsingContext.traverseHistory":
			return nil, "no such history entry"
		}
		return map[string]any{"echo": json.RawMessage(params)}, ""
	})
	b, err := connect(s.addr(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer b.close()
	ctx := context.Background()
	var s1 struct {
		Capabilities struct{ BrowserName string } `json:"capabilities"`
	}
	if err := b.call(ctx, "session.new", map[string]any{"capabilities": map[string]any{}}, &s1); err != nil || s1.Capabilities.BrowserName != "mullvadbrowser" {
		t.Fatalf("session.new: %+v, %v", s1, err)
	}
	var echo struct {
		Echo struct{ URL string } `json:"echo"`
	}
	if err := b.call(ctx, "browsingContext.navigate", map[string]any{"url": "https://example.com/"}, &echo); err != nil || echo.Echo.URL != "https://example.com/" {
		t.Fatalf("navigate: %+v, %v", echo, err)
	}
	err = b.call(ctx, "browsingContext.traverseHistory", nil, nil)
	var be *bidiError
	if !errors.As(err, &be) || be.Code != "no such history entry" {
		t.Fatalf("an error reply: %v", err)
	}

	short, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if err := b.call(short, "hang", nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a call that gets no reply: %v", err)
	}
	if !b.alive() {
		t.Fatal("a timed-out call ended the connection")
	}
	b.close()
	if err := b.call(ctx, "session.end", nil, nil); !errors.Is(err, errClosed) {
		t.Fatalf("a call after close: %v", err)
	}
}

func TestPlainTurnsRemoteValuesIntoJSON(t *testing.T) {
	var v remoteValue
	raw := `{"type":"object","value":[
		["title",{"type":"string","value":"Pricing"}],
		["plans",{"type":"array","value":[{"type":"number","value":9},{"type":"number","value":"NaN"},{"type":"null"}]}],
		["big",{"type":"bigint","value":"12"}],
		["el",{"type":"node","sharedId":"x","value":{"nodeType":1,"localName":"table"}}],
		["nothing",{"type":"undefined"}],
		["ok",{"type":"boolean","value":true}]]}`
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatal(err)
	}
	got, err := asText(plain(v))
	if err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	json.Compact(&compact, []byte(got))
	want := `{"big":"12n","el":"<table> element","nothing":"undefined","ok":true,"plans":[9,"NaN",null],"title":"Pricing"}`
	if compact.String() != want {
		t.Fatalf("plain = %s\nwant    %s", compact.String(), want)
	}
}
