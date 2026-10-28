// Package ui is the sidebar (DESIGN §4.1): a header pinned at the top, the
// List │ Repo tabs, the session list in groups of cards, a status strip or
// details card, and a command bar pinned at the bottom. It is a pure client
// of the core's socket and never calls tmux.
package ui

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/route"
)

// Backend is what the sidebar asks of the core.
type Backend interface {
	Spawn(ipc.SpawnArgs) (ipc.Session, error)
	Show(id string, focus bool) error
	// Rename gives a session a new name and returns it with the name the
	// core gave it.
	Rename(id, name string) (ipc.Session, error)
	// Complete returns the tokens that complete an @ or + token, and for a
	// + token what each profile runs and costs.
	Complete(text string) (ipc.CompleteResult, error)
	// Move carries a session that runs outside Irie on in Irie.
	Move(vendorID, profile string) (ipc.Session, error)
	// Rate rates a session's work good or bad, which teaches the router.
	Rate(id, verdict string) error
	// Tool starts a tool session: local setup, or the local models.
	Tool(name string) (ipc.Session, error)
	// Route says where a task would run, and why.
	Route(task string) (route.Choice, error)
	// Close closes a session; Reopen brings back a closed one, the one
	// closed last when id is empty.
	Close(id string) error
	Reopen(id string) (ipc.Session, error)
	// Undo and Redo step back and forth through the history; a choice
	// answers a question the core asked.
	Undo(ipc.UndoArgs) (ipc.UndoResult, error)
	Redo(ipc.UndoArgs) (ipc.UndoResult, error)
}

// FrameMsg carries one frame of the core's subscription.
type FrameMsg struct{ Frame ipc.Frame }

// OfflineMsg reports that the core cannot be reached.
type OfflineMsg struct{}

type (
	spawnedMsg struct {
		session ipc.Session
		err     error
	}
	shownMsg     struct{ err error }
	tickMsg      struct{}
	completedMsg struct {
		token string // what was completed
		items []string
		notes []string // what each item runs and costs, for profiles
		err   error
	}
	ratedMsg struct {
		name, verdict string
		err           error
	}
	renamedMsg struct {
		before  string
		session ipc.Session
		err     error
	}
	toolMsg struct {
		session ipc.Session
		err     error
	}
	routedMsg struct {
		choice route.Choice
		err    error
	}
)

// commands are what the command bar takes after a colon (DESIGN §4.2,
// §4.6, §6.4).
const commands = ":rename NAME, :move [NAME], :close, :reopen, :undo, :redo, :setup, :models, :rate good|bad, :route TASK, :freeze [all], :thaw [all], :prio [REASON], :park, :unpark"

type focusArea int

const (
	focusList focusArea = iota
	focusBar
)

type connState int

const (
	connecting connState = iota
	online
	offline
)

const (
	barPrompt   = " › "
	maxBarLines = 8
	// The textarea keeps at most this many lines and silently drops the
	// rest of a longer paste, so such a paste is refused instead.
	maxBarContent = 10000
)

// Options are the model's outside dependencies and settings.
type Options struct {
	Now       func() time.Time
	Home      string // for ~ paths
	RedOnDone bool   // ui.red_on_done: done rows are red, not green
	Theme     string // ui.theme: the palette; "" is rosewater
}

// Model is the sidebar's Bubble Tea model.
type Model struct {
	backend   Backend
	now       func() time.Time
	home      string
	redOnDone bool

	theme theme   // ui.theme's light and dark sets
	pal   palette // the one in use
	dark  bool    // the terminal's background is dark, or has not been told
	known bool    // the terminal has told its background
	asked int     // ticks that asked for it since the last size change

	width, height int
	conn          connState
	gotSnapshot   bool
	head          ipc.Header // the plan's use and the machine's memory

	sessions []ipc.Session
	view     listView        // how the list groups them (list.go)
	folded   map[string]bool // folded groups, by key; never changed in place
	sel      int             // the selected entry of items(), -1 when the list is empty
	offset   int             // the first entry on screen
	hover    string          // the key of the entry or tab under the pointer
	pending  string
	drag     *drag // what the mouse holds (control.go)

	outside        []ipc.OutsideSession // the Outside Irie group, after the sessions (outside.go)
	outsideProblem string
	moving         bool
	confirming     string    // the outside session a first m asked to move
	confirmedAt    time.Time // when it asked
	closing        string    // the session a first x asked to close (close.go)
	closingAt      time.Time // when it asked
	reopening      bool

	undoLabel, redoLabel string      // what u and U would do next, from the history frame (undo.go)
	menu                 *choiceMenu // a question about an undo or redo, while it waits for an answer

	focus     focusArea
	bar       textarea.Model
	chips     []chip // pastes over chipMin, shown in the bar by their labels
	spawning  bool
	status    string
	statusErr bool
}

// New returns a sidebar that talks to b.
func New(b Backend, o Options) Model {
	if o.Now == nil {
		o.Now = time.Now
	}
	bar := textarea.New()
	bar.ShowLineNumbers = false
	bar.CharLimit = 0 // a task has no length cap
	bar.MaxWidth = 0
	bar.MaxHeight = maxBarLines
	// MaxHeight alone would also stop input at that many lines.
	bar.MaxContentHeight = math.MaxInt32
	bar.DynamicHeight = true
	bar.MinHeight = 1
	bar.Placeholder = "What should we work on?"
	bar.SetPromptFunc(lipgloss.Width(barPrompt), func(info textarea.PromptInfo) string {
		if info.LineNumber == 0 {
			return barPrompt
		}
		return strings.Repeat(" ", lipgloss.Width(barPrompt))
	})
	bar.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "ctrl+j"))
	// The terminal's own cursor, not a drawn one, so a screen magnifier or
	// reader can follow the typing (View places it).
	bar.SetVirtualCursor(false)

	th, ok := themes[o.Theme]
	if !ok {
		th = themes[defaultTheme]
	}
	// Parked starts folded, so a session parked goes out of sight.
	folded := map[string]bool{parkedKey: true}
	m := Model{backend: b, now: o.Now, home: o.Home, redOnDone: o.RedOnDone, theme: th, folded: folded, sel: -1, focus: focusBar, bar: bar}
	m.setDark(true)
	m.bar.Focus()
	return m
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m Model) Init() tea.Cmd { return tea.Batch(tick(), tea.RequestBackgroundColor) }

func (m *Model) setFocus(f focusArea) {
	m.focus = f
	if f == focusBar {
		m.bar.Focus()
	} else {
		m.bar.Blur()
	}
}

func (m *Model) setStatus(s string, isErr bool) { m.status, m.statusErr = s, isErr }

func (m Model) index(id string) int {
	return slices.IndexFunc(m.sessions, func(s ipc.Session) bool { return s.ID == id })
}

func before(a, b ipc.Session) bool {
	if !a.Created.Equal(b.Created) {
		return a.Created.Before(b.Created)
	}
	return a.ID < b.ID
}

func (m *Model) apply(f ipc.Frame) {
	// The selection stays on what it is on. Entries that come and go above
	// the screen leave it as it is: the first entry on screen stays first,
	// unless it moved to another group.
	selected, top, topGroup := "", "", ""
	if items := m.items(); len(items) > 0 {
		if m.sel >= 0 && m.sel < len(items) {
			selected = items[m.sel].key
		}
		if m.offset < len(items) {
			top, topGroup = items[m.offset].key, items[m.offset].group.key
		}
	}
	switch f.T {
	case ipc.FrameOutside:
		m.setOutside(f)
	case ipc.FrameHeader:
		if f.Header != nil {
			m.head = *f.Header
		}
		return
	case ipc.FrameHistory:
		m.undoLabel, m.redoLabel = f.Undo, f.Redo
		return
	case ipc.FrameSnapshot:
		m.sessions = slices.Clone(f.Sessions)
		if f.Header != nil {
			m.head = *f.Header
		}
		if !m.gotSnapshot && len(m.sessions) > 0 && m.bar.Value() == "" {
			m.setFocus(focusList)
		}
		m.gotSnapshot = true
	case ipc.FrameUpsert:
		if f.Session == nil {
			return
		}
		if i := m.index(f.Session.ID); i >= 0 {
			m.sessions[i] = *f.Session
		} else {
			i := slices.IndexFunc(m.sessions, func(s ipc.Session) bool { return before(*f.Session, s) })
			if i < 0 {
				i = len(m.sessions)
			}
			m.sessions = slices.Insert(m.sessions, i, *f.Session)
		}
	case ipc.FrameRemove:
		i := m.index(f.ID)
		if i < 0 {
			return
		}
		m.sessions = slices.Delete(m.sessions, i, i+1)
	}
	// Rows keep the core's one order (control.go).
	m.sortByOrder()
	items := m.items()
	// The selection follows its entry; when that went, it moves to the next.
	if i := keyIndex(items, selected); i >= 0 {
		m.sel = i
	}
	// Only the row of a task started here scrolls into view. Nothing else
	// the core sends moves the list, so it holds still while you read it.
	reveal := false
	if m.pending != "" && m.index(m.pending) >= 0 {
		m.unfoldFor(m.pending)
		items = m.items()
		m.sel, m.pending, reveal = m.sessionIndex(m.pending), "", true
	}
	if m.sel < 0 && len(items) > 0 {
		m.sel = firstSession(items)
	}
	if i := keyIndex(items, top); i >= 0 && items[i].group.key == topGroup {
		m.offset = i
	}
	if reveal {
		m.reveal()
	} else {
		m.clamp()
	}
}

func (m Model) showCmd(focus bool) tea.Cmd {
	s, ok := m.selSession()
	if !ok {
		return nil
	}
	id, b := s.ID, m.backend
	return func() tea.Msg { return shownMsg{err: b.Show(id, focus)} }
}

// pick selects the session with id, once its row is in, unfolding its
// group, and scrolls it into view; else it waits for it.
func (m *Model) pick(id string) {
	m.pending = id
	if m.index(id) >= 0 {
		m.unfoldFor(id)
		m.moveTo(m.sessionIndex(id))
		m.pending = ""
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.bar.MaxHeight = m.maxBar()
		m.bar.SetWidth(m.barWidth())
		m.reveal()
		// A new size may be a client of another terminal attaching.
		m.asked = 0
		return m, tea.RequestBackgroundColor
	case tea.BackgroundColorMsg:
		m.known = true
		m.setDark(msg.IsDark())
	case tickMsg:
		if !m.known && m.asked < askBackground {
			m.asked++
			return m, tea.Batch(tick(), tea.RequestBackgroundColor)
		}
		return m, tick()
	case FrameMsg:
		m.conn = online
		m.apply(msg.Frame)
	case OfflineMsg:
		m.conn = offline
	case spawnedMsg:
		m.spawning = false
		if msg.err != nil {
			m.setStatus(msg.err.Error(), true)
			return m, nil
		}
		m.bar.Reset()
		m.chips = nil
		m.setFocus(focusList)
		m.setStatus(spawnSummary(msg.session), false)
		m.pick(msg.session.ID)
	case shownMsg:
		if msg.err != nil {
			m.setStatus(msg.err.Error(), true)
		}
	case renamedMsg:
		if msg.err != nil {
			m.setStatus(msg.err.Error(), true)
		} else {
			m.setStatus(fmt.Sprintf("renamed %s to %s", msg.before, msg.session.Name), false)
		}
	case movedMsg:
		m.moved(msg)
	case closedMsg:
		m.closedDone(msg)
	case reopenedMsg:
		m.reopened(msg)
	case undoneMsg:
		m.undone(msg)
	case completedMsg:
		m.completed(msg)
	case ratedMsg:
		if msg.err != nil {
			m.setStatus(msg.err.Error(), true)
		} else {
			m.setStatus(fmt.Sprintf("rated %s %s; the router learns from it", msg.name, msg.verdict), false)
		}
	case toolMsg:
		if msg.err != nil {
			m.setStatus(msg.err.Error(), true)
			return m, nil
		}
		// A tool session waits for its user: it goes into the slot.
		m.bar.Reset()
		m.chips = nil
		m.setFocus(focusList)
		m.pick(msg.session.ID)
		id, b := msg.session.ID, m.backend
		return m, func() tea.Msg { return shownMsg{err: b.Show(id, true)} }
	case routedMsg:
		if msg.err != nil {
			m.setStatus(msg.err.Error(), true)
		} else {
			m.setStatus(msg.choice.Reason, false)
		}
	case tea.PasteMsg:
		if m.focus != focusBar {
			m.setFocus(focusBar)
		}
		if len(msg.Content) > chipMin {
			c := newChip(msg.Content)
			m.chips = append(m.chips, c)
			m.bar.InsertString(c.label)
			m.clamp()
			return m, nil
		}
		if n := strings.Count(m.bar.Value(), "\n") + strings.Count(msg.Content, "\n") + 1; n > maxBarContent {
			m.setStatus(fmt.Sprintf("paste too long for the command bar (%d lines); save it to a file and name the file in the task", n), true)
			return m, nil
		}
		var cmd tea.Cmd
		m.bar, cmd = m.bar.Update(msg)
		m.clamp()
		return m, cmd
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			m.scroll(-1)
		case tea.MouseWheelDown:
			m.scroll(1)
		}
		m.hover = m.hoverAt(msg.X, msg.Y)
	case tea.MouseMotionMsg:
		// With the button held the pointer carries what it took hold of;
		// else it lights what it is over.
		if msg.Button == tea.MouseLeft && m.drag != nil {
			m.hover = ""
			m.dragTo(msg.Y)
			break
		}
		m.hover = m.hoverAt(msg.X, msg.Y)
	case tea.BlurMsg:
		m.hover = ""
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft || m.menu != nil {
			break
		}
		m.confirming, m.closing = "", ""
		if v, ok := tabAt(msg.X); ok && m.tabbed() && msg.Y == m.tabsTop() {
			m.setView(v)
			break
		}
		if i, _, ok := m.at(msg.Y); ok {
			// The press takes hold of what it is on. The release opens it,
			// as a click does, or drops a card a drag carried (control.go).
			m.press(i)
			return m, nil
		}
		if msg.Y >= m.barTop()-b2i(m.tabbed()) {
			m.setFocus(focusBar)
		}
	case tea.MouseReleaseMsg:
		return m.release(msg.Y)
	case controlledMsg:
		m.controlled(msg)
	case tea.KeyPressMsg:
		if m.menu != nil {
			return m.menuKey(msg)
		}
		if m.focus == focusBar {
			return m.barKey(msg)
		}
		return m.listKey(msg)
	}
	return m, nil
}

func (m Model) listKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.setStatus("", false)
	if msg.String() != "m" {
		m.confirming = "" // any other key cancels a move
	}
	if msg.String() != "x" {
		m.closing = "" // and a close
	}
	switch msg.String() {
	case "up", "k":
		m.moveTo(m.sel - 1)
	case "down", "j":
		m.moveTo(m.sel + 1)
	case "pgup":
		m.moveTo(m.sel - max(1, m.visible(m.items())))
	case "pgdown":
		m.moveTo(m.sel + max(1, m.visible(m.items())))
	case "home":
		m.moveTo(0)
	case "end":
		m.moveTo(m.rowCount() - 1)
	case "enter", "space":
		cmd := m.open(msg.String() == "enter")
		return m, cmd
	case "v":
		m.setView(1 - m.view)
	case "m":
		cmd := m.moveSelected()
		return m, cmd
	case "f", "F":
		return m.freezeKey(msg.String() == "F")
	case "!":
		return m.promoteKey("")
	case "z":
		return m.parkKey()
	case "Z":
		m.foldParked()
	case "r":
		// The name is edited in the command bar, with its keys and the
		// terminal's own cursor; Enter renames, Esc keeps the name.
		s, ok := m.selSession()
		if !ok {
			m.setStatus("select one of Irie's sessions to rename it", true)
			break
		}
		m.setFocus(focusBar)
		m.bar.SetValue(":rename " + s.Name)
		m.clamp()
		m.setStatus("Enter renames it, Esc keeps its name", false)
	case "x":
		cmd := m.closeSelected()
		return m, cmd
	case "u":
		return m, m.undoCmd(dirUndo, "", "")
	case "U":
		return m, m.undoCmd(dirRedo, "", "")
	case "+":
		return m, m.rateCmd(ipc.Good)
	case "-":
		return m, m.rateCmd(ipc.Bad)
	case "tab":
		m.setFocus(focusBar)
	case ":":
		m.setFocus(focusBar)
		m.bar.SetValue(":")
	}
	return m, nil
}

func (m Model) barKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		if m.spawning {
			return m, nil
		}
		if first, _, _ := strings.Cut(strings.TrimSpace(m.bar.Value()), "\n"); strings.HasPrefix(first, ":") {
			return m.command(first)
		}
		if q, ok := ParseHows(m.bar.Value()); ok {
			if s := FindSession(m.sessions, q); s != nil {
				m.setStatus(Hows(*s), false)
			} else {
				m.setStatus("no agent matches “"+q+"”", true)
			}
			m.bar.Reset()
			m.chips = nil
			return m, nil
		}
		args, err := ParseTask(m.bar.Value(), m.home)
		if err != nil {
			m.setStatus(err.Error(), true)
			return m, nil
		}
		args.Task = expand(args.Task, m.chips)
		m.spawning = true
		m.setStatus("starting…", false)
		b := m.backend
		return m, func() tea.Msg {
			s, err := b.Spawn(args)
			return spawnedMsg{session: s, err: err}
		}
	case "esc":
		m.bar.Reset()
		m.chips = nil
		m.setStatus("", false)
		m.setFocus(focusList)
		return m, nil
	case "tab":
		word, _, ok := m.wordAtCursor()
		if !ok || word == "" || !strings.ContainsAny(word[:1], "@+") {
			return m, nil
		}
		b := m.backend
		return m, func() tea.Msg {
			r, err := b.Complete(word)
			return completedMsg{token: word, items: r.Items, notes: r.Notes, err: err}
		}
	}
	// As the bar grows the list shows fewer rows, but the rows it shows stay
	// where they are, even when the selected one drops out of sight.
	var cmd tea.Cmd
	m.bar, cmd = m.bar.Update(msg)
	m.clamp()
	return m, cmd
}

// rateCmd rates the selected session. A row outside Irie has nothing to
// rate.
func (m Model) rateCmd(verdict string) tea.Cmd {
	s, ok := m.selSession()
	if !ok {
		return nil
	}
	b := m.backend
	return func() tea.Msg { return ratedMsg{name: s.Name, verdict: verdict, err: b.Rate(s.ID, verdict)} }
}

// command runs a command bar command: a line that starts with a colon.
func (m Model) command(line string) (tea.Model, tea.Cmd) {
	name, arg, _ := strings.Cut(strings.TrimPrefix(line, ":"), " ")
	arg = strings.TrimSpace(arg)
	b := m.backend
	done := func(cmd tea.Cmd) (tea.Model, tea.Cmd) {
		m.bar.Reset()
		m.chips = nil
		return m, cmd
	}
	if model, cmd, ok := m.controlCommand(name, arg); ok {
		return model, cmd
	}
	switch {
	case name == "move":
		o, err := m.pickOutside(arg)
		if err != nil {
			m.setStatus(err.Error(), true)
			return m, nil
		}
		m.setFocus(focusList)
		return done(m.moveCmd(o))
	case name == "rename" && arg != "":
		s, ok := m.selSession()
		if !ok {
			m.setStatus("no session to rename; select one of Irie's", true)
			return m, nil
		}
		m.setFocus(focusList)
		return done(func() tea.Msg {
			r, err := b.Rename(s.ID, arg)
			return renamedMsg{before: s.Name, session: r, err: err}
		})
	case name == "setup" && (arg == "" || arg == "local"):
		m.setStatus("starting local setup…", false)
		return m, func() tea.Msg { s, err := b.Tool(ipc.ToolSetup); return toolMsg{session: s, err: err} }
	case name == "models" && arg == "":
		return m, func() tea.Msg { s, err := b.Tool(ipc.ToolModels); return toolMsg{session: s, err: err} }
	case name == "rate" && (arg == ipc.Good || arg == ipc.Bad):
		if _, ok := m.selSession(); !ok {
			m.setStatus("no session to rate; select one of Irie's", true)
			return m, nil
		}
		return done(m.rateCmd(arg))
	case name == "close" && arg == "":
		m.setFocus(focusList)
		return done(m.closeNamed())
	case name == "reopen" && arg == "":
		m.setFocus(focusList)
		return done(m.reopenCmd())
	case (name == dirUndo || name == dirRedo) && arg == "":
		m.setFocus(focusList)
		return done(m.undoCmd(name, "", ""))
	case name == "route" && arg != "":
		return m, func() tea.Msg { ch, err := b.Route(arg); return routedMsg{choice: ch, err: err} }
	}
	m.setStatus(fmt.Sprintf("unknown command :%s; the commands are %s", name, commands), true)
	return m, nil
}

// wordAtCursor returns the part before the cursor of the word it is in,
// and the column where that word starts. Tokens live on the first line, so
// ok is false on the others.
func (m Model) wordAtCursor() (word string, start int, ok bool) {
	if m.bar.Line() != 0 {
		return "", 0, false
	}
	first, _, _ := strings.Cut(m.bar.Value(), "\n")
	line := []rune(first)
	col := min(m.bar.Column(), len(line))
	start = col
	for start > 0 && !unicode.IsSpace(line[start-1]) {
		start--
	}
	return string(line[start:col]), start, true
}

// completed applies Tab's answer, unless the bar has moved on: one
// candidate replaces the token, and the status strip says what a profile
// runs and costs; several complete their common start and are listed in
// the status strip.
func (m *Model) completed(msg completedMsg) {
	word, start, ok := m.wordAtCursor()
	if !ok || word != msg.token {
		return
	}
	switch {
	case msg.err != nil:
		m.setStatus(msg.err.Error(), true)
	case len(msg.items) == 0:
		m.setStatus("no match for "+word, false)
	case len(msg.items) == 1:
		// A directory leaves the cursor in the path, for the next part.
		m.replaceWord(start, word, msg.items[0], !strings.HasSuffix(msg.items[0], "/"))
		note := ""
		if len(msg.notes) == 1 {
			note = msg.notes[0]
		}
		m.setStatus(note, false)
	default:
		if p := commonPrefix(msg.items); len([]rune(p)) > len([]rune(word)) {
			m.replaceWord(start, word, p, false)
		}
		m.setStatus(strings.Join(msg.items, "  "), false)
	}
}

// replaceWord puts repl in place of the first line's word at start, with a
// space after it when space is set and none follows, and moves the cursor
// to its end.
func (m *Model) replaceWord(start int, word, repl string, space bool) {
	lines := strings.Split(m.bar.Value(), "\n")
	line := []rune(lines[0])
	tail := line[start+len([]rune(word)):]
	if space && (len(tail) == 0 || !unicode.IsSpace(tail[0])) {
		repl += " "
	}
	lines[0] = string(line[:start]) + repl + string(tail)
	m.bar.SetValue(strings.Join(lines, "\n"))
	m.bar.MoveToBegin()
	m.bar.SetCursorColumn(start + len([]rune(repl)))
	m.clamp()
}

func commonPrefix(items []string) string {
	p := []rune(items[0])
	for _, s := range items[1:] {
		r := []rune(s)
		n := 0
		for n < len(p) && n < len(r) && p[n] == r[n] {
			n++
		}
		p = p[:n]
	}
	return string(p)
}

// spawnSummary is what the status strip says about a new session: the
// worktree it got, the local files copied into it, and what Irie could not
// do or carry over.
func spawnSummary(s ipc.Session) string {
	var parts []string
	if s.Branch != "" {
		parts = append(parts, "worktree "+s.Branch)
	}
	if len(s.Copied) > 0 {
		parts = append(parts, "copied "+strings.Join(s.Copied, ", "))
	}
	return strings.Join(append(parts, s.Warnings...), " · ")
}
