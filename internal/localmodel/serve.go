package localmodel

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

// Grace periods for stopping a server: how long it has to unload its
// models after SIGTERM, and how long what is left of its process group has
// after its own SIGTERM, before SIGKILL.
var (
	serveGrace = 10 * time.Second
	reapGrace  = 2 * time.Second
)

// Serve is `irie runtime -- <ollama> serve`, the first process of the pane
// of Irie's own Ollama server (DESIGN §6.3). It makes sure the server's
// model runners go with it. Ollama unloads its runners on SIGTERM or
// SIGINT, but a hangup, which tmux sends when the pane or the whole tmux
// server goes, ends it at once and leaves its runners running, each holding
// a model's memory. So the server runs in a process group of its own; a
// hangup, an interrupt or a termination of Serve becomes SIGTERM for that
// group, and whatever is left of the group once the server has exited, on
// its own or after serveGrace, is stopped too. Serve returns the server's
// exit code.
func Serve(argv []string) int {
	if len(argv) == 0 {
		fmt.Fprintln(os.Stderr, "usage: irie runtime -- COMMAND [ARGS...]")
		return 2
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "irie runtime:", err)
		return 127
	}
	group := cmd.Process.Pid
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var err error
	select {
	case err = <-done:
	case <-sigs:
		syscall.Kill(-group, syscall.SIGTERM)
		select {
		case err = <-done:
		case <-time.After(serveGrace):
			syscall.Kill(-group, syscall.SIGKILL)
			err = <-done
		}
	}
	reap(group)

	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit):
		if ws, ok := exit.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		return exit.ExitCode()
	}
	return 1
}

// reap stops what is left of a process group: SIGTERM, then SIGKILL after
// reapGrace.
func reap(group int) {
	if syscall.Kill(-group, syscall.SIGTERM) != nil {
		return // nothing is left
	}
	for deadline := time.Now().Add(reapGrace); time.Now().Before(deadline); {
		if syscall.Kill(-group, 0) != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	syscall.Kill(-group, syscall.SIGKILL)
}
