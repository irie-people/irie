package core

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/irie-people/irie/internal/proc"
)

// How soon the supervisor starts its child again: at once after a run that
// lasted, so a killed core or sidebar is back before anyone waits for it,
// then twice as long after each run that ended within a minute, so one
// that cannot start does not spin.
const (
	firstRestart = 100 * time.Millisecond
	maxRestart   = 5 * time.Second
)

// Supervise runs `bin args...` and starts it again whenever it exits,
// waiting 0.1, 0.2, 0.4 … 5 s between starts. A run that lasted a minute
// resets the wait. A stop the child did not ask for, such as a stray
// SIGSTOP, is undone at once, and so is the background state: Irie never
// stops or slows its own core or sidebar, and a stopped one would leave
// every client waiting. A signal to the supervisor goes to the child, and
// both stop.
func Supervise(bin string, args []string, log *slog.Logger) int {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT)
	wait := firstRestart
	for {
		started := time.Now()
		p, err := os.StartProcess(bin, append([]string{bin}, args...), &os.ProcAttr{Files: []*os.File{os.Stdin, os.Stdout, os.Stderr}})
		if err != nil {
			log.Error("supervise: start", "args", args, "err", err)
		} else {
			done := make(chan error, 1)
			ended := make(chan struct{})
			go func() { done <- waitRunning(p.Pid, log); close(ended) }()
			go keepForeground(p.Pid, ended)
			select {
			case err := <-done:
				log.Warn("supervise: exited", "args", args, "err", err, "ran", time.Since(started).Round(time.Millisecond))
			case s := <-stop:
				p.Signal(s)
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					p.Kill()
					<-done
				}
				return 0
			}
		}
		if time.Since(started) > time.Minute {
			wait = firstRestart
		}
		select {
		case <-time.After(wait):
		case <-stop:
			return 0
		}
		wait = min(2*wait, maxRestart)
	}
}

// waitRunning waits for the process pid to exit, and continues it each
// time something stops it.
func waitRunning(pid int, log *slog.Logger) error {
	for {
		var ws syscall.WaitStatus
		_, err := syscall.Wait4(pid, &ws, syscall.WUNTRACED, nil)
		switch {
		case err == syscall.EINTR:
			continue
		case err != nil:
			return err
		case stoppedStatus(ws):
			log.Warn("supervise: continued a stopped child", "pid", pid, "signal", syscall.Signal((ws>>8)&0xff).String())
			syscall.Kill(pid, syscall.SIGCONT)
			continue
		case ws.Signaled():
			return fmt.Errorf("signal: %s", ws.Signal())
		case ws.ExitStatus() != 0:
			return fmt.Errorf("exit status %d", ws.ExitStatus())
		}
		return nil
	}
}

// keepForeground takes the child pid out of macOS's background state twice
// a second until ended closes. macOS keeps the state a process was put in
// by another apart from the one it set itself, and a process can clear only
// its own: so the core, which keeps Irie's other processes out of the
// background (steady.go), cannot take itself out, and its supervisor does.
func keepForeground(pid int, ended <-chan struct{}) {
	if !proc.CanBackground {
		return
	}
	t := time.NewTicker(steadyEvery)
	defer t.Stop()
	for {
		select {
		case <-ended:
			return
		case <-t.C:
			proc.SetBackground(pid, false)
		}
	}
}

// stoppedStatus reports whether ws is a stop. As the host's own wait does
// (internal/host), it reads the bits itself: on macOS WaitStatus.Stopped
// compares the signal with SIGSTOP where <sys/wait.h> has SIGCONT, so it
// takes a stop by SIGSTOP for a continue. Without WCONTINUED, wait4
// reports no continues, so the low seven bits all set mean a stop.
func stoppedStatus(ws syscall.WaitStatus) bool { return ws&0x7f == 0x7f }
