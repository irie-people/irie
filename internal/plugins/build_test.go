//go:build !noprivacy && !nowriting

package plugins

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestABuildWithoutPlugins: go build -tags noprivacy,nowriting builds every
// package, and its irie links none of the plugins' code (DESIGN §4.17), as
// a company's copy that leaves them out needs.
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
	tags := "-tags=noprivacy,nowriting"
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
	for _, pkg := range []string{"vpn", "anon", "browser", "dms", "htmltext", "mcp"} {
		for _, d := range deps {
			if d == "github.com/irie-people/irie/internal/"+pkg {
				t.Errorf("a build without plugins links internal/%s", pkg)
			}
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
