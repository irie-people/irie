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

// look is how a state is drawn (DESIGN §5.3): its glyph, the glyph's colour
// and the name's. Red is the palette's needs colour, green done, yellow
// paused and cyan frozen; default is ink and dim is muted, so a dim red or
// dim yellow row has a red or yellow glyph and a muted name. A loud row, one
// that needs you, takes the red for its name too. Each state has its own
// glyph, so colour is never the only cue: an exit with an error code shares
// failed's ✕, and pausing paused's ‖.
type look struct {
	glyph      string
	mark, name color.Color
	loud       bool
}

func (m Model) look(s ipc.Session) look {
	p := m.pal
	switch s.State {
	case ipc.StateFailed:
		return look{"✕", p.needs, p.needs, true}
	case ipc.StateExited:
		if s.ExitCode != nil && *s.ExitCode != 0 {
			return look{"✕", p.needs, p.muted, false}
		}
		return look{"·", p.muted, p.muted, false}
	case ipc.StateFrozen:
		return look{"◆", p.frozen, p.ink, false}
	case ipc.StatePaused:
		return look{"‖", p.paused, p.ink, false}
	case ipc.StateNeedsYou:
		return look{"●", p.needs, p.needs, true}
	case ipc.StateStalled:
		return look{"!", p.needs, p.needs, true}
	case ipc.StatePausing:
		return look{"‖", p.paused, p.muted, false}
	case ipc.StateStarting:
		return look{"◌", p.muted, p.muted, false}
	case ipc.StateDone:
		if m.redOnDone {
			return look{"✓", p.needs, p.needs, true}
		}
		return look{"✓", p.done, p.ink, false}
	case ipc.StateIdle:
		return look{"○", p.muted, p.muted, false}
	}
	return look{"◐", p.ink, p.ink, false}
}

// needsYou reports whether a session waits on you: it needs you, stalled or
// failed. These are red, and counted as needing you.
func needsYou(s ipc.Session) bool {
	switch s.State {
	case ipc.StateNeedsYou, ipc.StateStalled, ipc.StateFailed:
		return true
	}
	return false
}

func spaces(n int) string { return strings.Repeat(" ", max(0, n)) }

func (m Model) header() string {
	p := m.pal
	title := blank(p.sidebar, 1) + m.text(p.sun).Render("☀") + m.text(p.accentInk).Bold(true).Render(" Irie")
	right := ""
	switch m.conn {
	case offline:
		right = m.text(p.needs).Render("core offline")
	case online:
		// Low memory comes first, then the plan's use, then the counts,
		// each kept only while it fits.
		w := m.width - lipgloss.Width(title) - 2
		var extra []string
		if mem := m.head.Memory; mem != nil && mem.Low {
			extra = append(extra, m.text(p.needs).Render("mem low"))
		}
		if pl := m.head.Plan; pl != nil && pl.FiveHour != nil {
			c := p.muted
			switch {
			case pl.FiveHour.Used >= 90:
				c = p.needs
			case pl.FiveHour.Used >= 70:
				c = p.paused
			}
			extra = append(extra, m.text(c).Render(fmt.Sprintf("5h%.0f%%", pl.FiveHour.Used)))
		}
		for {
			lead := strings.Join(extra, blank(p.sidebar, 1))
			room := w - lipgloss.Width(lead)
			if lead != "" {
				room--
			}
			if counts := m.counts(room); counts != "" || len(extra) == 0 {
				right = lead
				if lead != "" && counts != "" {
					right += blank(p.sidebar, 1)
				}
				right += counts
				break
			}
			extra = extra[:len(extra)-1]
		}
	}
	return title + blank(p.sidebar, m.width-lipgloss.Width(title)-lipgloss.Width(right)-1) + right
}

// counts renders the state counts in at most w columns, in DESIGN §4.1's
// order: with labels when they fit, else glyphs and numbers. Needs you
// counts every red row: needs you, stalled and failed; paused counts pausing
// too. A zero count is faint, so colour only ever means that something is
// there, and each count keeps its glyph.
func (m Model) counts(w int) string {
	working, needs, done, paused, frozen := 0, 0, 0, 0, 0
	for _, s := range m.sessions {
		switch {
		case needsYou(s):
			needs++
		case s.State == ipc.StateWorking:
			working++
		case s.State == ipc.StateDone:
			done++
		case s.State == ipc.StatePaused, s.State == ipc.StatePausing:
			paused++
		case s.State == ipc.StateFrozen:
			frozen++
		}
	}
	p := m.pal
	all := []struct {
		glyph, label string
		n            int
		c            color.Color
	}{
		{"◐", "working", working, p.ink},
		{"●", "needs you", needs, p.needs},
		{"✓", "done", done, m.look(ipc.Session{State: ipc.StateDone}).mark},
		{"‖", "paused", paused, p.paused},
		{"◆", "frozen", frozen, p.frozen},
	}
	for _, long := range []bool{true, false} {
		var parts []string
		for _, x := range all {
			text := fmt.Sprintf("%s%d", x.glyph, x.n)
			if long {
				text += " " + x.label
			}
			c := x.c
			if x.n == 0 {
				c = p.muted
			}
			parts = append(parts, m.text(c).Render(text))
		}
		sep := blank(p.sidebar, 1)
		if long {
			sep = blank(p.sidebar, 2)
		}
		if line := strings.Join(parts, sep); lipgloss.Width(line) <= w {
			return line
		}
	}
	return ""
}

func (m Model) rule() string {
	return m.text(m.pal.edge).Render(" " + strings.Repeat("─", max(0, m.width-2)) + " ")
}

func (m Model) row(i int) string {
	s := m.sessions[i]
	return m.paint(i, m.look(s), s.Name, age(m.now().Sub(s.Created)))
}

// paint draws list row i, `[edge][glyph] name … age`, with the glyph's
// colour and the name's.
func (m Model) paint(i int, l look, name, a string) string {
	p := m.pal
	nameW := m.width - 5 - lipgloss.Width(a)
	name = cut(name, nameW)
	gap := spaces(nameW - lipgloss.Width(name))
	if i == m.sel && m.focus == focusList {
		// The selection is the selection fill with an accent edge.
		bg := p.selection
		return on(bg, p.accent).Render("▌") + on(bg, l.mark).Render(l.glyph) +
			on(bg, l.name).Render(" "+name+gap+" ") + on(bg, p.muted).Render(a) + blank(bg, 1)
	}
	edge, ns := blank(p.sidebar, 1), m.text(l.name)
	if i == m.sel {
		// Quieter while the command bar has focus.
		edge, ns = m.text(p.muted).Render("▌"), ns.Bold(true)
	}
	return edge + m.text(l.mark).Render(l.glyph) + ns.Render(" "+name+gap+" ") + m.text(p.muted).Render(a) + blank(p.sidebar, 1)
}

func (m Model) statusLine() string {
	p := m.pal
	if m.status != "" {
		c := p.muted
		if m.statusErr {
			c = p.needs
		}
		return blank(p.sidebar, 1) + m.text(c).Render(cut(m.status, m.width-2))
	}
	if m.sel < 0 {
		hint := "Tab: type a task"
		if m.focus == focusBar {
			hint = "Enter: start  Ctrl+J: new line"
		}
		return blank(p.sidebar, 1) + m.text(p.muted).Render(cut(hint, m.width-2))
	}
	if m.sel >= len(m.sessions) {
		return m.outsideStatus()
	}
	// What the selected row is doing or needs, in plain words, with the call
	// it is about; else what Irie cannot do for it; then what it costs; then
	// its full name and directory.
	s := m.sessions[m.sel]
	var parts []string
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
	if s.Cwd != "" {
		parts = append(parts, m.home2tilde(s.Cwd))
	}
	c := p.muted
	if l := m.look(s); l.loud {
		c = l.name
	}
	return blank(p.sidebar, 1) + m.text(c).Render(cut(strings.Join(parts, " · "), m.width-2))
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
	// Every line is painted on the sidebar's own ground, padded to the
	// pane's width, so the palette's contrast holds on any terminal theme.
	lines := []string{m.ground(m.header())}
	if m.ruled() {
		lines = append(lines, m.ground(m.rule()))
	}
	for i := range m.listHeight() {
		if r := m.offset + i; r < m.rowCount() {
			lines = append(lines, m.ground(m.line(r)))
		} else {
			lines = append(lines, m.ground(""))
		}
	}
	if m.ruled() {
		lines = append(lines, m.ground(m.rule()))
	}
	if m.statusVisible() {
		lines = append(lines, m.ground(m.statusLine()))
	}
	bar := strings.Split(m.bar.View(), "\n")
	if h := m.barHeight(); len(bar) > h {
		bar = bar[:h]
	}
	for i := range bar {
		bar[i] = m.ground(bar[i])
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
