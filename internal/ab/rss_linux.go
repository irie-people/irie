package ab

import (
	"os"
	"syscall"
)

// maxRSS is a finished process's peak resident memory in bytes. Linux's
// getrusage(2) gives ru_maxrss in kilobytes.
func maxRSS(ps *os.ProcessState) int64 {
	if ru, ok := ps.SysUsage().(*syscall.Rusage); ok {
		return ru.Maxrss * 1024
	}
	return 0
}
