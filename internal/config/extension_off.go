//go:build noextension

package config

// ExtensionBuilt says whether this build has the browser extension plugin
// (DESIGN §4.17); this one, built with -tags noextension, has not.
const ExtensionBuilt = false
