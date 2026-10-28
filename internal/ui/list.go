package ui

// The list (DESIGN §4.1): Irie's sessions in one list or a group per repo,
// then the Parked group and the Outside Irie group. Every group but the List
// view's one list has a header that folds it. The sessions keep the core's
// one order, which only the user changes, so a state change never moves
// one. The arrow keys walk the visible headers and sessions, and the
// selection and the scroll follow what they are on, not where it is, so what
// the core sends never moves the list under you.

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/irie-people/irie/internal/ipc"
)

// listView is how the list groups Irie's sessions.
type listView int

const (
	byRepo listView = iota // a group per repo, in the order they started
	byList                 // one list, in the core's order
)

type itemKind int

const (
	itemGroup   itemKind = iota // a group's header
	itemSession                 // one of Irie's sessions
	itemOutside                 // a session outside Irie
)

// group is a heading of the list: the List view's one list, a repo,
// Parked, or Outside Irie.
type group struct {
	key     string
	title   string
	count   int // the sessions in it
	needs   int // how many of them need you
	folded  bool
	outside bool
	bare    bool // drawn without a header: the List view's one list
}

// item is one entry of the list.
type item struct {
	kind  itemKind
	key   string // what the selection, the scroll and the hover follow
	group *group // the group it heads or belongs to
	s     int    // an index into m.sessions
	o     int    // an index into m.outside
}

// Keys. groupKey names the Outside Irie group, listKey the List view's one
// list and parkedKey the Parked group.
const (
	groupKey  = "\x00outside"
	listKey   = "\x00list"
	parkedKey = "\x00parked"
)

func sessionKey(id string) string { return "session:" + id }

func outsideItemKey(o ipc.OutsideSession) string { return "outside:" + o.Key() }

// notInARepo is the Repo view's group of sessions that run outside any repo.
const notInARepo = "repo:"

// homeKey is the group a session is in while it is not parked: the List
// view's one list, or in the Repo view its repo's group.
func (m Model) homeKey(s ipc.Session) string {
	switch {
	case m.view == byList:
		return listKey
	case s.Repo != "":
		return "repo:" + s.Repo
	}
	return notInARepo
}

// groupOf is the group a session is in: Parked while it is parked, else its
// home.
func (m Model) groupOf(s ipc.Session) string {
	if s.Parked {
		return parkedKey
	}
	return m.homeKey(s)
}

// repoNames names each repo by its folder, with as many parent folders as
// it takes to tell apart repos of the same name.
func repoNames(repos []string) map[string]string {
	names := map[string]string{}
	for depth := 1; ; depth++ {
		seen, clash := map[string]int{}, false
		for _, r := range repos {
			if _, done := names[r]; !done {
				seen[tail(r, depth)]++
			}
		}
		for _, r := range repos {
			if _, done := names[r]; done {
				continue
			}
			if n := tail(r, depth); seen[n] == 1 || n == filepath.Clean(r) {
				names[r] = n
			} else {
				clash = true
			}
		}
		if !clash {
			return names
		}
	}
}

// tail is the last n elements of path p.
func tail(p string, n int) string {
	parts := strings.Split(strings.Trim(filepath.ToSlash(filepath.Clean(p)), "/"), "/")
	if n >= len(parts) {
		return filepath.Clean(p)
	}
	return strings.Join(parts[len(parts)-n:], "/")
}

// groups returns the groups of Irie's sessions in the current view, in
// order, with the indexes of their sessions in the core's order (control.go):
// the List view's one list or a group per repo, then Parked. Only the user
// moves a session, by dragging, promoting or parking it, so a state change
// never does.
func (m Model) groups() ([]*group, [][]int) {
	var keys []string
	members := map[string][]int{}
	for i, s := range m.sessions {
		k := m.groupOf(s)
		if _, ok := members[k]; !ok && k != parkedKey {
			keys = append(keys, k)
		}
		members[k] = append(members[k], i)
	}
	titles := map[string]string{notInARepo: "Not in a repo", listKey: "Sessions", parkedKey: "Parked"}
	if m.view == byRepo {
		var repos []string
		for _, k := range keys {
			if k != notInARepo {
				repos = append(repos, strings.TrimPrefix(k, "repo:"))
			}
		}
		for r, name := range repoNames(repos) {
			titles["repo:"+r] = name
		}
		// Repos in the order their first session started, then the
		// sessions that run in none.
		first := map[string]ipc.Session{}
		for _, k := range keys {
			for _, i := range members[k] {
				if f, ok := first[k]; !ok || before(m.sessions[i], f) {
					first[k] = m.sessions[i]
				}
			}
		}
		slices.SortStableFunc(keys, func(a, b string) int {
			switch {
			case a == notInARepo || b == notInARepo:
				return b2i(a == notInARepo) - b2i(b == notInARepo)
			case before(first[a], first[b]):
				return -1
			case before(first[b], first[a]):
				return 1
			}
			return 0
		})
	}
	if len(members[parkedKey]) > 0 {
		keys = append(keys, parkedKey)
	}
	groups := make([]*group, len(keys))
	order := make([][]int, len(keys))
	for i, k := range keys {
		// The one list has no header, so it never folds.
		bare := k == listKey
		g := &group{key: k, title: titles[k], count: len(members[k]), folded: m.folded[k] && !bare, bare: bare}
		for _, s := range members[k] {
			if needsYou(m.sessions[s]) {
				g.needs++
			}
		}
		groups[i], order[i] = g, members[k]
	}
	return groups, order
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// items is the list as it stands: each group's header, but the one list's,
// and unless it is folded its sessions, then the Outside Irie group.
func (m Model) items() []item {
	var out []item
	groups, order := m.groups()
	for i, g := range groups {
		if !g.bare {
			out = append(out, item{kind: itemGroup, key: g.key, group: g})
		}
		if !g.folded {
			for _, s := range order[i] {
				out = append(out, item{kind: itemSession, key: sessionKey(m.sessions[s].ID), group: g, s: s})
			}
		}
	}
	if len(m.outside) > 0 {
		g := &group{key: groupKey, title: "Outside Irie", count: len(m.outside), folded: m.folded[groupKey], outside: true}
		for _, o := range m.outside {
			if o.Status == ipc.StatusWaiting {
				g.needs++
			}
		}
		out = append(out, item{kind: itemGroup, key: groupKey, group: g})
		if !g.folded {
			for j, o := range m.outside {
				out = append(out, item{kind: itemOutside, key: outsideItemKey(o), group: g, o: j})
			}
		}
	}
	return out
}

// rowCount is how many entries the list has.
func (m Model) rowCount() int { return len(m.items()) }

func keyIndex(items []item, key string) int {
	if key == "" {
		return -1
	}
	return slices.IndexFunc(items, func(it item) bool { return it.key == key })
}

// selected returns the selected entry.
func (m Model) selected() (item, bool) {
	items := m.items()
	if m.sel < 0 || m.sel >= len(items) {
		return item{}, false
	}
	return items[m.sel], true
}

// selSession returns the selected session, when one of Irie's is selected.
func (m Model) selSession() (ipc.Session, bool) {
	if it, ok := m.selected(); ok && it.kind == itemSession {
		return m.sessions[it.s], true
	}
	return ipc.Session{}, false
}

// selOutside returns the selected session outside Irie.
func (m Model) selOutside() (ipc.OutsideSession, bool) {
	if it, ok := m.selected(); ok && it.kind == itemOutside {
		return m.outside[it.o], true
	}
	return ipc.OutsideSession{}, false
}

// firstSession is the entry of the first session, or 0.
func firstSession(items []item) int {
	return max(0, slices.IndexFunc(items, func(it item) bool { return it.kind != itemGroup }))
}

// fold folds or unfolds a group. A selection inside it moves to its header.
func (m *Model) fold(key string) {
	folded := make(map[string]bool, len(m.folded)+1)
	for k, v := range m.folded {
		folded[k] = v
	}
	folded[key] = !folded[key]
	if !folded[key] {
		delete(folded, key)
	}
	sel := ""
	if it, ok := m.selected(); ok {
		sel = it.key
		if it.group != nil && it.group.key == key && folded[key] {
			sel = key
		}
	}
	m.folded = folded
	if i := keyIndex(m.items(), sel); i >= 0 {
		m.sel = i
	}
	m.clamp()
}

// unfoldFor unfolds the group a session is in, so it can be shown.
func (m *Model) unfoldFor(id string) {
	for _, it := range m.allItems() {
		if it.kind == itemSession && m.sessions[it.s].ID == id && m.folded[it.group.key] {
			m.fold(it.group.key)
			return
		}
	}
}

// allItems is items with every group unfolded.
func (m Model) allItems() []item {
	m.folded = nil
	return m.items()
}

// setView switches between the List and Repo views. The selected session
// stays selected, and so do Parked's and Outside Irie's headers, which both
// views have; another header's selection goes to the first session.
func (m *Model) setView(v listView) {
	if m.view == v {
		return
	}
	sel := ""
	if it, ok := m.selected(); ok && (it.kind != itemGroup || it.group.outside || it.key == parkedKey) {
		sel = it.key
	}
	m.view, m.hover = v, ""
	items := m.items()
	m.sel = keyIndex(items, sel)
	if m.sel < 0 && len(items) > 0 {
		m.sel = firstSession(items)
	}
	m.offset = 0
	m.reveal()
}

func (v listView) String() string {
	if v == byList {
		return "List"
	}
	return "Repo"
}

// Heights. The header and the command bar are pinned; the list takes the
// rest. From 10 rows the header gains its second line, what undo and redo
// would do, a tab bar sits under it and the command bar sits in a box;
// from 24 rows the status strip becomes the details card;
// below 6 the status strip is hidden and the bar is one line. Sessions are
// cards while the pane is at least 24 columns wide and the list has 8 rows,
// else one-line rows.

const (
	detailsHeight = 6  // the details card: its borders and 4 lines
	cardMinWidth  = 24 // narrower panes draw one-line rows
	cardMinList   = 8  // and so do lists shorter than this
	maxHelpers    = 3  // a card's helper lines before "and N more"
)

func (m Model) statusVisible() bool { return m.height >= 6 }

func (m Model) details() bool { return m.height >= 24 }

// tabbed reports whether the tab bar shows, and the bar has its box.
func (m Model) tabbed() bool { return m.height >= 10 }

// headerLines is 2 when the header shows the history's line (undo.go),
// else 1.
func (m Model) headerLines() int { return 1 + b2i(m.tabbed()) }

// tabsTop is the screen row of the tab bar, under the header.
func (m Model) tabsTop() int { return m.headerLines() }

// statusHeight is the status strip's lines, or the details card's.
func (m Model) statusHeight() int {
	switch {
	case m.details():
		return detailsHeight
	case m.statusVisible():
		return 1
	}
	return 0
}

// chrome is the number of rows that are neither the list nor the bar's
// text.
func (m Model) chrome() int {
	n := m.headerLines() + m.statusHeight()
	if m.tabbed() {
		n += 3 // the tab bar and the box around the command bar
	}
	return n
}

// compact reports whether sessions are one-line rows: the pane is narrow,
// or the list would be short even with a one-line command bar, so a growing
// bar never changes how the list is drawn.
func (m Model) compact() bool {
	return m.width < cardMinWidth || m.height-m.chrome()-1 < cardMinList
}

// listTop is the screen row of the list's first line.
func (m Model) listTop() int { return m.headerLines() + b2i(m.tabbed()) }

func (m Model) maxBar() int {
	if !m.statusVisible() {
		return 1
	}
	return max(1, min(maxBarLines, m.height-m.chrome()-1))
}

func (m Model) barHeight() int { return min(max(m.bar.Height(), 1), m.maxBar()) }

func (m Model) listHeight() int { return max(m.height-m.chrome()-m.barHeight(), 0) }

// barTop is the screen row of the command bar's first line of text.
func (m Model) barTop() int {
	top := m.listTop() + m.listHeight() + m.statusHeight()
	if m.tabbed() {
		top++
	}
	return top
}

// barWidth is the width of the bar's text, inside its box when it has one.
func (m Model) barWidth() int {
	if m.tabbed() {
		return max(m.width-4, 1)
	}
	return m.width
}

// lines is how many lines an entry takes: a card has its borders, its two
// lines and its helpers; anything else is one line.
func (m Model) lines(it item) int {
	if it.kind != itemSession || m.compact() {
		return 1
	}
	n := len(m.sessions[it.s].Subagents)
	if n > maxHelpers {
		n = maxHelpers + 1
	}
	return 4 + n
}

// span is how many lines entries a to b take.
func (m Model) span(items []item, a, b int) int {
	n := 0
	for _, it := range items[max(a, 0):min(b, len(items))] {
		n += m.lines(it)
	}
	return n
}

// maxOffset is the first entry from which the rest fills the list.
func (m Model) maxOffset(items []item) int {
	h, total := m.listHeight(), 0
	for i := len(items) - 1; i >= 0; i-- {
		if total += m.lines(items[i]); total > h {
			return min(i+1, len(items)-1)
		}
	}
	return 0
}

// visible is how many entries from the offset show whole.
func (m Model) visible(items []item) int {
	n, used := 0, 0
	for _, it := range items[min(m.offset, len(items)):] {
		if used += m.lines(it); used > m.listHeight() {
			break
		}
		n++
	}
	return n
}

func (m *Model) clamp() {
	items := m.items()
	if len(items) == 0 {
		m.sel, m.offset = -1, 0
		return
	}
	m.sel = min(max(m.sel, 0), len(items)-1)
	m.offset = min(max(m.offset, 0), m.maxOffset(items))
}

// reveal scrolls so the selected entry shows whole.
func (m *Model) reveal() {
	m.clamp()
	if m.sel < 0 {
		return
	}
	items := m.items()
	if m.sel < m.offset {
		m.offset = m.sel
	}
	for m.offset < m.sel && m.span(items, m.offset, m.sel+1) > m.listHeight() {
		m.offset++
	}
	m.clamp()
}

func (m *Model) moveTo(i int) {
	m.sel = i
	m.reveal()
}

func (m *Model) scroll(n int) {
	m.offset += n
	m.clamp()
}

// at returns the entry on screen row y, and the first row it takes.
func (m Model) at(y int) (int, int, bool) {
	row := y - m.listTop()
	if row < 0 || row >= m.listHeight() {
		return 0, 0, false
	}
	items := m.items()
	top := 0
	for i := m.offset; i < len(items); i++ {
		h := m.lines(items[i])
		if row < top+h {
			return i, m.listTop() + top, true
		}
		top += h
	}
	return 0, 0, false
}

// hoverAt is the key of what the pointer at x, y is over: an entry of the
// list, between the margins, or a tab other than the one in use.
func (m Model) hoverAt(x, y int) string {
	if m.tabbed() && y == m.tabsTop() {
		if v, ok := tabAt(x); ok && v != m.view {
			return tabKey(v)
		}
		return ""
	}
	if i, _, ok := m.at(y); ok && x >= 1 && x < m.width-1 {
		return m.items()[i].key
	}
	return ""
}

// sessionIndex is the entry of the session with id, or -1.
func (m Model) sessionIndex(id string) int { return keyIndex(m.items(), sessionKey(id)) }

// groupLabel says what a group holds, for the status strip and the details
// card: "4 sessions · 1 needs you".
func groupLabel(g *group) string {
	s := fmt.Sprintf("%d sessions", g.count)
	if g.count == 1 {
		s = "1 session"
	}
	if g.needs > 0 {
		s += fmt.Sprintf(" · %d needs you", g.needs)
	}
	return s
}
