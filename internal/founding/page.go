package founding

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"strings"
	"sync"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
)

// The Charters Graph page: the explorer (graph/) and the graph it draws,
// put together as one HTML file that loads nothing from elsewhere, so the
// Mac app's window can show it with the network shut, and a browser can
// open it from the disk. The web's copies, on the site and on the graph's
// own site, add a link home in the bar (tools/charters writes them).

//go:embed graph/index.html graph/graph.css graph/graph.js
var graphFiles embed.FS

var (
	pageOnce sync.Once
	page     []byte
	pageErr  error
)

// Page gives the page for the texts this build carries, made once: the
// texts are built into the binary and never change while it runs.
func Page() ([]byte, error) {
	pageOnce.Do(func() {
		docs, err := Load()
		if err != nil {
			pageErr = err
			return
		}
		page, pageErr = MakePage(docs, Home{})
	})
	return page, pageErr
}

// Home is a link home in the page's bar, for a copy on the web: where it
// goes and what it says. The Mac app's window has none.
type Home struct {
	URL, Text string
}

// SiteHome is the link home of the site's copy, site/charters/index.html,
// which sits a folder below the site's own pages.
var SiteHome = Home{URL: "../", Text: "← " + brand.Name}

// WebPage gives the page with a link home, for a copy on the web.
func WebPage(home Home) ([]byte, error) {
	docs, err := Load()
	if err != nil {
		return nil, err
	}
	return MakePage(docs, home)
}

// MakePage puts the page together from the texts: the graph, as JSON in a
// script element, then the page's own style and script, and the link home
// when there is one.
func MakePage(docs []ipc.FoundingDoc, home Home) ([]byte, error) {
	g, err := BuildGraph(docs)
	if err != nil {
		return nil, err
	}
	// json.Marshal writes <, > and & as \u escapes, so nothing in the data
	// can end the script element that holds it.
	data, err := json.Marshal(g)
	if err != nil {
		return nil, err
	}
	read := func(name string) (string, error) {
		b, err := graphFiles.ReadFile("graph/" + name)
		return string(b), err
	}
	tmpl, err := read("index.html")
	if err != nil {
		return nil, err
	}
	css, err := read("graph.css")
	if err != nil {
		return nil, err
	}
	js, err := read("graph.js")
	if err != nil {
		return nil, err
	}
	for name, s := range map[string]string{"graph.css": css, "graph.js": js} {
		if strings.Contains(strings.ToLower(s), "</style") || strings.Contains(strings.ToLower(s), "</script") {
			return nil, fmt.Errorf("%s would end its own element", name)
		}
	}
	link := ""
	if home.URL != "" {
		link = `<a class="home" href="` + html.EscapeString(home.URL) + `">` + html.EscapeString(home.Text) + `</a>`
	}
	var b bytes.Buffer
	for _, part := range []struct{ mark, with string }{
		{"/*{{style}}*/", css},
		{"<!--{{home}}-->", link},
		{"{{data}}", string(data)},
		{"/*{{script}}*/", js},
	} {
		i := strings.Index(tmpl, part.mark)
		if i < 0 {
			return nil, fmt.Errorf("index.html has no %s", part.mark)
		}
		b.WriteString(tmpl[:i])
		b.WriteString(part.with)
		tmpl = tmpl[i+len(part.mark):]
	}
	b.WriteString(tmpl)
	return b.Bytes(), nil
}
