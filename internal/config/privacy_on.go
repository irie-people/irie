//go:build !noprivacy

package config

// PrivacyBuilt says whether this build has the privacy plugin (DESIGN
// §4.17); go build -tags noprivacy leaves it out.
const PrivacyBuilt = true
