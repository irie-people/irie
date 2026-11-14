//go:build noprivacy

package config

// PrivacyBuilt says whether this build has the privacy plugin (DESIGN
// §4.17); this one, built with -tags noprivacy, has not: its switches read
// as off whatever config.toml says.
const PrivacyBuilt = false
