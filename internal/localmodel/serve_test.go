package localmodel

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/testutil"
)

func TestMain(m *testing.M) { testutil.Main(m) }

// runServe starts `irie runtime -- sh -c script`, where script starts a
// stand-in for a model runner and writes its pid to $RUNNER. It returns the
// command and the runner's pid.
func runServe(t *testing.T, script string) (*exec.Cmd, int) {
	t.Helper()
	pidFile := filepath.Join(t.TempDir(), "runner.pid")
	cmd := exec.Command(testutil.BuildIrie(t), "runtime", "--", "sh", "-c", script)
	cmd.Env = append(os.Environ(), "RUNNER="+pidFile)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var pid int
	testutil.Eventually(t, 5*time.Second, "the runner did not start", func() bool {
		b, _ := os.ReadFile(pidFile)
		pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		return pid > 0
	})
	t.Cleanup(func() {
		syscall.Kill(pid, syscall.SIGKILL)
		cmd.Process.Kill()
	})
	return cmd, pid
}

func gone(pid int) bool { return syscall.Kill(pid, 0) != nil }

func exitCode(t *testing.T, cmd *exec.Cmd) int {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("irie runtime did not exit")
	}
	return cmd.ProcessState.ExitCode()
}

func TestAHangupStopsTheServerAndItsRunners(t *testing.T) {
	// The server waits for its runner, as ollama serve does; SIGTERM ends
	// both, as it does for Ollama.
	cmd, runner := runServe(t, `sleep 1000 & echo $! > "$RUNNER"; wait`)
	cmd.Process.Signal(syscall.SIGHUP)
	exitCode(t, cmd)
	testutil.Eventually(t, 5*time.Second, "the runner outlived the server", func() bool { return gone(runner) })
}

func TestARunnerLeftBehindIsStopped(t *testing.T) {
	// The server exits on its own and leaves a runner in its group.
	cmd, runner := runServe(t, `sleep 1000 & echo $! > "$RUNNER"; sleep 0.2; exit 3`)
	if code := exitCode(t, cmd); code != 3 {
		t.Errorf("exit %d, want the server's 3", code)
	}
	if !gone(runner) {
		t.Error("the runner outlived the server")
	}
}

func TestARunnerThatIgnoresTerminationIsKilled(t *testing.T) {
	// It holds out through the irie binary's reapGrace, then is killed.
	cmd, runner := runServe(t, `(trap '' TERM; exec sleep 1000) & echo $! > "$RUNNER"; sleep 0.2; exit 0`)
	start := time.Now()
	if code := exitCode(t, cmd); code != 0 {
		t.Errorf("exit %d", code)
	}
	if !gone(runner) {
		t.Error("a runner that ignores SIGTERM outlived the server")
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("took %v", d)
	}
}

func TestServeNeedsACommand(t *testing.T) {
	if code := Serve(nil); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
}
