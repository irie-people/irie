package designcheck

// The site's themes page (site/themes.html) draws each palette in pictures
// and gives its text colours in a table, all as plain colours, since the
// site is static pages. Check reads both, as Irie's own check beside the
// library's of the other copies: each colour in a palette's
// pictures must be one of its colours, or one of the mixes the pictures
// use; each row of its table must give the role's colour and its lowest
// contrast. Sync writes the tables; a picture with an old colour has to
// be drawn again by hand, until the page is made from the library.

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/irie-people/design"
)

// ThemesPage is the site's page of the palettes.
const ThemesPage = "site/themes.html"

// pictureMixes are the colours the pictures mix from a palette's: the
// header's pills are the state, or a little ink, over the sidebar, and the
// needs card's edge is needs over its tint. Each is t of a over b, rounded
// as Python's round() rounds, which made them.
var pictureMixes = []struct {
	a, b string
	t    float64
}{
	{"needs", "sidebar", 0.12},
	{"done", "sidebar", 0.12},
	{"ink", "sidebar", 0.07},
	{"needs", "needs_tint", 0.45},
}

// tableRows are the themes page's table rows by their titles, and the
// role each gives.
var tableRows = map[string]string{
	"Ink": "ink", "Quiet text": "muted", "Needs you": "needs", "Done": "done",
	"Paused": "paused", "Frozen": "frozen", "Accent": "accent",
}

// tableGrounds are what the table measures each colour's lowest contrast
// on.
var tableGrounds = []string{"card", "selection", "needs_tint", "panel", "field"}

// lowest is a colour's lowest contrast on the table's grounds.
func lowest(s design.Set, role string) float64 {
	low := math.Inf(1)
	for _, g := range tableGrounds {
		low = min(low, design.Contrast(s[role], s[g]))
	}
	return low
}

var (
	section  = regexp.MustCompile(`<h2 id="(\w+)"`)
	figure   = regexp.MustCompile(`aria-labelledby="(\w+)-(light|dark)-title"`)
	anyHex   = regexp.MustCompile(`#[0-9A-Fa-f]{6}\b`)
	tableRow = regexp.MustCompile(`<tr><th scope="row">([^<]+?) <span class="role">[^<]*</span></th>` +
		`<td>(.*?fill="(#[0-9A-Fa-f]{6})".*?<code>(#[0-9A-Fa-f]{6})</code> ([\d.]+):1)</td>` +
		`<td>(.*?fill="(#[0-9A-Fa-f]{6})".*?<code>(#[0-9A-Fa-f]{6})</code> ([\d.]+):1)</td></tr>`)
)

// themesPage checks the page, and gives it back with its tables written
// from the library.
func themesPage(src string) (string, []design.Problem) {
	var probs []design.Problem
	bad := func(at int, f string, a ...any) {
		probs = append(probs, design.Problem{Path: ThemesPage, Line: strings.Count(src[:at], "\n") + 1, What: fmt.Sprintf(f, a...)})
	}
	type span struct {
		id         string
		start, end int
	}
	var secs []span
	all := section.FindAllStringSubmatchIndex(src, -1)
	for i, m := range all {
		id := src[m[2]:m[3]]
		if _, ok := design.PaletteByID(id); !ok {
			continue
		}
		end := len(src)
		if i+1 < len(all) {
			end = all[i+1][0]
		}
		secs = append(secs, span{id, m[0], end})
	}
	// The overview above the sections draws every palette's sidebar, sun,
	// ink, accent, done and needs.
	if o := strings.Index(src, `<figure class="picture small">`); o >= 0 && (len(secs) == 0 || o < secs[0].start) {
		known := map[string]bool{}
		for _, p := range design.Palettes() {
			for _, s := range []design.Set{p.Light, p.Dark} {
				for _, v := range s {
					known[v] = true
				}
			}
		}
		end := o + strings.Index(src[o:], "</figure>")
		for _, h := range anyHex.FindAllStringIndex(src[o:end], -1) {
			if c := strings.ToUpper(src[o+h[0] : o+h[1]]); !known[c] {
				bad(o+h[0], "the overview has %s, which is no palette's colour: draw it again from the library", c)
			}
		}
	} else {
		bad(0, "has no overview picture before its palettes")
	}
	seen := map[string]bool{}
	var out strings.Builder
	last := 0
	for _, sec := range secs {
		p, _ := design.PaletteByID(sec.id)
		seen[sec.id] = true
		body := src[sec.start:sec.end]
		table := strings.Index(body, "<table")
		if table < 0 {
			bad(sec.start, "%s has no table", p.Name)
			table = len(body)
		}
		figs := figure.FindAllStringSubmatchIndex(body[:table], -1)
		if len(figs) != 2 {
			bad(sec.start, "%s has %d pictures, want light and dark", p.Name, len(figs))
		}
		for i, f := range figs {
			end := table
			if i+1 < len(figs) {
				end = figs[i+1][0]
			}
			s := p.Mode(body[f[4]:f[5]] == "dark")
			ok := map[string]string{}
			for k, v := range s {
				ok[v] = k
			}
			for _, x := range pictureMixes {
				ok[design.Mix(s[x.a], s[x.b], x.t)] = fmt.Sprintf("%.0f%% %s over %s", x.t*100, x.a, x.b)
			}
			for _, h := range anyHex.FindAllStringIndex(body[f[0]:end], -1) {
				c := strings.ToUpper(body[f[0]+h[0] : f[0]+h[1]])
				if _, known := ok[c]; !known {
					bad(sec.start+f[0]+h[0], "%s's %s picture has %s, which is none of its colours: draw it again from the library", p.Name, body[f[4]:f[5]], c)
				}
			}
		}
		rows := tableRow.FindAllStringSubmatchIndex(body[table:], -1)
		if len(rows) != len(tableRows) {
			bad(sec.start+table, "%s's table has %d rows this check can read, want %d", p.Name, len(rows), len(tableRows))
		}
		for _, r := range rows {
			at := sec.start + table
			title := body[table+r[2] : table+r[3]]
			role, known := tableRows[title]
			if !known {
				bad(at+r[0], "%s's table has a row %q, which names no role", p.Name, title)
				continue
			}
			for i, dark := range []bool{false, true} {
				g := 4 + 8*i // the cell's groups: whole, fill, code, ratio
				cell := body[table+r[g] : table+r[g+1]]
				fill, code, ratio := body[table+r[g+2]:table+r[g+3]], body[table+r[g+4]:table+r[g+5]], body[table+r[g+6]:table+r[g+7]]
				want := p.Mode(dark)[role]
				low := lowest(p.Mode(dark), role)
				mode := map[bool]string{false: "light", true: "dark"}[dark]
				got, _ := strconv.ParseFloat(ratio, 64)
				if !strings.EqualFold(fill, want) || !strings.EqualFold(code, want) || math.Abs(got-low) > 0.006 {
					bad(at+r[g], "%s %s %s is %s at %s:1 in the table, %s at %.2f:1 in the library", p.Name, mode, role, code, ratio, want, low)
				}
				// The cell as the library gives it.
				fixed := strings.Replace(cell, fill, want, 1)
				fixed = strings.Replace(fixed, "<code>"+code+"</code> "+ratio+":1", fmt.Sprintf("<code>%s</code> %.2f:1", want, low), 1)
				out.WriteString(src[last : at+r[g]])
				out.WriteString(fixed)
				last = at + r[g+1]
			}
		}
	}
	out.WriteString(src[last:])
	for _, id := range design.IDs() {
		if !seen[id] {
			probs = append(probs, design.Problem{Path: ThemesPage, What: fmt.Sprintf("has no section for %s", id)})
		}
	}
	return out.String(), probs
}

// checkThemesPage reads the page under root.
func checkThemesPage(root string) []design.Problem {
	src, err := os.ReadFile(filepath.Join(root, ThemesPage))
	if err != nil {
		return []design.Problem{{Path: ThemesPage, What: err.Error()}}
	}
	_, probs := themesPage(string(src))
	return probs
}

// syncThemesPage writes the page's tables from the library, and says what
// only a new drawing fixes.
func syncThemesPage(root string) (changed bool, probs []design.Problem) {
	path := filepath.Join(root, ThemesPage)
	src, err := os.ReadFile(path)
	if err != nil {
		return false, []design.Problem{{Path: ThemesPage, What: err.Error()}}
	}
	fixed, _ := themesPage(string(src))
	if fixed != string(src) {
		if err := os.WriteFile(path, []byte(fixed), 0o644); err != nil {
			return false, []design.Problem{{Path: ThemesPage, What: err.Error()}}
		}
		changed = true
	}
	_, left := themesPage(fixed)
	return changed, left
}
