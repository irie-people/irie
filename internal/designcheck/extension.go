//go:build !noextension

package designcheck

import "github.com/irie-people/roots"

// The browser extension plugin's side panel keeps the library's colours
// too. A build without the plugin, and a copy pruned of it, has no panel,
// so this copy is listed only where the plugin is.
func init() {
	Copies = append(Copies, roots.Copy{
		Path:   "internal/extension/web/panel.css",
		Why:    "The browser extension's side panel loads nothing from elsewhere: its pages may run only their own files.",
		Blocks: []string{"evergreen light", "evergreen dark"},
	})
}
