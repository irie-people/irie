//go:build nojammin

package main

// This build leaves the jammin plugin out (go build -tags nojammin, DESIGN
// §4.17), with irie jammin and its lines in the usage.
const jamminUsage = ""
