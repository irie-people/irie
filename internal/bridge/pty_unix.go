//go:build linux || darwin

package bridge

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// control runs f on f's descriptor, without taking the file out of Go's
// poller, so a Read on the master still ends when the file is closed.
func control(file *os.File, f func(fd int) error) error {
	rc, err := file.SyscallConn()
	if err != nil {
		return err
	}
	var ferr error
	if err := rc.Control(func(fd uintptr) { ferr = f(int(fd)) }); err != nil {
		return err
	}
	return ferr
}

// setSize gives the terminal its size, which tmux hears as SIGWINCH.
func setSize(master *os.File, cols, rows int) error {
	return control(master, func(fd int) error {
		return unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, &unix.Winsize{Col: uint16(cols), Row: uint16(rows)})
	})
}

// ttyAttr starts tmux as a session leader with the terminal as its own,
// as a terminal starts its shell.
func ttyAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true, Setctty: true} }

// hangUp tells a tmux client that its terminal went, as closing a
// terminal's window does: it detaches and exits.
func hangUp(p *os.Process) { p.Signal(syscall.SIGHUP) }
