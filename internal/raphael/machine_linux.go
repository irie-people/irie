package raphael

import (
	"os"
	"strconv"
	"strings"
)

// readMemory reads the RAM and the swap from /proc/meminfo.
func readMemory() Memory {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return Memory{}
	}
	return parseMeminfo(string(b))
}

// parseMeminfo reads /proc/meminfo: MemTotal, SwapTotal and SwapFree, in kB.
func parseMeminfo(s string) Memory {
	kb := map[string]int64{}
	for _, line := range strings.Split(s, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		f := strings.Fields(v)
		if len(f) == 0 {
			continue
		}
		if n, err := strconv.ParseInt(f[0], 10, 64); err == nil {
			kb[k] = n << 10
		}
	}
	return Memory{Total: kb["MemTotal"], SwapTotal: kb["SwapTotal"], SwapUsed: kb["SwapTotal"] - kb["SwapFree"]}
}
