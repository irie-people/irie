package design

// The copies: files that keep some of the library's values themselves,
// because what reads them cannot read the library where it runs. A block
// of colours in one sits between two comment lines, in the file's own
// comment syntax:
//
//	/* design: evergreen light */
//	--sidebar: #F4F4EC; --card: #FBFBF6;
//	/* design: end */
//
// The first line names what the block holds: a palette and a mode
// ("evergreen dark"), a palette alone, whose block says which set each
// colour is in with a light or dark key (`light: Swatches(`, `"dark": {`),
// or "terminal". Every name with a colour in a block, `--needs-tint:
// #FBE5DC`, `needsTint: 0xFBE5DC` or `"needs_tint": 0xFBE5DC`, must be one
// of its roles in any of those spellings, or a name the first line gives
// one: `design: evergreen light page=sidebar` reads --page as sidebar. A
// colour of the file's own goes outside the blocks. The agents' numbers
// are not in blocks, since each file names them its own way: each Copy
// says how to find them.

import (
	"bytes"
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// A Copy is a file that keeps some of the library's values.
type Copy struct {
	// Path is the file's place in the repository.
	Path string
	// Why says what keeps the copy and why it cannot read the library.
	Why string
	// Blocks are the blocks it must have, by their first line's name.
	Blocks []string
	// Numbers find the agents' numbers in it.
	Numbers []Number
}

// A Number finds one or more of tokens.json's numbers in a copy: Pattern
// must match once, with a group for each of Paths.
type Number struct {
	Pattern string
	Paths   []string
}

// swiftAgents are the agents' numbers in a Swift copy, named as
// Theme.swift names them.
var swiftAgents = []Number{
	{`let gap = ([\d.]+), step`, []string{"agents.needs_gap"}},
	{`markChroma = ([\d.]+)`, []string{"agents.mark.chroma"}},
	{`markLightness = \(light: ([\d.]+), dark: ([\d.]+)\)`, []string{"agents.mark.lightness.light", "agents.mark.lightness.dark"}},
	{`inkLightness = \(light: ([\d.]+), dark: ([\d.]+)\)`, []string{"agents.ink.lightness.light", "agents.ink.lightness.dark"}},
	{`washChroma = \(light: ([\d.]+), dark: ([\d.]+)\)`, []string{"agents.wash.chroma.light", "agents.wash.chroma.dark"}},
	{`washLift = \(light: (-?[\d.]+), dark: (-?[\d.]+)\)`, []string{"agents.wash.lift.light", "agents.wash.lift.dark"}},
	{`mix\(s\.card, full, t\)\), muted\) >= ([\d.]+)`, []string{"agents.wash.contrast"}},
}

// Copies are every file that keeps some of the library's values. Go's own
// clients read the library itself (internal/ui).
var Copies = []Copy{
	{
		Path:   "mac/Sources/Irie/Theme.swift",
		Why:    "The Mac app builds its palettes in, so it draws its first frame without reading a file.",
		Blocks: append(IDs(), "terminal"),
		Numbers: append([]Number{
			{`agentSlots = (\d+)`, []string{"agents.slots"}},
			{`high \? 4\.5 : ([\d.]+)`, []string{"agents.mark.contrast"}},
			{`high \? 7 : ([\d.]+)`, []string{"agents.ink.contrast"}},
		}, swiftAgents...),
	},
	{
		Path:   "ios/Irie.swiftpm/Sources/Look.swift",
		Why:    "The iPhone app draws in Evergreen until a core sends the Mac app's palette.",
		Blocks: []string{"evergreen", "evergreen", "terminal"},
		Numbers: append([]Number{
			{`static let slots = (\d+)`, []string{"agents.slots"}},
			{`s\.selection\]\.map\(luminance\), ([\d.]+), dark\)`, []string{"agents.mark.contrast"}},
			{`s\.panel\]\.map\(luminance\), ([\d.]+), dark\)`, []string{"agents.ink.contrast"}},
		}, swiftAgents...),
	},
	{
		Path:   "internal/remote/web/js/look.js",
		Why:    "The phone web app draws in Evergreen until the header brings the Mac app's palette.",
		Blocks: []string{"evergreen"},
		Numbers: []Number{
			{`const SLOTS = (\d+);`, []string{"agents.slots"}},
			{`const gap = ([\d.]+), step`, []string{"agents.needs_gap"}},
			{`MARK_C = ([\d.]+)`, []string{"agents.mark.chroma"}},
			{`MARK_L = \[([\d.]+), ([\d.]+)\]`, []string{"agents.mark.lightness.light", "agents.mark.lightness.dark"}},
			{`INK_L = \[([\d.]+), ([\d.]+)\]`, []string{"agents.ink.lightness.light", "agents.ink.lightness.dark"}},
			{`WASH_C = \[([\d.]+), ([\d.]+)\]`, []string{"agents.wash.chroma.light", "agents.wash.chroma.dark"}},
			{`WASH_LIFT = \[(-?[\d.]+), (-?[\d.]+)\]`, []string{"agents.wash.lift.light", "agents.wash.lift.dark"}},
			{`mix\(s\.card, full, t\)\), muted\) >= ([\d.]+)`, []string{"agents.wash.contrast"}},
			{`MARK_L\[dark \? 1 : 0\], grounds, ([\d.]+)`, []string{"agents.mark.contrast"}},
			{`INK_L\[dark \? 1 : 0\], grounds, ([\d.]+)`, []string{"agents.ink.contrast"}},
		},
	},
	{
		Path:   "internal/remote/web/app.css",
		Why:    "The phone web app's first paint, before look.js sets the palette; .on-dark is the dark set for what sits on its bars.",
		Blocks: []string{"evergreen light", "evergreen dark", "evergreen dark", "terminal"},
	},
	{
		Path:   "internal/winapp/web/app.css",
		Why:    "The Windows app's first paint, before look.js sets the palette.",
		Blocks: []string{"evergreen light", "evergreen dark", "terminal"},
	},
	{
		Path:   "internal/remote/web/setup.html",
		Why:    "The phone's setup page stands alone: it loads before the app and its scripts.",
		Blocks: []string{"evergreen light", "evergreen dark"},
	},
}

// A Problem is a value in a copy that is not the library's, or a copy the
// checks cannot read.
type Problem struct {
	Path string
	Line int
	What string
}

func (p Problem) String() string {
	if p.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", p.Path, p.Line, p.What)
	}
	return p.Path + ": " + p.What
}

var (
	marker = regexp.MustCompile(`(?://|/\*|<!--)\s*design:\s*(.*?)\s*(?:\*/|-->)?\s*$`)
	// pair is a name and a colour: --needs-tint: #FBE5DC, needsTint:
	// 0xFBE5DC, "needs_tint": 0xFBE5DC, termInk = fixed(0xEEE5D5).
	pair = regexp.MustCompile(`(?:--|")?([A-Za-z_][A-Za-z0-9_-]*)"?\s*[:=]\s*(?:[A-Za-z]\w*\()?(?:#|0x)([0-9A-Fa-f]{6})\b`)
	// modeKey is a light or dark key, which in a palette's block says
	// which set the colours after it are in.
	modeKey = regexp.MustCompile(`\b"?(light|dark)"?\s*[:=]`)
	camel   = regexp.MustCompile(`([a-z0-9])([A-Z])`)
)

// snake spells a name as theme files do: --needs-tint, needsTint and
// needs_tint are all needs_tint.
func snake(name string) string {
	name = strings.TrimPrefix(name, "--")
	name = camel.ReplaceAllString(name, "${1}_${2}")
	return strings.ToLower(strings.ReplaceAll(name, "-", "_"))
}

// An edit is a value in a copy and what the library says it should be.
type edit struct {
	line       int
	start, end int // the value's bytes in the file
	what       string
	got, want  string
	colour     bool // a colour's six digits, or else a number
}

// scan reads one copy: the values it keeps, each with the library's, and
// what it cannot read.
func scan(c Copy, src []byte) ([]edit, []Problem) {
	var edits []edit
	var probs []Problem
	bad := func(line int, f string, a ...any) {
		probs = append(probs, Problem{c.Path, line, fmt.Sprintf(f, a...)})
	}
	type block struct {
		name   string
		line   int
		values int
	}
	var open *block
	var found []string
	var set Set     // the colours the open block is checked against
	var byMode bool // whether a light or dark key picks the set
	var pal Palette // the open block's palette, if it is one
	var aliases map[string]string
	lines := bytes.SplitAfter(src, []byte("\n"))
	off := 0
	for i, l := range lines {
		n := i + 1
		text := string(l)
		if m := marker.FindStringSubmatch(text); m != nil {
			words := strings.Fields(m[1])
			switch {
			case len(words) == 0:
				bad(n, "a design: line with no name")
			case words[0] == "end":
				if open == nil {
					bad(n, "design: end with no block open")
				} else if open.values == 0 {
					bad(open.line, "the %s block has no colours", open.name)
				}
				open = nil
			case open != nil:
				bad(n, "design: %s inside the %s block from line %d", m[1], open.name, open.line)
			default:
				name, opts := words[0], words[1:]
				set, byMode = nil, false
				switch p, ok := PaletteByID(name); {
				case name == "terminal":
					t := TheTerminal()
					set = Set{"ground": t.Ground, "ink": t.Ink}
				case ok:
					pal = p
					if len(opts) > 0 && (opts[0] == "light" || opts[0] == "dark") {
						set = p.Mode(opts[0] == "dark")
						name += " " + opts[0]
						opts = opts[1:]
					} else {
						byMode = true
					}
				default:
					bad(n, "design: %s names no palette, nor terminal", words[0])
				}
				aliases = map[string]string{}
				for _, o := range opts {
					from, to, ok := strings.Cut(o, "=")
					if !ok {
						bad(n, "design: %q is not name=role", o)
						continue
					}
					aliases[snake(from)] = snake(to)
				}
				open = &block{name: name, line: n}
				found = append(found, name)
			}
			off += len(l)
			continue
		}
		if open != nil {
			if byMode {
				if m := modeKey.FindStringSubmatch(text); m != nil {
					set = pal.Mode(m[1] == "dark")
				}
			}
			for _, m := range pair.FindAllStringSubmatchIndex(text, -1) {
				raw := strings.TrimPrefix(text[m[0]:m[3]], `"`)
				key := snake(raw)
				if to, ok := aliases[key]; ok {
					key = to
				}
				if set == nil {
					bad(n, "%s comes before a light or dark key in the %s block", raw, open.name)
					continue
				}
				want, ok := set[key]
				if !ok {
					bad(n, "%s is no colour of the %s block: name it in its design: line (%s=ROLE), or move it out of the block", raw, open.name, raw)
					continue
				}
				open.values++
				got := strings.ToUpper(text[m[4]:m[5]])
				edits = append(edits, edit{n, off + m[4], off + m[5], open.name + " " + key, got, strings.TrimPrefix(want, "#"), true})
			}
		}
		off += len(l)
	}
	if open != nil {
		bad(open.line, "the %s block has no design: end", open.name)
	}
	slices.Sort(found)
	if want := slices.Sorted(slices.Values(c.Blocks)); !slices.Equal(found, want) {
		bad(0, "has the blocks %q, Copies says %q", found, want)
	}
	for _, num := range c.Numbers {
		re := regexp.MustCompile(num.Pattern)
		all := re.FindAllSubmatchIndex(src, -1)
		if len(all) != 1 {
			bad(0, "%d matches of %s, want 1", len(all), num.Pattern)
			continue
		}
		m := all[0]
		for g, path := range num.Paths {
			s, e := m[2+2*g], m[3+2*g]
			want, ok := Value(path)
			if !ok {
				bad(0, "tokens.json has no %s", path)
				continue
			}
			got := string(src[s:e])
			if strings.Contains(got, ".") && !strings.Contains(want, ".") {
				want += ".0" // 30.0 stays a Double in Swift
			}
			line := bytes.Count(src[:s], []byte("\n")) + 1
			edits = append(edits, edit{line, s, e, path, got, want, false})
		}
	}
	return edits, probs
}

// differs says whether a copy's value is not the library's: colours by
// their digits, numbers by their worth, so 0.50 is 0.5.
func (e edit) differs() bool {
	if e.colour {
		return !strings.EqualFold(e.got, e.want)
	}
	a, errA := strconv.ParseFloat(e.got, 64)
	b, errB := strconv.ParseFloat(e.want, 64)
	return errA != nil || errB != nil || a != b
}

// Check reads every copy under root, the repository, and says where one
// is not the library's.
func Check(root string) []Problem {
	var out []Problem
	for _, c := range Copies {
		src, err := os.ReadFile(filepath.Join(root, c.Path))
		if err != nil {
			out = append(out, Problem{c.Path, 0, err.Error()})
			continue
		}
		edits, probs := scan(c, src)
		out = append(out, probs...)
		for _, e := range edits {
			if e.differs() {
				out = append(out, Problem{c.Path, e.line, fmt.Sprintf("%s is %s here and %s in the library", e.what, e.got, e.want)})
			}
		}
	}
	if src, err := os.ReadFile(filepath.Join(root, ThemesPage)); err != nil {
		out = append(out, Problem{ThemesPage, 0, err.Error()})
	} else {
		_, probs := themesPage(string(src))
		out = append(out, probs...)
	}
	for _, g := range generated(root) {
		if old, err := os.ReadFile(filepath.Join(root, g.path)); err != nil || !bytes.Equal(old, g.body) {
			out = append(out, Problem{g.path, 0, "is not what the library makes"})
		}
	}
	slices.SortStableFunc(out, func(a, b Problem) int { return cmp.Compare(a.Path, b.Path) })
	return out
}

// Sync writes the library's values into every copy under root and makes
// the generated files again. It changes nothing in a copy it cannot read
// whole, and says why.
func Sync(root string) (changed []string, probs []Problem) {
	for _, c := range Copies {
		path := filepath.Join(root, c.Path)
		src, err := os.ReadFile(path)
		if err != nil {
			probs = append(probs, Problem{c.Path, 0, err.Error()})
			continue
		}
		edits, p := scan(c, src)
		if len(p) > 0 {
			probs = append(probs, p...)
			continue
		}
		slices.SortFunc(edits, func(a, b edit) int { return cmp.Compare(a.start, b.start) })
		var out []byte
		last, dirty := 0, false
		for _, e := range edits {
			if !e.differs() {
				continue
			}
			out = append(out, src[last:e.start]...)
			out = append(out, e.want...)
			last, dirty = e.end, true
		}
		if !dirty {
			continue
		}
		out = append(out, src[last:]...)
		if err := os.WriteFile(path, out, 0o644); err != nil {
			probs = append(probs, Problem{c.Path, 0, err.Error()})
			continue
		}
		changed = append(changed, c.Path)
	}
	page := filepath.Join(root, ThemesPage)
	if src, err := os.ReadFile(page); err != nil {
		probs = append(probs, Problem{ThemesPage, 0, err.Error()})
	} else {
		fixed, _ := themesPage(string(src))
		if fixed != string(src) {
			if err := os.WriteFile(page, []byte(fixed), 0o644); err != nil {
				probs = append(probs, Problem{ThemesPage, 0, err.Error()})
			} else {
				changed = append(changed, ThemesPage)
			}
		}
		_, left := themesPage(fixed) // what only a new drawing fixes
		probs = append(probs, left...)
	}
	for _, g := range generated(root) {
		path := filepath.Join(root, g.path)
		if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, g.body) {
			continue
		}
		if err := os.WriteFile(path, g.body, 0o644); err != nil {
			probs = append(probs, Problem{g.path, 0, err.Error()})
			continue
		}
		changed = append(changed, g.path)
	}
	return changed, probs
}
