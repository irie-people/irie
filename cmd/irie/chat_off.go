//go:build nochat

package main

// This build leaves the chat plugin out (go build -tags nochat, DESIGN
// §4.17), with irie chat and its lines in the usage.
const chatUsage = ""
