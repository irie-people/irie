//go:build nofounding

package config

// FoundingBuilt says whether this build has the founding plugin (DESIGN
// §4.17); this one, built with -tags nofounding, has not.
const FoundingBuilt = false
