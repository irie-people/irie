package proc

import "testing"

func TestParseLoadavgTakesTheMinute(t *testing.T) {
	l, err := parseLoadavg("3.25 2.10 1.05 4/812 30411\n")
	if err != nil {
		t.Fatal(err)
	}
	if l.Avg != 3.25 || l.CPUs < 1 || l.Memory != 0 {
		t.Errorf("load = %+v, want 3.25 over the CPUs, pressure unknown", l)
	}
	if _, err := parseLoadavg(""); err == nil {
		t.Error("an empty loadavg parsed")
	}
}
