// Package extension is Irie in your own browser: a browser extension for
// Chrome and the browsers built like it, and for Firefox, whose side panel
// holds tools that run on this computer, and the native messaging host
// that is its one way into Irie (docs/proposals/browser-extension.md). Its
// first tool is Terms in short (internal/terms).
//
// The extension reads a page only when the person clicks its button, and
// sends what it reads only to the host, which the browser starts as a
// program of this computer's and talks to over its standard input and
// output. The host runs Irie's own code: nothing goes to a server, and no
// model is bought.
package extension

import (
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/paths"
)

// web is the extension's own files, written into its folder as they are
// but for manifest.json and brand.js, which take this build's names.
//
//go:embed web
var web embed.FS

// Key is the public half of the key that fixes the extension's ID in
// Chrome and the browsers built like it, so the host can name the one
// extension it answers. Its private half was never kept: it would only pack
// the extension for a store, which gives an ID of its own.
const Key = "MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAwyYH4AB9n8EmEmjkxPn+C9r8rvYak2ciCCLpK4buakotPlp8yp/3QG0YIa9KmCEotI4x3rL9WxGTs3GuZYQ2qymtHG8W4y4WBvOj8FJNoFpn6X7oKCj/q9a57YDqWTxfDvfZhUls1CU8w/YwiN6abv5EBLb/4louVLxO6+6XyvfWVZrsa7xlKHytKS42h2GafsdbAKI06IpDbyu1ypnO3XQoZ2SUvB1YaTQjIbZk2qLNbdrNtNB60GLxBKaYXFh9yh6R8R0dde+uN7fkvOzlIZ5KCqGq5nH6MrAb7Y2gIxSPB8pN3qhGtswK2x6xgbj/7DZ58CMHmAhbiet57ihOkQIDAQAB"

// ChromeID is the extension's ID in Chrome and the browsers built like it,
// made from its key as Chrome makes it: the first 16 bytes of the key's
// SHA-256, each half byte a letter from a to p.
func ChromeID() string {
	der, err := base64.StdEncoding.DecodeString(Key)
	if err != nil {
		panic("extension: Key is not base64: " + err.Error())
	}
	sum := sha256.Sum256(der)
	h := hex.EncodeToString(sum[:16])
	var b strings.Builder
	for _, c := range h {
		switch {
		case c >= '0' && c <= '9':
			b.WriteByte(byte('a' + c - '0'))
		default:
			b.WriteByte(byte('a' + 10 + c - 'a'))
		}
	}
	return b.String()
}

// GeckoID is the extension's ID in Firefox, which its manifest names.
func GeckoID() string {
	host := brand.Slug + ".invalid"
	if u, err := url.Parse(brand.Site); err == nil && u.Hostname() != "" {
		host = u.Hostname()
	}
	return "extension@" + host
}

// HostName is the native messaging host's name, which the extension asks
// the browser for: the Mac app's bundle ID in the letters a host's name
// may have.
func HostName() string {
	n := strings.ToLower(brand.BundleID)
	n = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.':
			return r
		}
		return '_'
	}, n)
	return strings.Trim(n, ".") + ".extension"
}

// Folder is where the extension's files are written, the folder a browser
// loads it from.
func Folder(p paths.Paths) string { return filepath.Join(p.Data, "extension") }

// HostFile is the program the browsers start as the host: a script that
// runs irie extension host.
func HostFile(p paths.Paths) string { return filepath.Join(p.Data, "extension-host") }

// seenFile is where the host notes which browsers connected, and when.
func seenFile(p paths.Paths) string { return filepath.Join(p.State, "extension-seen.json") }

// Status is how the extension stands on this computer, from its files
// alone.
type Status struct {
	Folder string `json:"folder"`
	// Written is the version of the extension in its folder, "" while
	// there is none; Current says whether it is this build's.
	Written string `json:"written,omitempty"`
	Current bool   `json:"current"`
	// Browsers are the browsers the host is set up for.
	Browsers []string `json:"browsers,omitempty"`
	// Seen are the browsers the extension last talked to Irie from, and
	// when.
	Seen map[string]time.Time `json:"seen,omitempty"`
}

// Look says how the extension stands, reading only files.
func Look(p paths.Paths, home string) Status {
	s := Status{Folder: Folder(p)}
	if b, err := os.ReadFile(filepath.Join(s.Folder, "manifest.json")); err == nil {
		var m struct {
			Version string `json:"version_name"`
		}
		if json.Unmarshal(b, &m) == nil {
			s.Written = m.Version
			s.Current = m.Version == Version()
		}
	}
	for _, b := range Browsers(home) {
		if _, err := os.Stat(filepath.Join(b.Dir, HostName()+".json")); err == nil {
			s.Browsers = append(s.Browsers, b.Name)
		}
	}
	if b, err := os.ReadFile(seenFile(p)); err == nil {
		_ = json.Unmarshal(b, &s.Seen)
	}
	return s
}
