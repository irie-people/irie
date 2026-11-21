//go:build !nojammin

package config

// JamminBuilt says whether this build has the jammin plugin (DESIGN
// §4.17); go build -tags nojammin leaves it out.
const JamminBuilt = true
