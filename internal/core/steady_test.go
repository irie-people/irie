package core

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/proc"
	"github.com/irie-people/irie/internal/testutil"
	"github.com/irie-people/irie/internal/tmux"
)

func TestWaitRunningContinuesAStoppedChild(t *testing.T) {
	p, err := os.StartProcess("/bin/sleep", []string{"sleep", "30"}, &os.ProcAttr{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Kill() })
	done := make(chan error, 1)
	go func() { done <- waitRunning(p.Pid, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
	for range 3 {
		if err := p.Signal(syscall.SIGSTOP); err != nil {
			t.Fatal(err)
		}
		testutil.Eventually(t, 5*time.Second, "the stopped child was not continued", func() bool {
			q, err := proc.One(p.Pid)
			return err == nil && !q.Stopped
		})
	}
	p.Kill()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "killed") {
			t.Errorf("waitRunning after a kill = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waitRunning did not return after the child was killed")
	}
}

// ownProcs are the pids of Irie's own processes in c's server: the tmux
// server, the core's supervisor and the core, the sidebar's supervisor and
// the sidebar.
func (c *coreEnv) ownProcs() map[string]int {
	c.t.Helper()
	pid := func(s string) int {
		n, err := strconv.Atoi(s)
		if err != nil || n <= 1 {
			c.t.Fatalf("no pid in %q", s)
		}
		return n
	}
	core := tmux.AgentsSession + ":core"
	return map[string]int{
		"server":             pid(c.Tmux(c.t, "display-message", "-p", "#{pid}")),
		"core's supervisor":  pid(c.Tmux(c.t, "display-message", "-p", "-t", core, "#{pane_pid}")),
		"core":               c.supervised(core),
		"sidebar supervisor": pid(c.Tmux(c.t, "display-message", "-p", "-t", tmux.Sidebar, "#{pane_pid}")),
		"sidebar":            c.supervised(tmux.Sidebar),
	}
}

// steadyBound is how soon Irie must have undone what the tests do to its
// own processes. A test in the background, or on a machine with more work
// waiting than CPUs, waits on the scheduler (slow).
func steadyBound(t *testing.T) time.Duration {
	if slow(t) {
		return 20 * time.Second
	}
	return 3 * time.Second
}

// TestIrieKeepsItsOwnProcessesRunning plays a chaos monkey: it stops each
// of Irie's own processes in turn, the tmux server first, and kills the
// core. Each runs again within moments, and the core answers.
func TestIrieKeepsItsOwnProcessesRunning(t *testing.T) {
	c := setup(t)
	own := c.ownProcs()
	bound := steadyBound(t)
	for _, name := range []string{"server", "core's supervisor", "core", "sidebar supervisor", "sidebar"} {
		pid := own[name]
		if err := syscall.Kill(pid, syscall.SIGSTOP); err != nil {
			t.Fatalf("stop the %s: %v", name, err)
		}
		start := time.Now()
		testutil.Eventually(t, bound, "the "+name+" stayed stopped", func() bool {
			p, err := proc.One(pid)
			return err == nil && !p.Stopped
		})
		t.Logf("the %s ran again after %s", name, time.Since(start).Round(time.Millisecond))
		var r ipc.ListResult
		testutil.Eventually(t, bound, "the core did not answer after the "+name+" was stopped", func() bool {
			return c.call("list", nil, &r) == nil
		})
	}

	kill(t, own["core"])
	start := time.Now()
	testutil.Eventually(t, bound, "the supervisor did not bring a killed core back", func() bool {
		var r ipc.ListResult
		return c.call("list", nil, &r) == nil
	})
	t.Logf("a killed core answered again after %s", time.Since(start).Round(time.Millisecond))
}

func TestIrieTakesItsOwnProcessesOutOfTheBackground(t *testing.T) {
	if !proc.CanBackground {
		t.Skip("no background state here")
	}
	c := setup(t)
	own := c.ownProcs()
	for name, pid := range own {
		if err := proc.SetBackground(pid, true); err != nil {
			t.Fatalf("put the %s in the background: %v", name, err)
		}
	}
	deadline := time.Now().Add(steadyBound(t))
	for {
		var still []string
		for name, pid := range own {
			if pri(t, pid) == "4" {
				still = append(still, fmt.Sprintf("the %s (pid %d)", name, pid))
			}
		}
		if len(still) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("still in the background: %s", strings.Join(still, ", "))
		}
		time.Sleep(50 * time.Millisecond)
	}
	// What the server starts from then on is out of the background too.
	a := c.spawn("task a")
	host := c.Tmux(t, "display-message", "-p", "-t", a.Pane, "#{pane_pid}")
	if out, _ := exec.Command("ps", "-o", "pri=", "-p", host).Output(); strings.TrimSpace(string(out)) == "4" {
		t.Errorf("a new agent's host (pid %s) started in the background", host)
	}
}
