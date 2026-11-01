package sysload

import (
	"encoding/binary"
	"testing"
)

func TestLoadavgDecodesTheKernelsStruct(t *testing.T) {
	b := make([]byte, 24)
	binary.LittleEndian.PutUint32(b[0:4], 3*2048+1024) // 3.5 at a scale of 2048
	binary.LittleEndian.PutUint32(b[4:8], 99)
	binary.LittleEndian.PutUint64(b[16:24], 2048)
	if v, err := loadavg(b); err != nil || v != 3.5 {
		t.Errorf("loadavg = %v, %v; want 3.5", v, err)
	}
	if _, err := loadavg(b[:20]); err == nil {
		t.Error("a short struct passed")
	}
	if _, err := loadavg(make([]byte, 24)); err == nil {
		t.Error("a zero scale passed")
	}
}

func TestOnlyTtysAndDigitsArePTYs(t *testing.T) {
	for name, want := range map[string]bool{"ttys000": true, "ttys12": true, "ttys": false, "tty": false, "ttyp0": false, "ttys00a": false, "cu.usb": false} {
		if got := isPTY(name); got != want {
			t.Errorf("isPTY(%q) = %v", name, got)
		}
	}
}
