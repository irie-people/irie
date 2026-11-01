package ipc

import (
	"net"

	"golang.org/x/sys/unix"
)

// peerPID is the process id of the client at c's other end, from
// LOCAL_PEERPID (macOS's sys/un.h); 0 when it can't be read.
func peerPID(c net.Conn) int {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return 0
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0
	}
	pid := 0
	raw.Control(func(fd uintptr) {
		if v, err := unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID); err == nil {
			pid = v
		}
	})
	return pid
}
