package web

import (
	"bytes"
	"io/fs"
	"strings"
	"testing"
)

func TestTheAppServesThePageAndItsModules(t *testing.T) {
	for _, tc := range []struct{ path, typ, has string }{
		{"/", "text/html; charset=utf-8", `<script type="module" src="app.js">`},
		{"/index.html", "text/html; charset=utf-8", "Content-Security-Policy"},
		{"/app.js", "text/javascript; charset=utf-8", "import * as L from './shared/look.js'"},
		{"/shared/look.js", "text/javascript; charset=utf-8", "export function look("},
		{"/shared/md.js", "text/javascript; charset=utf-8", "export function md("},
		{"/vendor/xterm/xterm.js", "text/javascript; charset=utf-8", "Terminal"},
		{"/vendor/NOTICES.txt", "application/octet-stream", "xterm.js"},
		{"/icon.svg", "image/svg+xml", "<svg"},
	} {
		b, typ, ok := File(tc.path)
		if !ok || typ != tc.typ || !bytes.Contains(b, []byte(tc.has)) {
			t.Errorf("%s: ok %v, type %q, has %q: %v", tc.path, ok, typ, tc.has, bytes.Contains(b, []byte(tc.has)))
		}
	}
	for _, p := range []string{"/nope.js", "/shared/net.js", "/../web.go", "/shared/../../web.go"} {
		if _, _, ok := File(p); ok {
			t.Errorf("%s was served", p)
		}
	}
}

// Every module the page imports is one it has: a missing one would leave
// the window blank.
func TestEveryImportIsThere(t *testing.T) {
	err := fs.WalkDir(FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".js") || strings.HasPrefix(path, "vendor/") {
			return err
		}
		b, _ := fs.ReadFile(FS(), path)
		for _, line := range strings.Split(string(b), "\n") {
			_, from, ok := strings.Cut(line, " from '")
			if !strings.HasPrefix(line, "import ") || !ok {
				continue
			}
			from, _, _ = strings.Cut(from, "'")
			dir := path[:strings.LastIndex(path, "/")+1]
			target := clean(dir + from)
			if _, _, ok := File("/" + target); !ok {
				t.Errorf("%s imports %s, which the page does not have", path, from)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func clean(p string) string {
	var out []string
	for _, part := range strings.Split(p, "/") {
		switch part {
		case "", ".":
		case "..":
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
		default:
			out = append(out, part)
		}
	}
	return strings.Join(out, "/")
}
