package core

import "strings"

// preferencesNote is what a session is told about how the user likes agents
// to work (spawn.preferences): a line each, or nothing for none.
func preferencesNote(prefs []string) string {
	if len(prefs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("[Irie] The user’s preferences, which always apply:")
	for _, p := range prefs {
		b.WriteString("\n- ")
		b.WriteString(strings.TrimSpace(p))
	}
	return b.String()
}

// startNote is what a session is told each time it starts, after a /clear
// or a compaction too: whether it can delegate, then the user's
// preferences, so that they hold all along. The caller holds c.mu.
func (c *Core) startNote(x *session) string {
	var notes []string
	for _, n := range []string{c.delegateHint(x), preferencesNote(c.cfg().Spawn.Preferences)} {
		if n != "" {
			notes = append(notes, n)
		}
	}
	return strings.Join(notes, "\n\n")
}
