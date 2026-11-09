package ui

// The palette the Mac app draws in, which may be one of the user's own.
// The Mac app's Make a Theme writes those to $XDG_CONFIG_HOME/irie/themes
// (mac/Sources/Irie/Looks.swift). The app tells the core its palette (the
// theme op), with both sets of colours for one of the user's own; the core
// works the agents' colours on the status line out in it, and sends it in
// the header (ipc.Header.Theme), so the sidebar, the slot and the phone app
// draw in it too.

import (
	"fmt"
	"image/color"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/irie-people/irie/internal/ipc"
)

// Palette is a palette by its colours: one of Irie's or one of the user's
// own, both its sets.
type Palette struct{ light, dark palette }

// roles are a palette's colours by their names in a theme file, the
// Mac app's ColorRole.all.
func (p *palette) roles() map[string]*color.Color {
	return map[string]*color.Color{
		"sidebar": &p.sidebar, "ground": &p.ground, "card": &p.card, "panel": &p.panel, "field": &p.field,
		"edge": &p.edge, "hover": &p.hover, "selection": &p.selection, "needs_tint": &p.needsTint,
		"ink": &p.ink, "muted": &p.muted, "accent": &p.accent, "accent_ink": &p.accentInk,
		"accent_fill": &p.accentFill, "on_accent": &p.onAccent,
		"needs": &p.needs, "done": &p.done, "paused": &p.paused, "frozen": &p.frozen, "sun": &p.sun, "waves": &p.waves,
	}
}

var hexColor = regexp.MustCompile(`^#?[0-9A-Fa-f]{6}$`)

// ParseTheme reads a palette of the user's own: each set's colours,
// "#RRGGBB" by role. A role a set leaves out keeps rosewater's colour in
// that set; a name that is no role, or a colour that is not #RRGGBB, is an
// error that names it.
func ParseTheme(light, dark map[string]string) (Palette, error) {
	l, err := parseSet(themes[defaultTheme].light, light)
	if err != nil {
		return Palette{}, fmt.Errorf("light: %w", err)
	}
	d, err := parseSet(themes[defaultTheme].dark, dark)
	if err != nil {
		return Palette{}, fmt.Errorf("dark: %w", err)
	}
	return Palette{l, d}, nil
}

func parseSet(p palette, colors map[string]string) (palette, error) {
	roles := p.roles()
	for _, k := range slices.Sorted(maps.Keys(colors)) {
		dst, ok := roles[k]
		if !ok {
			return palette{}, fmt.Errorf("%s is no colour of a palette", k)
		}
		v := colors[k]
		if !hexColor.MatchString(v) {
			return palette{}, fmt.Errorf("%s = %q is not #RRGGBB", k, v)
		}
		*dst = hex("#" + strings.TrimPrefix(v, "#"))
	}
	return p, nil
}

// BuiltIn is one of Irie's palettes, by its ui.theme name.
func BuiltIn(name string) (Palette, bool) {
	t, ok := themes[name]
	return Palette{t.light, t.dark}, ok
}

// Colors are a palette's sets, each colour "#RRGGBB" by its role.
func (p Palette) Colors() (light, dark map[string]string) {
	return roleHexes(p.light), roleHexes(p.dark)
}

func roleHexes(p palette) map[string]string {
	out := map[string]string{}
	for k, c := range p.roles() {
		r, g, b, _ := (*c).RGBA()
		out[k] = fmt.Sprintf("#%02X%02X%02X", r>>8, g>>8, b>>8)
	}
	return out
}

// AgentColorIn is AgentColor in a palette given by its colours.
func AgentColorIn(p Palette, n int) string {
	if n < 1 {
		return ""
	}
	c := agentMark((n-1)%AgentColors, p.dark)
	return fmt.Sprintf("#%02x%02x%02x", c.r, c.g, c.b)
}

// themeOf is the palette a header says the Mac app draws in, as the
// sidebar and the slot draw it: by its colours, or by its name for a core
// that sent none.
func themeOf(t *ipc.Theme) (theme, bool) {
	if t == nil {
		return theme{}, false
	}
	if len(t.Light) > 0 || len(t.Dark) > 0 {
		if p, err := ParseTheme(t.Light, t.Dark); err == nil {
			return theme{p.light, p.dark}, true
		}
	}
	th, ok := themes[t.Name]
	return th, ok
}
