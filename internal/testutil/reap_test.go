package testutil

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/proc"
)

func TestMain(m *testing.M) { Main(m) }

// serverTree is the tmux server at sock and what it runs.
func serverTree(t *testing.T, sock string) []proc.Proc {
	t.Helper()
	out, err := exec.Command("tmux", "-S", sock, "display-message", "-p", "#{pid}").Output()
	if err != nil {
		t.Fatalf("no tmux server at %s: %v", sock, err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	tb, err := proc.Read()
	if err != nil {
		t.Fatal(err)
	}
	srv, ok := tb.Get(pid)
	if !ok {
		t.Fatalf("no process %d", pid)
	}
	return append([]proc.Proc{srv}, tb.Tree(pid)...)
}

// startTmux starts a tmux server with no config of the user's in dir, a
// TMUX_TMPDIR, as `tmux -L name`, and returns its socket.
func startTmux(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command("tmux", append([]string{"-L", name, "-f", "/dev/null"}, args...)...)
	cmd.Env = append(ownEnviron(), "TMUX_TMPDIR="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tmux %v: %v\n%s", args, err, out)
	}
	return filepath.Join(dir, fmt.Sprintf("tmux-%d", os.Getuid()), name)
}

func TestCleanupEndsEveryServerOfTheEnvAndWhatItRuns(t *testing.T) {
	NeedTmux(t)
	defer func(g, r time.Duration) { stopGrace, restGrace = g, r }(stopGrace, restGrace)
	stopGrace, restGrace = 300*time.Millisecond, 100*time.Millisecond

	var procs []proc.Proc
	var short string
	t.Run("env", func(t *testing.T) {
		e := NewEnv(t)
		short = filepath.Dir(e.TmuxTmpDir)
		// A pane whose shell and its children ignore the SIGHUP that the
		// end of the pane brings, one of them stopped, as a hook a freeze
		// stopped is.
		main := startTmux(t, e.TmuxTmpDir, e.Socket, "new-session", "-d", "-s", "x", "--", "/bin/sh", "-c", "trap '' HUP; sleep 601 & kill -STOP $!; sleep 600")
		// A team server: no session, and kept by exit-empty off.
		conf := filepath.Join(t.TempDir(), "teams.conf")
		WriteFiles(t, filepath.Dir(conf), map[string]string{"teams.conf": "set -s exit-empty off\n"})
		cmd := exec.Command("tmux", "-L", e.Socket+"-teams", "-f", conf, "start-server")
		cmd.Env = append(ownEnviron(), "TMUX_TMPDIR="+e.TmuxTmpDir)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("team server: %v\n%s", err, out)
		}
		Eventually(t, 5*time.Second, "the pane's sleeps never started", func() bool {
			return len(serverTree(t, main)) >= 4
		})
		procs = append(serverTree(t, main), serverTree(t, filepath.Join(filepath.Dir(main), e.Socket+"-teams"))...)
	})
	if left := alive(procs); len(left) > 0 {
		t.Errorf("after the cleanup these still run: %+v", left)
	}
	if _, err := os.Stat(short); !os.IsNotExist(err) {
		t.Errorf("the short dir is still there: %v", err)
	}
}

// TestHelperKilledRun is the run that TestTheReaperCleansUpAfterAKilledRun
// kills: it starts a tmux server, says where, and waits.
func TestHelperKilledRun(t *testing.T) {
	if os.Getenv("IRIE_TEST_HELPER") != "killed-run" {
		t.Skip("the run that TestTheReaperCleansUpAfterAKilledRun kills")
	}
	e := NewEnv(t)
	sock := startTmux(t, e.TmuxTmpDir, e.Socket, "new-session", "-d", "-s", "x", "--", "sleep", "600")
	fmt.Printf("socket %s\n", sock)
	time.Sleep(time.Hour)
}

func TestTheReaperCleansUpAfterAKilledRun(t *testing.T) {
	NeedTmux(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperKilledRun$")
	cmd.Env = append(os.Environ(), "IRIE_TEST_HELPER=killed-run")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	socks := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			if s, ok := strings.CutPrefix(sc.Text(), "socket "); ok {
				socks <- s
			}
		}
		close(socks)
	}()
	var sock string
	select {
	case sock = <-socks:
	case <-time.After(30 * time.Second):
	}
	if sock == "" {
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatal("the helper run never started its tmux server")
	}
	procs := serverTree(t, sock)
	// tmux-<uid> in tmux in the short dir.
	short := filepath.Dir(filepath.Dir(filepath.Dir(sock)))
	if !strings.HasPrefix(filepath.Base(short), shortPrefix) {
		t.Fatalf("socket %s is not in a short dir", sock)
	}
	t.Cleanup(func() { cleanDir(short) })

	cmd.Process.Kill()
	cmd.Wait()
	Eventually(t, 20*time.Second, "the killed run's tmux server, its sleep or its short dir is still there", func() bool {
		_, err := os.Stat(short)
		return len(alive(procs)) == 0 && os.IsNotExist(err)
	})
}

func TestReapCleansOnlyTheDirsLeftUnreleased(t *testing.T) {
	kept, left := t.TempDir(), t.TempDir()
	if code := reap(strings.NewReader("+" + kept + "\n+" + left + "\n-" + kept + "\n")); code != 0 {
		t.Fatalf("reap = %d", code)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Errorf("a released dir went: %v", err)
	}
	if _, err := os.Stat(left); !os.IsNotExist(err) {
		t.Errorf("a dir left unreleased is still there: %v", err)
	}
}

func TestSweepCleansTheDirsOfARunThatDied(t *testing.T) {
	NeedTmux(t)
	prefix := "irie-sweep-" + Random(4) + "-"
	mk := func() string {
		d, err := os.MkdirTemp("/tmp", prefix)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { cleanDir(d) })
		return d
	}
	start := self()
	if start == 0 {
		t.Skip("the process table does not say when this process started")
	}
	// A run that died: the pid is this one's, the start is not.
	dead, live, unowned := mk(), mk(), mk()
	WriteFiles(t, dead, map[string]string{ownerFile: fmt.Sprintf("%d %d\n", os.Getpid(), start+1)})
	WriteFiles(t, live, map[string]string{ownerFile: fmt.Sprintf("%d %d\n", os.Getpid(), start)})
	procs := serverTree(t, startTmux(t, dead, "s", "new-session", "-d", "-s", "x", "--", "sleep", "600"))
	liveSock := startTmux(t, live, "s", "new-session", "-d", "-s", "x", "--", "sleep", "600")
	unownedSock := startTmux(t, unowned, "s", "new-session", "-d", "-s", "x", "--", "sleep", "600")

	sweep(filepath.Join("/tmp", prefix+"*"))
	if left := alive(procs); len(left) > 0 {
		t.Errorf("the dead run's server still runs: %+v", left)
	}
	if _, err := os.Stat(dead); !os.IsNotExist(err) {
		t.Errorf("the dead run's dir is still there: %v", err)
	}
	for _, s := range []string{liveSock, unownedSock} {
		if err := exec.Command("tmux", "-S", s, "has-session").Run(); err != nil {
			t.Errorf("the sweep ended %s: %v", s, err)
		}
	}
}
