//go:build nowriting

package config

// WritingBuilt says whether this build has the writing plugin (DESIGN
// §4.17); this one, built with -tags nowriting, has not.
const WritingBuilt = false
