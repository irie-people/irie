//go:build linux

package bridge

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// openPTY opens a new terminal: its master, which the bridge reads and
// writes, and its slave, which tmux runs on (pty(7), pts(4)).
func openPTY() (master, slave *os.File, err error) {
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	var n uint32
	if err := control(m, func(fd int) error {
		if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
			return fmt.Errorf("unlockpt: %w", err)
		}
		var err error
		n, err = unix.IoctlGetUint32(fd, unix.TIOCGPTN)
		if err != nil {
			return fmt.Errorf("ptsname: %w", err)
		}
		return nil
	}); err != nil {
		m.Close()
		return nil, nil, err
	}
	s, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		m.Close()
		return nil, nil, err
	}
	return m, s, nil
}
