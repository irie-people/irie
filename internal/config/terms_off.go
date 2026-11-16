//go:build noterms

package config

// TermsBuilt says whether this build has Terms in short (DESIGN §4.17);
// this one, built with -tags noterms, has not.
const TermsBuilt = false
