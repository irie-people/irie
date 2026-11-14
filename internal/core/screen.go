package core

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/ipc"
)

// screen reads what session's terminal shows now (ipc.OpScreen), for a
// client with no terminal of its own, such as the phone app: its lines as
// plain text, and while the session waits on you, the numbered choices of
// the prompt at its bottom.
func (c *Core) screen(a ipc.ScreenArgs) (ipc.ScreenResult, error) {
	c.mu.Lock()
	x := c.sessions[a.Session]
	if x == nil {
		c.mu.Unlock()
		return ipc.ScreenResult{}, ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Session)
	}
	pane, asks := x.pane, x.wire().State == ipc.StateNeedsYou
	c.mu.Unlock()
	out, err := c.tmux.Run("capture-pane", "-p", "-J", "-t", pane)
	if err != nil {
		return ipc.ScreenResult{}, err
	}
	r := ipc.ScreenResult{Lines: screenLines(out), Options: []ipc.ScreenOption{}}
	if asks {
		r.Options = promptOptions(r.Lines)
	}
	return r, nil
}

// screenLines splits what capture-pane printed into lines without the
// spaces after each and the blank lines after the last, and without tmux's
// note that the pane is dead.
func screenLines(out string) []string {
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRightFunc(l, unicode.IsSpace)
	}
	lines = slices.DeleteFunc(lines, func(l string) bool { return strings.HasPrefix(l, "Pane is dead") })
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// promptMarks start the line of the agent's own prompt: Claude Code draws
// ❯, and > before it.
var promptMarks = []string{"❯", ">"}

// agentPrompt finds the agent's own prompt, where you type it a message,
// on a screen's lines, as Claude Code draws it near the bottom: a line
// that starts with its mark, right under a rule across the terminal, with
// a title in the rule or none, and the lines down to the next rule. Before
// 2.0 a box drew the rules and edged the lines. typed is what the prompt
// holds, its lines joined by newlines, without the mark. shows is false
// when no prompt shows, as while a permission's, a question's or a plan's
// menu stands in for it, or a dialog such as /config. The prompt nearest
// the bottom is the one; a line of the conversation above never sits
// between two rules with nothing after its mark. The Mac app reads its
// terminal by the same rule (ContinueKey.swift).
func agentPrompt(lines []string) (typed string, shows bool) {
	for i := len(lines) - 1; i > 0; i-- {
		first, ok := promptLine(lines[i])
		if !ok || !isRule(lines[i-1]) {
			continue
		}
		text := []string{first}
		for _, l := range lines[i+1:] {
			if isRule(l) {
				return strings.TrimSpace(strings.Join(text, "\n")), true
			}
			text = append(text, unboxed(l))
		}
		return "", false
	}
	return "", false
}

// promptLine is what follows the prompt's mark on line l, and whether l
// starts with one.
func promptLine(l string) (string, bool) {
	l = unboxed(l)
	for _, m := range promptMarks {
		if rest, ok := strings.CutPrefix(l, m); ok && (rest == "" || unicode.IsSpace([]rune(rest)[0])) {
			return strings.TrimSpace(rest), true
		}
	}
	return "", false
}

// unboxed is line l without the blanks round it and a box's edges.
func unboxed(l string) string {
	l = strings.TrimSpace(l)
	l = strings.TrimSuffix(strings.TrimPrefix(l, "│"), "│")
	return strings.TrimSpace(l)
}

// isRule says whether line l is a rule across the terminal, as over and
// under Claude Code's prompt: ─ from its start, or a box's corner, and a
// run of them, maybe with a title such as the session's name.
func isRule(l string) bool {
	l = strings.TrimSpace(l)
	starts := strings.HasPrefix(l, "─") || strings.HasPrefix(l, "╭") || strings.HasPrefix(l, "╰")
	return starts && strings.Contains(l, strings.Repeat("─", 8))
}

// menuLine is one line of a prompt's menu, as Claude Code draws its
// permission prompts, questions and plans: a number, a dot and the words,
// the selected one after a ❯, within a box's edges or none.
var menuLine = regexp.MustCompile(`^[\s│┃║]*([❯›>]\s*)?(\d{1,2})\.\s+(\S.*?)[\s│┃║]*$`)

// promptOptions are the numbered choices of the prompt at the bottom of a
// screen: the last run of choices numbered from 1 up, near the bottom, with
// at most a few lines between two of them, as the description a question's
// choice may have. A numbered list higher up, such as one in the agent's
// reply, is not a prompt.
func promptOptions(lines []string) []ipc.ScreenOption {
	const (
		nearBottom = 12 // lines below the last choice: a box's edge and a hint
		between    = 3  // lines between two choices
	)
	last := -1
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-1-nearBottom; i-- {
		if menuLine.MatchString(lines[i]) {
			last = i
			break
		}
	}
	if last < 0 {
		return []ipc.ScreenOption{}
	}
	var found []ipc.ScreenOption
	want, gap := 0, 0
	for i := last; i >= 0 && gap <= between; i-- {
		m := menuLine.FindStringSubmatch(lines[i])
		if m == nil {
			gap++
			continue
		}
		n, _ := strconv.Atoi(m[2])
		if want == 0 {
			want = n
		}
		if n != want {
			gap++
			continue
		}
		found = append(found, ipc.ScreenOption{Key: m[2], Label: m[3], Selected: m[1] != ""})
		if want == 1 {
			slices.Reverse(found)
			return found
		}
		want, gap = want-1, 0
	}
	return []ipc.ScreenOption{}
}

// maxKeys and maxKeysText bound one keys request.
const (
	maxKeys     = 16
	maxKeysText = 4096
)

// keys presses keys in a session's terminal, then types text there without
// Enter (ipc.OpKeys): how a client with no terminal answers a prompt. It
// takes its turn with send, so a message and a key are never typed into
// each other.
func (c *Core) keys(a ipc.KeysArgs) error {
	if len(a.Keys) == 0 && a.Text == "" {
		return ipc.Errorf(ipc.CodeBadArgs, "there are no keys to press")
	}
	if len(a.Keys) > maxKeys {
		return ipc.Errorf(ipc.CodeBadArgs, "at most %d keys at a time", maxKeys)
	}
	for _, k := range a.Keys {
		if !keyName(k) {
			return ipc.Errorf(ipc.CodeBadArgs, "%q is not a key "+brand.Name+" presses: a letter, a digit, or one of %s", k, strings.Join(ipc.KeyNames, ", "))
		}
	}
	if len(a.Text) > maxKeysText {
		return ipc.Errorf(ipc.CodeBadArgs, "the text is longer than %d bytes", maxKeysText)
	}
	if strings.IndexFunc(a.Text, unicode.IsControl) >= 0 {
		return ipc.Errorf(ipc.CodeBadArgs, "the text has a control character, such as a new line; send keys for those")
	}

	c.sendMu.LockAt(c.levelOf(a.Session))
	defer c.sendMu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	x := c.sessions[a.Session]
	if x == nil {
		return ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Session)
	}
	switch {
	case x.dead:
		return ipc.Errorf(ipc.CodeBusy, "%s has ended", x.name)
	case x.wire().State == ipc.StateFrozen:
		return ipc.Errorf(ipc.CodeBusy, "%s is paused: resume it first", x.name)
	}
	var cmds [][]string
	for _, k := range a.Keys {
		if len(k) == 1 {
			// -l types the character itself, never a key that tmux
			// would read in its name.
			cmds = append(cmds, []string{"send-keys", "-l", "-t", x.pane, k})
		} else {
			cmds = append(cmds, []string{"send-keys", "-t", x.pane, k})
		}
	}
	var file string
	if a.Text != "" {
		// Text goes in by a file and a paste, as a message does, so no
		// shell or tmux parser reads it.
		file = filepath.Join(c.paths.Session(x.id), "keys.txt")
		if err := os.WriteFile(file, []byte(a.Text), 0o600); err != nil {
			return err
		}
		defer os.Remove(file)
		buf := brand.Slug + "-keys-" + x.id
		cmds = append(cmds, []string{"load-buffer", "-b", buf, file}, []string{"paste-buffer", "-p", "-d", "-b", buf, "-t", x.pane})
	}
	if _, err := c.tmux.RunList(cmds); err != nil {
		return err
	}
	x.quiet++
	pokeScreen(x)
	return nil
}

// keyName says whether k is a key that keys presses.
func keyName(k string) bool {
	if len(k) == 1 {
		b := k[0]
		return b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
	}
	return slices.Contains(ipc.KeyNames, k)
}
