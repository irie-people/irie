package ui

import (
	"image/color"
	"reflect"

	"charm.land/bubbles/v2/textarea"
	"charm.land/lipgloss/v2"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/roots"
)

// palette is one set of Irie's colours, the same as the Mac app's (DESIGN
// §5.5, ui.theme). Every text colour reads at 4.5:1 and the accent at 3:1 on
// every surface (WCAG 2.2, 1.4.3 and 1.4.11), which TestPalettesMeetWCAG
// checks. The sidebar paints its own ground, so this holds whatever the
// terminal's theme is. The state colours stay apart for colour-blind
// viewers too, from each other and from ink and muted, which
// TestStatesStayApartForColourBlindViewers checks: they differ in lightness
// as well as hue, as a deutan sees a red and an amber of one lightness as
// one olive (DESIGN §5.3).
type palette struct {
	// Surfaces: the pane's ground; card fills (quiet, hovered, selected, and
	// one that needs you); the details card and the command bar.
	sidebar, ground, card, panel, field, edge, hover, selection, needsTint color.Color
	// Text and marks: ink is the "default" colour role and muted the "dim"
	// one (DESIGN §5.3); accent marks the selection and the keyboard's place.
	ink, muted, accent, accentInk, accentFill, onAccent color.Color
	// State colours: needs is "red", done "green", paused "yellow" and frozen
	// "cyan". Sun is the sun of Irie's icon, and waves its sea.
	needs, done, paused, frozen, sun, waves color.Color
}

// theme is a palette's light and dark sets.
type theme struct{ light, dark palette }

func hex(s string) color.Color { return lipgloss.Color(s) }

// themes are the sidebar's palettes, by their ui.theme names: Irie's
// built-in palettes, which the design library keeps
// (github.com/irie-people/roots, palettes/).
var themes = builtIns()

// builtIns are the design library's palettes as the sidebar's.
func builtIns() map[string]theme {
	out := map[string]theme{}
	for _, p := range roots.Palettes() {
		out[p.ID] = theme{light: setOf(p.Light), dark: setOf(p.Dark)}
	}
	return out
}

// setOf is one of the library's sets as a palette.
func setOf(s roots.Set) palette {
	var p palette
	for k, dst := range p.roles() {
		*dst = hex(s[k])
	}
	return p
}

// defaultTheme is ui.theme's default.
var defaultTheme = roots.Default()

// setDark picks the theme's dark or light set. The sidebar asks the terminal
// for its background and uses the dark set until it answers.
func (m *Model) setDark(dark bool) {
	m.dark = dark
	m.pal = m.theme.light
	if dark {
		m.pal = m.theme.dark
	}
	m.bar.SetStyles(barStyles(m.pal))
}

// takeTheme draws the sidebar in the palette the Mac app draws in, once
// the header says which, so Irie looks the same wherever it shows; until
// then, and with no app, it keeps ui.theme.
func (m *Model) takeTheme(t *ipc.Theme) {
	if t == nil || reflect.DeepEqual(t, m.appTheme) {
		return
	}
	th, ok := themeOf(t)
	if !ok {
		return
	}
	m.appTheme = t
	m.theme = th
	m.setDark(m.dark)
}

// askBackground is how many ticks ask again for the terminal's background
// while it has not answered. tmux answers a pane's OSC 11 query itself, with
// the background of the first client attached when nothing inside tmux set
// one, and a client may attach after the sidebar starts. A new size, such as
// a client attaching, asks again.
const askBackground = 10

// on is a style that paints text in fg on bg, the background a cell must
// carry: an inner style's reset would otherwise drop the outer background.
func on(bg, fg color.Color) lipgloss.Style {
	return lipgloss.NewStyle().Background(bg).Foreground(fg)
}

// text is a style for text on the sidebar's ground.
func (m Model) text(fg color.Color) lipgloss.Style { return on(m.pal.sidebar, fg) }

// blank is n columns of bg.
func blank(bg color.Color, n int) string {
	if n <= 0 {
		return ""
	}
	return lipgloss.NewStyle().Background(bg).Render(spaces(n))
}

// barStyles draw the command bar on the field colour: the prompt in the
// accent while the bar has the keyboard, the placeholder muted, and the
// cursor in the terminal's own colour.
func barStyles(p palette) textarea.Styles {
	s := textarea.DefaultStyles(true)
	field := lipgloss.NewStyle().Background(p.field)
	ink := field.Foreground(p.ink)
	for _, st := range []*textarea.StyleState{&s.Focused, &s.Blurred} {
		st.Base = field
		st.Text = ink
		st.CursorLine = ink
		st.CursorLineNumber = ink
		st.LineNumber = field.Foreground(p.muted)
		st.EndOfBuffer = field.Foreground(p.muted)
		st.Placeholder = field.Foreground(p.muted)
		st.Prompt = field.Foreground(p.muted)
		st.Selection = ink.Background(p.selection)
	}
	s.Focused.Prompt = field.Foreground(p.accent)
	s.Cursor.Color = nil
	s.Cursor.Blink = false
	return s
}

// ground pads a drawn line to the pane's width on the sidebar's ground.
func (m Model) ground(line string) string {
	if pad := m.width - lipgloss.Width(line); pad > 0 {
		line += blank(m.pal.sidebar, pad)
	}
	return line
}
