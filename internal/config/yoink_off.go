//go:build noyoink

package config

// YoinkBuilt says whether this build has the yoink plugin (DESIGN §4.17);
// this one, built with -tags noyoink, has not.
const YoinkBuilt = false
