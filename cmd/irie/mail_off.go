//go:build nomail

package main

// This build leaves the mail plugin out (go build -tags nomail, DESIGN
// §4.17), with irie mail and its lines in the usage.
const mailUsage = ""
