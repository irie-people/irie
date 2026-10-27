package ui

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/localmodel"
)

// Irie's colours are the deep green and warm sun of its icon (DESIGN §7.2).
// State colours (DESIGN §5.3) come from the terminal's own palette, so they
// follow its theme.
var (
	sun       = lipgloss.Color("#F2B544")
	deepGreen = lipgloss.Color("#1E5A42")
	onGreen   = lipgloss.Color("#EAF6EF")
	ruleGreen = lipgloss.Color("#3D5247")
	sea       = lipgloss.Color("#2E9E77")
	deepSea   = lipgloss.Color("#1D7357")

	styleTitle = lipgloss.NewStyle().Foreground(sun).Bold(true)
	styleBold  = lipgloss.NewStyle().Bold(true)
	styleDim   = lipgloss.NewStyle().Faint(true)
	styleRule  = lipgloss.NewStyle().Foreground(ruleGreen)
	styleRed   = lipgloss.NewStyle().Foreground(lipgloss.Red)
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

// state returns a row's glyph and colour (DESIGN §5.3). A nil colour is the
// terminal's own; a dim row is drawn faint. A loud row, one that needs you,
// takes the colour for its name too. Each state has its own glyph, so colour
// is never the only cue; an exit with an error code shares failed's ✕.
func (m Model) state(s ipc.Session) (glyph string, c color.Color, dim, loud bool) {
	switch s.State {
	case ipc.StateFailed:
		return "✕", lipgloss.Red, false, true
	case ipc.StateExited:
		if s.ExitCode != nil && *s.ExitCode != 0 {
			return "✕", lipgloss.Red, true, false
		}
		return "·", nil, true, false
	case ipc.StateFrozen:
		return "◆", lipgloss.Cyan, false, false
	case ipc.StateNeedsYou:
		return "●", lipgloss.Red, false, true
	case ipc.StateStalled:
		return "!", lipgloss.Red, false, true
	case ipc.StateStarting:
		return "◌", nil, true, false
	case ipc.StateDone:
		if m.redOnDone {
			return "✓", lipgloss.Red, false, true
		}
		return "✓", lipgloss.Green, false, false
	case ipc.StateIdle:
		return "○", nil, true, false
	}
	return "◐", nil, false, false
}

func spaces(n int) string { return strings.Repeat(" ", max(0, n)) }

// tagline is the catch phrase in the title bar.
const tagline = "you've got this 🌴"

// header is the title bar: a deep-green band with the name, the tagline when
// it fits, and the state counts on the right.
func (m Model) header() string {
	bar := lipgloss.NewStyle().Background(deepGreen)
	title := bar.Foreground(sun).Bold(true).Render(" ☀ Irie")
	right := ""
	switch m.conn {
	case offline:
		right = bar.Foreground(lipgloss.Red).Render("core offline")
	case online:
		// Low memory comes first, then the plan's use, then the counts,
		// each kept only while it fits.
		w := m.width - lipgloss.Width(title) - 2
		var extra []string
		if mem := m.head.Memory; mem != nil && mem.Low {
			extra = append(extra, styleRed.Render("mem low"))
		}
		if p := m.head.Plan; p != nil && p.FiveHour != nil {
			st := styleDim
			switch {
			case p.FiveHour.Used >= 90:
				st = styleRed
			case p.FiveHour.Used >= 70:
				st = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
			}
			extra = append(extra, st.Render(fmt.Sprintf("5h%.0f%%", p.FiveHour.Used)))
		}
		for len(extra) >= 0 {
			lead := strings.Join(extra, " ")
			room := w - lipgloss.Width(lead)
			if lead != "" {
				room--
			}
			if counts := m.counts(room); counts != "" || len(extra) == 0 {
				right = strings.TrimSpace(lead + " " + counts)
				break
			}
			extra = extra[:len(extra)-1]
		}
	}
	left := title
	if phrase := "   " + tagline; lipgloss.Width(title)+lipgloss.Width(phrase)+lipgloss.Width(right)+2 <= m.width {
		left += bar.Foreground(onGreen).Faint(true).Render(phrase)
	}
	gap := bar.Render(spaces(m.width - lipgloss.Width(left) - lipgloss.Width(right) - 1))
	return left + gap + right + bar.Render(" ")
}

// counts renders the state counts in at most w columns: with labels when
// they fit, else glyphs and numbers. A zero count is faint, so colour only
// ever means that something is there.
func (m Model) counts(w int) string {
	working, needs, done, frozen := 0, 0, 0, 0
	for _, s := range m.sessions {
		switch s.State {
		case ipc.StateWorking:
			working++
		case ipc.StateNeedsYou, ipc.StateStalled:
			needs++
		case ipc.StateDone:
			done++
		case ipc.StateFrozen:
			frozen++
		}
	}
	// Paused comes from gates, which this version does not have yet.
	paused := 0
	all := []struct {
		glyph, label string
		n            int
		c            color.Color
	}{
		{"◐", "working", working, nil},
		{"●", "needs you", needs, lipgloss.Red},
		{"✓", "done", done, lipgloss.Green},
		{"‖", "paused", paused, lipgloss.Yellow},
		{"◆", "frozen", frozen, lipgloss.Cyan},
	}
	for _, long := range []bool{true, false} {
		var parts []string
		for _, x := range all {
			// With close_when_done a finished session closes itself: no
			// Done count, and history has what closed.
			if x.label == "done" && m.head.CloseWhenDone {
				continue
			}
			text := fmt.Sprintf("%s%d", x.glyph, x.n)
			if long {
				text += " " + x.label
			}
			st := lipgloss.NewStyle().Foreground(x.c).Background(deepGreen)
			if x.n == 0 {
				st = styleDim.Background(deepGreen)
			}
			parts = append(parts, st.Render(text))
		}
		sep := " "
		if long {
			sep = "  "
		}
		sep = lipgloss.NewStyle().Background(deepGreen).Render(sep)
		if line := strings.Join(parts, sep); lipgloss.Width(line) <= w {
			return line
		}
	}
	return ""
}

func (m Model) rule() string {
	return " " + styleRule.Render(strings.Repeat("─", max(0, m.width-2))) + " "
}

func (m Model) row(i int) string {
	s := m.sessions[i]
	g, c, dim, loud := m.state(s)
	return m.paintBadged(i, g, c, dim, loud, s.Priority > 0, s.Name, age(m.now().Sub(s.Created)))
}

// paint draws list row i, `[edge][glyph] name … age`, with the glyph's
// colour and, for a row that is dim or loud, its name's.
func (m Model) paint(i int, g string, c color.Color, dim, loud bool, name, a string) string {
	return m.paintBadged(i, g, c, dim, loud, false, name, a)
}

// paintBadged is paint with, for a promoted session, a sun-coloured ↑
// between the glyph and the name.
func (m Model) paintBadged(i int, g string, c color.Color, dim, loud, promoted bool, name, a string) string {
	sep, badge := " ", lipgloss.NewStyle()
	if promoted {
		sep, badge = "↑", styleBadge
	}
	nameW := m.width - 5 - lipgloss.Width(a)
	name = cut(name, nameW)
	gap := spaces(nameW - lipgloss.Width(name))
	if i == m.sel && m.focus == focusList {
		// The selection is a deep-green bar with a sun-coloured edge.
		bar := lipgloss.NewStyle().Background(deepGreen)
		text := bar.Foreground(onGreen)
		gc := c
		if gc == nil {
			gc = onGreen
		}
		return bar.Foreground(sun).Render("▌") + bar.Foreground(gc).Render(g) + bar.Inherit(badge).Render(sep) +
			text.Render(name+gap+" ") + text.Faint(true).Render(a) + text.Render(" ")
	}
	gs, ns := lipgloss.NewStyle().Foreground(c), lipgloss.NewStyle()
	if loud {
		ns = ns.Foreground(c)
	}
	if dim {
		gs, ns = gs.Faint(true), ns.Faint(true)
	}
	edge := " "
	if i == m.sel {
		// Quieter while the command bar has focus.
		edge = styleTitle.Faint(true).Render("▌")
		ns = ns.Bold(true)
	}
	return edge + gs.Render(g) + badge.Render(sep) + ns.Render(name) + gap + " " + styleDim.Render(a) + " "
}

func (m Model) statusLine() string {
	if m.status != "" {
		st := styleDim
		if m.statusErr {
			st = styleRed
		}
		return " " + st.Render(cut(m.status, m.width-2))
	}
	if m.sel < 0 {
		hint := "Tab: type a task"
		if m.focus == focusBar {
			hint = "Enter: start  Ctrl+J: new line"
		}
		return " " + styleDim.Render(cut(hint, m.width-2))
	}
	if m.sel >= len(m.sessions) {
		return m.outsideStatus()
	}
	// What the selected row is doing or needs, in plain words, with the call
	// it is about; else what Irie cannot do for it; then what it costs; then
	// its full name on the left and its directory right-aligned.
	s := m.sessions[m.sel]
	var parts []string
	if l := frozenLine(s); l != "" {
		parts = append(parts, l)
	}
	switch {
	case s.Detail != "" && s.Command != "":
		parts = append(parts, s.Detail+" ("+s.Command+")")
	case s.Detail != "":
		parts = append(parts, s.Detail)
	case len(s.Warnings) > 0:
		parts = append(parts, s.Warnings[0])
	}
	if c := cost(s); c != "" {
		parts = append(parts, c)
	}
	if r := routed(s); r != "" {
		parts = append(parts, r)
	}
	parts = append(parts, s.Name)
	st := styleDim
	if _, c, _, loud := m.state(s); loud {
		st = lipgloss.NewStyle().Foreground(c)
	}
	w := m.width - 2
	left := strings.Join(parts, " · ")
	dir := ""
	if s.Cwd != "" {
		dir = m.home2tilde(s.Cwd)
	}
	// The directory is dropped when it would leave the left column under a
	// third of the line.
	if dir == "" || w-lipgloss.Width(dir)-2 < w/3 {
		return " " + st.Render(cut(left, w))
	}
	left = cut(left, w-lipgloss.Width(dir)-2)
	gap := spaces(w - lipgloss.Width(left) - lipgloss.Width(dir))
	return " " + st.Render(left) + gap + styleDim.Render(dir)
}

// cost is what a session costs: dollars at list price for a cloud model,
// "local" for a local one, then how full its context is and what its
// processes take in memory, such as "$1.23 ctx 41% 640 MB".
func cost(s ipc.Session) string {
	var parts []string
	switch {
	case s.Local:
		parts = append(parts, "local")
	case s.CostUSD != nil:
		parts = append(parts, fmt.Sprintf("$%.2f", *s.CostUSD))
	}
	if s.Context != nil {
		parts = append(parts, fmt.Sprintf("ctx %.0f%%", *s.Context))
	}
	if s.Memory > 0 {
		parts = append(parts, localmodel.Bytes(s.Memory))
	}
	return strings.Join(parts, " ")
}

// routed says how a session was routed and rated (DESIGN §6.4): "small→local",
// "↳ delegated", "rated good".
func routed(s ipc.Session) string {
	var parts []string
	if s.Routed && s.Class != "" {
		parts = append(parts, s.Class+"→"+s.Profile)
	}
	if s.Parent != "" {
		parts = append(parts, "↳ delegated")
	}
	if s.Rated != "" {
		parts = append(parts, "rated "+s.Rated)
	}
	return strings.Join(parts, " ")
}

func (m Model) View() tea.View {
	var v tea.View
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	if m.width <= 0 || m.height <= 0 {
		return v
	}
	lines := []string{m.header()}
	if m.ruled() {
		lines = append(lines, m.rule())
	}
	for i := range m.listHeight() {
		if r := m.offset + i; r < m.rowCount() {
			lines = append(lines, m.line(r))
		} else {
			lines = append(lines, "")
		}
	}
	if m.ruled() {
		lines = append(lines, m.rule())
	}
	if m.statusVisible() {
		lines = append(lines, m.statusLine())
	}
	bar := strings.Split(m.bar.View(), "\n")
	if h := m.barHeight(); len(bar) > h {
		bar = bar[:h]
	}
	barTop := len(lines)
	lines = append(lines, bar...)
	if len(lines) > m.height {
		// Too short for everything: keep the header and the command bar.
		barTop = max(1, m.height-len(bar))
		lines = append(lines[:barTop], bar...)[:m.height]
	}
	v.SetContent(strings.Join(lines, "\n"))
	// The terminal's cursor marks the place in the command bar while it has
	// the keyboard.
	if c := m.bar.Cursor(); c != nil && c.Y < len(bar) && barTop+c.Y < m.height {
		c.Y += barTop
		v.Cursor = c
	}
	return v
}
