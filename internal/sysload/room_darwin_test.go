package sysload

import (
	"encoding/binary"
	"testing"
)

func TestSwapUsageDecodesTheKernelsStruct(t *testing.T) {
	// struct xsw_usage: total, avail, used, then the page size and the
	// encrypted flag.
	b := make([]byte, 32)
	binary.LittleEndian.PutUint64(b[0:], 11<<30)
	binary.LittleEndian.PutUint64(b[8:], 1<<30)
	binary.LittleEndian.PutUint64(b[16:], 10<<30)
	binary.LittleEndian.PutUint32(b[24:], 16384)
	total, used, err := swapUsage(b)
	if err != nil || total != 11<<30 || used != 10<<30 {
		t.Fatalf("total %d, used %d, err %v", total, used, err)
	}
	if _, _, err := swapUsage(b[:8]); err == nil {
		t.Error("a short struct decoded")
	}
}
