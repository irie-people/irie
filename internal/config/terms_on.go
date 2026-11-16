//go:build !noterms

package config

// TermsBuilt says whether this build has Terms in short (DESIGN §4.17); go
// build -tags noterms leaves it out.
const TermsBuilt = true
