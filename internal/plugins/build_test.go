//go:build !novpn && !noprivacy && !nowriting && !nomarkets && !nomail && !nobitwarden

package plugins

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestABuildWithoutPlugins: go build -tags novpn,noprivacy,nowriting,
// nomarkets,nomail,nobitwarden builds every package, and its irie links none of the
// plugins' code (DESIGN §4.17); and a company's copy, which keeps the VPN
// plugin to drive the VPN it approves (§4.18) and leaves the rest out,
// builds and links the VPN's code and none of the others'.
func TestABuildWithoutPlugins(t *testing.T) {
	if testing.Short() {
		t.Skip("builds Irie without its plugins")
	}
	// The go on PATH, as internal/testutil finds it; else the toolchain
	// that built this test.
	goBin, err := exec.LookPath("go")
	if err != nil {
		goBin = filepath.Join(runtime.GOROOT(), "bin", "go")
	}
	tags := "-tags=novpn,noprivacy,nowriting,nomarkets,nomail,nobitwarden"
	build := exec.Command(goBin, "build", tags, "./...")
	build.Dir = filepath.Join("..", "..")
	out, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("go build %s ./...: %v\n%s", tags, err, out)
	}
	list := exec.Command(goBin, "list", "-deps", tags, "./cmd/irie")
	list.Dir = build.Dir
	out, err = list.Output()
	if err != nil {
		t.Fatalf("go list -deps %s: %v", tags, err)
	}
	deps := strings.Fields(string(out))
	for _, pkg := range []string{"vpn", "anon", "browser", "dms", "htmltext", "mcp", "mail", "variorum"} {
		for _, d := range deps {
			if d == "github.com/irie-people/irie/internal/"+pkg {
				t.Errorf("a build without plugins links internal/%s", pkg)
			}
		}
	}
	// Nor the mail plugin's IMAP and MIME libraries.
	for _, d := range deps {
		if strings.HasPrefix(d, "github.com/emersion/") {
			t.Errorf("a build without plugins links %s", d)
		}
	}
	// A company's copy keeps only the VPN plugin: Mail reads a person's own
	// accounts, so it goes with the other personal plugins.
	company := "-tags=noprivacy,nowriting,nomarkets,nomail"
	build = exec.Command(goBin, "build", "-o", filepath.Join(t.TempDir(), "irie"), company, "./cmd/irie")
	build.Dir = list.Dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build %s ./cmd/irie: %v\n%s", company, err, out)
	}
	list = exec.Command(goBin, "list", "-deps", company, "./cmd/irie")
	list.Dir = build.Dir
	out, err = list.Output()
	if err != nil {
		t.Fatalf("go list -deps %s: %v", company, err)
	}
	links := map[string]bool{}
	for _, d := range strings.Fields(string(out)) {
		links[strings.TrimPrefix(d, "github.com/irie-people/irie/internal/")] = true
	}
	if !links["vpn"] {
		t.Error("a company's copy lacks internal/vpn, so it can't drive its VPN")
	}
	for _, pkg := range []string{"anon", "browser", "dms", "htmltext", "mcp", "mail", "variorum"} {
		if links[pkg] {
			t.Errorf("a company's copy links internal/%s", pkg)
		}
	}
	// The list must hold what every build has, or the look above means nothing.
	for _, want := range []string{"github.com/irie-people/irie/internal/config", "github.com/irie-people/irie/internal/core"} {
		found := false
		for _, d := range deps {
			found = found || d == want
		}
		if !found {
			t.Errorf("go list -deps lacks %s: is the list right?", want)
		}
	}
}
