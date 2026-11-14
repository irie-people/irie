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

func TestParsePSReadsMemoryAndCPU(t *testing.T) {
	ps := []byte(`    1     0   9664   0.1
  100     1  20480  12.5
  101   100  10240 230.0
  junk
  102   100   bad    1.0
`)
	procs, kids := parsePS(ps)
	if got := procs[101]; got.mem != 10240<<10 || got.cpu != 230 {
		t.Errorf("101: %+v", got)
	}
	if got := procs[100]; got.mem != 20480<<10 || got.cpu != 12.5 {
		t.Errorf("100: %+v", got)
	}
	if _, ok := procs[102]; ok || len(kids[100]) != 1 || kids[100][0] != 101 {
		t.Errorf("unreadable lines were taken: %v, kids %v", procs, kids)
	}
}
