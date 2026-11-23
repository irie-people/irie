//go:build unix

package filetx

import (
	"errors"
	"os"
	"syscall"
)

// tryLock takes f's exclusive flock, or fails at once while another holds
// it.
func tryLock(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) }

// held says whether tryLock failed because another holds the lock.
func held(err error) bool { return errors.Is(err, syscall.EWOULDBLOCK) }

// fsync flushes f to the disk. It is fsync(2) itself: os.File.Sync on
// macOS is F_FULLFSYNC, far slower, which every write would pay.
func fsync(f *os.File) error { return syscall.Fsync(int(f.Fd())) }
