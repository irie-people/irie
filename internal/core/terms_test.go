//go:build !noterms

package core

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/config"
)

// TestTheCoreServesTheTermsPage: while Terms in short is on, the core
// serves the Terms tab's page, which answers its own requests; turned off,
// the page goes, and turned on again it comes back.
func TestTheCoreServesTheTermsPage(t *testing.T) {
	c := bareCore(t, config.Default())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	oldAddr, oldLook := termsAddr, termsLook
	termsAddr, termsLook = addr, 50*time.Millisecond
	t.Cleanup(func() { termsAddr, termsLook = oldAddr, oldLook })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		c.serveTerms(ctx)
		close(done)
	}()
	defer func() {
		cancel()
		<-done
	}()
	served := func() bool {
		resp, err := http.Get("http://" + addr + "/")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode == 200 && strings.Contains(string(b), "panel.js")
	}
	wait := func(want bool, what string) {
		t.Helper()
		end := time.Now().Add(10 * time.Second)
		for served() != want {
			if time.Now().After(end) {
				t.Fatalf("waiting for the page to be %s", what)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	wait(true, "served")
	req, _ := http.NewRequest(http.MethodPost, "http://"+addr+"/api", strings.NewReader(`{"id":1,"op":"hello"}`))
	req.Header.Set("Origin", "http://"+addr)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(b), `"on":true`) {
		t.Errorf("hello: %s", b)
	}
	cfg := *c.cfg()
	cfg.Plugins.Terms = false
	c.useConfig(cfg)
	wait(false, "gone")
	cfg.Plugins.Terms = true
	c.useConfig(cfg)
	wait(true, "back")
}
