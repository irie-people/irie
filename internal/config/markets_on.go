//go:build !nomarkets

package config

// MarketsBuilt says whether this build has the markets plugin (DESIGN
// §4.17); go build -tags nomarkets leaves it out.
const MarketsBuilt = true
