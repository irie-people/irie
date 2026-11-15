//go:build darwin

package bridge

import (
	"bytes"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// openPTY opens a new terminal: its master, which the bridge reads and
// writes, and its slave, which tmux runs on. macOS's grantpt, unlockpt
// and ptsname are ioctls on the master (sys/ttycom.h). The bridge runs on
// Linux; this is for its tests on a Mac.
func openPTY() (master, slave *os.File, err error) {
	// A blocking descriptor, which Go's poller leaves alone: macOS's kqueue
	// takes a pty's master and then reports it readable when it is not.
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	m := os.NewFile(uintptr(fd), "/dev/ptmx")
	var name string
	if err := control(m, func(fd int) error {
		for _, req := range []uint{unix.TIOCPTYGRANT, unix.TIOCPTYUNLK} {
			if _, _, e := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(req), 0); e != 0 {
				return fmt.Errorf("ioctl %#x: %w", req, e)
			}
		}
		buf := make([]byte, 128)
		if _, _, e := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCPTYGNAME), uintptr(unsafe.Pointer(&buf[0]))); e != 0 {
			return fmt.Errorf("ptsname: %w", e)
		}
		if i := bytes.IndexByte(buf, 0); i >= 0 {
			buf = buf[:i]
		}
		name = string(buf)
		return nil
	}); err != nil {
		m.Close()
		return nil, nil, err
	}
	s, err := os.OpenFile(name, os.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		m.Close()
		return nil, nil, err
	}
	return m, s, nil
}
