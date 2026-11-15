//go:build !windows

package wsl

// RegistryDistros reads WSL's distributions from the registry, which only
// Windows has.
func RegistryDistros() []Distro { return nil }
