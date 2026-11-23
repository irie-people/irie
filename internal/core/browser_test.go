//go:build !noprivacy

package core

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/browser"
	"github.com/irie-people/irie/internal/harness"
)

// TestSessionsShareOneBrowserServer: the first session that wants Irie's
// browser runs a server of its own and starts the shared one; the
// sessions after it get the shared server's address and a token of their
// own, which reaches it for that session alone; and a shared server that
// was killed starts again where it listened, for the sessions that have
// its address.
func TestSessionsShareOneBrowserServer(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "mullvadbrowser")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\nexec sleep 600\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := setupWith(t, agentConfig{
		env:   map[string]string{"IRIE_FAKE_MCP": "1"},
		extra: fmt.Sprintf("[plugins]\nprivacy = true\n\n[browser]\nconnector = \"on\"\npath = %q\n", exe),
	})
	servers := func(id string) harness.MCPServer {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(c.paths.Session(id), harness.MCPFile))
		if err != nil {
			t.Fatal(err)
		}
		var f struct {
			Servers map[string]harness.MCPServer `json:"mcpServers"`
		}
		if err := json.Unmarshal(b, &f); err != nil {
			t.Fatal(err)
		}
		return f.Servers[browser.ServerName]
	}

	first := c.spawn("look up the pricing page")
	if s := servers(first.ID); s.Type != "" || s.Command != c.bin || len(s.Args) < 3 || s.Args[0] != "browser" || !strings.Contains(strings.Join(s.Args, " "), filepath.Join(c.paths.Session(first.ID), "browser")) {
		t.Fatalf("the first session's browser: %+v, want a server of its own", s)
	}
	st := waitStatus(t, c.paths.BrowserServer(), func(st browser.Status) bool { return st.Addr != "" })

	// The sessions after it share the server, once the core has seen it.
	var second string
	var s harness.MCPServer
	for range 15 {
		second = c.spawn("compare the plans").ID
		if s = servers(second); s.Type == harness.MCPHTTP {
			break
		}
		time.Sleep(time.Second)
	}
	if s.Type != harness.MCPHTTP || s.URL != "http://"+st.Addr+browser.Endpoint || s.Timeout != browser.CallTimeout || s.Command != "" {
		t.Fatalf("a later session's browser: %+v, want the shared server at %s", s, st.Addr)
	}
	token, ok := strings.CutPrefix(s.Headers["Authorization"], "Bearer ")
	if !ok || !strings.HasPrefix(token, second+".") {
		t.Fatalf("its token: %q", s.Headers["Authorization"])
	}
	if code, body := listTools(t, s.URL, token); code != 200 || !strings.Contains(body, `"name":"open"`) {
		t.Fatalf("tools/list with its token: %d %s", code, body)
	}
	_, mac, _ := strings.Cut(token, ".")
	if code, _ := listTools(t, s.URL, first.ID+"."+mac); code != http.StatusForbidden {
		t.Errorf("another session's id with its mark: %d, want 403", code)
	}

	// Killed, it starts again where it listened.
	if err := syscall.Kill(st.PID, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	again := waitStatus(t, c.paths.BrowserServer(), func(n browser.Status) bool {
		return n.PID != st.PID && n.PID > 1 && syscall.Kill(n.PID, 0) == nil
	})
	if again.Addr != st.Addr {
		t.Fatalf("it started again at %s, not %s", again.Addr, st.Addr)
	}
	if code, _ := listTools(t, s.URL, token); code != 200 {
		t.Errorf("the session's token after the restart: %d", code)
	}
}

// waitStatus waits for the shared browser server's status to pass ok.
func waitStatus(t *testing.T, dir string, ok func(browser.Status) bool) browser.Status {
	t.Helper()
	for deadline := time.Now().Add(30 * time.Second); ; {
		st, err := browser.ReadStatus(dir)
		if err == nil && ok(st) {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("the shared browser server's status: %+v, %v", st, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func listTools(t *testing.T, url, token string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("POST", url, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}
