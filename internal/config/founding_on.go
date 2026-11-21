//go:build !nofounding

package config

// FoundingBuilt says whether this build has the founding plugin (DESIGN
// §4.17); go build -tags nofounding leaves it out.
const FoundingBuilt = true
