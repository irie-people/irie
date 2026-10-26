package core

import (
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

// Supervise runs `bin args...` and starts it again whenever it exits,
// waiting 1, 2, 4 … 30 s between starts. A run that lasted a minute resets
// the wait. A signal to the supervisor goes to the child, and both stop.
func Supervise(bin string, args []string, log *slog.Logger) int {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT)
	wait := time.Second
	for {
		cmd := exec.Command(bin, args...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		started := time.Now()
		if err := cmd.Start(); err != nil {
			log.Error("supervise: start", "args", args, "err", err)
		} else {
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case err := <-done:
				log.Warn("supervise: exited", "args", args, "err", err, "ran", time.Since(started).Round(time.Millisecond))
			case s := <-stop:
				cmd.Process.Signal(s)
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					cmd.Process.Kill()
					<-done
				}
				return 0
			}
		}
		if time.Since(started) > time.Minute {
			wait = time.Second
		}
		select {
		case <-time.After(wait):
		case <-stop:
			return 0
		}
		wait = min(2*wait, 30*time.Second)
	}
}
