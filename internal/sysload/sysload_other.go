//go:build !darwin && !linux

package sysload

// read knows only the CPUs here, which Read fills in.
func read() (Load, error) { return Load{}, nil }
