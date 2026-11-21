package yoink

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/repo"
)

// Line says how a download stands, in a line: what it is and how far, as
// :yoink and irie yoink list say it.
func Line(d ipc.YoinkDownload, home string) string {
	what := "“" + d.Title + "”"
	if d.Get != ipc.YoinkBest {
		what += " (" + d.Label + ")"
	}
	switch d.State {
	case ipc.YoinkDone:
		return what + " is in " + repo.Tilde(filepath.Dir(d.File), home) + " as " + filepath.Base(d.File) + "."
	case ipc.YoinkFailed:
		return what + " failed: " + d.Error
	case ipc.YoinkCanceled:
		return what + " was canceled."
	case ipc.YoinkProcessing:
		return what + ": " + strings.ToLower(d.Step[:1]) + d.Step[1:] + "…"
	case ipc.YoinkDownloading:
		return what + ": " + Progress(d) + "."
	}
	return what + ": starting…"
}

// Progress says how far a download is: 45% of 27.0 MB, 2.1 MB/s, 12 s
// left; without a size, what has come.
func Progress(d ipc.YoinkDownload) string {
	var parts []string
	switch {
	case d.Total > 0:
		parts = append(parts, fmt.Sprintf("%d%% of %s", min(100, int(d.Done*100/d.Total)), Size(d.Total)))
	case d.Done > 0:
		parts = append(parts, Size(d.Done)+" so far")
	default:
		parts = append(parts, "starting")
	}
	if d.Speed > 0 {
		parts = append(parts, Size(int64(d.Speed))+"/s")
	}
	if d.ETA > 0 {
		parts = append(parts, Left(d.ETA))
	}
	return strings.Join(parts, ", ")
}

// Size is n bytes for a person, in the Finder's units: 27.0 MB.
func Size(n int64) string {
	switch {
	case n >= 1e9:
		return fmt.Sprintf("%.1f GB", float64(n)/1e9)
	case n >= 1e6:
		return fmt.Sprintf("%.1f MB", float64(n)/1e6)
	case n >= 1e3:
		return fmt.Sprintf("%d KB", n/1e3)
	}
	return fmt.Sprintf("%d bytes", n)
}

// Left is the time left of s seconds: 12 s left, 3 min left, 1 h 5 min
// left.
func Left(s int) string {
	switch {
	case s < 60:
		return fmt.Sprintf("%d s left", s)
	case s < 3600:
		return fmt.Sprintf("%d min left", (s+30)/60)
	}
	return fmt.Sprintf("%d h %d min left", s/3600, s%3600/60)
}
