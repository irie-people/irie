// Package filetx writes Irie's files as transactions (DESIGN §5.4).
//
// A writer holds the file's write lock from before it reads the file until
// the new contents are in place, so writers take turns, whether goroutines
// of one process or processes of their own, and none loses a change that
// another made meanwhile. The lock is an flock(2) on .<name>.lock beside
// the file. The kernel lets it go when its holder exits, so a crash never
// leaves it held. Lock files stay once made: removing one while a writer
// waits on it would let two writers in at once.
//
// New contents go to a temporary file in the same directory, which is
// synced and then renamed over the file. So a reader, which takes no lock,
// and a restart after a crash find the old file or the new one, never part
// of either.
package filetx

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// ErrBusy is Lock's error when the lock stayed held for the whole wait.
var ErrBusy = errors.New("held by another process")

// wait is how long WriteFile and Update wait for another writer's turn to
// end. A turn takes about a millisecond, so only a stuck writer outlasts it.
var wait = 5 * time.Second

// Lock takes an exclusive flock on the file at path, made if need be,
// waiting up to wait while another holds it. Every other open of the file,
// in this process or another, waits its turn. The lock lasts until the
// file returned is closed, or the process ends.
func Lock(path string, wait time.Duration) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(wait)
	for pause := time.Millisecond; ; pause = min(2*pause, 50*time.Millisecond) {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) || time.Now().After(deadline) {
			f.Close()
			if errors.Is(err, syscall.EWOULDBLOCK) {
				return nil, ErrBusy
			}
			return nil, err
		}
		time.Sleep(pause)
	}
}

// LockPath is the lock file of the file at path.
func LockPath(path string) string {
	return filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".lock")
}

// WriteFile replaces the file at path with data, holding its write lock.
// As with os.WriteFile, a new file gets perm and a file replaced keeps its
// mode. A symbolic link is followed: the file it names is replaced and the
// link stays, as dotfiles need. A missing directory is made, with mode 0700.
func WriteFile(path string, data []byte, perm fs.FileMode) error {
	unlock, err := begin(path)
	if err != nil {
		return err
	}
	defer unlock()
	return replace(target(path), data, perm)
}

// Update reads the file at path, nil when there is none, and puts what
// change makes of it in its place, holding the file's write lock all the
// while. When change fails, its error comes back as it is and the file is
// left alone, as it is when change gives back what it was given. Modes,
// links and directories are as in WriteFile. change must not write the
// same file through filetx: that would wait for the lock Update holds
// until the wait ran out.
func Update(path string, perm fs.FileMode, change func(old []byte) ([]byte, error)) error {
	unlock, err := begin(path)
	if err != nil {
		return err
	}
	defer unlock()
	path = target(path)
	old, err := os.ReadFile(path)
	exists := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	data, err := change(old)
	if err != nil {
		return err
	}
	if exists && bytes.Equal(data, old) {
		return nil
	}
	return replace(path, data, perm)
}

// begin takes the write lock of the file at path, making its directory
// when there is none, and returns what lets the lock go.
func begin(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := Lock(LockPath(path), wait)
	if err != nil {
		return nil, fmt.Errorf("%s: write lock: %w", path, err)
	}
	return func() { f.Close() }, nil
}

// target is the file that a write of path changes: the one a link names.
func target(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return path
}

// replace puts data in place of the file at path in one rename. The
// temporary file is synced first with fsync(2). os.File.Sync is
// F_FULLFSYNC on macOS, which also asks the drive to empty its cache: it
// took about 8 ms a write on an Apple silicon Mac, against under 1 ms for
// fsync, and Irie rewrites some files many times a minute. SQLite's
// default on macOS is fsync too.
func replace(path string, data []byte, perm fs.FileMode) error {
	mode := perm
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Chmod(mode)
	}
	if err == nil {
		err = syscall.Fsync(int(f.Fd()))
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}
