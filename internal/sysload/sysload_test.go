package sysload

import (
	"runtime"
	"testing"
)

func TestReadSaysHowBusyTheMachineIs(t *testing.T) {
	l, err := Read()
	if err != nil {
		t.Fatal(err)
	}
	if l.CPUs < 1 || l.RunQueue < 0 {
		t.Errorf("load = %+v", l)
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		// The test runs in some terminal or none; the most is always known.
		if l.PTYMax < 1 || l.PTYs < 0 || l.PTYs > l.PTYMax+64 {
			t.Errorf("ptys %d of %d", l.PTYs, l.PTYMax)
		}
		if l.Pressure != 0 && l.Pressure != PressureNormal && l.Pressure != PressureWarn && l.Pressure != PressureCritical {
			t.Errorf("pressure %d", l.Pressure)
		}
	}
}
