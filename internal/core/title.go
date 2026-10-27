package core

// Session titles (DESIGN §4.2). A session that was given no name is named
// after its task's first line, and then after the title Claude Code gives
// its conversation: Claude Code titles a session it got no --name for,
// from its first prompt, and writes the title to the transcript as an
// ai-title line. The core reads it with the token tally and names the row
// after it, in title case. A name the user chose, by rename or at spawn, is
// pinned: the pane's @irie_named keeps it from the titles, and a spawn
// passes it to Claude Code with --name, which then titles nothing.

import (
	"bytes"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/irie-hq/irie/internal/tmux"
)

// maxTitle caps a title made from a task, in characters.
const maxTitle = 40

// title names a session after its task: the first line, without the @ and
// + tokens that may lead it, its whitespace and control characters made
// single spaces, in title case and cut at a word's end to 40 characters
// with "…". A line with no words gets the task's slug.
func title(task string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(task), "\n")
	words := strings.Fields(cleanText(first, len(first)))
	for len(words) > 0 && (words[0][0] == '@' || words[0][0] == '+') {
		words = words[1:]
	}
	if len(words) == 0 {
		return titleCase(slug(task))
	}
	r := []rune(titleCase(strings.Join(words, " ")))
	if len(r) <= maxTitle {
		return string(r)
	}
	// The last space at or before the cut ends the last whole word; a first
	// word longer than the cut is cut where it is.
	cut := maxTitle - 1
	end := cut
	for end > 0 && r[end] != ' ' {
		end--
	}
	if end == 0 {
		end = cut
	}
	return string(r[:end]) + "…"
}

// minor are the words title case leaves in lower case but first or last:
// the articles, the coordinating conjunctions The Chicago Manual of Style
// lowers, and the prepositions of three letters or fewer that the AMA
// Manual of Style lowers, but for those that in a task's title are mostly
// adverbs or nouns, such as up, out, off, bar and sub.
var minor = map[string]bool{
	"a": true, "an": true, "the": true,
	"and": true, "but": true, "for": true, "nor": true, "or": true,
	"as": true, "at": true, "by": true, "in": true, "of": true, "on": true, "per": true, "to": true, "via": true,
}

// titleCase capitalises the words of s, which single spaces part, but the
// minor ones, unless a word is the first or last of the title or of a part
// that a colon ends. A word with a capital, a digit or any of / . _ `, or a
// flag's leading -, stays as it is: an acronym, iOS, a path or code.
func titleCase(s string) string {
	words := strings.Split(s, " ")
	for i, w := range words {
		if strings.ContainsAny(w, "/._`") || strings.HasPrefix(w, "-") || strings.IndexFunc(w, func(r rune) bool { return unicode.IsUpper(r) || unicode.IsDigit(r) }) >= 0 {
			continue
		}
		edge := i == 0 || i == len(words)-1 || strings.HasSuffix(w, ":") || strings.HasSuffix(words[i-1], ":")
		if !edge && minor[strings.TrimFunc(w, func(r rune) bool { return !unicode.IsLetter(r) })] {
			continue
		}
		if j := strings.IndexFunc(w, unicode.IsLetter); j >= 0 {
			r, n := utf8.DecodeRuneInString(w[j:])
			words[i] = w[:j] + string(unicode.ToUpper(r)) + w[j+n:]
		}
	}
	return strings.Join(words, " ")
}

// numberedOf reports whether name is base, or base numbered as unique
// numbers it.
func numberedOf(name, base string) bool {
	if name == base {
		return true
	}
	n, ok := strings.CutPrefix(name, base+"-")
	i, err := strconv.Atoi(n)
	return ok && err == nil && i >= 2 && strconv.Itoa(i) == n
}

// oldName settles the name of a session from before Irie kept whether the
// user chose it: its task's slug, or that numbered, as Irie named every
// session then, becomes the task's title, and any other name, given with
// --name, by rename or by a move, is the user's. It reports whether the name
// is the user's.
func (c *Core) oldName(id, name string) (string, bool) {
	task, err := os.ReadFile(c.paths.TaskFile(id))
	if err != nil || !numberedOf(name, slug(string(task))) {
		return name, true
	}
	return title(string(task)), false
}

// settle settles the name of an adopted session whose pane is from before
// @irie_named (oldName) and writes the outcome on its pane, so it is
// settled once. The caller holds c.mu.
func (c *Core) settle(x *session) {
	name, named := c.oldName(x.id, x.name)
	if !named {
		name = unique(name, c.names(x))
	}
	cmds := [][]string{{"set-option", "-p", "-t", x.pane, tmux.OptNamed, flag(named)}}
	if name != x.name {
		cmds = append(cmds, []string{"set-option", "-p", "-t", x.pane, tmux.OptName, name})
	}
	if _, err := c.tmux.RunList(cmds); err != nil {
		c.log.Warn("settle name", "session", x.id, "err", err)
		return
	}
	if name != x.name {
		c.log.Info("titled", "session", x.id, "before", x.name, "after", name)
	}
	x.name, x.named = name, named
}

// flag is a tmux option's value for b.
func flag(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// followTitles names each session whose name is not pinned after the title
// Claude Code last gave its own conversation, in title case, when a new one
// arrived. A title another row has is numbered. The caller holds c.mu.
func (c *Core) followTitles() {
	for _, x := range c.sessions {
		if t, ok := x.tally.title(x.track.own); ok && !x.named {
			if t = titleCase(cleanText(t, maxName)); t != "" {
				c.retitle(x, t)
			}
		}
	}
}

// retitle gives x the name t, numbered when another row has it, and keeps it
// in @irie_name, so a restarted core has it. A name that is t already,
// numbered or not, stays, so a title read again, as a restarted core reads
// them all, changes nothing. Unlike rename it records nothing: the user did
// nothing. The caller holds c.mu.
func (c *Core) retitle(x *session, t string) {
	taken := c.names(x)
	if numberedOf(x.name, t) && !taken[x.name] {
		return
	}
	name := unique(t, taken)
	if _, err := c.tmux.Run("set-option", "-p", "-t", x.pane, tmux.OptName, name); err != nil {
		c.log.Warn("title", "session", x.id, "err", err)
		return
	}
	c.log.Info("titled", "session", x.id, "before", x.name, "after", name)
	x.name = name
}

// titleLine reads a transcript line for the session's title. An ai-title
// line has the title Claude Code made for a session it got no name for. A
// custom-title line, which a name given in Claude Code itself writes, gives
// "", so the titles before it are passed over.
func titleLine(b []byte) (string, bool) {
	if !bytes.Contains(b, []byte(`"ai-title"`)) && !bytes.Contains(b, []byte(`"custom-title"`)) {
		return "", false
	}
	var rec struct {
		Type    string `json:"type"`
		AITitle string `json:"aiTitle"`
	}
	if json.Unmarshal(b, &rec) != nil {
		return "", false
	}
	switch rec.Type {
	case "ai-title":
		return rec.AITitle, true
	case "custom-title":
		return "", true
	}
	return "", false
}
