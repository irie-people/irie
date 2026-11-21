//go:build !noextension

package config

// ExtensionBuilt says whether this build has the browser extension plugin
// (DESIGN §4.17); go build -tags noextension leaves it out.
const ExtensionBuilt = true
