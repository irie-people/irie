package core

import "testing"

func TestShownMemoryKeepsTwoFiguresRoundedDown(t *testing.T) {
	for _, c := range []struct{ in, want int64 }{
		{0, 0},
		{7, 7},
		{99, 99},
		{4_612_345, 4_600_000},
		{452_123_456, 450_000_000},
		{459_999_999, 450_000_000},
		{1_234_567_890, 1_200_000_000},
	} {
		if got := shownMemory(c.in); got != c.want {
			t.Errorf("shownMemory(%d) = %d, want %d", c.in, got, c.want)
		}
	}
	// A few pages either way leave the row as it was.
	if shownMemory(452_000_000) != shownMemory(452_000_000+5*16384) {
		t.Error("a few pages more changed the row")
	}
}
