package ui

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/localmodel"
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
		mark := p.done
		if m.redOnDone {
			mark = p.needs
		}
		switch {
		case s.Parked:
			// Out of the way, so quiet.
			return look{"✓", mark, p.muted, false}
		case m.redOnDone:
			return look{"✓", mark, p.needs, true}
		}
		return look{"✓", mark, p.ink, false}
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

// tagline is the catch phrase in the title bar.
const tagline = "you've got this 🌴"

// header is the title bar: a band of the palette's ground, a shade deeper
// than the sidebar's, with the name, the tagline when it fits, and the
// state counts on the right.
func (m Model) header() string {
	p := m.pal
	band := func(fg color.Color) lipgloss.Style { return on(p.ground, fg) }
	title := blank(p.ground, 1) + band(p.sun).Render("☀") + band(p.accentInk).Bold(true).Render(" Irie")
	right := ""
	switch m.conn {
	case offline:
		right = band(p.needs).Render("core offline")
	case online:
		// Low memory comes first, then the plan's use, then the counts,
		// each kept only while it fits.
		w := m.width - lipgloss.Width(title) - 2
		var extra []string
		if mem := m.head.Memory; mem != nil && mem.Low {
			extra = append(extra, band(p.needs).Render("mem low"))
		}
		if v := m.head.VPN; v != nil {
			extra = append(extra, vpnMark(v, band, p))
		}
		if pl := m.head.Plan; pl != nil && pl.FiveHour != nil {
			c := p.muted
			switch {
			case pl.FiveHour.Used >= 90:
				c = p.needs
			case pl.FiveHour.Used >= 70:
				c = p.paused
			}
			extra = append(extra, band(c).Render(fmt.Sprintf("5h%.0f%%", pl.FiveHour.Used)))
		}
		for {
			lead := strings.Join(extra, blank(p.ground, 1))
			room := w - lipgloss.Width(lead)
			if lead != "" {
				room--
			}
			if counts := m.counts(room); counts != "" || len(extra) == 0 {
				right = lead
				if lead != "" && counts != "" {
					right += blank(p.ground, 1)
				}
				right += counts
				break
			}
			extra = extra[:len(extra)-1]
		}
	}
	left := title
	if phrase := "   " + tagline; lipgloss.Width(title)+lipgloss.Width(phrase)+lipgloss.Width(right)+2 <= m.width {
		left += band(p.muted).Render(phrase)
	}
	return left + blank(p.ground, m.width-lipgloss.Width(left)-lipgloss.Width(right)-1) + right + blank(p.ground, 1)
}

// vpnMark is Mullvad VPN in the title bar, while vpn.on
// (docs/proposals/extensions.md §4.1): green while connected, yellow while
// it comes or goes, red in error, and faint when off, unless lockdown blocks
// the network, which is red too. `:vpn` says the rest.
func vpnMark(v *ipc.VPN, band func(color.Color) lipgloss.Style, p palette) string {
	switch v.State {
	case "connected":
		return band(p.done).Render("vpn")
	case "connecting", "disconnecting":
		return band(p.paused).Render("vpn…")
	case "error":
		return band(p.needs).Render("vpn!")
	case "disconnected":
		if v.Blocked {
			return band(p.needs).Render("vpn⊘")
		}
		return band(p.muted).Render("vpn off")
	}
	return band(p.muted).Render("vpn?")
}

// counts renders the state counts in at most w columns, in DESIGN §4.1's
// order: with labels when they fit, else glyphs and numbers. Needs you
// counts every red row: needs you, stalled and failed; to verify leaves out
// the parked ones, which are out of the way; paused counts pausing too. A
// zero count is faint, so colour only ever means that something is there,
// and each count keeps its glyph.
func (m Model) counts(w int) string {
	working, needs, done, paused, frozen := 0, 0, 0, 0, 0
	for _, s := range m.sessions {
		switch {
		case needsYou(s):
			needs++
		case s.State == ipc.StateWorking:
			working++
		case s.State == ipc.StateDone && !s.Parked:
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
		{"✓", "to verify", done, m.look(ipc.Session{State: ipc.StateDone}).mark},
		{"‖", "paused", paused, p.paused},
		{"◆", "frozen", frozen, p.frozen},
	}
	for _, long := range []bool{true, false} {
		var parts []string
		for _, x := range all {
			// With close_when_done a finished session closes itself: no
			// To verify count, and history has what closed.
			if x.label == "to verify" && m.head.CloseWhenDone {
				continue
			}
			text := fmt.Sprintf("%s%d", x.glyph, x.n)
			if long {
				text += " " + x.label
			}
			c := x.c
			if x.n == 0 {
				c = p.muted
			}
			parts = append(parts, on(p.ground, c).Render(text))
		}
		sep := blank(p.ground, 1)
		if long {
			sep = blank(p.ground, 2)
		}
		if line := strings.Join(parts, sep); lipgloss.Width(line) <= w {
			return line
		}
	}
	return ""
}

// statusLine is the one-line status strip, for panes under 24 rows.
func (m Model) statusLine() string {
	p := m.pal
	if mn := m.menu; mn != nil {
		// What the highlighted answer will do.
		return blank(p.sidebar, 1) + m.text(p.muted).Render(cut(mn.choice.Options[mn.at].Detail, m.width-2))
	}
	if m.status != "" {
		c := p.muted
		if m.statusErr {
			c = p.needs
		}
		return blank(p.sidebar, 1) + m.text(c).Render(cut(m.status, m.width-2))
	}
	it, ok := m.selected()
	if !ok {
		return blank(p.sidebar, 1) + m.text(p.muted).Render(cut(m.hint(), m.width-2))
	}
	switch it.kind {
	case itemOutside:
		return m.outsideStatus(m.outside[it.o])
	case itemGroup:
		g := it.group
		text := g.title + " · " + groupLabel(g)
		if g.outside {
			text = "Claude Code sessions in other terminals · m moves an idle one in"
		}
		return blank(p.sidebar, 1) + m.text(p.muted).Render(cut(text, m.width-2))
	}
	// What the selected row is doing or needs, in plain words, with the call
	// it is about; else what Irie cannot do for it; then what it costs; then
	// its full name on the left and its directory right-aligned.
	s := m.sessions[it.s]
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
	c := p.muted
	if l := m.look(s); l.loud {
		c = l.name
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
		return blank(p.sidebar, 1) + m.text(c).Render(cut(left, w))
	}
	left = cut(left, w-lipgloss.Width(dir)-2)
	return blank(p.sidebar, 1) + m.text(c).Render(left) + blank(p.sidebar, w-lipgloss.Width(left)-lipgloss.Width(dir)) + m.text(p.muted).Render(dir)
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
// "↳ delegated", "rated good"; and its tutor mode, "teach mode"
// (docs/proposals/learning.md).
func routed(s ipc.Session) string {
	var parts []string
	if s.Tutor != "" {
		parts = append(parts, s.Tutor+" mode")
	}
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
	// Every motion, so the card under the pointer lights up; focus, so the
	// light goes out when the pane loses it.
	v.MouseMode = tea.MouseModeAllMotion
	v.ReportFocus = true
	if m.width <= 0 || m.height <= 0 {
		return v
	}
	// Every line is painted on the sidebar's own ground, padded to the
	// pane's width, so the palette's contrast holds on any terminal theme.
	lines := []string{m.ground(m.header())}
	if m.headerLines() > 1 {
		lines = append(lines, m.ground(m.historyLine()))
	}
	if m.tabbed() {
		lines = append(lines, m.ground(m.tabBar()))
	}
	// The list: whole entries from the offset; the last may be cut short.
	// A question about an undo or redo covers the bottom of the list, and
	// above it only whole entries show.
	var menu []string
	room := m.listHeight()
	if m.menu != nil {
		menu = m.menuLines(room)
		room -= len(menu)
	}
	items, list := m.items(), make([]string, 0, m.listHeight())
	for i := m.offset; i < len(items) && len(list) < room; i++ {
		e := m.entry(items, i)
		if menu != nil && len(list)+len(e) > room {
			break
		}
		list = append(list, e...)
	}
	for len(list) < room {
		list = append(list, "")
	}
	for _, l := range append(list[:room], menu...) {
		lines = append(lines, m.ground(l))
	}
	switch {
	case m.details():
		for _, l := range m.detailsCard() {
			lines = append(lines, m.ground(l))
		}
	case m.statusVisible():
		lines = append(lines, m.ground(m.statusLine()))
	}
	bar := strings.Split(m.bar.View(), "\n")
	if h := m.barHeight(); len(bar) > h {
		bar = bar[:h]
	}
	barTop, barLeft := len(lines), 0
	if m.tabbed() {
		bar, barTop, barLeft = m.barBox(bar), barTop+1, 2
	}
	for i := range bar {
		bar[i] = m.ground(bar[i])
	}
	lines = append(lines, bar...)
	if len(lines) > m.height {
		// Too short for everything: keep the header and the command bar.
		top := max(1, m.height-len(bar))
		barTop += top - (len(lines) - len(bar))
		lines = append(lines[:top], bar...)[:m.height]
	}
	v.SetContent(strings.Join(lines, "\n"))
	// The terminal's cursor marks the place in the command bar while it has
	// the keyboard.
	if c := m.bar.Cursor(); c != nil && c.Y < m.barHeight() && barTop+c.Y < m.height {
		c.X += barLeft
		c.Y += barTop
		v.Cursor = c
	}
	return v
}
