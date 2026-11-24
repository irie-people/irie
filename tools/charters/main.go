//go:build !nofounding

// Command charters writes the Charters Graph page's copies for the web
// (internal/founding, page.go; DESIGN §4.17): the site's, at
// site/charters/index.html, whose link home is the site itself, and, for
// each folder named, an index.html whose link home is the site's address,
// as the graph's own site has it. Run it from the checkout's top whenever
// a text, a link, an idea or the page changes:
// TestTheSiteHasTheGraph fails while the site's copy is stale.
//
//	go run ./tools/charters [DIR...]
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/founding"
)

func main() {
	if err := write(filepath.Join("site", "charters"), founding.SiteHome); err != nil {
		fail(err)
	}
	for _, dir := range os.Args[1:] {
		if err := write(dir, founding.Home{URL: brand.Site, Text: "Part of " + brand.Name}); err != nil {
			fail(err)
		}
	}
}

func write(dir string, home founding.Home) error {
	b, err := founding.WebPage(home)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "index.html")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s, %d bytes\n", path, len(b))
	return nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "charters:", err)
	os.Exit(1)
}
