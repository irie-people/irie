//go:build noterms

package core

import "context"

// This build leaves Terms in short out (go build -tags noterms, DESIGN
// §4.17): no page to serve.
func (c *Core) serveTerms(context.Context) {}
