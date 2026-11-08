package browser

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
func session(t *testing.T, tools []tool, lines ...string) map[string]map[string]any {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	s := &server{name: "irie-browser", version: "test", instructions: "Use it.", tools: tools, out: outW}
	done := make(chan error, 1)
	go func() {
		done <- s.serve(context.Background(), inR)
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
		t.Fatal("serve did not return at the end of its input")
	}
	return replies
}

func errCode(r map[string]any) float64 {
	e, _ := r["error"].(map[string]any)
	c, _ := e["code"].(float64)
	return c
}

func TestMCPHandshakeAndErrors(t *testing.T) {
	r := session(t, nil,
		`{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"initialize","params":{"protocolVersion":"2031-01-01"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":"four","method":"ping"}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"nope","arguments":{}}}`,
		`this is not json`,
	)
	// server/discover is revision 2026-07-28's: method not found keeps a
	// dual-era client such as Claude Code on the handshake.
	if errCode(r["1"]) != codeMethodNotFound {
		t.Fatalf("server/discover: %v", r["1"])
	}
	res, _ := r["2"]["result"].(map[string]any)
	if res["protocolVersion"] != "2025-06-18" || res["instructions"] != "Use it." {
		t.Fatalf("initialize: %v", r["2"])
	}
	if caps, _ := res["capabilities"].(map[string]any); caps["tools"] == nil {
		t.Fatalf("no tools capability: %v", res)
	}
	if res, _ := r["3"]["result"].(map[string]any); res["protocolVersion"] != protocolVersions[0] {
		t.Fatalf("an unknown version should get the newest: %v", r["3"])
	}
	if _, ok := r[`"four"`]["result"]; !ok {
		t.Fatalf("ping: %v", r[`"four"`])
	}
	if errCode(r["5"]) != codeInvalidParams {
		t.Fatalf("an unknown tool: %v", r["5"])
	}
	if errCode(r["null"]) != codeParse {
		t.Fatalf("a parse error: %v", r["null"])
	}
	if len(r) != 6 {
		t.Fatalf("%d replies, want 6: a notification gets none: %v", len(r), r)
	}
}

func TestMCPToolCallsRunAndAPanicIsAnError(t *testing.T) {
	tools := []tool{
		{Name: "echo", InputSchema: obj(nil), call: func(ctx context.Context, args json.RawMessage) toolResult {
			return textResult("got " + string(args))
		}},
		{Name: "boom", InputSchema: obj(nil), call: func(ctx context.Context, args json.RawMessage) toolResult {
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

func TestToolsAreWellFormed(t *testing.T) {
	b := New(Options{})
	seen := map[string]bool{}
	for _, tl := range b.tools() {
		if seen[tl.Name] || tl.Description == "" || tl.call == nil || tl.InputSchema["type"] != "object" {
			t.Errorf("tool %q is not well formed", tl.Name)
		}
		seen[tl.Name] = true
		required, _ := tl.InputSchema["required"].([]string)
		for _, req := range required {
			if _, ok := tl.InputSchema["properties"].(map[string]any)[req]; !ok {
				t.Errorf("%s requires %s, which it does not define", tl.Name, req)
			}
		}
	}
	// Claude Code cuts server instructions at 4,096 characters.
	if n := len(b.instructions()); n > 2000 {
		t.Errorf("the instructions take %d characters", n)
	}
}

func TestCallsOnAPageNeedAnOpenBrowser(t *testing.T) {
	b := New(Options{})
	for _, name := range []string{"read", "links", "click", "screenshot", "back", "eval", "tabs"} {
		args := `{"n":1,"text":"x","script":"1"}`
		i := slicesIndex(b.tools(), name)
		r := b.tools()[i].call(context.Background(), json.RawMessage(args))
		if !r.IsError || !strings.Contains(r.Content[0].Text, "nothing is open yet") {
			t.Errorf("%s with no browser: %+v", name, r)
		}
	}
}

func slicesIndex(ts []tool, name string) int {
	for i, t := range ts {
		if t.Name == name {
			return i
		}
	}
	return -1
}
