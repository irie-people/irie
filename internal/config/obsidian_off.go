//go:build noobsidian

package config

// ObsidianBuilt says whether this build has the obsidian plugin (DESIGN
// §4.17); this one, built with -tags noobsidian, has not.
const ObsidianBuilt = false
