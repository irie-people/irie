//go:build !darwin && !linux

package proc

import "errors"

// CanBackground reports whether this system has a background state that a
// process can put another of its user's processes in, and take it out of.
const CanBackground = false

func read() ([]Proc, error) { return nil, errors.ErrUnsupported }

func readGroup(pgid int) ([]Proc, error) { return nil, errors.ErrUnsupported }

func readOne(pid int) (Proc, error) { return Proc{}, errors.ErrUnsupported }

// SetBackground is not available here; see CanBackground.
func SetBackground(pid int, on bool) error { return errors.ErrUnsupported }

func readLoad() (Load, error) { return Load{}, errors.ErrUnsupported }
