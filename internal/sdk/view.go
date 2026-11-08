package sdk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
)

// Styles: calm, with no colours of their own. What the agent does is
// faint; what the user said is bold.
const (
	dim   = "\x1b[2m"
	bold  = "\x1b[1m"
	reset = "\x1b[0m"
)

// view turns the CLI's lines into what the terminal shows: whole lines for
// the log, which scrolls up, and the line of the reply in progress, which
// the bottom of the screen shows until it is whole.
type view struct {
	width    int               // the terminal's columns
	line     string            // reply text that is not a whole line yet
	current  string            // the main conversation's message being streamed
	streamed map[string]bool   // messages whose text came as a stream
	tools    map[string]string // tool call id → tool name, to word its result
	model    string
	began    bool    // the first init was shown
	context  int     // tokens the main conversation's last request sent
	window   int     // the model's context window, in tokens
	cost     float64 // what the session has cost, at list price
	costed   bool
	stopped  bool // the turn that ends next was stopped by the user
}

func newView(width int) *view {
	return &view{width: max(width, 20), streamed: map[string]bool{}, tools: map[string]string{}}
}

// add reads one line from the CLI, other than a result or a control
// message, and returns the lines it makes for the log.
func (v *view) add(m message) []string {
	switch m.Type {
	case "system":
		return v.system(m)
	case "stream_event":
		if m.sub() || m.Event == nil {
			return nil
		}
		return v.stream(*m.Event)
	case "assistant":
		if m.Message == nil {
			return nil
		}
		return v.assistant(m)
	case "user":
		if m.Message == nil {
			return nil
		}
		return v.user(m)
	}
	return nil
}

func (v *view) system(m message) []string {
	switch m.Subtype {
	case "init":
		if !v.began || m.Model != v.model {
			v.began, v.model = true, m.Model
			s := "  · " + m.Model
			if m.PermissionMode != "" {
				s += " · " + m.PermissionMode + " mode"
			}
			return []string{v.faint(s)}
		}
	case "api_retry":
		var category string
		json.Unmarshal(m.Error, &category)
		what := strings.ReplaceAll(category, "_", " ")
		if what == "" {
			what = "the API failed"
		}
		return []string{v.faint(fmt.Sprintf("  · %s; trying again (%d of %d)", what, m.Attempt, m.MaxRetries))}
	case "status":
		if m.Status != nil && *m.Status == "compacting" {
			return []string{v.faint("  · compacting the conversation…")}
		}
	case "compact_boundary":
		return []string{v.faint("  · the conversation was compacted")}
	}
	return nil
}

func (v *view) stream(e streamEvent) []string {
	switch e.Type {
	case "message_start":
		if e.Message != nil {
			v.current = e.Message.ID
			if u := e.Message.Usage; u != nil {
				v.context = u.Input + u.CacheCreation + u.CacheRead
			}
		}
	case "content_block_delta":
		if e.Delta != nil && e.Delta.Type == "text_delta" {
			v.streamed[v.current] = true
			return v.feed(e.Delta.Text)
		}
	case "content_block_stop":
		return v.flush()
	}
	return nil
}

func (v *view) assistant(m message) []string {
	out := v.flush()
	indent := ""
	if m.sub() {
		indent = "  "
	}
	for _, b := range blocks(m.Message.Content) {
		switch b.Type {
		case "text":
			// The main conversation's text came as a stream already; a
			// subagent's goes back to the agent, not to the user.
			if m.sub() || v.streamed[m.Message.ID] {
				continue
			}
			out = append(out, v.feed(b.Text)...)
			out = append(out, v.flush()...)
		case "tool_use":
			v.tools[b.ID] = b.Name
			out = append(out, v.faint(indent+"  ▸ "+b.Name+"  "+summary(b.Name, b.Input)))
		}
	}
	return out
}

func (v *view) user(m message) []string {
	var out []string
	indent := ""
	if m.sub() {
		indent = "  "
	}
	for _, b := range blocks(m.Message.Content) {
		switch b.Type {
		case "tool_result":
			out = append(out, v.faint(indent+"    "+v.outcome(b)))
		case "text":
			// A subagent's first message is its prompt; the main
			// conversation's are notes such as an interrupted request.
			if t := strings.TrimSpace(b.Text); t != "" && !m.sub() {
				out = append(out, v.faint("  · "+firstLine(t)))
			}
		}
	}
	return out
}

// outcome is a tool result in a few words.
func (v *view) outcome(b block) string {
	text := strings.TrimSpace(plain(b.Content))
	n := 0
	if text != "" {
		n = strings.Count(text, "\n") + 1
	}
	switch {
	case b.IsError:
		return "✗ " + firstLine(text)
	case n == 0:
		return "no output"
	case v.tools[b.ToolUseID] == "Read":
		return plural(n, "line")
	case n == 1:
		return text
	}
	return firstLine(text) + " (+" + plural(n-1, "line") + ")"
}

// result is the end of a turn, which took took.
func (v *view) result(m message, took time.Duration) []string {
	out := v.flush()
	if m.CostUSD != nil {
		v.cost, v.costed = *m.CostUSD, true
	}
	for name, u := range m.ModelUsage {
		if name == v.model || v.window == 0 {
			v.window = u.ContextWindow
		}
	}
	if m.IsError {
		for i, l := range wrap(m.errorText(), v.width-4) {
			prefix := "    "
			if i == 0 {
				prefix = "  ! "
			}
			out = append(out, prefix+l)
		}
		return out
	}
	s := "  · done in " + took.Round(time.Second).String()
	if v.stopped {
		v.stopped = false
		s = "  · stopped after " + took.Round(time.Second).String()
	}
	if v.costed {
		s += " · " + dollars(v.cost) + " so far"
	}
	return append(out, v.faint(s))
}

// feed adds reply text and returns the lines it completes. A line that
// would not fit breaks at its last space, so each fits on one row.
func (v *view) feed(s string) []string {
	var out []string
	for _, r := range safe(s) {
		if r == '\n' {
			out = append(out, v.line)
			v.line = ""
			continue
		}
		v.line += string(r)
		if lipgloss.Width(v.line) < v.width {
			continue
		}
		if i := strings.LastIndexByte(v.line, ' '); i > 0 {
			out = append(out, v.line[:i])
			v.line = v.line[i+1:]
		} else {
			out = append(out, strings.TrimSuffix(v.line, string(r)))
			v.line = string(r)
		}
	}
	return out
}

// wrap breaks text into lines that fit in width columns, as feed does.
func wrap(text string, width int) []string {
	w := newView(width)
	return append(w.feed(text), w.flush()...)
}

// flush returns the line in progress, ending it.
func (v *view) flush() []string {
	if v.line == "" {
		return nil
	}
	l := v.line
	v.line = ""
	return []string{l}
}

// faint is s, cut to one row, in the faint style.
func (v *view) faint(s string) string { return dim + v.fit(s) + reset }

// fit cuts s to one row of the terminal, marking the cut with "…".
func (v *view) fit(s string) string { return fitTo(s, v.width-1) }

// fitTo cuts s to room columns, marking the cut with "…". What could steer
// the terminal is taken out first (safe).
func fitTo(s string, room int) string {
	s = strings.ReplaceAll(safe(s), "\n", " ")
	if lipgloss.Width(s) <= room {
		return s
	}
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if w+rw > room-1 {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String() + "…"
}

// summary is what a tool call is about, in one line: a command, a file, a
// pattern, or else its input as JSON.
func summary(tool string, raw json.RawMessage) string {
	var in map[string]any
	json.Unmarshal(raw, &in)
	str := func(k string) string { s, _ := in[k].(string); return s }
	var s string
	switch tool {
	case "Bash":
		s = str("command")
	case "Read", "Write", "Edit", "MultiEdit":
		s = str("file_path")
	case "NotebookEdit":
		s = str("notebook_path")
	case "Glob", "Grep":
		if s = str("pattern"); str("path") != "" {
			s += " in " + str("path")
		}
	case "WebFetch":
		s = str("url")
	case "WebSearch":
		s = str("query")
	case "Agent", "Task":
		s = str("description")
	case "Skill":
		s = str("skill")
	}
	if s == "" {
		var b bytes.Buffer
		if json.Compact(&b, raw) == nil {
			s = b.String()
		}
	}
	if strings.Contains(strings.TrimSpace(s), "\n") {
		return firstLine(s) + " …"
	}
	return strings.TrimSpace(s)
}

// details are the lines that show a call the agent asks to make in full,
// before the question: the whole command, the edit, the plan.
func details(tool string, raw json.RawMessage) []string {
	var in map[string]any
	json.Unmarshal(raw, &in)
	str := func(k string) string { s, _ := in[k].(string); return s }
	var lines []string
	add := func(prefix, text string, most int) {
		ls := strings.Split(strings.TrimRight(text, "\n"), "\n")
		for i, l := range ls {
			if i == most {
				lines = append(lines, prefix+"… "+plural(len(ls)-most, "more line"))
				break
			}
			lines = append(lines, prefix+l)
		}
	}
	switch tool {
	case "Bash":
		add("    $ ", str("command"), 20)
	case "Edit":
		lines = append(lines, "    "+str("file_path"))
		add("    - ", str("old_string"), 10)
		add("    + ", str("new_string"), 10)
	case "Write":
		lines = append(lines, "    "+str("file_path"))
		add("    + ", str("content"), 15)
	case "ExitPlanMode":
		add("    ", str("plan"), 60)
	default:
		var b bytes.Buffer
		if json.Indent(&b, raw, "", "  ") == nil {
			add("    ", b.String(), 20)
		}
	}
	return lines
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}

// dollars is a cost in cents, or in tenths of a cent below one.
func dollars(usd float64) string {
	if usd < 0.01 {
		return fmt.Sprintf("$%.3f", usd)
	}
	return fmt.Sprintf("$%.2f", usd)
}

// safe is s with what could steer the terminal taken out: escape sequences
// and other control characters, which the model's text, a tool's output or
// a paste may hold, such as a sequence that sets the clipboard. A tab
// becomes four spaces, since the terminal would widen it past what was
// measured; a new line stays.
func safe(s string) string {
	if utf8.ValidString(s) && !strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 && r != '\n' || r >= 0x7f && r <= 0x9f }) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == 0x1b:
			n = escapeLen(s[i:])
		case r == '\t':
			b.WriteString("    ")
		case r == '\n':
			b.WriteByte('\n')
		case r < 0x20 || r >= 0x7f && r <= 0x9f:
		case r == utf8.RuneError && n == 1:
			// A byte that is not UTF-8, which a terminal may take as a
			// control of its own.
			b.WriteString("\uFFFD")
		default:
			b.WriteString(s[i : i+n])
		}
		i += n
	}
	return b.String()
}

// escapeLen is how many bytes the escape sequence at the start of s takes:
// a CSI to its final byte, an OSC to its BEL or string terminator, a DCS,
// SOS, PM or APC to its terminator, or an escape and one character.
func escapeLen(s string) int {
	if len(s) < 2 {
		return len(s)
	}
	switch s[1] {
	case '[':
		for i := 2; i < len(s); i++ {
			if s[i] >= 0x40 && s[i] <= 0x7e {
				return i + 1
			}
		}
		return len(s)
	case ']', 'P', 'X', '^', '_':
		for i := 2; i < len(s); i++ {
			switch {
			case s[i] == 0x07 && s[1] == ']':
				return i + 1
			case s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\':
				return i + 2
			}
		}
		return len(s)
	}
	_, n := utf8.DecodeRuneInString(s[1:])
	return 1 + n
}
