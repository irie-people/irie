package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/irie-hq/irie/internal/ipc"
)

var (
	styleTitle    = lipgloss.NewStyle().Bold(true)
	styleDim      = lipgloss.NewStyle().Faint(true)
	styleRed      = lipgloss.NewStyle().Foreground(lipgloss.Red)
	styleSelected = lipgloss.NewStyle().Reverse(true)
	styleMarked   = lipgloss.NewStyle().Bold(true)
)

// cut shortens s to w columns, ending in an ellipsis when it had to cut.
func cut(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if used+rw > w-1 {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	return b.String() + "…"
}

func pad(s string, w int) string {
	return s + strings.Repeat(" ", max(0, w-lipgloss.Width(s)))
}

// age is a row's age: now, 42s, 3m, 2h, 5d.
func age(d time.Duration) string {
	switch {
	case d < 10*time.Second:
		return "now"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
}

func (m Model) home2tilde(p string) string {
	if m.home != "" && (p == m.home || strings.HasPrefix(p, m.home+"/")) {
		return "~" + p[len(m.home):]
	}
	return p
}

// glyph returns a row's state glyph and its style (DESIGN §5.3).
func glyph(s ipc.Session) (string, lipgloss.Style) {
	if s.State == ipc.StateExited {
		if s.ExitCode != nil && *s.ExitCode != 0 {
			return "·", styleRed
		}
		return "·", styleDim
	}
	return "●", lipgloss.NewStyle()
}

func (m Model) header() string {
	title := styleTitle.Render("Irie")
	switch m.conn {
	case connecting:
		return title
	case offline:
		return title + "  " + styleRed.Render(cut("core offline", m.width-6))
	}
	working := 0
	for _, s := range m.sessions {
		if s.State == ipc.StateWorking {
			working++
		}
	}
	// Needs you, done, paused and frozen come from hooks, gates and freezes,
	// which this version does not have yet.
	needs, done, paused, frozen := 0, 0, 0, 0
	long := []string{
		fmt.Sprintf("●%d working", working),
		styleRed.Render(fmt.Sprintf("●%d needs you", needs)),
		fmt.Sprintf("✓%d done", done),
		fmt.Sprintf("‖%d paused", paused),
		fmt.Sprintf("◆%d frozen", frozen),
	}
	short := []string{
		fmt.Sprintf("●%d", working),
		styleRed.Render(fmt.Sprintf("●%d", needs)),
		fmt.Sprintf("✓%d", done),
		fmt.Sprintf("‖%d", paused),
		fmt.Sprintf("◆%d", frozen),
	}
	for _, counts := range []string{strings.Join(long, "  "), strings.Join(short, " ")} {
		if line := title + "  " + counts; lipgloss.Width(line) <= m.width {
			return line
		}
	}
	return title
}

func (m Model) row(i int) string {
	s := m.sessions[i]
	g, gs := glyph(s)
	a := age(m.now().Sub(s.Created))
	nameW := m.width - 3 - lipgloss.Width(a) - 2
	name := cut(s.Name, nameW)
	gap := strings.Repeat(" ", max(0, nameW-lipgloss.Width(name)))
	switch {
	case i == m.sel && m.focus == focusList:
		return styleSelected.Render(" " + g + " " + name + gap + " " + a + " ")
	case i == m.sel:
		// The selection stays visible, quieter, while the bar has focus.
		return " " + gs.Render(g) + " " + styleMarked.Render(name) + gap + " " + styleDim.Render(a)
	}
	return " " + gs.Render(g) + " " + name + gap + " " + styleDim.Render(a)
}

func (m Model) statusLine() string {
	if m.status != "" {
		st := styleDim
		if m.statusErr {
			st = styleRed
		}
		return st.Render(cut(m.status, m.width))
	}
	if m.sel < 0 {
		hint := "Tab: type a task"
		if m.focus == focusBar {
			hint = "Enter: start  Ctrl+J: new line"
		}
		return styleDim.Render(cut(hint, m.width))
	}
	s := m.sessions[m.sel]
	text := s.Name
	if s.State == ipc.StateExited && s.ExitCode != nil {
		text = fmt.Sprintf("exited %d · %s", *s.ExitCode, s.Name)
	}
	if s.Cwd != "" {
		text += " · " + m.home2tilde(s.Cwd)
	}
	return styleDim.Render(cut(text, m.width))
}

func (m Model) View() tea.View {
	var v tea.View
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	if m.width <= 0 || m.height <= 0 {
		return v
	}
	lines := []string{m.header()}
	for i := range m.listHeight() {
		if r := m.offset + i; r < len(m.sessions) {
			lines = append(lines, m.row(r))
		} else {
			lines = append(lines, "")
		}
	}
	if m.statusVisible() {
		lines = append(lines, m.statusLine())
	}
	bar := strings.Split(m.bar.View(), "\n")
	if h := m.barHeight(); len(bar) > h {
		bar = bar[:h]
	}
	lines = append(lines, bar...)
	if len(lines) > m.height {
		// Too short for everything: keep the header and the command bar.
		lines = append(lines[:max(1, m.height-len(bar))], bar...)[:m.height]
	}
	v.SetContent(strings.Join(lines, "\n"))
	return v
}
