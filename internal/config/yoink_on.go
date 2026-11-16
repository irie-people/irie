//go:build !noyoink

package config

// YoinkBuilt says whether this build has the yoink plugin (DESIGN §4.17);
// go build -tags noyoink leaves it out.
const YoinkBuilt = true
