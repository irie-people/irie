package raphael

import (
	"io/fs"
	"syscall"
	"time"
)

// statusChange is fi's status change time.
func statusChange(fi fs.FileInfo) (time.Time, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return time.Time{}, false
	}
	return time.Unix(st.Ctimespec.Unix()), true
}
