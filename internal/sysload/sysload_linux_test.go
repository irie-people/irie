package sysload

import "testing"

func TestParseLoadavg(t *testing.T) {
	if v, err := parseLoadavg("0.52 0.40 0.31 2/811 12345\n"); err != nil || v != 0.52 {
		t.Errorf("parseLoadavg = %v, %v", v, err)
	}
	if _, err := parseLoadavg(""); err == nil {
		t.Error("an empty file passed")
	}
}

func TestPSIBecomesMacOSLevels(t *testing.T) {
	for in, want := range map[string]int{
		"some avg10=0.00 avg60=0.00 avg300=0.00 total=1\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=0\n":   PressureNormal,
		"some avg10=12.50 avg60=3.00 avg300=1.00 total=9\nfull avg10=2.00 avg60=0.10 avg300=0.00 total=1\n":  PressureWarn,
		"some avg10=40.00 avg60=9.00 avg300=1.00 total=9\nfull avg10=11.00 avg60=2.00 avg300=0.50 total=5\n": PressureCritical,
		"": 0,
	} {
		if got := parsePSI(in); got != want {
			t.Errorf("parsePSI(%q) = %d, want %d", in, got, want)
		}
	}
}
