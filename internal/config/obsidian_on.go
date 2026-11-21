//go:build !noobsidian

package config

// ObsidianBuilt says whether this build has the obsidian plugin (DESIGN
// §4.17); go build -tags noobsidian leaves it out.
const ObsidianBuilt = true
