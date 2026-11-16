package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

// A run of the test runner (tools/test.sh) builds the helpers once, and
// each test process links that build under the name its tests use.
func TestATestProcessTakesTheRunnersBuild(t *testing.T) {
	bins := t.TempDir()
	if err := os.WriteFile(filepath.Join(bins, "fakellama"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(BinsEnv, bins)
	dir := t.TempDir()
	bin := filepath.Join(dir, "llama-server")
	if !prebuilt("github.com/irie-people/irie/internal/testutil/fakellama", bin) {
		t.Fatal("the runner's build was not taken")
	}
	if b, err := os.ReadFile(bin); err != nil || string(b) != "#!/bin/sh\n" {
		t.Errorf("llama-server = %q, %v; want the runner's build", b, err)
	}
	if prebuilt("github.com/irie-people/irie/cmd/irie", filepath.Join(dir, "irie")) {
		t.Error("a helper the runner did not build was taken as built")
	}
	t.Setenv(BinsEnv, "")
	if prebuilt("github.com/irie-people/irie/internal/testutil/fakellama", filepath.Join(dir, "other")) {
		t.Error("without the runner a build was taken as made")
	}
}
