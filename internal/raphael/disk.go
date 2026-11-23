package raphael

import (
	"fmt"
	"runtime"

	"github.com/irie-people/irie/internal/sysload"
)

// The thresholds for the disk where the room for swap can't be read: it is
// nearly full under diskLowShare of its size free or under diskLow, and
// getting full under twice that.
const (
	diskLowShare = 0.10
	diskLow      = 15 << 30
)

// dataVolume is the volume that holds the person's files: on macOS the
// data volume, which shares its room with the system's.
func dataVolume() string {
	if runtime.GOOS == "darwin" {
		return "/System/Volumes/Data"
	}
	return "/"
}

// Disk is the room on the volume that holds the person's files, with the
// room the swap needs: the buffer Raffi keeps free and the floor below
// which new heavy work waits (sysload.Room). The swap's volume shares its
// free space with the person's files on macOS, as / does on Linux.
type Disk struct {
	Total, Free int64
	// Buffer and Floor are sysload.Room's, or 0 where it can't be read.
	Buffer, Floor int64
}

// ReadDisk reads the room on the data volume, and the room the swap needs.
func ReadDisk() (Disk, error) {
	total, free, err := readDisk(dataVolume())
	if err != nil {
		total, free, err = readDisk("/")
	}
	d := Disk{Total: total, Free: free}
	if room, rerr := sysload.ReadRoom(); rerr == nil && room.Known() {
		d.Buffer, d.Floor = room.Buffer(), room.Floor()
	}
	return d, err
}

// DiskOf is the disk as the room reads it.
func DiskOf(room sysload.Room) Disk {
	if !room.Known() {
		return Disk{}
	}
	return Disk{Total: room.DiskTotal, Free: room.DiskFree, Buffer: room.Buffer(), Floor: room.Floor()}
}

// Low reports whether the disk is short of room: under the buffer Raffi
// keeps for swap, or, where that can't be read, nearly full.
func (d Disk) Low() bool {
	if d.Total <= 0 {
		return false
	}
	if d.Buffer > 0 {
		return d.Free < d.Buffer
	}
	return float64(d.Free) < diskLowShare*float64(d.Total) || d.Free < diskLow
}

// Starved reports whether the disk is under the floor, where the swap may
// not grow and new heavy work waits.
func (d Disk) Starved() bool { return d.Total > 0 && d.Floor > 0 && d.Free < d.Floor }

// diskFinding says how much room the disk has left.
func diskFinding(d Disk) Finding {
	if d.Total <= 0 {
		return Finding{}
	}
	f := Finding{ID: "disk", Area: AreaDisk, Level: OK, Size: d.Free}
	share := 100 * float64(d.Free) / float64(d.Total)
	words := fmt.Sprintf("%s free of %s (%.0f%%)", Size(d.Free), Size(d.Total), share)
	switch {
	case d.Starved():
		f.Level = Warn
		f.Text = fmt.Sprintf("Your disk is nearly full: %s, under the %s floor the swap needs to grow.", words, Size(d.Floor))
		f.Advice = "New installs, test runs, local models and sessions wait until there is room. Raffi clears the caches that come back by themselves; the biggest files below are yours to clear."
	case d.Buffer > 0 && d.Low():
		f.Level = Warn
		f.Text = fmt.Sprintf("Your disk is short of the %s Raffi keeps free for swap: %s.", Size(d.Buffer), words)
		f.Advice = "macOS grows the swap onto the disk when memory runs short, and a full disk stops it: on 23 Nov 2026 that brought a Mac down. Raffi clears the caches that come back by themselves; the biggest files below are yours to clear."
	case d.Low():
		f.Level = Warn
		f.Text = "Your disk is nearly full: " + words + "."
		f.Advice = "macOS needs room for updates and swap, and slows down without it. Clear the caches below, and empty the Trash; then move or delete big files you no longer need."
	case d.Buffer > 0 && d.Free < 2*d.Buffer:
		f.Level = Note
		f.Text = fmt.Sprintf("Your disk is getting full: %s. Raffi keeps %s free for swap, clearing caches by himself below it.", words, Size(d.Buffer))
		f.Advice = "The caches below are safe to clear, and the Trash may hold more."
	case d.Buffer == 0 && (float64(d.Free) < 2*diskLowShare*float64(d.Total) || d.Free < 2*diskLow):
		f.Level = Note
		f.Text = "Your disk is getting full: " + words + "."
		f.Advice = "The caches below are safe to clear, and the Trash may hold more."
	default:
		f.Text = "Disk: " + words + "."
		if d.Buffer > 0 {
			f.Text = fmt.Sprintf("Disk: %s, with the %s Raffi keeps free for swap.", words, Size(d.Buffer))
		}
	}
	return f
}
