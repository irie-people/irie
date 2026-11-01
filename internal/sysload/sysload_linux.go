package sysload

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// read reads /proc/loadavg (proc_loadavg(5)), the memory stall figures
// (/proc/pressure/memory, the kernel's PSI) and the ptys in use and their
// most (/proc/sys/kernel/pty/nr and max, the devpts filesystem's).
func read() (Load, error) {
	var l Load
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return l, err
	}
	if l.RunQueue, err = parseLoadavg(string(b)); err != nil {
		return l, err
	}
	l.CPUs = runtime.NumCPU()
	if b, err := os.ReadFile("/proc/pressure/memory"); err == nil {
		l.Pressure = parsePSI(string(b))
	}
	l.PTYs = readInt("/proc/sys/kernel/pty/nr")
	l.PTYMax = readInt("/proc/sys/kernel/pty/max")
	return l, nil
}

// parseLoadavg reads the one-minute average, the first field of
// /proc/loadavg.
func parseLoadavg(s string) (float64, error) {
	f := strings.Fields(s)
	if len(f) == 0 {
		return 0, fmt.Errorf("/proc/loadavg: empty")
	}
	v, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		return 0, fmt.Errorf("/proc/loadavg: %w", err)
	}
	return v, nil
}

// parsePSI turns the kernel's memory stall figures into macOS's levels:
// a warning once some task waited on memory for a tenth of the last ten
// seconds, critical once every task did. Without the figures it is 0.
func parsePSI(s string) int {
	some, full := -1.0, -1.0
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		v, ok := strings.CutPrefix(f[1], "avg10=")
		if !ok {
			continue
		}
		x, err := strconv.ParseFloat(v, 64)
		if err != nil {
			continue
		}
		switch f[0] {
		case "some":
			some = x
		case "full":
			full = x
		}
	}
	switch {
	case some < 0:
		return 0
	case full >= 10:
		return PressureCritical
	case some >= 10:
		return PressureWarn
	}
	return PressureNormal
}

func readInt(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}
