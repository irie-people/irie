//go:build darwin || linux

package localmodel

import (
	"os"

	"golang.org/x/sys/unix"
)

func freeDisk(path string) (int64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, &os.PathError{Op: "statfs", Path: path, Err: err}
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}
