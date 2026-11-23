package sysload

import (
	"runtime"
	"strings"
	"testing"
)

func TestTheBufferLeavesTheSwapRoomToGrow(t *testing.T) {
	for _, c := range []struct {
		room          Room
		buffer, floor int64
	}{
		// The Mac that panicked on 2026-11-23: 18 GiB of RAM.
		{Room{RAM: 18 * GiB}, 25 * GiB, 10 * GiB},
		// The same Mac with 15 GiB swapped out, as on 2026-11-22.
		{Room{RAM: 18 * GiB, SwapUsed: 15 * GiB}, 33 * GiB, 10 * GiB},
		{Room{RAM: 64 * GiB}, 64 * GiB, 32 * GiB},
		{Room{RAM: 8 * GiB}, 25 * GiB, 10 * GiB},
	} {
		if b, f := c.room.Buffer(), c.room.Floor(); b != c.buffer || f != c.floor {
			t.Errorf("%+v: buffer %d GiB and floor %d GiB, want %d and %d", c.room, b/GiB, f/GiB, c.buffer/GiB, c.floor/GiB)
		}
	}
}

func TestShortAndStarvedFollowTheFreeDisk(t *testing.T) {
	r := Room{RAM: 18 * GiB, DiskTotal: 926 * GiB}
	for _, c := range []struct {
		free           int64
		short, starved bool
	}{
		{33 * GiB, false, false},
		{24 * GiB, true, false},
		{9 * GiB, true, true},
	} {
		r.DiskFree = c.free
		if r.Short() != c.short || r.Starved() != c.starved {
			t.Errorf("%d GiB free: short %v, starved %v; want %v, %v", c.free/GiB, r.Short(), r.Starved(), c.short, c.starved)
		}
		if waits := r.HeavyWaits(); (waits != "") != c.starved {
			t.Errorf("%d GiB free: heavy work waits %q", c.free/GiB, waits)
		}
	}
	// A disk that could not be read holds nothing back.
	if (Room{RAM: 18 * GiB}).Starved() {
		t.Error("an unknown disk starves the swap")
	}
}

func TestRoomWordsSayWhereTheDiskStands(t *testing.T) {
	r := Room{RAM: 18 * GiB, DiskTotal: 926 * GiB, DiskFree: 9 * GiB, SwapUsed: 3 * GiB}
	if w := r.Words(); !strings.Contains(w, "9.7 GB free of the disk, under the 10.7 GB floor") || !strings.Contains(w, "swap 3.2 GB") {
		t.Errorf("words %q", w)
	}
	if w := r.HeavyWaits(); !strings.Contains(w, "only 9.7 GB of the disk is free") {
		t.Errorf("heavy work waits %q", w)
	}
}

func TestReadRoomReadsThisMachine(t *testing.T) {
	r, err := ReadRoom()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return
	}
	if err != nil || r.RAM <= 0 || !r.Known() || r.DiskFree > r.DiskTotal || r.SwapUsed < 0 {
		t.Fatalf("room %+v, err %v", r, err)
	}
}
