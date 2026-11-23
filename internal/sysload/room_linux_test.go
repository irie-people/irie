package sysload

import "testing"

func TestMeminfoReadsTheSwap(t *testing.T) {
	m := meminfo("MemTotal:       16384000 kB\nMemFree:  100 kB\nSwapTotal:       2097148 kB\nSwapFree:        1048574 kB\nHugePages_Total:       0\n")
	if m["MemTotal"] != 16384000<<10 || m["SwapTotal"] != 2097148<<10 || m["SwapFree"] != 1048574<<10 || m["HugePages_Total"] != 0 {
		t.Fatalf("meminfo %v", m)
	}
}
