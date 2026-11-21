//go:build nochat

package config

// ChatBuilt says whether this build has the chat plugin (DESIGN §4.17);
// this one, built with -tags nochat, has not.
const ChatBuilt = false
