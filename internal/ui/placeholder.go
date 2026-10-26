package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Placeholder is `irie placeholder`: what the slot shows while no session
// is shown. It lists the keys and ignores input.
func Placeholder(focusKey string) error {
	_, err := tea.NewProgram(placeholder{focusKey: keyLabel(focusKey)}).Run()
	return err
}

// keyLabel turns a tmux key name such as C-] into Ctrl+].
func keyLabel(k string) string {
	for _, m := range [][2]string{{"C-", "Ctrl+"}, {"M-", "Alt+"}, {"S-", "Shift+"}} {
		k = strings.ReplaceAll(k, m[0], m[1])
	}
	return k
}

type placeholder struct {
	w, h     int
	focusKey string
}

func (placeholder) Init() tea.Cmd { return nil }

func (p placeholder) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(tea.WindowSizeMsg); ok {
		p.w, p.h = m.Width, m.Height
	}
	return p, nil
}

func (p placeholder) View() tea.View {
	keys := [][2]string{
		{"Tab", "type a new task"},
		{"↑ ↓", "pick a session"},
		{"Enter", "show it here"},
		{"Space", "show it, stay in the sidebar"},
	}
	width := lipgloss.Width(p.focusKey)
	for _, k := range keys {
		width = max(width, lipgloss.Width(k[0]))
	}
	key := func(k, what string) string { return styleTitle.Render(pad(k, width)) + "  " + styleDim.Render(what) }
	lines := []string{styleTitle.Render("Irie"), "", key(p.focusKey, "move between the sidebar and here"), "", "In the sidebar:"}
	for _, k := range keys {
		lines = append(lines, key(k[0], k[1]))
	}
	v := tea.NewView(lipgloss.Place(p.w, p.h, lipgloss.Center, lipgloss.Center, lipgloss.JoinVertical(lipgloss.Left, lines...)))
	v.AltScreen = true
	return v
}
