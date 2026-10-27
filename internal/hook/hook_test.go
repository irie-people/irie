package hook

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/irie-hq/irie/internal/event"
	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/testutil"
)

func TestMain(m *testing.M) { testutil.Main(m) }

const session = "s0k3j9x2m4a7q"

// core is a stand-in for `irie core` that records hook requests.
type core struct {
	got   chan ipc.HookArgs
	reply ipc.HookResult
	err   error
	wait  time.Duration
}

func (c *core) Call(op string, raw json.RawMessage) (any, error) {
	if op != ipc.OpHook {
		return nil, ipc.Errorf(ipc.CodeUnknownOp, "no op %q", op)
	}
	var a ipc.HookArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, err
	}
	c.got <- a
	time.Sleep(c.wait)
	return c.reply, c.err
}

func (c *core) Subscribe(json.RawMessage) (<-chan ipc.Frame, func(), error) {
	return nil, nil, ipc.Errorf(ipc.CodeUnknownOp, "no subscribe")
}

func serve(t *testing.T, c *core) string {
	t.Helper()
	sock := filepath.Join(testutil.ShortDir(t), "core.sock")
	l, err := ipc.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	if c.got == nil {
		c.got = make(chan ipc.HookArgs, 8)
	}
	go ipc.Serve(l, c)
	return sock
}

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func run(t *testing.T, sock, stdin string) (string, time.Duration) {
	t.Helper()
	var out bytes.Buffer
	start := time.Now()
	code := Run(strings.NewReader(stdin), &out, env(map[string]string{"IRIE_SOCKET": sock, "IRIE_SESSION": session}))
	if code != 0 {
		t.Errorf("exit code %d, want 0", code)
	}
	return out.String(), time.Since(start)
}

const preToolUse = `{"session_id":"6f1c2b4e-8a9d-4c3b-9e7f-0a1b2c3d4e5f","transcript_path":"/home/alice/transcripts/6f1c.jsonl","cwd":"/home/alice/code/website","permission_mode":"auto","hook_event_name":"PreToolUse","tool_name":"Write","tool_input":{"file_path":"/home/alice/code/website/big.txt","content":"CONTENT"},"tool_use_id":"toolu_01"}`

func TestForwardsAndCapsStrings(t *testing.T) {
	c := &core{}
	sock := serve(t, c)
	big := strings.Repeat("é", 3000) // 6000 bytes
	out, _ := run(t, sock, strings.Replace(preToolUse, "CONTENT", big, 1))
	if out != "" {
		t.Errorf("printed %q with nothing to say", out)
	}
	a := <-c.got
	if a.Harness != "claude-code" || a.Session != session {
		t.Errorf("args = %+v", a)
	}
	var p map[string]any
	if err := json.Unmarshal(a.Payload, &p); err != nil {
		t.Fatal(err)
	}
	content := p["tool_input"].(map[string]any)["content"].(string)
	if len(content) > event.MaxString || !strings.HasPrefix(big, content) || len(content) < event.MaxString-1 {
		t.Errorf("content is %d bytes, want the first %d", len(content), event.MaxString)
	}
	if p["tool_name"] != "Write" || p["tool_use_id"] != "toolu_01" {
		t.Errorf("payload lost fields: %v", p)
	}
	if lengths, _ := p["irie_lengths"].(map[string]any); lengths["tool_input.content"] != 3000.0 {
		t.Errorf("irie_lengths = %v, want tool_input.content: 3000", p["irie_lengths"])
	}
	if len(a.Payload) > 4*event.MaxString {
		t.Errorf("forwarded %d bytes", len(a.Payload))
	}
}

func TestATurnsLastMessageGoesWhole(t *testing.T) {
	// A delegated session reports it back whole; the journal cuts it.
	c := &core{}
	sock := serve(t, c)
	big := strings.Repeat("ü", 4000)
	stop := `{"session_id":"6f1c2b4e-8a9d-4c3b-9e7f-0a1b2c3d4e5f","hook_event_name":"Stop","stop_hook_active":false,"last_assistant_message":"` + big + `","transcript_path":"` + big + `"}`
	run(t, sock, stop)
	var p map[string]any
	json.Unmarshal((<-c.got).Payload, &p)
	if p["last_assistant_message"] != big {
		t.Errorf("the last message is %d bytes, want all %d", len(p["last_assistant_message"].(string)), len(big))
	}
	if s, _ := p["transcript_path"].(string); len(s) > event.MaxString {
		t.Errorf("other strings are not cut: %d bytes", len(s))
	}
}

func TestPrintsTheReplyInClaudesDialect(t *testing.T) {
	c := &core{reply: ipc.HookResult{Context: "[Irie] Note from the user: the staging database is down"}}
	sock := serve(t, c)
	out, _ := run(t, sock, preToolUse)
	var got struct {
		HookSpecificOutput map[string]string `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output %q: %v", out, err)
	}
	if got.HookSpecificOutput["hookEventName"] != "PreToolUse" || got.HookSpecificOutput["additionalContext"] != c.reply.Context {
		t.Errorf("output = %s", out)
	}

	c.reply = ipc.HookResult{Deny: "[Irie] Still paused by the user; this call did not run. Retry the same call to keep waiting."}
	out, _ = run(t, sock, preToolUse)
	if !strings.Contains(out, `"permissionDecision":"deny"`) || strings.Contains(out, "allow") {
		t.Errorf("deny output = %s", out)
	}
}

func TestAFrozenSessionsToolCallWaitsForTheThaw(t *testing.T) {
	// An agent whose host predates freeze runs on while its session is
	// frozen; its next tool call waits at the shim until the thaw, with the
	// core down as well.
	defer func(d time.Duration) { holdPoll = d }(holdPoll)
	holdPoll = 10 * time.Millisecond
	state := t.TempDir()
	dir := filepath.Join(state, "irie", "sessions", session)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	freeze := filepath.Join(dir, "freeze.json")
	if err := os.WriteFile(freeze, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &core{reply: ipc.HookResult{Context: "[Irie] noted"}}
	for _, sock := range []string{serve(t, c), filepath.Join(testutil.ShortDir(t), "none.sock")} {
		vars := env(map[string]string{"IRIE_SOCKET": sock, "IRIE_SESSION": session, "HOME": "/home/alice", "XDG_STATE_HOME": state})
		var out bytes.Buffer
		done := make(chan struct{})
		go func() {
			Run(strings.NewReader(preToolUse), &out, vars)
			close(done)
		}()
		select {
		case <-done:
			t.Fatalf("%s: the call went through while frozen", sock)
		case <-time.After(200 * time.Millisecond):
		}
		// Other hooks never wait.
		stop := strings.Replace(preToolUse, "PreToolUse", "Stop", 1)
		start := time.Now()
		Run(strings.NewReader(stop), &bytes.Buffer{}, vars)
		if d := time.Since(start); d > time.Second {
			t.Errorf("%s: a Stop hook waited %v", sock, d)
		}
		os.Remove(freeze)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s: the call still waits after the thaw", sock)
		}
		if strings.Contains(sock, "none.sock") != (out.Len() == 0) {
			t.Errorf("%s: printed %q", sock, out.String())
		}
		os.WriteFile(freeze, []byte("{}\n"), 0o600)
	}
}

func TestFailsOpen(t *testing.T) {
	defer func(d time.Duration) { replyTimeout = d }(replyTimeout)
	replyTimeout = 300 * time.Millisecond

	stale := filepath.Join(testutil.ShortDir(t), "core.sock")
	l, err := net.Listen("unix", stale)
	if err != nil {
		t.Fatal(err)
	}
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	l.Close()

	failing := serve(t, &core{err: ipc.Errorf(ipc.CodeNotFound, "no session")})
	silent := serve(t, &core{wait: 10 * time.Second})

	for _, c := range []struct {
		name, sock, stdin string
		env               map[string]string
	}{
		{"no socket", filepath.Join(testutil.ShortDir(t), "core.sock"), preToolUse, nil},
		{"stale socket", stale, preToolUse, nil},
		{"core error", failing, preToolUse, nil},
		{"core never answers", silent, preToolUse, nil},
		{"not JSON", failing, "not json", nil},
		{"empty stdin", failing, "", nil},
		{"no session", failing, preToolUse, map[string]string{"IRIE_SESSION": ""}},
	} {
		vars := map[string]string{"IRIE_SOCKET": c.sock, "IRIE_SESSION": session}
		for k, v := range c.env {
			vars[k] = v
		}
		var out bytes.Buffer
		start := time.Now()
		if code := Run(strings.NewReader(c.stdin), &out, env(vars)); code != 0 {
			t.Errorf("%s: exit %d", c.name, code)
		}
		if out.Len() != 0 {
			t.Errorf("%s: printed %q", c.name, out.String())
		}
		if d := time.Since(start); d > replyTimeout+time.Second {
			t.Errorf("%s: took %v", c.name, d)
		}
	}
}

// TestStoppedCoreCostsUnder300ms runs the real binary, as Claude Code does,
// against a core that is not running.
func TestStoppedCoreCostsUnder300ms(t *testing.T) {
	bin := testutil.BuildIrie(t)
	dir := testutil.ShortDir(t)
	stale := filepath.Join(dir, "stale.sock")
	l, err := net.Listen("unix", stale)
	if err != nil {
		t.Fatal(err)
	}
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	l.Close()
	// Warm the page cache so the first run measures the shim, not the disk.
	exec.Command(bin, "version").Run()

	for _, sock := range []string{filepath.Join(dir, "missing.sock"), stale} {
		cmd := exec.Command(bin, "hook")
		cmd.Env = append(os.Environ(), "IRIE_SOCKET="+sock, "IRIE_SESSION="+session)
		cmd.Stdin = strings.NewReader(preToolUse)
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		start := time.Now()
		err := cmd.Run()
		d := time.Since(start)
		if err != nil {
			t.Errorf("%s: %v", sock, err)
		}
		if stdout.Len() != 0 {
			t.Errorf("%s: printed %q", sock, stdout.String())
		}
		if d > 300*time.Millisecond {
			t.Errorf("%s: irie hook took %v, want under 300 ms", sock, d)
		}
	}
}
