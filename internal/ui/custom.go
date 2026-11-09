package ui

// A palette of the user's own. The Mac app's Make a Theme writes one to
// $XDG_CONFIG_HOME/irie/themes (mac/Sources/Irie/Looks.swift), and while
// the app draws in it, the app gives the core its dark set (the theme op),
// so the agents' colours on the status line match their cards.

import (
	"fmt"
	"image/color"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// Palette is the dark set of a palette of the user's own.
type Palette struct{ p palette }

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

// ParseDark reads the dark set of a palette of the user's own: each
// colour "#RRGGBB" by its role's name. A role it leaves out keeps
// rosewater's dark colour; a name that is no role, or a colour that is not
// #RRGGBB, is an error that names it.
func ParseDark(colors map[string]string) (Palette, error) {
	p := themes[defaultTheme].dark
	roles := p.roles()
	for _, k := range slices.Sorted(maps.Keys(colors)) {
		dst, ok := roles[k]
		if !ok {
			return Palette{}, fmt.Errorf("%s is no colour of a palette", k)
		}
		v := colors[k]
		if !hexColor.MatchString(v) {
			return Palette{}, fmt.Errorf("%s = %q is not #RRGGBB", k, v)
		}
		*dst = hex("#" + strings.TrimPrefix(v, "#"))
	}
	return Palette{p}, nil
}

// AgentColorIn is AgentColor in a palette of the user's own.
func AgentColorIn(p Palette, n int) string {
	if n < 1 {
		return ""
	}
	c := agentMark((n-1)%AgentColors, p.p)
	return fmt.Sprintf("#%02x%02x%02x", c.r, c.g, c.b)
}
