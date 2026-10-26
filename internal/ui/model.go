// Package ui is the sidebar (DESIGN §4.1): a header pinned at the top, the
// session list, a status strip and a command bar pinned at the bottom. It is
// a pure client of the core's socket and never calls tmux.
package ui

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/irie-hq/irie/internal/ipc"
)

// Backend is what the sidebar asks of the core.
type Backend interface {
	Spawn(ipc.SpawnArgs) (ipc.Session, error)
	Show(id string, focus bool) error
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
	shownMsg struct{ err error }
	tickMsg  struct{}
)

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
	barPrompt   = "› "
	maxBarLines = 8
	// The textarea keeps at most this many lines and silently drops the
	// rest of a longer paste, so such a paste is refused instead.
	maxBarContent = 10000
)

// Options are the model's outside dependencies.
type Options struct {
	Now  func() time.Time
	Home string // for ~ paths
}

// Model is the sidebar's Bubble Tea model.
type Model struct {
	backend Backend
	now     func() time.Time
	home    string

	width, height int
	conn          connState
	gotSnapshot   bool

	sessions []ipc.Session
	sel      int // selected row, -1 when the list is empty
	offset   int // first visible row
	pending  string

	focus     focusArea
	bar       textarea.Model
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
	bar.Placeholder = "new task (Enter starts it)"
	bar.SetPromptFunc(lipgloss.Width(barPrompt), func(info textarea.PromptInfo) string {
		if info.LineNumber == 0 {
			return barPrompt
		}
		return strings.Repeat(" ", lipgloss.Width(barPrompt))
	})
	bar.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "ctrl+j"))
	styles := textarea.DefaultStyles(true)
	styles.Focused.CursorLine = lipgloss.NewStyle()
	styles.Cursor.Blink = false
	bar.SetStyles(styles)

	m := Model{backend: b, now: o.Now, home: o.Home, sel: -1, focus: focusBar, bar: bar}
	m.bar.Focus()
	return m
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m Model) Init() tea.Cmd { return tick() }

// Heights. The header and the command bar are pinned; the list takes the
// rest. Below 6 rows the status strip is hidden and the bar is one line.

func (m Model) statusVisible() bool { return m.height >= 6 }

func (m Model) maxBar() int {
	if !m.statusVisible() {
		return 1
	}
	return max(1, min(maxBarLines, m.height-3))
}

func (m Model) barHeight() int { return min(max(m.bar.Height(), 1), m.maxBar()) }

func (m Model) listHeight() int {
	h := m.height - 1 - m.barHeight()
	if m.statusVisible() {
		h--
	}
	return max(h, 0)
}

func (m *Model) setFocus(f focusArea) {
	m.focus = f
	if f == focusBar {
		m.bar.Focus()
	} else {
		m.bar.Blur()
	}
}

func (m *Model) setStatus(s string, isErr bool) { m.status, m.statusErr = s, isErr }

func (m *Model) clamp() {
	if len(m.sessions) == 0 {
		m.sel, m.offset = -1, 0
		return
	}
	m.sel = min(max(m.sel, 0), len(m.sessions)-1)
	m.offset = min(max(m.offset, 0), max(0, len(m.sessions)-m.listHeight()))
}

// reveal scrolls so the selected row is visible.
func (m *Model) reveal() {
	m.clamp()
	if m.sel < 0 {
		return
	}
	if h := m.listHeight(); h > 0 {
		if m.sel < m.offset {
			m.offset = m.sel
		}
		if m.sel >= m.offset+h {
			m.offset = m.sel - h + 1
		}
	}
	m.clamp()
}

func (m *Model) moveTo(i int) {
	m.sel = i
	m.reveal()
}

func (m *Model) scroll(n int) {
	m.offset += n
	if len(m.sessions) > 0 {
		m.offset = min(max(m.offset, 0), max(0, len(m.sessions)-m.listHeight()))
	} else {
		m.offset = 0
	}
}

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
	selected := ""
	if m.sel >= 0 && m.sel < len(m.sessions) {
		selected = m.sessions[m.sel].ID
	}
	switch f.T {
	case ipc.FrameSnapshot:
		m.sessions = slices.Clone(f.Sessions)
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
		if f.ID == selected {
			// The selection moves to the next row.
			m.sel = i
			selected = ""
		}
	}
	if selected != "" {
		m.sel = m.index(selected)
	}
	if m.pending != "" {
		if i := m.index(m.pending); i >= 0 {
			m.sel, m.pending = i, ""
		}
	}
	if m.sel < 0 && len(m.sessions) > 0 {
		m.sel = 0
	}
	m.reveal()
}

func (m Model) showCmd(focus bool) tea.Cmd {
	if m.sel < 0 {
		return nil
	}
	id, b := m.sessions[m.sel].ID, m.backend
	return func() tea.Msg { return shownMsg{err: b.Show(id, focus)} }
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.bar.MaxHeight = m.maxBar()
		m.bar.SetWidth(m.width)
		m.reveal()
	case tickMsg:
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
		m.setFocus(focusList)
		m.setStatus("", false)
		m.pending = msg.session.ID
		if i := m.index(msg.session.ID); i >= 0 {
			m.moveTo(i)
			m.pending = ""
		}
	case shownMsg:
		if msg.err != nil {
			m.setStatus(msg.err.Error(), true)
		}
	case tea.PasteMsg:
		if m.focus != focusBar {
			m.setFocus(focusBar)
		}
		if n := strings.Count(m.bar.Value(), "\n") + strings.Count(msg.Content, "\n") + 1; n > maxBarContent {
			m.setStatus(fmt.Sprintf("paste too long for the command bar (%d lines); save it to a file and name the file in the task", n), true)
			return m, nil
		}
		var cmd tea.Cmd
		m.bar, cmd = m.bar.Update(msg)
		m.reveal()
		return m, cmd
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			m.scroll(-1)
		case tea.MouseWheelDown:
			m.scroll(1)
		}
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft {
			break
		}
		if row := msg.Y - 1; row >= 0 && row < m.listHeight() {
			if i := m.offset + row; i < len(m.sessions) {
				m.sel = i
				m.setFocus(focusList)
				return m, m.showCmd(true)
			}
		}
		if msg.Y >= m.height-m.barHeight() {
			m.setFocus(focusBar)
		}
	case tea.KeyPressMsg:
		if m.focus == focusBar {
			return m.barKey(msg)
		}
		return m.listKey(msg)
	}
	return m, nil
}

func (m Model) listKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.setStatus("", false)
	switch msg.String() {
	case "up", "k":
		m.moveTo(m.sel - 1)
	case "down", "j":
		m.moveTo(m.sel + 1)
	case "pgup":
		m.moveTo(m.sel - max(1, m.listHeight()))
	case "pgdown":
		m.moveTo(m.sel + max(1, m.listHeight()))
	case "home":
		m.moveTo(0)
	case "end":
		m.moveTo(len(m.sessions) - 1)
	case "enter":
		return m, m.showCmd(true)
	case "space":
		return m, m.showCmd(false)
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
		args, err := ParseTask(m.bar.Value(), m.home)
		if err != nil {
			m.setStatus(err.Error(), true)
			return m, nil
		}
		m.spawning = true
		m.setStatus("starting…", false)
		b := m.backend
		return m, func() tea.Msg {
			s, err := b.Spawn(args)
			return spawnedMsg{session: s, err: err}
		}
	case "esc":
		m.bar.Reset()
		m.setStatus("", false)
		m.setFocus(focusList)
		return m, nil
	case "tab":
		return m, nil
	}
	var cmd tea.Cmd
	m.bar, cmd = m.bar.Update(msg)
	m.reveal()
	return m, cmd
}
