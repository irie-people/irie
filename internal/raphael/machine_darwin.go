package raphael

import (
	"encoding/binary"

	"golang.org/x/sys/unix"
)

// readMemory reads the RAM (hw.memsize) and the swap (vm.swapusage, a
// struct xsw_usage: its total, what is free and what is used, then the
// page size and whether it is encrypted).
func readMemory() Memory {
	var m Memory
	if n, err := unix.SysctlUint64("hw.memsize"); err == nil {
		m.Total = int64(n)
	}
	if b, err := unix.SysctlRaw("vm.swapusage"); err == nil {
		m.SwapTotal, m.SwapUsed = swapUsage(b)
	}
	return m
}

// swapUsage decodes vm.swapusage.
func swapUsage(b []byte) (total, used int64) {
	if len(b) < 24 {
		return 0, 0
	}
	return int64(binary.LittleEndian.Uint64(b[0:8])), int64(binary.LittleEndian.Uint64(b[16:24]))
}
