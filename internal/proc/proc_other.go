//go:build !darwin && !linux

package proc

import "errors"

// CanBackground reports whether this system has a background state that a
// process can put another of its user's processes in, and take it out of.
const CanBackground = false

func read() ([]Proc, error) { return nil, errors.ErrUnsupported }

// SetBackground is not available here; see CanBackground.
func SetBackground(pid int, on bool) error { return errors.ErrUnsupported }
