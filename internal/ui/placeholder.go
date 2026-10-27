package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

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
	sunStyle := lipgloss.NewStyle().Foreground(sun)
	return lipgloss.JoinVertical(lipgloss.Center,
		sunStyle.Render(`\ | /`),
		sunStyle.Render(`— ( ☀ ) —`),
		lipgloss.NewStyle().Foreground(sea).Render(strings.Repeat("～", 9)),
		lipgloss.NewStyle().Foreground(deepSea).Render(strings.Repeat("～", 7)),
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
