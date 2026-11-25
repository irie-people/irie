//go:build !nofounding

package designcheck

import "github.com/irie-people/roots"

// The Charters Graph page keeps the library's colours too (DESIGN §4.17),
// and so does the Common Schools page, the plugin's second part.
// A build without the founding plugin, and a copy pruned of it, has no
// page, so this copy is listed only where the plugin is.
func init() {
	Copies = append(Copies, roots.Copy{
		Path:   "internal/founding/graph/graph.css",
		Why:    "The Charters Graph page is one file that loads nothing from elsewhere, so it carries its own colours.",
		Blocks: []string{"evergreen light", "evergreen dark"},
	}, roots.Copy{
		Path:   "internal/schools/pies/pies.css",
		Why:    "The Common Schools page is one file that loads nothing from elsewhere, so it carries its own colours; its slices' are worked out from the library's agent hues (internal/schools, colour.go).",
		Blocks: []string{"evergreen light", "evergreen dark"},
	})
}
