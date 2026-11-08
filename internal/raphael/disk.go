package raphael

import (
	"fmt"
	"runtime"
)

// The thresholds for the disk: it is nearly full under diskLowShare of its
// size free or under diskLow, and getting full under twice that.
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

// Disk is the room on the volume that holds the person's files.
type Disk struct {
	Total, Free int64
}

// ReadDisk reads the room on the data volume.
func ReadDisk() (Disk, error) {
	total, free, err := readDisk(dataVolume())
	if err != nil {
		total, free, err = readDisk("/")
	}
	return Disk{Total: total, Free: free}, err
}

// Low reports whether the disk is nearly full.
func (d Disk) Low() bool {
	return d.Total > 0 && (float64(d.Free) < diskLowShare*float64(d.Total) || d.Free < diskLow)
}

// diskFinding says how much room the disk has left.
func diskFinding(d Disk) Finding {
	if d.Total <= 0 {
		return Finding{}
	}
	f := Finding{ID: "disk", Area: AreaDisk, Level: OK, Size: d.Free}
	share := 100 * float64(d.Free) / float64(d.Total)
	words := fmt.Sprintf("%s free of %s (%.0f%%)", Size(d.Free), Size(d.Total), share)
	switch {
	case d.Low():
		f.Level = Warn
		f.Text = "Your disk is nearly full: " + words + "."
		f.Advice = "macOS needs room for updates and swap, and slows down without it. Clear the caches below, and empty the Trash; then move or delete big files you no longer need."
	case float64(d.Free) < 2*diskLowShare*float64(d.Total) || d.Free < 2*diskLow:
		f.Level = Note
		f.Text = "Your disk is getting full: " + words + "."
		f.Advice = "The caches below are safe to clear, and the Trash may hold more."
	default:
		f.Text = "Disk: " + words + "."
	}
	return f
}
