// Package design is Irie's design library in code: the built-in palettes
// and the tokens every client draws with, read from the JSON files beside
// it. Those files are the one place each value is kept. Go reads them
// here; the clients that cannot (the Mac and iPhone apps, the web apps'
// CSS and JavaScript) keep copies, which Copies lists and tools/design.sh
// writes, and TestCopiesMatchTheLibrary fails when one has drifted
// (design/README.md, DESIGN §2.11).
package design

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

//go:embed tokens.json palettes/*.json
var files embed.FS

// Group is how Make a Theme groups a role (mac/Sources/Irie/Looks.swift,
// ColorRole.Kind).
type Group string

const (
	Neutral Group = "neutral"
	Accent  Group = "accent"
	State   Group = "state"
	Sun     Group = "sun"
)

// Use is how a role is drawn, which says what contrast it must keep.
type Use string

const (
	// Surface is a ground other colours are drawn on.
	Surface Use = "surface"
	// Line is a hairline between surfaces; it carries no meaning alone.
	Line Use = "line"
	// Text is drawn as words or a state's glyph: 4.5:1 on every surface
	// (WCAG 2.2, 1.4.3).
	Text Use = "text"
	// Mark is drawn as a mark that is not text: 3:1 on every surface
	// (WCAG 2.2, 1.4.11).
	Mark Use = "mark"
	// Fill is a button's face; its own text goes on it at 4.5:1.
	Fill Use = "fill"
	// OnFill is the text or arrow on a Fill.
	OnFill Use = "on_fill"
	// Picture is a colour of Irie's icon, drawn large.
	Picture Use = "picture"
)

// Role is one of a palette's colours.
type Role struct {
	// Key is its name in a theme file: "needs_tint".
	Key string
	// Title is its name in Make a Theme: "Card that needs you".
	Title string
	Group Group
	Use   Use
}

// roles are a palette's 21 colours, in the palette files' order.
var roles = []Role{
	{"sidebar", "Sidebar", Neutral, Surface},
	{"ground", "Window", Neutral, Surface},
	{"card", "Card", Neutral, Surface},
	{"panel", "Details panel", Neutral, Surface},
	{"field", "Field", Neutral, Surface},
	{"edge", "Hairline", Neutral, Line},
	{"hover", "Card under the pointer", Neutral, Surface},
	{"selection", "Selected card", Accent, Surface},
	{"needs_tint", "Card that needs you", State, Surface},
	{"ink", "Text", Neutral, Text},
	{"muted", "Quiet text", Neutral, Text},
	{"accent", "Accent", Accent, Mark},
	{"accent_ink", "Accent text", Accent, Text},
	{"accent_fill", "Start button", Accent, Fill},
	{"on_accent", "Arrow on the start button", Accent, OnFill},
	{"needs", "Needs you", State, Text},
	{"done", "Done", State, Text},
	{"paused", "Paused", State, Text},
	{"frozen", "Frozen", State, Text},
	{"sun", "Sun", Sun, Picture},
	{"waves", "Waves", Accent, Picture},
}

// Roles are a palette's colours, in the palette files' order.
func Roles() []Role { return slices.Clone(roles) }

// RolesFor are the roles drawn as use.
func RolesFor(use Use) []Role {
	var out []Role
	for _, r := range roles {
		if r.Use == use {
			out = append(out, r)
		}
	}
	return out
}

// Set is one mode of a palette: "#RRGGBB" by role key.
type Set map[string]string

// Palette is one of Irie's built-in palettes, both its sets.
type Palette struct {
	// ID is its file's name and ui.theme's value: "evergreen".
	ID string
	// Name is as the menus show it: "Evergreen".
	Name        string
	Light, Dark Set
}

// Mode is the palette's dark set, or its light one.
func (p Palette) Mode(dark bool) Set {
	if dark {
		return p.Dark
	}
	return p.Light
}

// Pair is a number for each mode.
type Pair struct {
	Light float64 `json:"light"`
	Dark  float64 `json:"dark"`
}

// Of is the pair's number for the mode.
func (p Pair) Of(dark bool) float64 {
	if dark {
		return p.Dark
	}
	return p.Light
}

// Agents are the steps that work out the agents' own colours, a working
// card's wash, mark and text, from a palette (DESIGN §4.1). Every client
// that draws them takes the same steps with these numbers.
type Agents struct {
	// Slots is how many colours there are: hues evenly spread round the
	// wheel but NeedsGap degrees either side of the palette's needs red.
	Slots    int     `json:"slots"`
	NeedsGap float64 `json:"needs_gap"`
	// Mark is the card's mark: its OKLCH chroma, the lightness it starts
	// from, and the contrast it is moved until it keeps on the card's
	// wash and on the selection.
	Mark struct {
		Chroma    float64 `json:"chroma"`
		Lightness Pair    `json:"lightness"`
		Contrast  float64 `json:"contrast"`
	} `json:"mark"`
	// Ink is the card's text in the agent's hue, at the mark's chroma.
	Ink struct {
		Lightness Pair    `json:"lightness"`
		Contrast  float64 `json:"contrast"`
	} `json:"ink"`
	// Wash is the working card's ground: its chroma, how far its
	// lightness moves from the card's, and the contrast the card's quiet
	// text keeps on it, which says how far the wash goes.
	Wash struct {
		Chroma   Pair    `json:"chroma"`
		Lift     Pair    `json:"lift"`
		Contrast float64 `json:"contrast"`
	} `json:"wash"`
}

// Terminal is the terminal's own ground and text, the same in every
// built-in palette; a theme file may set its own.
type Terminal struct {
	Ground string `json:"ground"`
	Ink    string `json:"ink"`
}

type tokens struct {
	Palettes []string `json:"palettes"`
	Default  string   `json:"default"`
	Contrast struct {
		Text float64 `json:"text"`
		Mark float64 `json:"mark"`
	} `json:"contrast"`
	Terminal Terminal `json:"terminal"`
	Agents   Agents   `json:"agents"`
}

// library is everything the files hold.
type library struct {
	tokens   tokens
	palettes []Palette
	// values are tokens.json's numbers and colours by their dotted path
	// ("agents.mark.chroma"), as written there.
	values map[string]string
}

var lib = mustLoad()

func mustLoad() library {
	l, err := load()
	if err != nil {
		panic("design: " + err.Error())
	}
	return l
}

var hexColor = regexp.MustCompile(`^#[0-9A-F]{6}$`)

func load() (library, error) {
	var l library
	raw, err := files.ReadFile("tokens.json")
	if err != nil {
		return l, err
	}
	if err := strict(raw, &l.tokens); err != nil {
		return l, fmt.Errorf("tokens.json: %w", err)
	}
	if l.values, err = flatten(raw); err != nil {
		return l, fmt.Errorf("tokens.json: %w", err)
	}
	t := l.tokens
	if !slices.Contains(t.Palettes, t.Default) {
		return l, fmt.Errorf("tokens.json: the default palette %q is not in palettes", t.Default)
	}
	for _, c := range []string{t.Terminal.Ground, t.Terminal.Ink} {
		if !hexColor.MatchString(c) {
			return l, fmt.Errorf("tokens.json: terminal colour %q is not #RRGGBB in capitals", c)
		}
	}
	listed, _ := files.ReadDir("palettes")
	if len(listed) != len(t.Palettes) {
		return l, fmt.Errorf("palettes/ has %d files, tokens.json lists %d palettes", len(listed), len(t.Palettes))
	}
	for _, id := range t.Palettes {
		p, err := loadPalette(id)
		if err != nil {
			return l, fmt.Errorf("palettes/%s.json: %w", id, err)
		}
		l.palettes = append(l.palettes, p)
	}
	return l, nil
}

func loadPalette(id string) (Palette, error) {
	raw, err := files.ReadFile("palettes/" + id + ".json")
	if err != nil {
		return Palette{}, err
	}
	var f struct {
		Name  string `json:"name"`
		Light Set    `json:"light"`
		Dark  Set    `json:"dark"`
	}
	if err := strict(raw, &f); err != nil {
		return Palette{}, err
	}
	if f.Name == "" {
		return Palette{}, fmt.Errorf("no name")
	}
	for mode, s := range map[string]Set{"light": f.Light, "dark": f.Dark} {
		for _, r := range roles {
			c, ok := s[r.Key]
			if !ok {
				return Palette{}, fmt.Errorf("%s has no %s", mode, r.Key)
			}
			if !hexColor.MatchString(c) {
				return Palette{}, fmt.Errorf("%s %s is %q, not #RRGGBB in capitals", mode, r.Key, c)
			}
		}
		if len(s) != len(roles) {
			for _, k := range slices.Sorted(maps.Keys(s)) {
				if !slices.ContainsFunc(roles, func(r Role) bool { return r.Key == k }) {
					return Palette{}, fmt.Errorf("%s has %q, which is no role", mode, k)
				}
			}
		}
	}
	return Palette{ID: id, Name: f.Name, Light: f.Light, Dark: f.Dark}, nil
}

// strict decodes JSON that may hold only the fields v has.
func strict(raw []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(v)
}

// flatten gives every number and string in a JSON object by its dotted
// path, as written.
func flatten(raw []byte) (map[string]string, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	out := map[string]string{}
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, e := range x {
				walk(strings.TrimPrefix(prefix+"."+k, "."), e)
			}
		case json.Number:
			out[prefix] = x.String()
		case string:
			out[prefix] = x
		}
	}
	walk("", v)
	return out, nil
}

// Palettes are Irie's built-in palettes, in the menus' order.
func Palettes() []Palette { return slices.Clone(lib.palettes) }

// IDs are the built-in palettes' IDs, in the menus' order: ui.theme's
// values.
func IDs() []string { return slices.Clone(lib.tokens.Palettes) }

// Default is the palette Irie draws in until one is chosen.
func Default() string { return lib.tokens.Default }

// PaletteByID is the built-in palette id.
func PaletteByID(id string) (Palette, bool) {
	i := slices.IndexFunc(lib.palettes, func(p Palette) bool { return p.ID == id })
	if i < 0 {
		return Palette{}, false
	}
	return lib.palettes[i], true
}

// TheAgents are the steps of the agents' colours.
func TheAgents() Agents { return lib.tokens.Agents }

// TheTerminal is the terminal's ground and text in the built-in palettes.
func TheTerminal() Terminal { return lib.tokens.Terminal }

// TextContrast and MarkContrast are the least contrast text and marks keep
// on every surface.
func TextContrast() float64 { return lib.tokens.Contrast.Text }
func MarkContrast() float64 { return lib.tokens.Contrast.Mark }

// Value is one of tokens.json's numbers or colours by its dotted path, as
// written there, and whether it has one.
func Value(path string) (string, bool) {
	v, ok := lib.values[path]
	return v, ok
}

// RGB is a "#RRGGBB" colour's channels, 0 to 255.
func RGB(hex string) (r, g, b uint8, err error) {
	s := strings.TrimPrefix(strings.TrimPrefix(hex, "#"), "0x")
	if len(s) != 6 {
		return 0, 0, 0, fmt.Errorf("%q is not a six-digit hex colour", hex)
	}
	n, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("%q is not a six-digit hex colour", hex)
	}
	return uint8(n >> 16), uint8(n >> 8), uint8(n), nil
}

// Luminance is a colour's relative luminance (WCAG 2.2).
func Luminance(hex string) float64 {
	r, g, b, err := RGB(hex)
	if err != nil {
		return math.NaN()
	}
	lin := func(v uint8) float64 {
		c := float64(v) / 255
		if c <= 0.04045 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

// Contrast is the contrast ratio of two colours (WCAG 2.2), from 1 to 21.
func Contrast(a, b string) float64 {
	la, lb := Luminance(a), Luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}
