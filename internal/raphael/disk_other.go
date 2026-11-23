//go:build !darwin && !linux

package raphael

import (
	"errors"
	"io/fs"
	"time"
)

func readDisk(path string) (total, free int64, err error) { return 0, 0, errors.ErrUnsupported }

// allocated is what a file takes on the disk: here, its size.
func allocated(fi fs.FileInfo) int64 { return fi.Size() }

// statusChange is unknown here.
func statusChange(fs.FileInfo) (time.Time, bool) { return time.Time{}, false }
