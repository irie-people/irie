//go:build noextension

package main

// This build leaves the browser extension plugin out (go build -tags
// noextension, DESIGN §4.17), with irie terms, irie extension and their
// lines in the usage.
const (
	extensionUsage       = ""
	extensionServerUsage = ""
)
