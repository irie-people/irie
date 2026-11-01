package sysload

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// read reads vm.loadavg, the CPUs (hw.logicalcpu), the memory pressure
// (kern.memorystatus_vm_pressure_level, which memory_pressure(1) reports),
// the most ptys (kern.tty.ptmx_max) and the ptys in use.
func read() (Load, error) {
	var l Load
	b, err := unix.SysctlRaw("vm.loadavg")
	if err != nil {
		return l, err
	}
	if l.RunQueue, err = loadavg(b); err != nil {
		return l, err
	}
	if n, err := unix.SysctlUint32("hw.logicalcpu"); err == nil {
		l.CPUs = int(n)
	}
	if n, err := unix.SysctlUint32("kern.memorystatus_vm_pressure_level"); err == nil {
		l.Pressure = int(n)
	}
	if n, err := unix.SysctlUint32("kern.tty.ptmx_max"); err == nil {
		l.PTYMax = int(n)
	}
	l.PTYs = countPTYs("/dev")
	return l, nil
}

// loadavg decodes vm.loadavg, a struct loadavg: three fixed-point averages
// (one, five and fifteen minutes), then their scale as a long.
func loadavg(b []byte) (float64, error) {
	if len(b) < 24 {
		return 0, fmt.Errorf("vm.loadavg: %d bytes, want 24", len(b))
	}
	scale := binary.LittleEndian.Uint64(b[16:24])
	if scale == 0 {
		return 0, errors.New("vm.loadavg: no scale")
	}
	return float64(binary.LittleEndian.Uint32(b[0:4])) / float64(scale), nil
}

// countPTYs counts the ptys in use: macOS makes a ttysNNN in /dev for each
// pty while it is open.
func countPTYs(dir string) int {
	f, err := os.Open(dir)
	if err != nil {
		return 0
	}
	defer f.Close()
	names, _ := f.Readdirnames(-1)
	n := 0
	for _, s := range names {
		if isPTY(s) {
			n++
		}
	}
	return n
}

// isPTY reports whether a name in /dev is a pty's: ttys and digits.
func isPTY(name string) bool {
	rest, ok := strings.CutPrefix(name, "ttys")
	if !ok || rest == "" {
		return false
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
