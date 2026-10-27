package localmodel

import (
	"strings"
	"testing"
)

func TestParseMeminfo(t *testing.T) {
	m, err := parseMeminfo(strings.NewReader("MemTotal:       16384000 kB\nMemFree:          100000 kB\nMemAvailable:    8192000 kB\n"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Total != 16384000<<10 || m.Available != 8192000<<10 {
		t.Errorf("machine = %+v", m)
	}
	if _, err := parseMeminfo(strings.NewReader("MemFree: 1 kB\n")); err == nil {
		t.Error("no error without MemTotal")
	}
}
