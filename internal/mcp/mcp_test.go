package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

// session runs a server with tools on lines and returns its replies by id.
func session(t *testing.T, tools []Tool, lines ...string) map[string]map[string]any {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	s := Server{Name: "irie-test", Version: "test", Instructions: "Use it.", Tools: tools}
	done := make(chan error, 1)
	go func() {
		done <- s.Serve(context.Background(), inR, outW)
		outW.Close()
	}()
	go func() {
		for _, l := range lines {
			io.WriteString(inW, l+"\n")
		}
		inW.Close()
	}()
	replies := map[string]map[string]any{}
	sc := bufio.NewScanner(outR)
	for sc.Scan() {
		var r map[string]any
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("a reply that is not JSON: %q", sc.Text())
		}
		id, _ := json.Marshal(r["id"])
		replies[string(id)] = r
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return at the end of its input")
	}
	return replies
}

func errCode(r map[string]any) float64 {
	e, _ := r["error"].(map[string]any)
	c, _ := e["code"].(float64)
	return c
}

func object() map[string]any { return map[string]any{"type": "object"} }

func TestHandshakeAndErrors(t *testing.T) {
	r := session(t, nil,
		`{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"initialize","params":{"protocolVersion":"2031-01-01"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":"four","method":"ping"}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"nope","arguments":{}}}`,
		`this is not json`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/list"}`,
	)
	// server/discover is revision 2026-07-28's: method not found keeps a
	// dual-era client such as Claude Code on the handshake.
	if errCode(r["1"]) != CodeMethodNotFound {
		t.Fatalf("server/discover: %v", r["1"])
	}
	res, _ := r["2"]["result"].(map[string]any)
	if res["protocolVersion"] != "2025-06-18" || res["instructions"] != "Use it." {
		t.Fatalf("initialize: %v", r["2"])
	}
	if caps, _ := res["capabilities"].(map[string]any); caps["tools"] == nil {
		t.Fatalf("no tools capability: %v", res)
	}
	if res, _ := r["3"]["result"].(map[string]any); res["protocolVersion"] != ProtocolVersions[0] {
		t.Fatalf("an unknown version should get the newest: %v", r["3"])
	}
	if _, ok := r[`"four"`]["result"]; !ok {
		t.Fatalf("ping: %v", r[`"four"`])
	}
	if errCode(r["5"]) != CodeInvalidParams {
		t.Fatalf("an unknown tool: %v", r["5"])
	}
	if errCode(r["null"]) != CodeParse {
		t.Fatalf("a parse error: %v", r["null"])
	}
	// A server without tools lists none, not null.
	if list, ok := r["6"]["result"].(map[string]any)["tools"].([]any); !ok || len(list) != 0 {
		t.Fatalf("tools/list without tools: %v", r["6"])
	}
	if len(r) != 7 {
		t.Fatalf("%d replies, want 7: a notification gets none: %v", len(r), r)
	}
}

func TestToolCallsRunAndAPanicIsAnError(t *testing.T) {
	tools := []Tool{
		{Name: "echo", InputSchema: object(), Call: func(ctx context.Context, args json.RawMessage) Result {
			return Text("got " + string(args))
		}},
		{Name: "boom", InputSchema: object(), Call: func(ctx context.Context, args json.RawMessage) Result {
			panic("broken")
		}},
	}
	r := session(t, tools,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":{"url":"x"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo"}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"boom","arguments":{}}}`,
	)
	list, _ := r["1"]["result"].(map[string]any)["tools"].([]any)
	if len(list) != 2 || list[0].(map[string]any)["name"] != "echo" {
		t.Fatalf("tools/list: %v", r["1"])
	}
	if _, ok := list[0].(map[string]any)["Call"]; ok {
		t.Fatalf("tools/list shows the call: %v", list[0])
	}
	text := func(id string) (string, bool) {
		res := r[id]["result"].(map[string]any)
		c := res["content"].([]any)[0].(map[string]any)
		isErr, _ := res["isError"].(bool)
		return c["text"].(string), isErr
	}
	if s, isErr := text("2"); s != `got {"url":"x"}` || isErr {
		t.Fatalf("echo: %q %v", s, isErr)
	}
	if s, _ := text("3"); s != "got {}" {
		t.Fatalf("no arguments should be {}: %q", s)
	}
	if s, isErr := text("4"); !isErr || !strings.Contains(s, "broken") {
		t.Fatalf("a panic: %q %v", s, isErr)
	}
}
