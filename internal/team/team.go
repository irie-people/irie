// Package team is Irie's side of Claude Code's agent teams (DESIGN §2.3,
// §6.3). A session's teammates run in terminal panes of their own on the
// team server, a tmux server apart from Irie's, where each session has an
// anchor: a dead pane that stands for the session's agent, which Claude Code
// takes for its own pane. The tmux shim on the agent's PATH (shim.go) takes
// Claude Code's tmux calls there, and puts each teammate in a tmux session
// of its own, which a view shows by attaching to it.
package team

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Names on the team server: a session's anchor is the session named after
// the Irie session, and each of its teammates is a session named after it
// with a suffix: <id>-<n>.

// TeammateName is the name of a new teammate session of the Irie session
// id; seq orders the teammates by when they started.
func TeammateName(id string, seq int64) string {
	return id + "-" + strconv.FormatInt(seq, 36)
}

// Owner returns the Irie session a team server session belongs to, and
// whether the session is a teammate's rather than the anchor.
func Owner(name string) (id string, teammate bool) {
	id, _, teammate = strings.Cut(name, "-")
	return id, teammate
}

// Teammate is one teammate's pane, as the team server lists it.
type Teammate struct {
	Session string // its tmux session on the team server, which a view attaches to
	Pane    string
	PID     int    // the pane's first process
	Name    string // the name the team knows it by, from its command line
	Color   string // the colour Claude Code gave it, such as blue
	Type    string // the agent definition it runs, such as an Explore
	Title   string // its terminal's title, without Claude Code's state glyph
	Working bool   // its title starts with Claude Code's spinner
	Dead    bool
	Exit    int // its exit status, once dead
}

// Format is the list-panes format Parse reads. The start command goes last:
// it is the one field that may hold a tab.
const Format = "#{session_name}\t#{pane_id}\t#{pane_pid}\t#{pane_dead}\t#{pane_dead_status}\t#{pane_title}\t#{pane_start_command}"

// Parse reads `list-panes -a -F Format` into each Irie session's
// teammates, in the order they started. Anchors are left out; every
// session that names an anchor is in the map, with no teammates or some.
func Parse(out string) map[string][]Teammate {
	teams := map[string][]Teammate{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.SplitN(line, "\t", 7)
		if len(f) != 7 {
			continue
		}
		id, teammate := Owner(f[0])
		if !teammate {
			if _, ok := teams[id]; !ok {
				teams[id] = nil
			}
			continue
		}
		start := unquote(f[6])
		t := Teammate{
			Session: f[0], Pane: f[1], Dead: f[3] == "1",
			Name: flag(start, "--agent-name"), Color: flag(start, "--agent-color"), Type: flag(start, "--agent-type"),
		}
		t.PID, _ = strconv.Atoi(f[2])
		t.Exit, _ = strconv.Atoi(f[4])
		t.Title, t.Working = title(f[5])
		if t.Name == "" {
			t.Name = t.Title
		}
		teams[id] = append(teams[id], t)
	}
	for id := range teams {
		sort.SliceStable(teams[id], func(i, j int) bool { return teams[id][i].Session < teams[id][j].Session })
	}
	return teams
}

// unquote undoes the quoting tmux gives pane_start_command: one pair of
// double quotes round it, and a backslash before each special character.
func unquote(s string) string {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return s
	}
	var b strings.Builder
	for i := 1; i < len(s)-1; i++ {
		if s[i] == '\\' && i+1 < len(s)-1 {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

var flagValue = regexp.MustCompile(`^\s+(?:'([^']*)'|"([^"]*)"|(\S+))`)

// flag returns the value after name in the shell command cmd, unquoted.
func flag(cmd, name string) string {
	for i := 0; ; {
		j := strings.Index(cmd[i:], name)
		if j < 0 {
			return ""
		}
		i += j + len(name)
		if m := flagValue.FindStringSubmatch(cmd[i:]); m != nil {
			return m[1] + m[2] + m[3]
		}
	}
}

// title cuts Claude Code's state glyph off a terminal title: a braille
// spinner while it works, ✳ while it waits.
func title(t string) (string, bool) {
	r, n := utf8.DecodeRuneInString(t)
	switch {
	case r >= 0x2800 && r <= 0x28FF:
		return strings.TrimSpace(t[n:]), true
	case r == '✳':
		return strings.TrimSpace(t[n:]), false
	}
	return t, false
}
