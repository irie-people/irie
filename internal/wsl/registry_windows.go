package wsl

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

// RegistryDistros reads the distributions WSL keeps for this user in
// HKCU\Software\Microsoft\Windows\CurrentVersion\Lxss: a key each, named
// by its GUID, with its DistributionName and Version, and the default's
// GUID in the root's DefaultDistribution. The state is unknown there.
func RegistryDistros() []Distro {
	root, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Lxss`, registry.READ)
	if err != nil {
		return nil
	}
	defer root.Close()
	def, _, _ := root.GetStringValue("DefaultDistribution")
	names, err := root.ReadSubKeyNames(-1)
	if err != nil {
		return nil
	}
	var ds []Distro
	for _, id := range names {
		k, err := registry.OpenKey(root, id, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		name, _, err := k.GetStringValue("DistributionName")
		v, _, verr := k.GetIntegerValue("Version")
		k.Close()
		if err != nil || name == "" {
			continue
		}
		d := Distro{Name: name, State: "Unknown", Version: 2, Default: strings.EqualFold(id, def)}
		if verr == nil && v == 1 {
			d.Version = 1
		}
		ds = append(ds, d)
	}
	return ds
}
