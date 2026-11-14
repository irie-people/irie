//go:build noprivacy

package main

// This build leaves the privacy plugin out (go build -tags noprivacy,
// DESIGN §4.17), with its commands and their lines in the usage.
const (
	privacyUsage        = ""
	privacySetupUsage   = ""
	privacyServerUsage  = ""
	privacyRuntimeUsage = ""
)
