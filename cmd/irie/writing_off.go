//go:build nowriting

package main

// This build leaves the writing plugin out (go build -tags nowriting,
// DESIGN §4.17), with its command and its lines in the usage.
const (
	writingUsage       = ""
	writingServerUsage = ""
)
