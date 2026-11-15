// Package web is the phone app's files (docs/PHONE.md §4), which `irie
// remote serve` serves from the binary. The Windows app's page imports
// some of its modules too (internal/winapp/web): look.js, how a row looks,
// and md.js, so a session reads the same in both.
package web

import "embed"

// FS is the files, as the gateway serves them.
//
//go:embed *.html *.css *.js *.png *.webmanifest js
var FS embed.FS
