package ui

import (
	"fmt"
	"image/color"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
)

// Zen in the terminal sidebar (docs/proposals/zen.md §10, step 4), on ~: the
// card of the session on show alone, a soft line when another session
// would like the person, and what zen offers, one thing at a time, with y
// and n for its two answers. The core keeps the rules (internal/core/
// zen.go): the sidebar says it is in zen, says so again every
// ipc.ZenRenew, and draws what the header says.

// Zenner is the backend's zen, where it has one.
type Zenner interface {
	Zen(ipc.ZenArgs) (ipc.ZenResult, error)
}

func (b socketBackend) Zen(a ipc.ZenArgs) (ipc.ZenResult, error) {
	var r ipc.ZenResult
	err := ipc.Call(b.socket, callTimeout, ipc.OpZen, a, &r)
	return r, err
}

// zenMsg reports how a zen call went.
type zenMsg struct{ err error }

// zenGrace is how long after saying it is in zen the sidebar trusts itself
// over a header that doesn't say so yet.
const zenGrace = 5 * time.Second

func (m Model) zenner() (Zenner, bool) {
	z, ok := m.backend.(Zenner)
	return z, ok
}

// zenSession is the session zen shows: the one it said, while it is one
// of the sessions.
func (m Model) zenSession() (ipc.Session, bool) {
	if i := m.index(m.zenShows); i >= 0 {
		return m.sessions[i], true
	}
	return ipc.Session{}, false
}

// zenKey is ~: into zen with the selected session, or the one on show, or
// out of it.
func (m Model) zenKey() (tea.Model, tea.Cmd) {
	z, ok := m.zenner()
	if !ok {
		m.setStatus("zen needs a newer "+brand.Name+" core", true)
		return m, nil
	}
	if m.zen {
		m.zen, m.zenShows = false, ""
		m.setStatus("left zen", false)
		return m, m.zenCall(z, ipc.ZenArgs{On: ptr(false), Client: ipc.ZenSidebar})
	}
	s, ok := m.selSession()
	if !ok {
		i := slices.IndexFunc(m.sessions, func(s ipc.Session) bool { return s.Shown })
		if i < 0 {
			m.setStatus("~ is zen: select one of "+brand.Name+"'s sessions first", true)
			return m, nil
		}
		s = m.sessions[i]
	}
	m.zen = true
	return m.zenGo(z, s)
}

// zenGo shows session s in zen, in the slot too, and tells the core.
func (m Model) zenGo(z Zenner, s ipc.Session) (tea.Model, tea.Cmd) {
	m.zenShows = s.ID
	m.moveTo(m.shownAt(s.ID))
	m.zenSaid = m.now()
	return m, tea.Batch(m.showCmd(false), m.zenCall(z, ipc.ZenArgs{On: ptr(true), Client: ipc.ZenSidebar, Session: s.ID}))
}

func (m Model) zenCall(z Zenner, a ipc.ZenArgs) tea.Cmd {
	return func() tea.Msg {
		_, err := z.Zen(a)
		return zenMsg{err: err}
	}
}

func ptr[T any](v T) *T { return &v }

// zenKeys are the keys in zen. The list's others wait until zen ends,
// but for the command bar's.
func (m Model) zenKeys(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	z, ok := m.zenner()
	if !ok {
		m.zen = false
		return m, nil
	}
	switch msg.String() {
	case "~":
		return m.zenKey()
	case "y", "n":
		o := m.zenOffer()
		if o == nil {
			return m, nil
		}
		answer := ipc.ZenNo
		if msg.String() == "y" {
			answer = ipc.ZenYes
			if o.Show == ipc.ZenShowHistory {
				m.setStatus(brand.Slug+" ctl zen log lists your zen's versions", false)
			}
		}
		return m, m.zenCall(z, ipc.ZenArgs{Offer: o.ID, Answer: answer})
	case "up", "k", "down", "j":
		step := 1
		if msg.String() == "up" || msg.String() == "k" {
			step = -1
		}
		if s, ok := m.zenNext(step); ok {
			return m.zenGo(z, s)
		}
	case "o":
		if s, ok := m.zenOther(); ok {
			return m.zenGo(z, s)
		}
	case "enter":
		return m, m.showCmd(true)
	case "tab":
		m.setFocus(focusBar)
	case ":":
		m.setFocus(focusBar)
		m.bar.SetValue(":")
	}
	return m, nil
}

// zenNext is the session after the one zen shows, or before it (step -1),
// in the list's order, round from the end to the start; parked ones wait.
func (m Model) zenNext(step int) (ipc.Session, bool) {
	var order []ipc.Session
	for _, s := range m.sessions {
		if !s.Parked {
			order = append(order, s)
		}
	}
	if len(order) == 0 {
		return ipc.Session{}, false
	}
	i := slices.IndexFunc(order, func(s ipc.Session) bool { return s.ID == m.zenShows })
	if i < 0 {
		return order[0], true
	}
	return order[(i+step+len(order))%len(order)], true
}

// zenOthers are the sessions but the one zen shows that would like the
// person: they need them, as the Mac app's soft line counts.
func (m Model) zenOthers() []ipc.Session {
	var out []ipc.Session
	for _, s := range m.sessions {
		if s.ID != m.zenShows && needsYou(s) {
			out = append(out, s)
		}
	}
	return out
}

func (m Model) zenOther() (ipc.Session, bool) {
	if o := m.zenOthers(); len(o) > 0 {
		return o[0], true
	}
	return ipc.Session{}, false
}

func (m Model) zenOffer() *ipc.ZenOffer {
	if z := m.head.Zen; z != nil {
		return z.Offer
	}
	return nil
}

// zenFollow keeps the sidebar's zen with the core's, as each header
// comes: a sidebar that restarted comes back into zen, and one whose zen
// lapsed in the core leaves it. It trusts what it said itself a moment
// ago over a header that doesn't show it yet.
func (m *Model) zenFollow() {
	z := m.head.Zen
	if z == nil {
		return
	}
	held := slices.Contains(z.Clients, ipc.ZenSidebar)
	switch {
	case held && !m.zen:
		m.zen = true
		if i := slices.IndexFunc(m.sessions, func(s ipc.Session) bool { return s.Shown }); i >= 0 {
			m.zenShows = m.sessions[i].ID
		} else if s, ok := m.selSession(); ok {
			m.zenShows = s.ID
		}
		m.zenSaid = time.Time{} // say so at the next tick
	case !held && m.zen && m.now().Sub(m.zenSaid) > zenGrace:
		m.zen, m.zenShows = false, ""
	}
}

// zenRenew says again, every ipc.ZenRenew, that the sidebar is in zen.
func (m *Model) zenRenew() tea.Cmd {
	z, ok := m.zenner()
	if !m.zen || !ok || m.now().Sub(m.zenSaid) < ipc.ZenRenew {
		return nil
	}
	if _, ok := m.zenSession(); !ok {
		if s, ok := m.zenNext(1); ok {
			m.zenShows = s.ID
		}
	}
	m.zenSaid = m.now()
	return m.zenCall(z, ipc.ZenArgs{On: ptr(true), Client: ipc.ZenSidebar, Session: m.zenShows})
}

// zenHeader is the title bar in zen: the name alone, quiet, with no counts
// and no tagline, and ~ to leave.
func (m Model) zenHeader() string {
	p := m.pal
	title := blank(p.ground, 1) + on(p.ground, p.accentInk).Render("zen")
	right := ""
	if m.conn == offline {
		right = on(p.ground, p.needs).Render("core offline")
	} else if lipgloss.Width(title)+10 <= m.width {
		right = on(p.ground, p.muted).Render("~ leaves")
	}
	return title + blank(p.ground, m.width-lipgloss.Width(title)-lipgloss.Width(right)-1) + right + blank(p.ground, 1)
}

// zenList is what zen draws where the list goes, n lines: the session's
// card, then a soft line for the others that would like the person, then
// what zen offers. Where it doesn't fit, the breath goes first, then the
// soft line, then the card's second line; the answers always stay.
func (m Model) zenList(n int) []string {
	for _, f := range []zenFit{{breath: true, soft: true}, {soft: true}, {}, {row: true}} {
		if out := m.zenLines(f); len(out) <= n {
			for len(out) < n {
				out = append(out, "")
			}
			return out
		}
	}
	out := m.zenLines(zenFit{row: true})
	if o := m.zenOffer(); o != nil && n > 0 {
		answers := m.zenAnswers(*o)
		out = append(out[:n-1], answers)
	}
	return out[:n]
}

// zenFit is what zenLines draws in the room there is.
type zenFit struct {
	breath, soft bool // the breath, and the soft line
	row          bool // the session as a one-line row, not its card
}

func (m Model) zenLines(f zenFit) []string {
	p := m.pal
	var out []string
	if i := m.index(m.zenShows); i >= 0 {
		it := item{kind: itemSession, key: m.zenShows, s: i}
		if m.compact() || f.row {
			s := m.sessions[i]
			out = append(out, m.row(m.marks(s), m.look(s).name, s.Name, age(m.now().Sub(s.Created)), false, false))
		} else {
			out = append(out, m.card(it, 0, false, false)...)
		}
	} else {
		out = append(out, m.zenText("The session zen showed has gone; j or k shows another.", p.muted)...)
	}
	if others := m.zenOthers(); len(others) > 0 && f.soft {
		words := "Another session would like you"
		if len(others) > 1 {
			words = fmt.Sprintf("%d other sessions would like you", len(others))
		}
		out = append(out, "")
		out = append(out, m.zenText(words+"; o shows it, when you're ready", p.muted)...)
	}
	if o := m.zenOffer(); o != nil {
		out = append(out, "")
		out = append(out, m.zenText(o.Text, p.ink)...)
		if o.Link != "" {
			out = append(out, m.zenText(o.Link, p.accentInk)...)
		}
		if o.Kind == ipc.ZenKindRest && f.breath {
			out = append(out, "")
			out = append(out, m.zenBreath()...)
		}
		out = append(out, "", m.zenAnswers(*o))
	}
	return out
}

// zenText is words wrapped to the pane, a space in from its edges.
func (m Model) zenText(words string, fg color.Color) []string {
	w := max(m.width-2, 8)
	var out []string
	for _, l := range strings.Split(lipgloss.Wrap(words, w, " "), "\n") {
		out = append(out, blank(m.pal.sidebar, 1)+on(m.pal.sidebar, fg).Render(l))
	}
	return out
}

// zenAnswers is the offer's two answers, each after its key.
func (m Model) zenAnswers(o ipc.ZenOffer) string {
	p := m.pal
	key := func(k string) string { return on(p.sidebar, p.accentInk).Bold(true).Render(k) }
	word := func(s string) string { return on(p.sidebar, p.ink).Render(s) }
	line := blank(p.sidebar, 1) + key("y") + blank(p.sidebar, 1) + word(o.Yes)
	if o.No != "" {
		line += blank(p.sidebar, 3) + key("n") + blank(p.sidebar, 1) + word(o.No)
	}
	return line
}

// The breath zen's rest offers: in for 4 seconds, as the dot rises, and
// out for 6, as it falls, over breathRows rows.
const (
	breathIn   = 4 * time.Second
	breathOut  = 6 * time.Second
	breathRows = 5
)

// breathRow is the row the dot is on at t, 0 at the top.
func breathRow(t time.Time) int {
	cycle := breathIn + breathOut
	at := time.Duration(t.UnixNano()) % cycle
	top := breathRows - 1
	if at < breathIn {
		return top - int(float64(top)*float64(at)/float64(breathIn)+0.5)
	}
	return int(float64(top)*float64(at-breathIn)/float64(breathOut) + 0.5)
}

// zenBreath is the dot to follow, in the middle of the pane: it moves,
// and nothing else does.
func (m Model) zenBreath() []string {
	p := m.pal
	at := breathRow(m.now())
	pad := max(m.width/2-1, 1)
	out := make([]string, breathRows)
	for i := range out {
		mark, fg := "·", p.muted
		if i == at {
			mark, fg = "●", p.accentInk
		}
		out[i] = blank(p.sidebar, pad) + on(p.sidebar, fg).Render(mark)
	}
	return out
}

// zenView draws zen in the rows the sidebar's own parts take, so that the
// command bar and the pointer's rows stay where they are.
func (m Model) zenView(v tea.View) tea.View {
	lines := []string{m.ground(m.zenHeader())}
	for range m.listTop() - 1 {
		lines = append(lines, m.ground(""))
	}
	// The status strip's rows are zen's too, with what the sidebar has to
	// say on the last.
	room := m.listHeight() + m.statusHeight()
	say := m.status != "" && m.statusVisible()
	body := m.zenList(room - b2i(say))
	if say {
		fg := m.pal.muted
		if m.statusErr {
			fg = m.pal.needs
		}
		body = append(body, m.zenText(cut(m.status, max(m.width-2, 1)), fg)[0])
	}
	for _, l := range body {
		lines = append(lines, m.ground(l))
	}
	return m.withBar(v, lines)
}
