//go:build nobitwarden

package config

// BitwardenBuilt says whether this build has the Bitwarden plugin (DESIGN
// §4.17); this one, built with -tags nobitwarden, has not.
const BitwardenBuilt = false
