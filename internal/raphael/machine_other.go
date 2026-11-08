//go:build !darwin && !linux

package raphael

func readMemory() Memory { return Memory{} }
