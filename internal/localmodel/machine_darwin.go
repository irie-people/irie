package localmodel

import "golang.org/x/sys/unix"

// readMachine reads physical memory (hw.memsize) and the share of it the
// kernel counts as free (kern.memorystatus_level, the "memory free
// percentage" that memory_pressure prints).
func readMachine() (Machine, error) {
	total, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return Machine{}, err
	}
	level, err := unix.SysctlUint32("kern.memorystatus_level")
	if err != nil {
		return Machine{}, err
	}
	return Machine{Total: int64(total), Available: int64(total) / 100 * int64(min(level, 100))}, nil
}
