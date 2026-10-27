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
// is never the only cue.
func (m Model) state(s ipc.Session) (glyph string, c color.Color, dim, loud bool) {
	switch s.State {
	case ipc.StateFailed:
		return "✕", lipgloss.Red, false, true
	case ipc.StateExited:
		if s.ExitCode != nil && *s.ExitCode != 0 {
			return "·", lipgloss.Red, true, false
		}
		return "·", nil, true, false
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
	return "●", nil, false, false
}

func spaces(n int) string { return strings.Repeat(" ", max(0, n)) }

func (m Model) header() string {
	title := " " + styleTitle.Render("☀ Irie")
	right := ""
	switch m.conn {
	case offline:
		right = styleRed.Render("core offline")
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
	return title + spaces(m.width-lipgloss.Width(title)-lipgloss.Width(right)-1) + right
}

// counts renders the state counts in at most w columns: with labels when
// they fit, else glyphs and numbers. A zero count is faint, so colour only
// ever means that something is there.
func (m Model) counts(w int) string {
	working, needs, done := 0, 0, 0
	for _, s := range m.sessions {
		switch s.State {
		case ipc.StateWorking:
			working++
		case ipc.StateNeedsYou, ipc.StateStalled:
			needs++
		case ipc.StateDone:
			done++
		}
	}
	// Paused and frozen come from gates and freezes, which this version does
	// not have yet.
	paused, frozen := 0, 0
	all := []struct {
		glyph, label string
		n            int
		c            color.Color
	}{
		{"●", "working", working, nil},
		{"●", "needs you", needs, lipgloss.Red},
		{"✓", "done", done, lipgloss.Green},
		{"‖", "paused", paused, lipgloss.Yellow},
		{"◆", "frozen", frozen, lipgloss.Cyan},
	}
	for _, long := range []bool{true, false} {
		var parts []string
		for _, x := range all {
			text := fmt.Sprintf("%s%d", x.glyph, x.n)
			if long {
				text += " " + x.label
			}
			st := lipgloss.NewStyle().Foreground(x.c)
			if x.n == 0 {
				st = styleDim
			}
			parts = append(parts, st.Render(text))
		}
		sep := " "
		if long {
			sep = "  "
		}
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
	a := age(m.now().Sub(s.Created))
	nameW := m.width - 5 - lipgloss.Width(a)
	name := cut(s.Name, nameW)
	gap := spaces(nameW - lipgloss.Width(name))
	if i == m.sel && m.focus == focusList {
		// The selection is a deep-green bar with a sun-coloured edge.
		bar := lipgloss.NewStyle().Background(deepGreen)
		text := bar.Foreground(onGreen)
		gc := c
		if gc == nil {
			gc = onGreen
		}
		return bar.Foreground(sun).Render("▌") + bar.Foreground(gc).Render(g) +
			text.Render(" "+name+gap+" ") + text.Faint(true).Render(a) + text.Render(" ")
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
	return edge + gs.Render(g) + " " + ns.Render(name) + gap + " " + styleDim.Render(a) + " "
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
	// What the selected row is doing or needs, else what Irie cannot do for
	// it, then what it costs, then its full name and directory.
	s := m.sessions[m.sel]
	var parts []string
	switch {
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
	if s.Cwd != "" {
		parts = append(parts, m.home2tilde(s.Cwd))
	}
	st := styleDim
	if _, c, _, loud := m.state(s); loud {
		st = lipgloss.NewStyle().Foreground(c)
	}
	return " " + st.Render(cut(strings.Join(parts, " · "), m.width-2))
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
		if r := m.offset + i; r < len(m.sessions) {
			lines = append(lines, m.row(r))
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
	lines = append(lines, bar...)
	if len(lines) > m.height {
		// Too short for everything: keep the header and the command bar.
		lines = append(lines[:max(1, m.height-len(bar))], bar...)[:m.height]
	}
	v.SetContent(strings.Join(lines, "\n"))
	return v
}
