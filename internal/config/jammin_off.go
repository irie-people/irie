//go:build nojammin

package config

// JamminBuilt says whether this build has the jammin plugin (DESIGN
// §4.17); this one, built with -tags nojammin, has not.
const JamminBuilt = false
