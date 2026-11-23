package localmodel

import (
	"strings"
	"testing"
)

func TestAModelLoadsOnlyWithMemoryToSpare(t *testing.T) {
	need := Need{Weights: 6600 << 20, Total: 8 * GiB}
	mac := Machine{Total: 18 * GiB}
	// The moment a 9B model brought a laptop down: about 6.5 GB free, by
	// the kernel's count.
	mac.Available = 6500 << 20
	if err := RoomToLoad(need, mac); err == nil || !strings.Contains(err.Error(), "too little memory is free") || !strings.Contains(err.Error(), "8.6 GB") {
		t.Errorf("a model past the memory free: %v", err)
	}
	// Enough for the model but not the reserve beside it.
	mac.Available = 9 * GiB
	if err := RoomToLoad(need, mac); err == nil {
		t.Error("a model that leaves less than the reserve free loaded")
	}
	mac.Available = 12 * GiB
	if err := RoomToLoad(need, mac); err != nil {
		t.Errorf("a model with room to spare: %v", err)
	}
}
