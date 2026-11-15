package variorum

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Keys shaped as Variorum makes them: Claude Code's carries its client's
// name, Any MCP client's none.
var (
	sessionsKey = "vmk2_claude-code_" + strings.Repeat("k7", 21) + "x"
	tabKey      = "vmk1_" + strings.Repeat("t9", 21) + "z"
)

// TestMain runs the tests; or, run as variorum-mcp inside a fake
// Variorum.app, stands in for Variorum's door; or, when a test asks, runs
// `irie variorum door`.
func TestMain(m *testing.M) {
	switch {
	case filepath.Base(os.Args[0]) == Program:
		os.Exit(standIn())
	case os.Getenv("IRIE_TEST_DOOR") != "":
		Folders = filepath.SplitList(os.Getenv("IRIE_TEST_FOLDERS"))
		os.Exit(Door(os.Getenv("IRIE_TEST_DOOR"), os.Stdin, os.Stdout))
	}
	os.Exit(m.Run())
}

// script says how the stand-in door behaves.
type script struct {
	Refuse  string              `json:"refuse,omitempty"`  // answers initialize with this error
	Answers map[string][]answer `json:"answers,omitempty"` // each tool's answers, in turn
	Hang    bool                `json:"hang,omitempty"`    // never answers a call
	Die     string              `json:"die,omitempty"`     // at a call: half an answer, these words on stderr, and the end
}

type answer struct {
	Text    string `json:"text"`
	IsError bool   `json:"isError,omitempty"`
}

// record is what the stand-in was given: its environment, its arguments
// and each line the client sent.
type record struct {
	Env   []string          `json:"env"`
	Args  []string          `json:"args"`
	Lines []json.RawMessage `json:"lines"`
}

// standIn is Variorum's door as these tests need it, from the door's
// protocol: it reads its script beside it, records what it is given and
// answers as the script says. Before each answer to a call it sends a
// notification and an answer to another id, which a client must skip.
func standIn() int {
	res := filepath.Join(filepath.Dir(filepath.Dir(os.Args[0])), "Resources")
	var sc script
	if b, err := os.ReadFile(filepath.Join(res, "script.json")); err == nil {
		json.Unmarshal(b, &sc)
	}
	got := record{Env: os.Environ(), Args: os.Args[1:]}
	save := func() {
		b, _ := json.Marshal(got)
		os.WriteFile(filepath.Join(res, "got.json"), b, 0o600)
	}
	save()
	send := func(v any) {
		b, _ := json.Marshal(v)
		os.Stdout.Write(append(b, '\n'))
	}
	turn := map[string]int{}
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(nil, 1<<20)
	for in.Scan() {
		line := append([]byte(nil), in.Bytes()...)
		got.Lines = append(got.Lines, line)
		save()
		var m struct {
			ID     any    `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		json.Unmarshal(line, &m)
		switch m.Method {
		case "initialize":
			if sc.Refuse != "" {
				send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "error": map[string]any{"code": -32000, "message": sc.Refuse}})
				return 0
			}
			send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": map[string]any{
				"protocolVersion": protocolVersion, "capabilities": map[string]any{"tools": map[string]any{}},
				"serverInfo": map[string]any{"name": "stand-in", "version": "0"},
			}})
		case "tools/call":
			switch {
			case sc.Hang:
				continue
			case sc.Die != "":
				io.WriteString(os.Stdout, `{"jsonrpc":"2.0","id":`)
				fmt.Fprintln(os.Stderr, "a line before")
				fmt.Fprintln(os.Stderr, sc.Die)
				return 1
			}
			send(map[string]any{"jsonrpc": "2.0", "method": "notifications/message", "params": map[string]any{"level": "info", "data": "logged"}})
			send(map[string]any{"jsonrpc": "2.0", "id": 999, "result": map[string]any{}})
			n := turn[m.Params.Name]
			turn[m.Params.Name]++
			a := answer{Text: "no answer in the script", IsError: true}
			if n < len(sc.Answers[m.Params.Name]) {
				a = sc.Answers[m.Params.Name][n]
			}
			send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": map[string]any{
				"content": []any{map[string]any{"type": "text", "text": a.Text}}, "isError": a.IsError,
			}})
		}
	}
	return 0
}

// plist is an Info.plist naming bundle, with a CFBundleIdentifier in a dict
// inside it first, which isn't the app's.
func plist(bundle string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleExecutable</key>
	<string>Variorum</string>
	<key>CFBundleURLTypes</key>
	<array>
		<dict>
			<key>CFBundleIdentifier</key>
			<string>io.example.Nested</string>
		</dict>
	</array>
	<key>CFBundleIdentifier</key>
	<string>` + bundle + `</string>
</dict>
</plist>
`
}

// fakeApp makes Variorum.app, of bundle, in an Applications folder of its
// own, which Folders point to, with the test binary as its door.
func fakeApp(t *testing.T, bundle string) string {
	t.Helper()
	folder := t.TempDir()
	old := Folders
	Folders = []string{folder}
	t.Cleanup(func() { Folders = old })
	return makeApp(t, folder, "Variorum.app", bundle)
}

// makeApp makes an app named name in folder, of bundle, with the test
// binary as its door.
func makeApp(t *testing.T, folder, name, bundle string) string {
	t.Helper()
	app := filepath.Join(folder, name)
	for _, d := range []string{"MacOS", "Resources"} {
		if err := os.MkdirAll(filepath.Join(app, "Contents", d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(plist(bundle)), 0o644); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Link(self, doorOf(app)); err != nil {
		copyFile(t, self, doorOf(app))
	}
	return app
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, b, 0o755); err != nil {
		t.Fatal(err)
	}
}

func doorOf(app string) string { return filepath.Join(app, "Contents", "MacOS", Program) }

// setScript tells the stand-in in app how to behave.
func setScript(t *testing.T, app string, sc script) {
	t.Helper()
	b, err := json.Marshal(sc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "Contents", "Resources", "script.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// got is what the stand-in in app was given last.
func got(t *testing.T, app string) record {
	t.Helper()
	var r record
	b, err := os.ReadFile(filepath.Join(app, "Contents", "Resources", "got.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

// message is one JSON-RPC message, as a test reads it.
type message struct {
	ID     any            `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
	Result any            `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decode(t *testing.T, b []byte) message {
	t.Helper()
	var m message
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%s: %v", b, err)
	}
	return m
}
