package winapp

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/irie-people/irie/internal/brand"
)

// TestTheWindowsProgramsBuild: Irie for Windows and its wrapper build for
// Windows, as a release builds them (tools/release.sh), from any computer.
// Each links everything internal/ipc imports, so the wire types must keep
// clear of code that builds on Linux and macOS alone: release 1.10.0's
// downloads failed when ipc came to import internal/lessons, and with it
// the harness, the host and their flock, kill and process groups.
func TestTheWindowsProgramsBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the Windows programs")
	}
	// The go on PATH, as internal/testutil finds it; else the toolchain
	// that built this test.
	goBin, err := exec.LookPath("go")
	if err != nil {
		goBin = filepath.Join(runtime.GOROOT(), "bin", "go")
	}
	args := []string{"build", "-o", t.TempDir()}
	if brand.Tags != "" {
		args = append(args, "-tags", brand.Tags)
	}
	build := exec.Command(goBin, append(args, "./cmd/windows", "./cmd/windows-app")...)
	build.Dir = filepath.Join("..", "..")
	build.Env = append(os.Environ(), "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("GOOS=windows go build ./cmd/windows ./cmd/windows-app: %v\n%s", err, out)
	}
}
