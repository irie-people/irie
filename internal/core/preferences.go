package core

import (
	"strings"

	"github.com/irie-people/irie/internal/brand"
)

// preferencesNote is what a session is told about how the user likes agents
// to work (spawn.preferences): a line each, or nothing for none.
func preferencesNote(prefs []string) string {
	if len(prefs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("[" + brand.Name + "] The user’s preferences, which always apply:")
	for _, p := range prefs {
		b.WriteString("\n- ")
		b.WriteString(strings.TrimSpace(p))
	}
	return b.String()
}

// codeNote is what a Claude Code session is told about showing its person
// a file: a session whose shell can reach ctl, which an anonymous
// session's sandboxed shell can't.
func codeNote(x *session) string {
	if x.harness != "claude-code" || x.anon != "" {
		return ""
	}
	return `[` + brand.Name + `] To show the user a file beside your terminal, run ` + "`" + `"$` + brand.EnvPrefix + `_BIN" ctl open FILE[:LINE]` + "`" + `: it opens in their code pane, where they can read and edit it, and their edits save to the file as they type.`
}

// notesNote is what a session on a local model, in a scratch folder, is
// told about what it finds: its context holds a few long pages before it
// compacts to a summary (DESIGN §6.1), so its findings go to a file, which
// it reads again as it starts after a compaction. A session in a worktree
// is told nothing, since an untracked file there is work that has not
// landed (§4.8). The caller holds c.mu.
func (c *Core) notesNote(x *session) string {
	if !x.local || x.worktree != "" || !inside(x.cwd, c.paths.Scratch()) {
		return ""
	}
	return "[" + brand.Name + "] Your context holds only a few long pages before it is compacted to a summary. Keep what you find in notes.md in this folder as you go, a line or two each, with its source. If notes.md is there already, read it first: it is what you found before."
}

// startNote is what a session is told each time it starts, after a /clear
// or a compaction too: whether it can delegate, how to show a file, its
// profile's playbook and where a local model keeps its findings, then the
// user's preferences, so that they hold all along. The lessons the user
// kept follow it, as many as fit (withLessons). The caller holds c.mu.
func (c *Core) startNote(x *session) string {
	var notes []string
	for _, n := range []string{c.delegateHint(x), codeNote(x), anonNote(x), c.playbookNote(x), c.notesNote(x), preferencesNote(c.cfg().Spawn.Preferences), c.zenStartNote(x)} {
		if n != "" {
			notes = append(notes, n)
		}
	}
	return strings.Join(notes, "\n\n")
}
