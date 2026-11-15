//go:build darwin || linux

package proc

import (
	"os"
	"syscall"
)

// fileKey is the device and inode of the file path leads to.
func fileKey(path string) (fileID, bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return fileID{}, false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fileID{}, false
	}
	return fileID{dev: uint64(st.Dev), ino: uint64(st.Ino)}, true
}
