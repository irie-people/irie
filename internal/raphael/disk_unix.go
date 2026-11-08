//go:build darwin || linux

package raphael

import "golang.org/x/sys/unix"

// readDisk reads the size of the volume at path and what is free on it for
// the person's own files.
func readDisk(path string) (total, free int64, err error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	return int64(st.Blocks) * int64(st.Bsize), int64(st.Bavail) * int64(st.Bsize), nil
}
