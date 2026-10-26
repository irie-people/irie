package core

import (
	"errors"
	"os"
	"syscall"
	"time"
)

// lockFile takes an exclusive flock on path, retrying until timeout. The
// lock lasts until the returned file is closed or the process exits.
func lockFile(path string, timeout time.Duration) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) || time.Now().After(deadline) {
			f.Close()
			if errors.Is(err, syscall.EWOULDBLOCK) {
				return nil, errors.New("held by another process")
			}
			return nil, err
		}
		time.Sleep(50 * time.Millisecond)
	}
}
