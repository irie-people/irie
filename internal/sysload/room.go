package sysload

import (
	"fmt"

	"github.com/irie-people/irie/internal/brand"
)

// Room is the machine's room to page: its memory, the swap in use and the
// free disk where the swap files go. macOS keeps them on the startup
// disk's VM volume, which shares its free space with the user's files, and
// adds a file whenever the memory it compresses runs out of room. On
// 2026-11-23 a disk 97% full left the swap no room to grow, and the kernel
// panicked (DESIGN §2.9). A read is a sysctl and a statfs on macOS, two
// small files and a statfs on Linux; it starts no process.
type Room struct {
	RAM       int64 // physical memory
	SwapUsed  int64 // swap in use
	SwapTotal int64 // the size of the swap files there are now
	DiskFree  int64 // free bytes a program of the user's may take where the swap goes
	DiskTotal int64 // that disk's size
}

// GiB is a gibibyte.
const GiB = 1 << 30

// MinBuffer is the least free disk Irie keeps for the swap to grow into,
// on any machine: 25 GiB.
const MinBuffer = 25 * GiB

// MinFloor is the least free disk below which new heavy work waits, on any
// machine: 10 GiB.
const MinFloor = 10 * GiB

// Buffer is the free disk to keep so that the swap can always grow: room
// for as much again as the RAM and the swap in use hold, since a machine
// short of memory pages out what it compresses, and never under MinBuffer.
// Raffi keeps it free, clearing caches below it (DESIGN §4.11).
func (r Room) Buffer() int64 { return max(MinBuffer, r.RAM+r.SwapUsed) }

// Floor is the free disk below which new heavy work waits: installs, test
// runs, local model loads and new sessions, each of which writes
// gigabytes, or takes memory that the swap may have to hold. Half the RAM,
// and never under MinFloor.
func (r Room) Floor() int64 { return max(MinFloor, r.RAM/2) }

// Known says whether the free disk could be read.
func (r Room) Known() bool { return r.DiskTotal > 0 }

// Short says the free disk is under the buffer.
func (r Room) Short() bool { return r.Known() && r.DiskFree < r.Buffer() }

// Starved says the free disk is under the floor: the swap may not grow.
func (r Room) Starved() bool { return r.Known() && r.DiskFree < r.Floor() }

// Words say the room in plain words: "8.2 GB free of the disk, under the
// 10.7 GB floor; swap 3.1 GB".
func (r Room) Words() string {
	if !r.Known() {
		return "the free disk is not known"
	}
	s := Size(r.DiskFree) + " free of the disk"
	switch {
	case r.Starved():
		s += ", under the " + Size(r.Floor()) + " floor"
	case r.Short():
		s += ", under the " + Size(r.Buffer()) + " kept for swap"
	}
	if r.SwapUsed > 0 {
		s += "; swap " + Size(r.SwapUsed)
	}
	return s
}

// HeavyWaits is why new heavy work waits now, in a sentence, or "" when
// it need not: the free disk is under the floor.
func (r Room) HeavyWaits() string {
	if !r.Starved() {
		return ""
	}
	return fmt.Sprintf("only %s of the disk is free, under the %s floor that leaves the swap room to grow, so new heavy work waits until there is more ("+brand.Raffi+" clears caches; the biggest files are yours to clear)",
		Size(r.DiskFree), Size(r.Floor()))
}

// Size renders bytes in decimal units, as the Finder and df -H do: 8.2
// GB, 640 MB.
func Size(n int64) string {
	switch {
	case n >= 1e9:
		return fmt.Sprintf("%.1f GB", float64(n)/1e9)
	case n >= 1e6:
		return fmt.Sprintf("%d MB", (n+5e5)/1e6)
	}
	return fmt.Sprintf("%d KB", (n+500)/1e3)
}

// ReadRoom reads the machine's room to page.
func ReadRoom() (Room, error) { return readRoom() }
