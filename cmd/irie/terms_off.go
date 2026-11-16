//go:build noterms

package main

// This build leaves Terms in short out (go build -tags noterms, DESIGN
// §4.17), with irie terms and its lines in the usage.
const termsUsage = ""
