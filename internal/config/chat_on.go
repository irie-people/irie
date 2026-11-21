//go:build !nochat

package config

// ChatBuilt says whether this build has the chat plugin (DESIGN §4.17);
// go build -tags nochat leaves it out.
const ChatBuilt = true
