//go:build !darwin && !linux

package sysload

// readRoom knows nothing here: the room is unknown, so nothing waits for it.
func readRoom() (Room, error) { return Room{}, nil }
