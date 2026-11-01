package ui

import (
	"fmt"
	"image/color"
	"math"
	"regexp"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/irie-people/irie/internal/ipc"
)

// SlotOptions configure the slot's own pane.
type SlotOptions struct {
	FocusKey string // ui.focus_key, as tmux names it
	FlipKey  string // ui.flip_key, as tmux names it; "" when there is none
	Theme    string // ui.theme: the sidebar's palette, which the slot shares; "" is rosewater
}

// Slot is `irie placeholder`: what the slot shows besides an agent's raw
// terminal (DESIGN §4.7). With a session shown, it draws that session's
// conversation from the core: you and Claude in cards, each tool call one
// line in plain words, subagents, what the agent waits on you for, and a
// message line that types to the agent. With none shown, it lists the keys.
// The flip key, bound in tmux, swaps the agent's raw terminal in and out.
func Slot(socket string, o SlotOptions) error {
	m := newSlot(slotSocket{socket}, o)
	p := tea.NewProgram(m)
	want := make(chan string, 1)
	m.follow = want
	go followSlot(p, socket, want)
	_, err := p.Run()
	return err
}

// slotBackend sends the slot's requests to the core.
type slotBackend interface {
	Send(session, text string) error
}

type slotSocket struct{ socket string }

func (b slotSocket) Send(session, text string) error {
	return ipc.Call(b.socket, callTimeout, ipc.OpSend, ipc.SendArgs{Session: session, Text: text}, nil)
}

// followSlot feeds the core's rows to p, and the conversation of the
// session the slot shows, which want names.
func followSlot(p *tea.Program, socket string, want <-chan string) {
	go func() {
		for {
			if c, err := ipc.Dial(socket, time.Second); err == nil {
				if frames, err := c.Subscribe(ipc.SubscribeArgs{}); err == nil {
					for f := range frames {
						p.Send(FrameMsg{Frame: f})
					}
				}
				c.Close()
			}
			p.Send(OfflineMsg{})
			time.Sleep(time.Second)
		}
	}()
	var stop func()
	for id := range want {
		if stop != nil {
			stop()
			stop = nil
		}
		if id == "" {
			continue
		}
		done := make(chan struct{})
		var client *ipc.Client
		stop = func() {
			close(done)
			if client != nil {
				client.Close()
			}
		}
		go func(id string) {
			for {
				c, err := ipc.Dial(socket, time.Second)
				if err == nil {
					client = c
					if frames, err := c.Subscribe(ipc.SubscribeArgs{Session: id, Conversation: true}); err == nil {
						for f := range frames {
							if f.T == ipc.FrameConversation && f.Conversation != nil {
								p.Send(talkMsg{session: id, update: *f.Conversation})
							}
						}
					}
					c.Close()
				}
				select {
				case <-done:
					return
				case <-time.After(time.Second):
				}
			}
		}(id)
	}
}

// talkMsg carries a frame of the shown session's conversation.
type talkMsg struct {
	session string
	update  ipc.Conversation
}

// sentMsg says how sending a message went.
type sentMsg struct {
	text string
	err  error
}

// talk is the shown session's conversation, as the frames built it.
type talk struct {
	session    string
	items      []ipc.Item
	index      map[string]int
	needs      *ipc.Item
	pending    []ipc.Item
	transcript string
	more       bool
	loaded     bool
}

func (t *talk) apply(u ipc.Conversation) {
	if u.Reset {
		t.items, t.index, t.more = nil, map[string]int{}, u.More
	}
	if t.index == nil {
		t.index = map[string]int{}
	}
	for _, it := range u.Items {
		if i, ok := t.index[it.ID]; ok {
			t.items[i] = it
			continue
		}
		t.index[it.ID] = len(t.items)
		t.items = append(t.items, it)
	}
	t.needs, t.pending, t.transcript, t.loaded = u.Needs, u.Pending, u.Transcript, true
}

type slotModel struct {
	backend slotBackend
	follow  chan<- string
	help    placeholder
	flipKey string
	theme   theme   // ui.theme's light and dark sets
	pal     palette // the one in use

	w, h    int
	rows    []ipc.Session
	listed  bool // a snapshot came, so no rows means no sessions
	offline bool
	shown   string
	talk    talk
	input   textarea.Model
	scroll  int // lines scrolled up from the newest
	status  string
	failed  bool
	sending bool
}

func newSlot(b slotBackend, o SlotOptions) *slotModel {
	in := textarea.New()
	in.ShowLineNumbers = false
	in.CharLimit = 0
	in.MaxWidth = 0
	in.MaxHeight = 6
	in.MaxContentHeight = math.MaxInt32
	in.DynamicHeight = true
	in.MinHeight = 1
	in.Prompt = ""
	in.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "ctrl+j", "alt+enter"))
	in.SetVirtualCursor(false)
	in.Focus()
	flip := ""
	if o.FlipKey != "" {
		flip = keyLabel(o.FlipKey)
	}
	th, ok := themes[o.Theme]
	if !ok {
		th = themes[defaultTheme]
	}
	m := &slotModel{backend: b, help: placeholder{focusKey: keyLabel(o.FocusKey), flipKey: flip}, flipKey: flip, input: in, theme: th}
	m.setDark(true)
	return m
}

// setDark draws the slot in its theme's dark or light set, as the sidebar
// does: dark until the terminal tells its background.
func (m *slotModel) setDark(dark bool) {
	m.pal = m.theme.light
	if dark {
		m.pal = m.theme.dark
	}
	slotColours(m.pal)
	styles := textarea.DefaultStyles(true)
	ink := lipgloss.NewStyle().Foreground(m.pal.ink)
	for _, st := range []*textarea.StyleState{&styles.Focused, &styles.Blurred} {
		st.Base, st.Text, st.CursorLine = lipgloss.NewStyle(), ink, ink
		st.Placeholder, st.EndOfBuffer = styleDim, styleDim
	}
	styles.Cursor.Color = nil
	styles.Cursor.Blink = false
	m.input.SetStyles(styles)
}

// paint puts a drawn screen on the palette's ground: every line, padded to
// the pane's width.
func (m *slotModel) paint(content string) string {
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		lines[i] = grounded(l, m.pal.ground, m.pal.ink, m.w)
	}
	return strings.Join(lines, "\n")
}

func (m *slotModel) Init() tea.Cmd { return tea.RequestBackgroundColor }

// session is the row of the shown session.
func (m *slotModel) session() (ipc.Session, bool) {
	for _, s := range m.rows {
		if s.ID == m.shown {
			return s, true
		}
	}
	return ipc.Session{}, false
}

func (m *slotModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.help.w, m.help.h = msg.Width, msg.Height
		m.input.SetWidth(max(10, m.column()-4))
		// A new size may be a client of another terminal attaching.
		return m, tea.RequestBackgroundColor
	case tea.BackgroundColorMsg:
		m.setDark(msg.IsDark())
	case FrameMsg:
		m.offline = false
		m.applyFrame(msg.Frame)
		return m, m.followShown()
	case OfflineMsg:
		m.offline = true
	case talkMsg:
		if msg.session == m.shown {
			m.talk.apply(msg.update)
		}
	case sentMsg:
		m.sending = false
		if msg.err != nil {
			m.status, m.failed = msg.err.Error(), true
		} else {
			m.status, m.failed = "", false
			if strings.TrimSpace(m.input.Value()) == msg.text {
				m.input.Reset()
			}
			m.scroll = 0
		}
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			m.scroll += 3
		case tea.MouseWheelDown:
			m.scroll = max(0, m.scroll-3)
		}
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m *slotModel) applyFrame(f ipc.Frame) {
	switch f.T {
	case ipc.FrameSnapshot:
		m.rows, m.listed = f.Sessions, true
	case ipc.FrameUpsert:
		if f.Session == nil {
			return
		}
		for i := range m.rows {
			if m.rows[i].ID == f.Session.ID {
				m.rows[i] = *f.Session
				return
			}
		}
		m.rows = append(m.rows, *f.Session)
	case ipc.FrameRemove:
		for i := range m.rows {
			if m.rows[i].ID == f.ID {
				m.rows = append(m.rows[:i], m.rows[i+1:]...)
				return
			}
		}
	}
}

// followShown follows the conversation of the session the core marks
// shown, when that changed.
func (m *slotModel) followShown() tea.Cmd {
	shown := ""
	for _, s := range m.rows {
		if s.Shown {
			shown = s.ID
		}
	}
	if shown == m.shown {
		return nil
	}
	m.shown, m.talk, m.scroll, m.status, m.failed = shown, talk{session: shown}, 0, "", false
	m.input.Reset()
	if m.follow == nil {
		return nil
	}
	follow := m.follow
	return func() tea.Msg {
		select {
		case follow <- shown:
		default:
			// The follower has not taken the last one yet; this one wins.
			go func() { follow <- shown }()
		}
		return nil
	}
}

func (m *slotModel) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "pgup":
		m.scroll += max(1, m.feedHeight()-2)
		return m, nil
	case "pgdown":
		m.scroll = max(0, m.scroll-max(1, m.feedHeight()-2))
		return m, nil
	case "end":
		if m.input.Value() == "" {
			m.scroll = 0
			return m, nil
		}
	}
	s, ok := m.session()
	if !ok || waitsOnYou(s) || ended(s) {
		return m, nil
	}
	if msg.String() == "enter" {
		text := strings.TrimSpace(m.input.Value())
		if text == "" || m.sending {
			return m, nil
		}
		m.sending, m.status, m.failed = true, "", false
		b, id := m.backend, s.ID
		return m, func() tea.Msg { return sentMsg{text: text, err: b.Send(id, text)} }
	}
	if m.failed {
		m.status, m.failed = "", false
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func waitsOnYou(s ipc.Session) bool { return s.State == ipc.StateNeedsYou }
func ended(s ipc.Session) bool      { return s.State == ipc.StateExited || s.State == ipc.StateFailed }

// column is the width of the conversation's column: the pane's, up to a
// width that reads well.
func (m *slotModel) column() int { return max(20, min(m.w-4, 100)) }

// footer is the message box or the banner, with the hint line under it.
func (m *slotModel) footer(s ipc.Session) []string {
	col := m.column()
	var box string
	switch {
	case waitsOnYou(s):
		text := styleRed.Bold(true).Render("● "+s.Name+" is waiting for you in its terminal.") +
			styleDim.Render("  "+m.flipHint("answer it there"))
		box = cardStyle(needsRed, col).Render(text)
	case ended(s):
		box = cardStyle(quietEdge, col).Render(styleDim.Render(s.Name + " has ended. " + m.flipHint("see its terminal")))
	case s.State == ipc.StateStarting:
		box = cardStyle(quietEdge, col).Render(styleDim.Render(s.Name + " is starting…"))
	default:
		m.input.Placeholder = "Message " + s.Name + "…"
		box = cardStyle(sun, col).Render(m.input.View())
	}
	hint := styleDim.Render("Enter sends · Shift+Enter new line · PgUp, PgDn scroll")
	if waitsOnYou(s) || ended(s) || s.State == ipc.StateStarting {
		hint = styleDim.Render("PgUp, PgDn scroll")
	}
	if m.flipKey != "" {
		hint += styleDim.Render(" · ") + keycap(m.flipKey) + styleDim.Render(" terminal")
	}
	lines := append([]string{""}, strings.Split(box, "\n")...)
	if m.status != "" {
		st := styleDim
		if m.failed {
			st = styleRed
		}
		lines = append(lines, st.Render(cut(m.status, col)))
	}
	return append(lines, cut(hint, col))
}

func (m *slotModel) flipHint(what string) string {
	if m.flipKey == "" {
		return ""
	}
	return m.flipKey + " to " + what + "."
}

// header is the session's name and state, over a rule.
func (m *slotModel) header(s ipc.Session) []string {
	col := m.column()
	l := Model{pal: m.pal}.look(s)
	g, glyph := l.glyph, lipgloss.NewStyle().Foreground(l.mark)
	what := s.Detail
	if what == "" {
		what = strings.ReplaceAll(s.State, "_", " ")
	}
	left := glyph.Render(g) + " " + styleTitle.Render(s.Name) + styleDim.Render(" · "+what)
	return []string{cut(left, col), styleRule.Render(strings.Repeat("─", col))}
}

func (m *slotModel) feedHeight() int {
	s, ok := m.session()
	if !ok {
		return m.h
	}
	return max(1, m.h-len(m.header(s))-len(m.footer(s)))
}

func (m *slotModel) View() tea.View {
	s, ok := m.session()
	if !ok || m.offline || m.w <= 0 || m.h <= 0 {
		m.help.first = m.listed && len(m.rows) == 0 && !m.offline
		v := m.help.View()
		if m.w > 0 && m.h > 0 {
			v.SetContent(m.paint(v.Content))
		}
		return v
	}
	var v tea.View
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	col := m.column()
	pad := strings.Repeat(" ", max(0, (m.w-col)/2))

	head, foot := m.header(s), m.footer(s)
	height := max(1, m.h-len(head)-len(foot))
	feed := m.feed(s, col)
	maxScroll := max(0, len(feed)-height)
	m.scroll = min(m.scroll, maxScroll)
	end := len(feed) - m.scroll
	start := max(0, end-height)
	window := feed[start:end]
	for len(window) < height {
		window = append([]string{""}, window...)
	}

	lines := append(append(append([]string{}, head...), window...), foot...)
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	v.SetContent(m.paint(strings.Join(lines, "\n")))
	if c := m.input.Cursor(); c != nil && !waitsOnYou(s) && !ended(s) && s.State != ipc.StateStarting {
		// The box's border and padding come before the text.
		c.X += len(pad) + 2
		c.Y += len(head) + height + 2 // the blank line and the box's top border
		v.Cursor = c
	}
	return v
}

// cardStyle is a rounded card of the given width, its border included.
func cardStyle(border color.Color, width int) lipgloss.Style {
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(0, 1).Width(width)
}

// feed is the conversation as lines in a column of width col, oldest
// first, with what the agent waits on you for at the end.
func (m *slotModel) feed(s ipc.Session, col int) []string {
	t := m.talk
	if !t.loaded {
		return []string{styleDim.Render("Loading the conversation…")}
	}
	if len(t.items) == 0 && len(t.pending) == 0 && t.needs == nil {
		if t.transcript == "" && s.State != ipc.StateStarting {
			return wrapLines(styleDim.Render("This one lives in its terminal: it started before Irie could follow conversations. "+m.flipHint("see it")), col)
		}
		return []string{styleDim.Render("Nothing here yet. Messages show here as they come.")}
	}
	var out []string
	if t.more {
		out = append(out, styleDim.Render("Earlier messages are in its terminal."), "")
	}
	items := t.items
	if len(items) > maxShownItems {
		items = items[len(items)-maxShownItems:]
		out = append(out, styleDim.Render("Earlier messages are in its terminal."), "")
	}
	speaker, last := "", ""
	for i := 0; i < len(items); i++ {
		it := items[i]
		// Text and calls take turns with a blank line between.
		if kind := it.Kind; speaker == "claude" && (kind == ipc.ItemText) != (last == ipc.ItemText) && kind != ipc.ItemUser && kind != ipc.ItemNote {
			out = append(out, "")
		}
		last = it.Kind
		switch it.Kind {
		case ipc.ItemUser:
			out = append(out, "")
			out = append(out, youCard(it, col)...)
			speaker = "you"
		case ipc.ItemNote:
			out = append(out, "", note(it.Text, col))
			speaker = ""
		default:
			if speaker != "claude" {
				out = append(out, "", label("✦ Claude", it.Time, col))
				speaker = "claude"
			}
			if it.Kind == ipc.ItemText {
				out = append(out, markdown(it.Text, col-2)...)
				continue
			}
			// A run of two or more finished reads, edits or searches is
			// one line.
			j := i
			for j+1 < len(items) && it.Group != "" && it.State == ipc.ItemOK && items[j+1].Group == it.Group && items[j+1].State == ipc.ItemOK && items[j+1].Kind == ipc.ItemTool {
				j++
			}
			if j > i {
				out = append(out, groupLine(items[i:j+1], col))
				i = j
				continue
			}
			out = append(out, callLine(it, col))
		}
	}
	for _, it := range t.pending {
		out = append(out, "")
		out = append(out, youCard(it, col)...)
	}
	if n := t.needs; n != nil {
		out = append(out, "")
		out = append(out, needsCard(*n, m.flipHint("answer it in its terminal"), col)...)
	}
	// One line each, so the window counts them right.
	return strings.Split(strings.Join(out, "\n"), "\n")
}

// maxShownItems is how many of the newest items the slot draws.
const maxShownItems = 300

// label is a speaker's name, with the time on the right.
func label(name string, at time.Time, col int) string {
	return row(styleTitle.Render(name), clock(at), col)
}

// clock is the time of day of a message; "" for none.
func clock(at time.Time) string {
	if at.IsZero() || at.Year() < 2000 {
		return ""
	}
	return styleDim.Render(at.Local().Format("15:04"))
}

func youCard(it ipc.Item, col int) []string {
	name := "You"
	switch {
	case it.Pending && it.Queued:
		name += styleDim.Render(" · it reads this at its next step")
	case it.Pending:
		name += styleDim.Render(" · on its way")
	case it.Queued:
		name += styleDim.Render(" · sent while it worked")
	}
	head := row(styleBold.Render(name), clock(it.Time), col)
	text := it.Text
	if lines := strings.Split(text, "\n"); len(lines) > 12 {
		text = strings.Join(lines[:12], "\n") + "\n" + styleDim.Render(fmt.Sprintf("… %d more lines", len(lines)-12))
	}
	border := youEdge
	if it.Pending {
		border = quietEdge
	}
	card := cardStyle(border, col).Render(text)
	return append([]string{head}, strings.Split(card, "\n")...)
}

func note(text string, col int) string {
	t := " " + text + " "
	side := max(2, (col-lipgloss.Width(t))/2)
	return styleRule.Render(strings.Repeat("─", side)) + styleDim.Render(t) + styleRule.Render(strings.Repeat("─", side))
}

// callMark is how a call went: ✓, ✕, an open ring while it runs, or a dash
// once its turn ended without it.
func callMark(state string) string {
	switch state {
	case ipc.ItemOK:
		return lipgloss.NewStyle().Foreground(sea).Render("✓")
	case ipc.ItemError:
		return styleRed.Render("✕")
	case ipc.ItemStopped:
		return styleDim.Render("–")
	}
	return lipgloss.NewStyle().Foreground(sun).Render("◌")
}

// took is how long a call ran: 0.4s, 40s, 3m, 1h5m.
func took(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	switch {
	case d <= 0:
		return ""
	case d < 10*time.Second:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()+0.5))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

func changes(added, removed int) string {
	var parts []string
	if added > 0 {
		parts = append(parts, lipgloss.NewStyle().Foreground(sea).Render(fmt.Sprintf("+%d", added)))
	}
	if removed > 0 {
		parts = append(parts, styleRed.Render(fmt.Sprintf("−%d", removed)))
	}
	return strings.Join(parts, " ")
}

// callEnd is what goes at the end of a call's line: the lines it changed and
// how long it took, either of which may be missing.
func callEnd(changed, took string) string {
	switch {
	case took == "":
		return changed
	case changed == "":
		return styleDim.Render(took)
	}
	return changed + " " + styleDim.Render(took)
}

// row puts left and right on one line of width col.
func row(left, right string, col int) string {
	left = cut(left, max(4, col-lipgloss.Width(right)-1))
	return left + strings.Repeat(" ", max(1, col-lipgloss.Width(left)-lipgloss.Width(right))) + right
}

func callLine(it ipc.Item, col int) string {
	phrase := it.Phrase
	if phrase == "" {
		phrase = it.Tool
	}
	left := "  " + callMark(it.State) + " " + phrase
	if it.Kind == ipc.ItemAgent {
		left = "  ↳ " + styleBold.Render(or(it.AgentType, "Helper")) + " " + styleDim.Render(or(it.Text, phrase)) + " " + callMark(it.State)
	}
	line := row(left, callEnd(changes(it.Added, it.Removed), took(it.Ms)), col)
	if it.State == ipc.ItemError && it.Output != "" {
		line += "\n" + styleRed.Render(cut("    "+firstLine(it.Output), col))
	}
	return line
}

func groupLine(items []ipc.Item, col int) string {
	files := map[string]bool{}
	added, removed, ms := 0, 0, int64(0)
	for _, it := range items {
		files[it.Detail] = true
		added, removed, ms = added+it.Added, removed+it.Removed, ms+it.Ms
	}
	var phrase string
	switch items[0].Group {
	case "edit":
		phrase = fmt.Sprintf("Edited %d files", len(files))
	case "read":
		phrase = fmt.Sprintf("Read %d files", len(files))
	default:
		phrase = fmt.Sprintf("Searched %d times", len(items))
	}
	if len(files) == 1 && items[0].Group != "search" {
		phrase = fmt.Sprintf("%s %s %d times", strings.Fields(phrase)[0], baseName(items[0].Detail), len(items))
	}
	return row("  "+callMark(ipc.ItemOK)+" "+phrase, callEnd(changes(added, removed), took(ms)), col)
}

func needsCard(n ipc.Item, hint string, col int) []string {
	lines := []string{styleRed.Bold(true).Render("● " + n.Text)}
	if len(n.Options) > 0 {
		var opts []string
		for _, o := range n.Options {
			opts = append(opts, "○ "+o)
		}
		lines = append(lines, "  "+strings.Join(opts, "   "))
	}
	if n.Detail != "" {
		lines = append(lines, markdown(n.Detail, col-4)...)
	}
	if hint != "" {
		lines = append(lines, styleDim.Render(hint))
	}
	return strings.Split(cardStyle(needsRed, col).Render(strings.Join(lines, "\n")), "\n")
}

// Markdown, drawn for a terminal: headings bold, list items with a bullet,
// code blocks behind a bar, and inline code, bold and italics styled.
var (
	inlineCode = regexp.MustCompile("`([^`]+)`")
	strong     = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	emphasis   = regexp.MustCompile(`(^|[^*\w])\*([^*\s][^*]*)\*`)
	link       = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	listItem   = regexp.MustCompile(`^(\s*)([-*+]|\d+[.)])\s+(.*)$`)
)

func inline(s string) string {
	s = link.ReplaceAllString(s, "$1 ("+"$2"+")")
	s = inlineCode.ReplaceAllStringFunc(s, func(m string) string { return codeStyle.Render(strings.Trim(m, "`")) })
	s = strong.ReplaceAllStringFunc(s, func(m string) string { return styleBold.Render(strings.Trim(m, "*")) })
	s = emphasis.ReplaceAllString(s, "$1"+lipgloss.NewStyle().Italic(true).Render("$2"))
	return s
}

func markdown(text string, width int) []string {
	var out []string
	inCode := false
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, " ")
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~"):
			inCode = !inCode
			continue
		case inCode:
			out = append(out, styleRule.Render("  │ ")+codeStyle.Render(cut(line, max(4, width-4))))
			continue
		case trimmed == "":
			out = append(out, "")
			continue
		case strings.HasPrefix(trimmed, "#"):
			out = append(out, wrapLines("  "+styleBold.Render(strings.TrimSpace(strings.TrimLeft(trimmed, "#"))), width)...)
			continue
		case strings.HasPrefix(trimmed, ">"):
			out = append(out, wrapLines(styleRule.Render("  ▎ ")+styleDim.Render(strings.TrimSpace(trimmed[1:])), width)...)
			continue
		}
		if m := listItem.FindStringSubmatch(line); m != nil {
			bullet := "•"
			if m[2][0] >= '0' && m[2][0] <= '9' {
				bullet = m[2]
			}
			indent := "  " + strings.Repeat(" ", len(m[1]))
			out = append(out, wrapLines(indent+styleDim.Render(bullet)+" "+inline(m[3]), width)...)
			continue
		}
		out = append(out, wrapLines("  "+inline(line), width)...)
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// wrapLines wraps styled text to width columns.
func wrapLines(s string, width int) []string {
	return strings.Split(lipgloss.Wrap(s, max(8, width), " "), "\n")
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

func baseName(p string) string {
	p = strings.TrimRight(firstLine(p), "/")
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
