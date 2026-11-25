package schools

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

// The Common Schools page: the explorer (pies/) and the pies it draws, the
// texts and their sources, put together as one HTML file that loads
// nothing from elsewhere, so the Mac app's window can show it with the
// network shut, and a browser can open it from the disk.

//go:embed pies/index.html pies/pies.css pies/pies.js
var pieFiles embed.FS

var (
	pageOnce sync.Once
	page     []byte
	pageErr  error
)

// Page gives the page this build makes, made once: its texts and figures
// are built into the binary and never change while it runs.
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

// SiteHome is the link home of the site's copy, site/schools/index.html,
// which sits a folder below the site's own pages.
var SiteHome = Home{URL: "../", Text: "← " + brand.Name}

// WebPage gives the page with a link home, for a copy on the web: the
// site's (tools/schools writes it) and its own site's.
func WebPage(home Home) ([]byte, error) {
	docs, err := Load()
	if err != nil {
		return nil, err
	}
	return MakePage(docs, home)
}

// Text is a text as the page carries it: the document, with where it
// comes from and its passages about schools.
type Text struct {
	ipc.FoundingDoc
	Place string   `json:"place"`
	Skill string   `json:"skill"`
	File  string   `json:"file"`
	Lit   []string `json:"lit,omitempty"`
}

// Data is everything the page draws, as JSON in the page.
type Data struct {
	Lenses  []Lens            `json:"lenses"`
	Texts   map[string]Text   `json:"texts"`
	Order   []string          `json:"order"`
	Links   []Link            `json:"links"`
	Sources map[string]Source `json:"sources"`
	Colours Colours           `json:"colours"`
}

// MakeData puts the page's data together from the texts.
func MakeData(docs []ipc.FoundingDoc) (Data, error) {
	d := Data{Lenses: Lenses(), Texts: map[string]Text{}, Links: Links, Sources: map[string]Source{}, Colours: SliceColours()}
	byID := map[string]ipc.FoundingDoc{}
	for _, x := range docs {
		byID[x.ID] = x
	}
	for _, m := range Documents {
		x, ok := byID[m.ID]
		if !ok {
			return Data{}, fmt.Errorf("no text %s", m.ID)
		}
		d.Texts[m.ID] = Text{FoundingDoc: x, Place: m.Place, Skill: m.skill(), File: m.File, Lit: m.Lit}
		d.Order = append(d.Order, m.ID)
	}
	for _, s := range AllSources() {
		d.Sources[s.ID] = s
	}
	return d, nil
}

// MakePage puts the page together from the texts: the data, as JSON in a
// script element, then the page's own style and script, and the link home
// when there is one.
func MakePage(docs []ipc.FoundingDoc, home Home) ([]byte, error) {
	d, err := MakeData(docs)
	if err != nil {
		return nil, err
	}
	// json.Marshal writes <, > and & as \u escapes, so nothing in the data
	// can end the script element that holds it.
	data, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	read := func(name string) (string, error) {
		b, err := pieFiles.ReadFile("pies/" + name)
		return string(b), err
	}
	tmpl, err := read("index.html")
	if err != nil {
		return nil, err
	}
	css, err := read("pies.css")
	if err != nil {
		return nil, err
	}
	js, err := read("pies.js")
	if err != nil {
		return nil, err
	}
	for name, s := range map[string]string{"pies.css": css, "pies.js": js} {
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
