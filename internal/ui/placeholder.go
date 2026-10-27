package ui

import (
	"fmt"
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// The slot's colours. The slot's pane draws the keys and the shown
// session's conversation (slot.go) in the sidebar's palette, on its ground
// (slotColours, slotModel.paint). They start as rosewater's dark set, the
// one used before the terminal tells its background.
var (
	sun        color.Color // the accent: the message box while it has the keyboard, a call that runs
	sea        color.Color // what went well: a call's ✓, lines an edit added
	artSun     color.Color // the mark's sun
	artWaves   color.Color // the mark's waves
	slotGround color.Color // the pane's own background
	needsRed   color.Color // what needs you, and its card's border
	quietEdge  color.Color // a quiet card's border, a rule
	youEdge    color.Color // the border of your messages

	styleTitle, styleBold, styleDim, styleRule, styleRed, codeStyle lipgloss.Style
)

func init() { slotColours(themes[defaultTheme].dark) }

// slotColours sets the slot's colours from p: names in the accent's ink,
// text in ink, quiet words muted, rules and quiet borders in the edge
// colour, what needs you or failed in red, what went well in green.
func slotColours(p palette) {
	sun, sea, artSun, artWaves, slotGround = p.accent, p.done, p.sun, p.waves, p.ground
	needsRed, quietEdge, youEdge = p.needs, p.edge, p.accent
	styleTitle = lipgloss.NewStyle().Foreground(p.accentInk).Bold(true)
	styleBold = lipgloss.NewStyle().Foreground(p.ink).Bold(true)
	styleDim = lipgloss.NewStyle().Foreground(p.muted)
	styleRule = lipgloss.NewStyle().Foreground(p.edge)
	styleRed = lipgloss.NewStyle().Foreground(p.needs)
	codeStyle = lipgloss.NewStyle().Foreground(p.accentInk)
}

// grounded paints a drawn line on bg, padded to w columns: after each reset
// an inner style ends with, bg comes back, so no cell shows the terminal's
// own background. Text with no colour of its own is drawn in ink.
func grounded(line string, bg, ink color.Color, w int) string {
	r, g, b, _ := bg.RGBA()
	ir, ig, ib, _ := ink.RGBA()
	on := fmt.Sprintf("\x1b[48;2;%d;%d;%dm\x1b[38;2;%d;%d;%dm", r>>8, g>>8, b>>8, ir>>8, ig>>8, ib>>8)
	line = on + strings.NewReplacer("\x1b[m", "\x1b[m"+on, "\x1b[0m", "\x1b[0m"+on).Replace(line)
	if pad := w - lipgloss.Width(line); pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	return line + "\x1b[m"
}

// keyLabel turns a tmux key name such as C-] into Ctrl+].
func keyLabel(k string) string {
	for _, m := range [][2]string{{"C-", "Ctrl+"}, {"M-", "Alt+"}, {"S-", "Shift+"}} {
		k = strings.ReplaceAll(k, m[0], m[1])
	}
	return k
}

// placeholder is what the slot shows while no session is shown (Slot):
// the keys. It ignores input.
type placeholder struct {
	w, h     int
	focusKey string
	flipKey  string // "" when there is no flip key
}

func (placeholder) Init() tea.Cmd { return nil }

func (p placeholder) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(tea.WindowSizeMsg); ok {
		p.w, p.h = m.Width, m.Height
	}
	return p, nil
}

// mark is the icon's picture in text: a sun rising over two waves.
func mark() string {
	sunStyle, waves := lipgloss.NewStyle().Foreground(artSun), lipgloss.NewStyle().Foreground(artWaves)
	return lipgloss.JoinVertical(lipgloss.Center,
		sunStyle.Render(`\ | /`),
		sunStyle.Render(`— ( ☀ ) —`),
		waves.Render(strings.Repeat("～", 9)),
		waves.Render(strings.Repeat("～", 7)),
	)
}

// keycap draws a key as [Key].
func keycap(k string) string {
	return styleDim.Render("[") + styleBold.Render(k) + styleDim.Render("]")
}

// markMinHeight is the slot height below which only the keys are shown.
const markMinHeight = 20

func (p placeholder) View() tea.View {
	type key struct {
		caps []string
		what string
	}
	inSidebar := []key{
		{[]string{"Tab"}, "type a new task"},
		{[]string{"↑", "↓"}, "pick a session"},
		{[]string{"Enter"}, "show it here"},
		{[]string{"Space"}, "show it, stay in the sidebar"},
	}
	focus := key{[]string{p.focusKey}, "move between the sidebar and here"}
	flip := key{[]string{p.flipKey}, "flip between its conversation and its terminal"}
	width := 0
	for _, k := range append([]key{focus, flip}, inSidebar...) {
		w := 0
		for _, c := range k.caps {
			w += lipgloss.Width(c) + 2
		}
		width = max(width, w)
	}
	line := func(k key) string {
		var caps string
		for _, c := range k.caps {
			caps += keycap(c)
		}
		return caps + spaces(width-lipgloss.Width(caps)) + "  " + styleDim.Render(k.what)
	}
	keys := []string{line(focus)}
	if p.flipKey != "" {
		keys = append(keys, line(flip))
	}
	keys = append(keys, "", styleDim.Render("In the sidebar:"))
	for _, k := range inSidebar {
		keys = append(keys, line(k))
	}

	var parts []string
	if p.h >= markMinHeight {
		parts = append(parts, mark(), "")
	}
	parts = append(parts, styleTitle.Render("Irie"), "", lipgloss.JoinVertical(lipgloss.Left, keys...))
	v := tea.NewView(lipgloss.Place(p.w, p.h, lipgloss.Center, lipgloss.Center, lipgloss.JoinVertical(lipgloss.Center, parts...)))
	v.AltScreen = true
	return v
}
