package sysload

import (
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// readRoom reads MemTotal, SwapTotal and SwapFree from /proc/meminfo
// (proc_meminfo(5)) and the free space of /, where a swap file usually is.
func readRoom() (Room, error) {
	var r Room
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		m := meminfo(string(b))
		r.RAM, r.SwapTotal = m["MemTotal"], m["SwapTotal"]
		r.SwapUsed = max(0, r.SwapTotal-m["SwapFree"])
	}
	var st unix.Statfs_t
	if err := unix.Statfs("/", &st); err != nil {
		return r, err
	}
	r.DiskFree, r.DiskTotal = int64(st.Bavail)*int64(st.Bsize), int64(st.Blocks)*int64(st.Bsize)
	return r, nil
}

// meminfo reads /proc/meminfo's lines, "SwapFree:  1024 kB", into bytes by
// name.
func meminfo(s string) map[string]int64 {
	out := map[string]int64{}
	for _, line := range strings.Split(s, "\n") {
		name, rest, ok := strings.Cut(line, ":")
		f := strings.Fields(rest)
		if !ok || len(f) == 0 {
			continue
		}
		n, err := strconv.ParseInt(f[0], 10, 64)
		if err != nil {
			continue
		}
		if len(f) > 1 && f[1] == "kB" {
			n <<= 10
		}
		out[name] = n
	}
	return out
}
