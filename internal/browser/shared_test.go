package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/host"
)

// fakeBrowser is a browser for tests: it says it listens where the file
// fake-port beside its profile says, as Firefox's Remote Agent prints it,
// notes the profile it was given beside it, and waits.
const fakeBrowser = `#!/bin/sh
profile=
while [ $# -gt 0 ]; do
  if [ "$1" = "--profile" ]; then profile=$2; shift; fi
  shift
done
dir=$(dirname "$profile")
echo "$profile" > "$dir/fake-profile"
echo "WebDriver BiDi listening on ws://$(cat "$dir/fake-port")" >&2
exec sleep 600
`

// pageRemote is a Remote Agent for tests whose one tab shows the page of
// session name, at the address it was last sent to.
func pageRemote(t *testing.T, name string) *wsServer {
	address := "about:blank"
	return fakeRemote(t, func(method string, params json.RawMessage) (any, string) {
		switch method {
		case "session.new":
			return map[string]any{"capabilities": map[string]any{"browserName": "mullvadbrowser", "browserVersion": "15.0"}}, ""
		case "browsingContext.getTree":
			return map[string]any{"contexts": []map[string]any{{"context": "tab-of-" + name}}}, ""
		case "browsingContext.navigate":
			var p struct{ URL string }
			json.Unmarshal(params, &p)
			address = p.URL
			return map[string]any{}, ""
		case "script.callFunction":
			var p struct{ FunctionDeclaration string }
			json.Unmarshal(params, &p)
			var v any
			switch {
			case p.FunctionDeclaration == jsState:
				v = map[string]any{"url": address, "ready": "complete", "len": 12}
			case p.FunctionDeclaration == jsTitle:
				v = map[string]any{"title": "Page of " + name, "url": address}
			default:
				v = map[string]any{"title": "Page of " + name, "url": address, "text": "the page of " + name}
			}
			b, _ := json.Marshal(v)
			return map[string]any{"type": "success", "result": map[string]any{"type": "string", "value": string(b)}}, ""
		}
		return map[string]any{}, ""
	})
}

type sharedEnv struct {
	t        *testing.T
	sessions string
	s        *Shared
	url      string
	key      []byte
}

// newSharedEnv is a shared server for the sessions in ids, each with a
// browser of the test's own.
func newSharedEnv(t *testing.T, ids ...string) *sharedEnv {
	t.Helper()
	old := quitWait
	quitWait = 100 * time.Millisecond
	t.Cleanup(func() { quitWait = old })
	root := t.TempDir()
	exe := filepath.Join(root, "bin", "mullvadbrowser")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte(fakeBrowser), 0o755); err != nil {
		t.Fatal(err)
	}
	sessions := filepath.Join(root, "state", "sessions")
	for _, id := range ids {
		dir := filepath.Join(sessions, id)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		r := pageRemote(t, id)
		if err := os.WriteFile(filepath.Join(dir, "fake-port"), []byte(r.ln.Addr().String()), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s, err := NewShared(SharedOptions{Options: Options{Path: exe, Headless: true, Idle: time.Minute}, Sessions: sessions, Dir: filepath.Join(root, "state", "browser")})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	srv := httptest.NewServer(s.Handler(ctx))
	t.Cleanup(func() {
		srv.Close()
		cancel()
		s.CloseAll()
	})
	key, err := LoadKey(filepath.Join(root, "state", "browser"))
	if err != nil {
		t.Fatal(err)
	}
	return &sharedEnv{t: t, sessions: sessions, s: s, url: srv.URL + Endpoint, key: key}
}

// call posts a tool call with token and returns the status and the text
// of the result.
func (e *sharedEnv) call(token, tool, args string) (int, string) {
	e.t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + tool + `","arguments":` + args + `}}`
	req, _ := http.NewRequest("POST", e.url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var r struct {
		Result struct {
			Content []struct{ Text string } `json:"content"`
		} `json:"result"`
	}
	json.Unmarshal(b, &r)
	text := string(b)
	if len(r.Result.Content) > 0 {
		text = r.Result.Content[0].Text
	}
	return resp.StatusCode, text
}

// TestOneServerServesTwoSessionsEachInItsOwnProfile: one shared server
// gives each session a browser of its own, on a profile in that session's
// folder, and a session's token reaches its own tabs and no other's.
func TestOneServerServesTwoSessionsEachInItsOwnProfile(t *testing.T) {
	e := newSharedEnv(t, "s1alice", "s2alice")
	a, b := Token(e.key, "s1alice"), Token(e.key, "s2alice")

	if code, text := e.call(a, "open", `{"url":"https://a.example.com/"}`); code != 200 || !strings.Contains(text, "Page of s1alice") || !strings.Contains(text, "https://a.example.com/") {
		t.Fatalf("session 1 opens a page: %d %q", code, text)
	}
	// The other session's browser hasn't started: its call sees nothing
	// of the first's.
	if code, text := e.call(b, "tabs", `{}`); code != 200 || !strings.Contains(text, "nothing is open yet") {
		t.Fatalf("session 2 before it opened anything: %d %q", code, text)
	}
	if code, text := e.call(b, "open", `{"url":"https://b.example.com/"}`); code != 200 || !strings.Contains(text, "Page of s2alice") {
		t.Fatalf("session 2 opens a page: %d %q", code, text)
	}
	for _, c := range []struct{ token, want, not string }{
		{a, "https://a.example.com/", "b.example.com"},
		{b, "https://b.example.com/", "a.example.com"},
	} {
		if code, text := e.call(c.token, "tabs", `{}`); code != 200 || !strings.Contains(text, c.want) || strings.Contains(text, c.not) {
			t.Errorf("tabs: %d %q, want %s and not %s", code, text, c.want, c.not)
		}
	}
	for _, id := range []string{"s1alice", "s2alice"} {
		got, _ := os.ReadFile(filepath.Join(e.sessions, id, "fake-profile"))
		if want := filepath.Join(e.sessions, id, "browser"); strings.TrimSpace(string(got)) != want {
			t.Errorf("%s's browser ran on %q, want its own %q", id, got, want)
		}
	}
	// The status names both browsers, for a server after this one.
	e.s.save()
	st, err := ReadStatus(e.s.so.Dir)
	if err != nil || len(st.Open) != 2 || st.Open[0].Session != "s1alice" || st.Open[1].Profile != filepath.Join(e.sessions, "s2alice", "browser") {
		t.Errorf("status: %+v, %v", st, err)
	}

	// Another session's token, or the key's mark on another id, reaches
	// nothing.
	_, bMac, _ := strings.Cut(b, ".")
	for name, token := range map[string]string{
		"no token":                 "",
		"session 1 with 2's mark":  "s1alice." + bMac,
		"a mark of another key":    Token([]byte(strings.Repeat("k", 32)), "s1alice"),
		"a path for an id":         "../s1alice." + bMac,
		"an id and no mark at all": "s1alice",
	} {
		if code, text := e.call(token, "tabs", `{}`); code != http.StatusForbidden || strings.Contains(text, "example.com") {
			t.Errorf("%s: %d %q, want 403", name, code, text)
		}
	}
	if code, _ := e.call(Token(e.key, "s9gone"), "tabs", `{}`); code != http.StatusNotFound {
		t.Errorf("a session whose folder is gone: %d, want 404", code)
	}
}

// TestASessionsBrowserFollowsItsFreezeAndItsEnd: the shared server stops a
// session's browser while the session is frozen and continues it at the
// thaw, and closes it, removing its profile, once its agent has exited.
func TestASessionsBrowserFollowsItsFreezeAndItsEnd(t *testing.T) {
	e := newSharedEnv(t, "s1alice", "s2alice")
	a, b := Token(e.key, "s1alice"), Token(e.key, "s2alice")
	for _, token := range []string{a, b} {
		if code, text := e.call(token, "open", `{"url":"https://example.com/"}`); code != 200 {
			t.Fatalf("open: %d %q", code, text)
		}
	}
	ea, eb := e.s.entry("s1alice"), e.s.entry("s2alice")
	pidA, pidB := int(ea.b.pid.Load()), int(eb.b.pid.Load())
	if pidA == 0 || pidB == 0 || int(ea.b.pgid.Load()) != pidA {
		t.Fatalf("pids %d %d, group %d: each browser leads a group of its own", pidA, pidB, ea.b.pgid.Load())
	}
	dirA := filepath.Join(e.sessions, "s1alice")
	agent := exec.Command("sleep", "600")
	if err := agent.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { agent.Process.Kill(); agent.Wait() })
	writeJSON(t, filepath.Join(dirA, host.InfoFile), host.Info{PID: os.Getpid(), Agent: agent.Process.Pid, Freeze: 1})
	writeJSON(t, filepath.Join(dirA, host.FreezeFile), map[string]any{})
	e.s.Tick(context.Background(), time.Now())
	if !stopped(t, pidA) || stopped(t, pidB) {
		t.Fatalf("frozen session 1: its browser stopped %v, session 2's %v", stopped(t, pidA), stopped(t, pidB))
	}
	os.Remove(filepath.Join(dirA, host.FreezeFile))
	e.s.Tick(context.Background(), time.Now())
	if stopped(t, pidA) {
		t.Fatal("session 1's browser stays stopped after the thaw")
	}
	if code, text := e.call(a, "tabs", `{}`); code != 200 || !strings.Contains(text, "Page of s1alice") {
		t.Fatalf("after the thaw the tabs are still there: %d %q", code, text)
	}

	// The agent exits: its browser closes and its profile goes.
	agent.Process.Kill()
	agent.Wait()
	e.s.Tick(context.Background(), time.Now())
	for deadline := time.Now().Add(10 * time.Second); syscall.Kill(pidA, 0) == nil; {
		if time.Now().After(deadline) {
			t.Fatal("session 1's browser still runs after its agent exited")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(dirA, "browser")); !os.IsNotExist(err) {
		t.Errorf("session 1's profile is still there: %v", err)
	}
	if syscall.Kill(pidB, 0) != nil {
		t.Error("session 2's browser closed with session 1's")
	}
	// Woken from rest, the session browses again: a fresh browser, which
	// the tick leaves alone while its new agent runs.
	woken := exec.Command("sleep", "600")
	if err := woken.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { woken.Process.Kill(); woken.Wait() })
	time.Sleep(20 * time.Millisecond)
	writeJSON(t, filepath.Join(dirA, host.InfoFile), host.Info{PID: os.Getpid(), Agent: woken.Process.Pid, Freeze: 1})
	if code, text := e.call(a, "open", `{"url":"https://example.com/again"}`); code != 200 || !strings.Contains(text, "example.com/again") {
		t.Fatalf("the woken session opens a page: %d %q", code, text)
	}
	again := int(e.s.entry("s1alice").b.pid.Load())
	e.s.Tick(context.Background(), time.Now())
	if again == 0 || again == pidA || syscall.Kill(again, 0) != nil {
		t.Errorf("the woken session's browser: pid %d (was %d)", again, pidA)
	}
	// An exit file newer than the host's says the same.
	dirB := filepath.Join(e.sessions, "s2alice")
	writeJSON(t, filepath.Join(dirB, host.InfoFile), host.Info{PID: os.Getpid(), Agent: os.Getpid()})
	time.Sleep(20 * time.Millisecond)
	writeJSON(t, filepath.Join(dirB, host.ExitFile), host.Exit{Code: 0, At: time.Now()})
	if !Ended(dirB) {
		t.Error("a session whose agent wrote its exit has not ended")
	}
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, _ := json.Marshal(v)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// stopped reports whether process pid is stopped, as ps shows it.
func stopped(t *testing.T, pid int) bool {
	t.Helper()
	for range 50 {
		out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			t.Fatalf("ps %d: %v", pid, err)
		}
		// A signal lands a moment after kill returns.
		if s := strings.TrimSpace(string(out)); s != "" && s[0] != 'R' {
			return s[0] == 'T'
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func TestTokensAndTheKey(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "browser")
	k1, err := LoadKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	k2, err := LoadKey(dir)
	if err != nil || string(k1) != string(k2) || len(k1) != 32 {
		t.Fatalf("the key is made once and read after: %x %x %v", k1, k2, err)
	}
	if fi, err := os.Stat(filepath.Join(dir, KeyFile)); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("the key's mode: %v %v", fi.Mode(), err)
	}
	tok := Token(k1, "s1alice")
	if id, ok := sessionOf(k1, tok); !ok || id != "s1alice" {
		t.Errorf("a good token: %q %v", id, ok)
	}
	if _, ok := sessionOf(k1, strings.Replace(tok, "s1alice", "s2alice", 1)); ok {
		t.Error("a token renamed to another session passes")
	}
	for _, id := range []string{"", "a/b", "..", "s 1", strings.Repeat("a", 65)} {
		if sessionID(id) {
			t.Errorf("%q passes as a session's id", id)
		}
	}
	if s := fmt.Sprint(sessionID("s38g8kkjky1b5")); s != "true" {
		t.Error("a session's id fails")
	}
}

// TestTheServerKeepsItsPortSweepsWhatItsPredecessorLeftAndRetiresWhenIdle:
// a shared server listens where the one before it did, which its sessions
// have in their configs; stops the browsers that one left running when it
// was killed; and, asked to retire, exits only once no browser of its is
// open.
func TestTheServerKeepsItsPortSweepsWhatItsPredecessorLeftAndRetiresWhenIdle(t *testing.T) {
	e := newSharedEnv(t, "s1alice")
	dir := e.s.so.Dir
	so := e.s.so
	so.Idle = 2 * time.Second

	// The one before was killed: its browser runs on, named in its status.
	stale := filepath.Join(e.sessions, "s1alice", "browser")
	if err := os.MkdirAll(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	left := exec.Command(so.Path, "--profile", stale)
	if err := left.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { left.Process.Kill() })
	exited := make(chan struct{})
	go func() { left.Wait(); close(exited) }()
	os.WriteFile(filepath.Join(stale, pidFile), []byte(strconv.Itoa(left.Process.Pid)+"\n"), 0o600)
	free, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(dir, StatusFile), Status{PID: 999999, Addr: free, Open: []Opened{{Session: "s1alice", PID: left.Process.Pid, Profile: stale}}})

	served := make(chan error, 1)
	ready := make(chan *Shared, 1)
	go func() { served <- ServeShared(context.Background(), so, func(s *Shared) { ready <- s }) }()
	var s *Shared
	select {
	case s = <-ready:
	case err := <-served:
		t.Fatal(err)
	case <-time.After(10 * time.Second):
		t.Fatal("the server did not start")
	}
	if s.Addr() != free {
		t.Errorf("it listens at %s, not at %s, where the one before did", s.Addr(), free)
	}
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("the browser the server before left running still runs")
	}
	if st, err := ReadStatus(dir); err != nil || st.PID != os.Getpid() || st.Addr != free || len(st.Open) != 0 {
		t.Errorf("status: %+v, %v", st, err)
	}

	// Asked to retire with a browser open, it waits for the browser to
	// close, here after so.Idle without a call.
	e.url = "http://" + s.Addr() + Endpoint
	if code, text := e.call(Token(e.key, "s1alice"), "open", `{"url":"https://example.com/"}`); code != 200 {
		t.Fatalf("open: %d %q", code, text)
	}
	s.Retire()
	select {
	case err := <-served:
		t.Fatalf("it retired with a browser open: %v", err)
	case <-time.After(1500 * time.Millisecond):
	}
	select {
	case err := <-served:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("it did not retire once its browser closed")
	}
	if st, err := ReadStatus(dir); err != nil || len(st.Open) != 0 || st.Addr != free {
		t.Errorf("status after it retired: %+v, %v", st, err)
	}
}

func freePort() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer ln.Close()
	return ln.Addr().String(), nil
}
