package sdk

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/testutil"
)

// fakeEnv makes the test binary the fake CLI: a `claude -p` that speaks
// the Agent SDK's JSON lines.
const fakeEnv = "IRIE_SDK_FAKE_CLAUDE"

func TestMain(m *testing.M) {
	if os.Getenv(fakeEnv) != "" {
		os.Exit(fakeCLI(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// fakeCLI answers each message: "ask" asks to run a command first and says
// what the answer was, "slow" works until it is interrupted, and anything
// else comes back as an echo, streamed in two pieces. It ends when its
// input does.
func fakeCLI(args []string) int {
	for _, f := range Flags {
		if !slices.Contains(args, f) {
			fmt.Fprintln(os.Stderr, "fake claude: no", f, "in", args)
			return 3
		}
	}
	out := bufio.NewWriter(os.Stdout)
	emit := func(v any) {
		b, _ := json.Marshal(v)
		out.Write(append(b, '\n'))
		out.Flush()
	}
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(nil, 1<<20)
	cost, n := 0.0, 0
	reply := func(text string) {
		n++
		id := fmt.Sprintf("msg_%d", n)
		emit(map[string]any{"type": "system", "subtype": "init", "model": "claude-test", "permissionMode": "auto"})
		emit(map[string]any{"type": "stream_event", "event": map[string]any{"type": "message_start", "message": map[string]any{"id": id, "usage": map[string]any{"input_tokens": 10, "cache_read_input_tokens": 240}}}})
		half := len(text) / 2
		for _, part := range []string{text[:half], text[half:]} {
			emit(map[string]any{"type": "stream_event", "event": map[string]any{"type": "content_block_delta", "delta": map[string]any{"type": "text_delta", "text": part}}})
		}
		emit(map[string]any{"type": "stream_event", "event": map[string]any{"type": "content_block_stop"}})
		emit(map[string]any{"type": "assistant", "message": map[string]any{"id": id, "content": []any{map[string]any{"type": "text", "text": text}}}})
		cost += 0.01
		emit(map[string]any{"type": "result", "subtype": "success", "result": text, "total_cost_usd": cost, "modelUsage": map[string]any{"claude-test": map[string]any{"contextWindow": 1000}}})
	}
	type line struct {
		Type      string `json:"type"`
		RequestID string `json:"request_id"`
		Message   struct {
			Content string `json:"content"`
		} `json:"message"`
		Request struct {
			Subtype string `json:"subtype"`
		} `json:"request"`
		Response struct {
			RequestID string `json:"request_id"`
			Response  struct {
				Behavior     string          `json:"behavior"`
				Message      string          `json:"message"`
				UpdatedInput json.RawMessage `json:"updatedInput"`
				Interrupt    bool            `json:"interrupt"`
			} `json:"response"`
		} `json:"response"`
	}
	read := func() (line, bool) {
		var l line
		if !in.Scan() {
			return l, false
		}
		json.Unmarshal(in.Bytes(), &l)
		return l, true
	}
	for {
		l, ok := read()
		if !ok {
			return 0
		}
		switch {
		case l.Type == "user" && l.Message.Content == "ask":
			emit(map[string]any{"type": "control_request", "request_id": "perm_1", "request": map[string]any{
				"subtype": "can_use_tool", "tool_name": "Bash", "display_name": "Bash", "input": map[string]any{"command": "touch made-by-fake"}, "tool_use_id": "tu_1",
			}})
			a, ok := read()
			if !ok || a.Type != "control_response" || a.Response.RequestID != "perm_1" {
				fmt.Fprintf(os.Stderr, "fake claude: no answer to the prompt: %+v\n", a)
				return 4
			}
			r := a.Response.Response
			// An answer without updatedInput reads as null, as JSON
			// writes it, whichever Go reads it.
			input := string(r.UpdatedInput)
			if input == "" {
				input = "null"
			}
			reply(fmt.Sprintf("answer %s %s %s stop=%v", r.Behavior, input, r.Message, r.Interrupt))
		case l.Type == "user" && l.Message.Content == "slow":
			emit(map[string]any{"type": "system", "subtype": "init", "model": "claude-test", "permissionMode": "auto"})
			s, ok := read()
			if !ok || s.Type != "control_request" || s.Request.Subtype != "interrupt" {
				fmt.Fprintf(os.Stderr, "fake claude: no interrupt: %+v\n", s)
				return 5
			}
			emit(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": s.RequestID}})
			emit(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "[Request interrupted by user]"}}}})
			emit(map[string]any{"type": "result", "subtype": "error_during_execution", "is_error": true, "result": "[ede_diagnostic] stop_reason=tool_use", "total_cost_usd": cost})
		case l.Type == "user":
			reply("echo: " + l.Message.Content)
		}
	}
}

// screen is what Run writes, safe to read while it runs.
type screen struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *screen) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *screen) text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.ReplaceAll(csi.ReplaceAllString(s.b.String(), ""), "\r", "")
}

// session runs Run on the fake CLI with first as the first message.
type session struct {
	t    *testing.T
	keys io.WriteCloser
	out  *screen
	code chan int
}

func start(t *testing.T, first string) *session {
	t.Helper()
	t.Setenv(fakeEnv, "1")
	r, w := io.Pipe()
	s := &session{t: t, keys: w, out: &screen{}, code: make(chan int, 1)}
	go func() {
		s.code <- Run(Options{Argv: []string{os.Args[0], "-test.run=^$"}, First: first, In: r, Out: s.out, Width: 100})
	}()
	t.Cleanup(func() { w.Close() })
	return s
}

func (s *session) type_(keys string) {
	s.t.Helper()
	if _, err := io.WriteString(s.keys, keys); err != nil {
		s.t.Fatal(err)
	}
}

func (s *session) see(what string) {
	s.t.Helper()
	testutil.Eventually(s.t, 10*time.Second, fmt.Sprintf("the screen to show %q", what), func() bool {
		return strings.Contains(s.out.text(), what)
	})
}

func (s *session) exit() int {
	s.t.Helper()
	select {
	case c := <-s.code:
		return c
	case <-time.After(10 * time.Second):
		s.t.Fatalf("Run did not return; the screen:\n%s", s.out.text())
		return -1
	}
}

// TestAConversation is a session from its first message to Ctrl+D: replies
// stream in, typed and pasted messages go as the next, and the session
// ends when the user ends it.
func TestAConversation(t *testing.T) {
	s := start(t, "hello")
	s.see("› hello")
	s.see("echo: hello")
	s.see("done in")
	s.see("$0.01 so far")
	s.see("· claude-test · auto mode")

	// A message typed as the core types one: a paste, then Enter.
	s.type_("\x1b[200~two\nlines\x1b[201~")
	s.type_("\r")
	s.see("echo: two\nlines")
	s.see("$0.02 so far")

	s.type_("\x04")
	if code := s.exit(); code != 0 {
		t.Errorf("exit code %d; the screen:\n%s", code, s.out.text())
	}
	s.see("the session ended")
}

// TestAPermissionPrompt is the agent asking to run a command: the
// question shows as a numbered menu, which the phone app reads too, and
// the answer goes back with the call's input.
func TestAPermissionPrompt(t *testing.T) {
	s := start(t, "ask")
	s.see("$ touch made-by-fake")
	s.see("Allow Bash: touch made-by-fake")
	s.see("❯ 1. Yes")
	s.see("  2. No")
	if opts := lastMenu(s.out.text()); !slices.Equal(opts, []string{"1. Yes", "2. No"}) {
		t.Errorf("the menu at the bottom = %q", opts)
	}
	s.type_("1")
	s.see("allowed")
	s.see(`answer allow {"command":"touch made-by-fake"}  stop=false`)

	s.type_("ask\r")
	testutil.Eventually(t, 10*time.Second, "a second prompt", func() bool {
		return strings.Count(s.out.text(), "Allow Bash") >= 2
	})
	s.type_("\x1b") // Escape says no
	s.see("not allowed")
	s.see("answer deny null " + noteDenied + " stop=true")
	s.type_("\x04")
	if code := s.exit(); code != 0 {
		t.Errorf("exit code %d; the screen:\n%s", code, s.out.text())
	}
}

// lastMenu is the numbered choices among the last lines of screen.
func lastMenu(screen string) []string {
	lines := strings.Split(screen, "\n")
	var opts []string
	for _, l := range lines[max(0, len(lines)-6):] {
		l = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "❯"))
		if len(l) > 2 && l[0] >= '1' && l[0] <= '9' && l[1] == '.' {
			opts = append(opts, l)
		}
	}
	return opts
}

// TestEscapeStopsATurn is Escape while the agent works: an interrupt goes
// to the CLI, whose turn ends.
func TestEscapeStopsATurn(t *testing.T) {
	s := start(t, "slow")
	s.see("working · Esc stops it")
	s.type_("\x1b")
	s.see("stopping…")
	s.see("[Request interrupted by user]")
	s.see("· stopped after")
	if strings.Contains(s.out.text(), "ede_diagnostic") {
		t.Errorf("a stopped turn showed the CLI's diagnostic:\n%s", s.out.text())
	}
	s.type_("\x03")
	s.see("Ctrl+C again ends the session")
	s.type_("\x03")
	if code := s.exit(); code != 0 {
		t.Errorf("exit code %d; the screen:\n%s", code, s.out.text())
	}
}

// TestTheCLIsFailureShows is a CLI that cannot start its session: what it
// says, and its exit code, are the pane's last lines.
func TestTheCLIsFailureShows(t *testing.T) {
	out := &screen{}
	code := Run(Options{Argv: []string{"/bin/sh", "-c", "echo 'error: unknown option' >&2; exit 7", "sh"}, In: strings.NewReader(""), Out: out})
	if code != 7 {
		t.Errorf("exit code %d, want 7", code)
	}
	for _, want := range []string{"error: unknown option", "the session ended (exit 7)"} {
		if !strings.Contains(out.text(), want) {
			t.Errorf("the screen lacks %q:\n%s", want, out.text())
		}
	}
}
