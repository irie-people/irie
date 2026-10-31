package ui

// Drawing the list and the boxes around it (DESIGN §4.1): rounded cards
// with a column of padding, a header for each group, one-line rows, the
// List │ Repo tabs, the details card and the command bar's box. A box sits
// a column in from each side of the pane; its border's cells keep the
// sidebar's ground, so the corners look round, and its inside takes the
// fill. Every cell carries its background (theme.go).

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/irie-people/irie/internal/ipc"
)

var rounded = lipgloss.RoundedBorder()

// piece is text in one colour.
type piece struct {
	text string
	fg   color.Color
	bold bool
}

// spread draws pieces side by side on fill in exactly w columns: cut with
// an ellipsis where they run out of room, padded where they end early.
func spread(fill color.Color, w int, ps ...piece) string {
	var b strings.Builder
	used := 0
	for _, p := range ps {
		if p.text == "" {
			continue
		}
		room := w - used
		if room <= 0 {
			break
		}
		t := cut(p.text, room)
		st := on(fill, p.fg)
		if p.bold {
			st = st.Bold(true)
		}
		b.WriteString(st.Render(t))
		used += lipgloss.Width(t)
		if t != p.text {
			break
		}
	}
	b.WriteString(blank(fill, w-used))
	return b.String()
}

// boxTop is a box's top border in edge, with a title in it when there is
// one.
func (m Model) boxTop(edge color.Color, title string) string {
	p := m.pal
	inner := m.width - 4
	out := blank(p.sidebar, 1) + m.text(edge).Render(rounded.TopLeft)
	if title != "" && inner > 4 {
		t := cut(title, inner-4)
		out += m.text(edge).Render(rounded.Top) + blank(p.sidebar, 1) + m.text(p.ink).Bold(true).Render(t) + blank(p.sidebar, 1)
		inner -= lipgloss.Width(t) + 3
	}
	return out + m.text(edge).Render(strings.Repeat(rounded.Top, max(inner, 0))+rounded.TopRight) + blank(p.sidebar, 1)
}

// boxBottom is a box's bottom border.
func (m Model) boxBottom(edge color.Color) string {
	return blank(m.pal.sidebar, 1) + m.text(edge).Render(rounded.BottomLeft+strings.Repeat(rounded.Bottom, max(m.width-4, 0))+rounded.BottomRight) + blank(m.pal.sidebar, 1)
}

// boxLine is one line inside a box: inside is drawn m.width-4 columns wide.
func (m Model) boxLine(edge color.Color, inside string) string {
	p := m.pal
	return blank(p.sidebar, 1) + m.text(edge).Render(rounded.Left) + inside + m.text(edge).Render(rounded.Right) + blank(p.sidebar, 1)
}

// padded is a line of text inside a box, with a column of padding on each
// side.
func (m Model) padded(edge, fill color.Color, ps ...piece) string {
	return m.boxLine(edge, blank(fill, 1)+spread(fill, m.width-6, ps...)+blank(fill, 1))
}

// marks are what a session's line starts with: its state's glyph, then the
// flags of DESIGN §5.3 next to it (◆ when it is frozen under a state that
// comes first), then for a promoted session an ↑ in the accent, else a
// space before the name.
func (m Model) marks(s ipc.Session) []piece {
	p, l := m.pal, m.look(s)
	ps := []piece{{l.glyph, l.mark, false}}
	if s.Frozen != nil && s.State != ipc.StateFrozen {
		ps = append(ps, piece{"◆", p.frozen, false})
	}
	if s.Priority > 0 {
		return append(ps, piece{"↑", p.accent, true})
	}
	return append(ps, piece{" ", p.ink, false})
}

// columns is how many columns pieces take.
func columns(ps []piece) int {
	n := 0
	for _, p := range ps {
		n += lipgloss.Width(p.text)
	}
	return n
}

// stateLabel is a state in plain words.
func stateLabel(s ipc.Session) string {
	if waiting(s) {
		// On whom, its detail says.
		return "Waiting"
	}
	switch s.State {
	case ipc.StateFailed:
		return "Failed"
	case ipc.StateExited:
		if s.ExitCode != nil && *s.ExitCode != 0 {
			return fmt.Sprintf("Exited with code %d", *s.ExitCode)
		}
		return "Exited"
	case ipc.StateFrozen:
		return "Frozen"
	case ipc.StatePaused:
		return "Paused"
	case ipc.StateNeedsYou:
		return "Needs you"
	case ipc.StateStalled:
		return "Stalled"
	case ipc.StatePausing:
		return "Pausing"
	case ipc.StateStarting:
		return "Starting"
	case ipc.StateDone:
		// Parked is out of the way: checked, or put by for later.
		if s.Parked {
			return "Parked"
		}
		return "To verify"
	case ipc.StateIdle:
		return "Idle"
	}
	return "Working"
}

// card draws a session as a rounded card: its glyph, name and age, then
// what it is doing in plain words, then its helpers. One that needs you is
// tinted and edged in red; the selected one takes the selection fill, and
// the accent edge while the list has the keyboard; a hovered one the hover
// fill.
func (m Model) card(it item, selected, hovered bool) []string {
	p := m.pal
	s := m.sessions[it.s]
	l := m.look(s)
	needs := needsYou(s)
	fill, edge := p.card, p.edge
	switch {
	case selected:
		fill = p.selection
	case hovered:
		fill = p.hover
	case needs:
		fill = p.needsTint
	}
	switch {
	case selected && m.focus == focusList:
		edge = p.accent
	case needs:
		edge = p.needs
	}
	w := m.width - 6
	a := age(m.now().Sub(s.Created))
	marks := m.marks(s)
	nameW := max(w-columns(marks)-1-lipgloss.Width(a), 1)
	name := cut(s.Name, nameW)
	detail, dc := s.Detail, p.muted
	if detail == "" {
		detail = stateLabel(s)
	}
	if needs {
		dc = p.needs
	}
	first := append(marks, piece{name, l.name, selected}, piece{spaces(nameW - lipgloss.Width(name) + 1), p.ink, false}, piece{a, p.muted, false})
	lines := []string{
		m.boxTop(edge, ""),
		m.padded(edge, fill, first...),
		m.padded(edge, fill, piece{detail, dc, false}),
	}
	for i, sub := range s.Subagents {
		if i == maxHelpers {
			lines = append(lines, m.padded(edge, fill, piece{fmt.Sprintf("and %d more", len(s.Subagents)-maxHelpers), p.muted, false}))
			break
		}
		kind, c := sub.Type, p.muted
		if kind == "" {
			kind = "agent"
		}
		if sub.NeedsYou {
			c = p.needs
		}
		lines = append(lines, m.padded(edge, fill, piece{"↳ ", p.muted, false}, piece{kind, p.ink, false}, piece{"  " + printable(sub.Detail), c, false}))
	}
	return append(lines, m.boxBottom(edge))
}

// edgeMark is the first column of a selected one-line entry: the accent
// while the list has the keyboard, quieter while the command bar has it.
func (m Model) edgeMark(selected bool) string {
	switch {
	case selected && m.focus == focusList:
		return m.text(m.pal.accent).Render("▌")
	case selected:
		return m.text(m.pal.muted).Render("▌")
	}
	return blank(m.pal.sidebar, 1)
}

// rowFill is a one-line entry's fill: the selection while the list has the
// keyboard, the hover fill under the pointer.
func (m Model) rowFill(selected, hovered bool) color.Color {
	switch {
	case selected && m.focus == focusList:
		return m.pal.selection
	case hovered:
		return m.pal.hover
	}
	return m.pal.sidebar
}

// row draws a one-line entry, `[edge][marks]name … age`. Among cards it
// lines up with their glyphs and ages and keeps the margin.
func (m Model) row(marks []piece, nameFG color.Color, name, a string, selected, hovered bool) string {
	p := m.pal
	fill := m.rowFill(selected, hovered)
	lead, trail, w := 0, 1, m.width-1
	if !m.compact() {
		lead, trail, w = 2, 2, m.width-2
	}
	nameW := max(w-lead-trail-columns(marks)-1-lipgloss.Width(a), 1)
	name = cut(name, nameW)
	ps := append([]piece{{spaces(lead), p.ink, false}}, marks...)
	out := m.edgeMark(selected) + spread(fill, w, append(ps,
		piece{name, nameFG, selected && m.focus != focusList},
		piece{spaces(nameW - lipgloss.Width(name) + 1), p.ink, false}, piece{a, p.muted, false})...)
	if !m.compact() {
		out += blank(p.sidebar, 1)
	}
	return out
}

// groupRow draws a group's header: `▾ irie  4`, and folded `▸ irie  4 · 1
// needs you`, its count of what needs you in red. Parked's title is red
// while any parked session needs you, folded or not.
func (m Model) groupRow(g *group, selected, hovered bool) string {
	p := m.pal
	fill := m.rowFill(selected, hovered)
	arrow := "▾ "
	if g.folded {
		arrow = "▸ "
	}
	title := p.ink
	if g.key == parkedKey && g.needs > 0 {
		title = p.needs
	}
	ps := []piece{{arrow, p.muted, false}, {g.title, title, true}, {fmt.Sprintf("  %d", g.count), p.muted, false}}
	if g.folded && g.needs > 0 {
		ps = append(ps, piece{fmt.Sprintf(" · %d needs you", g.needs), p.needs, false})
	}
	if m.compact() {
		return m.edgeMark(selected) + spread(fill, m.width-1, ps...)
	}
	return m.edgeMark(selected) + spread(fill, m.width-2, append([]piece{{" ", p.ink, false}}, ps...)...) + blank(p.sidebar, 1)
}

// entry draws list entry i: its lines on screen.
func (m Model) entry(items []item, i int) []string {
	it := items[i]
	selected, hovered := i == m.sel, it.key == m.hover
	switch it.kind {
	case itemGroup:
		return []string{m.groupRow(it.group, selected, hovered)}
	case itemOutside:
		o := m.outside[it.o]
		a := ""
		if !o.Started.IsZero() {
			a = age(m.now().Sub(o.Started))
		}
		l := m.outsideLook(o)
		return []string{m.row([]piece{{l.glyph, l.mark, false}, {" ", m.pal.ink, false}}, l.name, outsideLabel(o), a, selected, hovered)}
	}
	if m.compact() {
		s := m.sessions[it.s]
		return []string{m.row(m.marks(s), m.look(s).name, s.Name, age(m.now().Sub(s.Created)), selected, hovered)}
	}
	return m.card(it, selected, hovered)
}

// tabs are the List │ Repo bar's tabs, by where they sit.
var tabs = []struct {
	view     listView
	from, to int // columns
}{{byList, 1, 7}, {byRepo, 8, 14}}

func tabKey(v listView) string { return "tab:" + v.String() }

// tabAt is the tab at column x.
func tabAt(x int) (listView, bool) {
	for _, t := range tabs {
		if x >= t.from && x < t.to {
			return t.view, true
		}
	}
	return 0, false
}

// tabBar draws `List │ Repo`, the view in use on the selection fill.
func (m Model) tabBar() string {
	p := m.pal
	out := blank(p.sidebar, 1)
	for i, t := range tabs {
		if i > 0 {
			out += m.text(p.edge).Render("│")
		}
		fill, fg, bold := p.sidebar, p.muted, false
		switch {
		case m.view == t.view:
			fill, fg, bold = p.selection, p.ink, true
		case m.hover == tabKey(t.view):
			fill = p.hover
		}
		out += spread(fill, t.to-t.from, piece{" " + t.view.String(), fg, bold})
	}
	return out
}

// detailsCard is the status strip as a card, for panes at least 24 rows
// tall: what the status strip says, over four lines, with the selection's
// name in its top border.
func (m Model) detailsCard() []string {
	p := m.pal
	title, rows := m.detailRows()
	out := []string{m.boxTop(p.edge, title)}
	for i := range detailsHeight - 2 {
		var ps []piece
		if i < len(rows) {
			ps = rows[i]
		}
		out = append(out, m.padded(p.edge, p.panel, ps...))
	}
	return append(out, m.boxBottom(p.edge))
}

// detailRows are the details card's title and lines.
func (m Model) detailRows() (string, [][]piece) {
	p := m.pal
	w := m.width - 6
	words := func(s string, c color.Color) [][]piece {
		var rows [][]piece
		for _, l := range strings.Split(lipgloss.Wrap(s, w, ""), "\n") {
			rows = append(rows, []piece{{l, c, false}})
		}
		if len(rows) > detailsHeight-2 {
			rows = rows[:detailsHeight-2]
			last := rows[len(rows)-1][0]
			rows[len(rows)-1][0].text = cut(last.text+" …", w)
		}
		return rows
	}
	if mn := m.menu; mn != nil {
		// What the highlighted answer to an undo's question will do.
		return "", words(mn.choice.Options[mn.at].Detail, p.muted)
	}
	if m.status != "" {
		c := p.muted
		if m.statusErr {
			c = p.needs
		}
		return "", words(m.status, c)
	}
	it, ok := m.selected()
	if !ok {
		return "", words(m.hint(), p.muted)
	}
	switch it.kind {
	case itemGroup:
		g := it.group
		what := "Enter folds it"
		if g.folded {
			what = "Enter unfolds it"
		}
		rows := [][]piece{{{groupLabel(g), p.muted, false}}}
		switch {
		case g.outside:
			rows = append(rows, []piece{{"Claude Code sessions in other terminals", p.muted, false}}, []piece{{"m moves an idle one in", p.muted, false}})
		case g.key == parkedKey:
			rows = append(rows, []piece{{"out of the way; they keep running", p.muted, false}}, []piece{{"z unparks one, Z folds or opens them", p.muted, false}})
		}
		return g.title, append(rows, []piece{{what, p.muted, false}})
	case itemOutside:
		o := m.outside[it.o]
		l := m.outsideLook(o)
		c := p.muted
		if l.loud {
			c = l.name
		}
		rows := [][]piece{{{l.glyph + " ", l.mark, false}, {outsideDoing(o), c, false}}}
		if o.Cwd != "" {
			rows = append(rows, []piece{{printable(m.home2tilde(o.Cwd)), p.muted, false}})
		}
		rows = append(rows, []piece{{where(o), p.muted, false}})
		if !o.Movable && o.Why != "" {
			rows = append(rows, []piece{{o.Why, p.muted, false}})
		}
		return outsideLabel(o), rows
	}
	s := m.sessions[it.s]
	l := m.look(s)
	// The marks without the badge, which the words say instead.
	marks := m.marks(s)
	state := append(marks[:len(marks)-1], piece{" " + stateLabel(s), l.name, false})
	if s.Priority > 0 {
		state = append(state, piece{" · ↑ promoted", p.ink, false})
	}
	if c := strings.TrimSpace(cost(s) + " " + routed(s)); c != "" {
		state = append(state, piece{" · " + c, p.muted, false})
	}
	detail, dc := s.Detail, p.muted
	if detail == "" && len(s.Warnings) > 0 {
		detail = s.Warnings[0]
	}
	// What freezing or promoting it means comes first, as in the strip.
	if fl := frozenLine(s); fl != "" {
		detail = strings.TrimSuffix(fl+" · "+detail, " · ")
	}
	if l.loud {
		dc = l.name
	}
	var where []string
	if s.Branch != "" {
		where = append(where, s.Branch)
	}
	if s.Cwd != "" {
		where = append(where, m.home2tilde(s.Cwd))
	}
	return s.Name, [][]piece{
		state,
		{{detail, dc, false}},
		{{s.Command, p.muted, false}},
		{{strings.Join(where, " · "), p.muted, false}},
	}
}

// hint is what the status strip says with nothing selected.
func (m Model) hint() string {
	if m.focus == focusBar {
		return "Enter: start  Ctrl+J: new line"
	}
	return "Tab: type a task"
}

// barBox puts the command bar's lines in a rounded box, edged in the accent
// while it has the keyboard.
func (m Model) barBox(bar []string) []string {
	edge := m.pal.edge
	if m.focus == focusBar {
		edge = m.pal.accent
	}
	out := []string{m.boxTop(edge, "")}
	for _, l := range bar {
		if pad := m.width - 4 - lipgloss.Width(l); pad > 0 {
			l += blank(m.pal.field, pad)
		}
		out = append(out, m.boxLine(edge, l))
	}
	return append(out, m.boxBottom(edge))
}
