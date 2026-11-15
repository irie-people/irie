//go:build !nobitwarden

package config

// BitwardenBuilt says whether this build has the Bitwarden plugin (DESIGN
// §4.17); go build -tags nobitwarden leaves it out.
const BitwardenBuilt = true
