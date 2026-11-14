//go:build nomarkets

package config

// MarketsBuilt says whether this build has the markets plugin (DESIGN
// §4.17); this one, built with -tags nomarkets, has not.
const MarketsBuilt = false
