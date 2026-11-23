package sysload

import (
	"encoding/binary"
	"fmt"

	"golang.org/x/sys/unix"
)

// swapVolume is where macOS keeps its swap files (sysctl vm.swapfileprefix
// names /System/Volumes/VM/swapfile since macOS 10.15); it shares the
// startup disk's free space. Before it there was only /.
var swapVolumes = []string{"/System/Volumes/VM", "/"}

// readRoom reads hw.memsize, vm.swapusage (struct xsw_usage, which
// sysctl(8) prints) and the free space of the swap's volume.
func readRoom() (Room, error) {
	var r Room
	if n, err := unix.SysctlUint64("hw.memsize"); err == nil {
		r.RAM = int64(n)
	}
	if b, err := unix.SysctlRaw("vm.swapusage"); err == nil {
		r.SwapTotal, r.SwapUsed, _ = swapUsage(b)
	}
	var err error
	for _, v := range swapVolumes {
		var st unix.Statfs_t
		if err = unix.Statfs(v, &st); err == nil {
			r.DiskFree, r.DiskTotal = int64(st.Bavail)*int64(st.Bsize), int64(st.Blocks)*int64(st.Bsize)
			break
		}
	}
	return r, err
}

// swapUsage decodes vm.swapusage, a struct xsw_usage: the swap's total,
// what is free of it and what is used, each a u_int64_t, then the page
// size and whether it is encrypted.
func swapUsage(b []byte) (total, used int64, err error) {
	if len(b) < 24 {
		return 0, 0, fmt.Errorf("vm.swapusage: %d bytes, want at least 24", len(b))
	}
	return int64(binary.LittleEndian.Uint64(b[0:8])), int64(binary.LittleEndian.Uint64(b[16:24])), nil
}
