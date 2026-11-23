package filetx

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// tryLock takes an exclusive lock on f's first byte, or fails at once
// while another holds it: Windows' LockFileEx, as flock is elsewhere. It
// lets the packages that use filetx build for Windows, such as the stand-in
// core of Irie for Windows' tests (internal/winapp/fixture).
func tryLock(f *os.File) error {
	var o windows.Overlapped
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &o)
}

// held says whether tryLock failed because another holds the lock.
func held(err error) bool { return errors.Is(err, windows.ERROR_LOCK_VIOLATION) }

// fsync flushes f to the disk.
func fsync(f *os.File) error { return f.Sync() }
