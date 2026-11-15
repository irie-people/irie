// Package web is the page the Windows app draws in its window
// (docs/WINDOWS.md): the sidebar of session cards, the slot's terminal
// and the task bar, in HTML, CSS and JavaScript, with xterm.js (MIT)
// drawing the terminal. WebView2 loads it from the app itself, never from
// the network. It imports the phone app's modules for what both draw the
// same way: look.js, how a row looks, md.js and ui.js's elements, under
// shared/.
package web

import (
	"embed"
	"io/fs"
	"path"
	"strings"

	"github.com/irie-people/irie/internal/brand"
	phone "github.com/irie-people/irie/internal/remote/web"
)

//go:embed index.html app.css app.js icon.svg js vendor
var own embed.FS

// shared are the phone app's modules the page imports, by their names
// under shared/.
var shared = map[string]string{
	"look.js": "js/look.js",
	"md.js":   "js/md.js",
	"ui.js":   "js/ui.js",
}

// Origin is where the page is loaded from: a name that no network has, so
// nothing but the app answers for it.
const Origin = "https://app." + brand.Slug + ".invalid"

// FS is the page's files, the phone app's shared modules among them.
func FS() fs.FS { return files{} }

type files struct{}

func (files) Open(name string) (fs.File, error) {
	if rest, ok := strings.CutPrefix(name, "shared/"); ok {
		if p, ok := shared[rest]; ok {
			return phone.FS.Open(p)
		}
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return own.Open(name)
}

// File is the file at a URL's path, such as "/" or "/js/net.js", with its
// media type; ok is false when the page has no such file.
func File(urlPath string) (body []byte, mediaType string, ok bool) {
	name := strings.TrimPrefix(path.Clean("/"+urlPath), "/")
	if name == "" {
		name = "index.html"
	}
	b, err := fs.ReadFile(FS(), name)
	if err != nil {
		return nil, "", false
	}
	return b, Type(name), true
}

// Type is a file's media type by its extension. It does not ask Windows,
// whose registry may say text/plain for .js, which a browser refuses for
// a module.
func Type(name string) string {
	switch path.Ext(name) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".json", ".webmanifest":
		return "application/json"
	case ".woff2":
		return "font/woff2"
	}
	return "application/octet-stream"
}
