//go:build noyoink

package main

// This build leaves the yoink plugin out (go build -tags noyoink, DESIGN
// §4.17), with irie yoink and its lines in the usage.
const yoinkUsage = ""
