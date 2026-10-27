package ui

import (
	"image/color"

	"charm.land/bubbles/v2/textarea"
	"charm.land/lipgloss/v2"
)

// palette is one set of Irie's colours, the same as the Mac app's (DESIGN
// §5.5, ui.theme). Every text colour reads at 4.5:1 and the accent at 3:1 on
// every surface (WCAG 2.2, 1.4.3 and 1.4.11), which TestPalettesMeetWCAG
// checks. The sidebar paints its own ground, so this holds whatever the
// terminal's theme is.
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

// themes are the sidebar's palettes, by their ui.theme names.
var themes = map[string]theme{
	"rosewater": {
		light: palette{
			sidebar: hex("#FBF1EE"), ground: hex("#F3E3DE"), card: hex("#FFF9F7"), panel: hex("#FCEFEB"), field: hex("#FFFCFB"),
			edge: hex("#EBD3CB"), hover: hex("#FFF3EF"), selection: hex("#F3DDEA"), needsTint: hex("#FEE4D8"),
			ink: hex("#3D2236"), muted: hex("#75566A"), accent: hex("#C4577A"), accentInk: hex("#A23E62"), accentFill: hex("#E58FA8"), onAccent: hex("#3D2236"),
			needs: hex("#B42A1E"), done: hex("#44694B"), paused: hex("#855700"), frozen: hex("#286873"), sun: hex("#F2B544"), waves: hex("#D9819C"),
		},
		dark: palette{
			sidebar: hex("#251B22"), ground: hex("#1D151B"), card: hex("#30242C"), panel: hex("#2C2129"), field: hex("#33262F"),
			edge: hex("#4A3843"), hover: hex("#392B34"), selection: hex("#45293F"), needsTint: hex("#4A251C"),
			ink: hex("#F7E9EF"), muted: hex("#C2AAB7"), accent: hex("#EE8FAD"), accentInk: hex("#F4A7BF"), accentFill: hex("#EE8FAD"), onAccent: hex("#2A1A24"),
			needs: hex("#FF8070"), done: hex("#9BC9A2"), paused: hex("#EFC468"), frozen: hex("#7DC4CE"), sun: hex("#F2B544"), waves: hex("#EE8FAD"),
		},
	},
	"terracotta": {
		light: palette{
			sidebar: hex("#F9F2EA"), ground: hex("#F0E4D6"), card: hex("#FFFBF6"), panel: hex("#FBF3EA"), field: hex("#FFFDF9"),
			edge: hex("#E7D6C3"), hover: hex("#FFF6EC"), selection: hex("#F8E0CF"), needsTint: hex("#FBE2E4"),
			ink: hex("#3A2629"), muted: hex("#735E58"), accent: hex("#C2603A"), accentInk: hex("#A24A26"), accentFill: hex("#E58A62"), onAccent: hex("#2B1A14"),
			needs: hex("#B3203F"), done: hex("#46694A"), paused: hex("#845600"), frozen: hex("#286773"), sun: hex("#F2B544"), waves: hex("#7FA383"),
		},
		dark: palette{
			sidebar: hex("#231B17"), ground: hex("#1B1411"), card: hex("#2E241F"), panel: hex("#2A211C"), field: hex("#312722"),
			edge: hex("#48392F"), hover: hex("#372B24"), selection: hex("#4A2F22"), needsTint: hex("#46202A"),
			ink: hex("#F6EBE2"), muted: hex("#BFAA9B"), accent: hex("#EE9469"), accentInk: hex("#F4A983"), accentFill: hex("#EE9469"), onAccent: hex("#2A1810"),
			needs: hex("#FF7F96"), done: hex("#9CC79C"), paused: hex("#EFC468"), frozen: hex("#7EC3CC"), sun: hex("#F2B544"), waves: hex("#9CC79C"),
		},
	},
	"lilac": {
		light: palette{
			sidebar: hex("#F7F2F7"), ground: hex("#EEE5EF"), card: hex("#FFFCFF"), panel: hex("#F9F3F9"), field: hex("#FFFDFF"),
			edge: hex("#E2D5E4"), hover: hex("#FBF5FB"), selection: hex("#EBDDF1"), needsTint: hex("#FBE4E0"),
			ink: hex("#33223F"), muted: hex("#6C5A75"), accent: hex("#9A5CB0"), accentInk: hex("#7F4495"), accentFill: hex("#C39AD3"), onAccent: hex("#2A1838"),
			needs: hex("#B02E1C"), done: hex("#426A4F"), paused: hex("#845700"), frozen: hex("#276776"), sun: hex("#F2B544"), waves: hex("#B58BC9"),
		},
		dark: palette{
			sidebar: hex("#1F1A26"), ground: hex("#18141E"), card: hex("#2A2332"), panel: hex("#26202E"), field: hex("#2D2636"),
			edge: hex("#433950"), hover: hex("#332B3C"), selection: hex("#3E2E4D"), needsTint: hex("#44221F"),
			ink: hex("#F2EAF6"), muted: hex("#B9A9C4"), accent: hex("#C79BE0"), accentInk: hex("#D2AEE8"), accentFill: hex("#C79BE0"), onAccent: hex("#241731"),
			needs: hex("#FF836F"), done: hex("#9CC9A6"), paused: hex("#EFC468"), frozen: hex("#80C3D0"), sun: hex("#F2B544"), waves: hex("#C79BE0"),
		},
	},
	"evergreen": {
		light: palette{
			sidebar: hex("#F4F4EC"), ground: hex("#E9EBDF"), card: hex("#FBFBF6"), panel: hex("#F1F2E8"), field: hex("#FDFDF9"),
			edge: hex("#D8DCC9"), hover: hex("#F6F7EF"), selection: hex("#DDE5D3"), needsTint: hex("#FBE5DC"),
			ink: hex("#1E2B22"), muted: hex("#56635A"), accent: hex("#2F5D46"), accentInk: hex("#264D3A"), accentFill: hex("#9DB8A3"), onAccent: hex("#1E2B22"),
			needs: hex("#B42A1E"), done: hex("#3F6A5F"), paused: hex("#855700"), frozen: hex("#286873"), sun: hex("#D9A93F"), waves: hex("#5E8A6E"),
		},
		dark: palette{
			sidebar: hex("#141C17"), ground: hex("#0E1410"), card: hex("#1C2620"), panel: hex("#18221C"), field: hex("#202B24"),
			edge: hex("#34443A"), hover: hex("#25322A"), selection: hex("#2A3D31"), needsTint: hex("#4A251C"),
			ink: hex("#ECF0E6"), muted: hex("#B3BFB5"), accent: hex("#9CC5A1"), accentInk: hex("#B3D6B8"), accentFill: hex("#9CC5A1"), onAccent: hex("#12201A"),
			needs: hex("#FF9585"), done: hex("#8FC3B2"), paused: hex("#EFC468"), frozen: hex("#7DC4CE"), sun: hex("#D9A93F"), waves: hex("#9CC5A1"),
		},
	},
}

// defaultTheme is ui.theme's default.
const defaultTheme = "rosewater"

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
