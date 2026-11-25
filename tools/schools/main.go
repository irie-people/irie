//go:build !nofounding

// Command schools writes the Common Schools page's copies for the web
// (internal/schools, page.go; docs/proposals/common-schools.md): the
// site's, at site/schools/index.html, whose link home is the site itself,
// and, for each folder named, an index.html whose link home is the site's
// address, as the page's own site has it. Run it from the checkout's top
// whenever a text, a link, a figure or the page changes:
// TestTheSiteHasThePage fails while the site's copy is stale.
//
//	go run ./tools/schools [DIR...]
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/schools"
)

func main() {
	if err := write(filepath.Join("site", "schools"), schools.SiteHome); err != nil {
		fail(err)
	}
	for _, dir := range os.Args[1:] {
		if err := write(dir, schools.Home{URL: brand.Site, Text: "Part of " + brand.Name}); err != nil {
			fail(err)
		}
	}
}

func write(dir string, home schools.Home) error {
	b, err := schools.WebPage(home)
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
	fmt.Fprintln(os.Stderr, "schools:", err)
	os.Exit(1)
}
