package browser

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRealBrowser drives the browser this machine has, Mullvad Browser or
// browser.path's, through the tools. It needs the network and starts a
// browser, so it runs only with IRIE_BROWSER_E2E=1:
//
//	IRIE_BROWSER_E2E=1 go test ./internal/browser -run TestRealBrowser -v
func TestRealBrowser(t *testing.T) {
	if os.Getenv("IRIE_BROWSER_E2E") == "" {
		t.Skip("set IRIE_BROWSER_E2E=1 to drive a real browser")
	}
	exe, err := Find(os.Getenv("IRIE_BROWSER_PATH"))
	if err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	b := New(Options{Path: os.Getenv("IRIE_BROWSER_PATH"), Profile: filepath.Join(dir, "profile"), Template: filepath.Join(dir, "template"), Headless: true})
	defer b.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := MakeTemplate(ctx, b.o, exe); err != nil {
		t.Fatalf("making the template: %v", err)
	}
	tools := b.tools()
	use := func(name string, args map[string]any) toolResult {
		t.Helper()
		raw, _ := json.Marshal(args)
		r := tools[slicesIndex(tools, name)].Call(ctx, raw)
		t.Logf("%s %s:\n%s", name, raw, r.Content[len(r.Content)-1].Text)
		if r.IsError {
			t.Fatalf("%s failed", name)
		}
		return r
	}
	r := use("open", map[string]any{"url": "https://example.com/"})
	if !strings.Contains(r.Content[0].Text, "Tab 1 · Example Domain") {
		t.Fatal("example.com did not open in tab 1")
	}
	r = use("links", nil)
	if !strings.Contains(r.Content[0].Text, "[1] ") {
		t.Fatal("no numbered link")
	}
	r = use("eval", map[string]any{"script": "navigator.webdriver"})
	if strings.TrimSpace(r.Content[0].Text) != "true" {
		t.Fatal("navigator.webdriver is not true under remote control")
	}
	r = use("eval", map[string]any{"script": "Intl.DateTimeFormat().resolvedOptions().timeZone"})
	t.Logf("the page sees the time zone %s", r.Content[0].Text)
	r = use("screenshot", nil)
	if r.Content[0].Type != "image" || len(r.Content[0].Data) < 1000 {
		t.Fatal("no screenshot")
	}
	profile := b.run.profile
	b.Close()
	if _, err := os.Stat(profile); !os.IsNotExist(err) {
		t.Fatalf("the profile is still there after close: %v", err)
	}
}
