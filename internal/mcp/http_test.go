package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type ctxKey struct{}

// post sends body to the handler as a client on this computer would, with
// head's headers, and returns the status and the body.
func post(t *testing.T, h http.Handler, method, body string, head map[string]string) (int, string, http.Header) {
	t.Helper()
	r := httptest.NewRequest(method, "http://127.0.0.1:7440/mcp", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range head {
		if k == "Host" {
			r.Host = v
			continue
		}
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	b, _ := io.ReadAll(w.Result().Body)
	return w.Code, string(b), w.Result().Header
}

func TestHTTPAnswersEachRequestWithOneJSONObject(t *testing.T) {
	life := context.WithValue(context.Background(), ctxKey{}, "the server's")
	var ran string
	tools := []Tool{{Name: "echo", Description: "Echo.", InputSchema: object(), Call: func(ctx context.Context, args json.RawMessage) Result {
		ran, _ = ctx.Value(ctxKey{}).(string)
		return Text(string(args))
	}}}
	h := HTTP{Context: life, Pick: func(r *http.Request) (Server, int) {
		if r.Header.Get("Authorization") != "Bearer ok" {
			return Server{}, http.StatusForbidden
		}
		return Server{Name: "irie-test", Version: "test", Tools: tools}, 0
	}}
	ok := map[string]string{"Authorization": "Bearer ok"}

	code, body, head := post(t, h, "POST", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`, ok)
	if code != 200 || head.Get("Content-Type") != "application/json" || head.Get("Mcp-Session-Id") != "" || !strings.Contains(body, `"protocolVersion":"2025-11-25"`) {
		t.Fatalf("initialize: %d %q %v", code, body, head)
	}
	// Stateless: a call needs no handshake before it, so a client of a
	// server that restarted carries on.
	code, body, _ = post(t, h, "POST", `{"jsonrpc":"2.0","id":"c","method":"tools/call","params":{"name":"echo","arguments":{"x":1}}}`, ok)
	if code != 200 || !strings.Contains(body, `"id":"c"`) || !strings.Contains(body, `{\"x\":1}`) || ran != "the server's" {
		t.Fatalf("tools/call: %d %q, ran under %q", code, body, ran)
	}
	for _, msg := range []string{
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":7,"result":{}}`, // a response: the server asks nothing
	} {
		if code, body, _ := post(t, h, "POST", msg, ok); code != http.StatusAccepted || body != "" {
			t.Errorf("%s: %d %q, want 202 and no body", msg, code, body)
		}
	}
	if code, body, _ := post(t, h, "POST", `not json`, ok); code != 400 || errCodeOf(t, body) != CodeParse {
		t.Errorf("a parse error: %d %q", code, body)
	}
	for _, m := range []string{"GET", "DELETE"} {
		if code, _, head := post(t, h, m, "", ok); code != http.StatusMethodNotAllowed || head.Get("Allow") != "POST" {
			t.Errorf("%s: %d %v", m, code, head)
		}
	}
	if code, _, _ := post(t, h, "POST", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, nil); code != http.StatusForbidden {
		t.Errorf("no credentials: %d", code)
	}
	if code, _, _ := post(t, h, "POST", `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"x":"`+strings.Repeat("a", MaxBody)+`"}}`, ok); code != http.StatusRequestEntityTooLarge {
		t.Errorf("a message over MaxBody: %d", code)
	}
}

func TestHTTPAnswersOnlyThisComputer(t *testing.T) {
	h := HTTP{Pick: func(*http.Request) (Server, int) { return Server{Name: "irie-test"}, 0 }}
	ping := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	for name, head := range map[string]map[string]string{
		"a page's origin":   {"Origin": "https://example.com"},
		"a renamed address": {"Host": "attacker.example.com:7440"},
		"a null origin":     {"Origin": "null"},
	} {
		if code, _, _ := post(t, h, "POST", ping, head); code != http.StatusForbidden {
			t.Errorf("%s: %d, want 403", name, code)
		}
	}
	for name, head := range map[string]map[string]string{
		"no origin":       nil,
		"its own origin":  {"Origin": "http://127.0.0.1:7440"},
		"localhost":       {"Host": "localhost:7440", "Origin": "http://localhost:7440"},
		"IPv6's loopback": {"Host": "[::1]:7440"},
	} {
		if code, body, _ := post(t, h, "POST", ping, head); code != 200 {
			t.Errorf("%s: %d %q, want 200", name, code, body)
		}
	}
}

func errCodeOf(t *testing.T, body string) float64 {
	t.Helper()
	var r map[string]any
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatalf("not JSON: %q", body)
	}
	return errCode(r)
}
