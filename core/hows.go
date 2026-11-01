package core

import (
	"fmt"
	"regexp"
	"strings"
)

// howsRe is a status question: "hows X", "how's X", "how is X going", "how
// are X doing", with an optional "?".
var howsRe = regexp.MustCompile(`(?i)^how(?:'s|’s|s| is| are)\s+(.+?)(?:\s+(?:going|doing|coming along|looking))?\s*\??$`)

// ParseHows reports whether input asks how a session is doing, and about
// which: "hows the parser?" gives "parser". It starts nothing.
func ParseHows(input string) (string, bool) {
	m := howsRe.FindStringSubmatch(strings.TrimSpace(input))
	if m == nil {
		return "", false
	}
	q := strings.TrimSpace(m[1])
	for _, p := range []string{"the ", "my ", "that "} {
		if len(q) > len(p) && strings.EqualFold(q[:len(p)], p) {
			q = q[len(p):]
		}
	}
	return q, q != ""
}

// FindSession picks the session a question is about: the best match of its
// words in the session's name, branch, repo, agent and profile, or in a
// subagent's type. Every word must match; a name match outranks the rest.
// It returns nil when nothing matches, and the first of equals, which is
// the first in the list's order.
func FindSession(sessions []Session, query string) *Session {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return nil
	}
	best, bestScore := -1, 0
	for i, s := range sessions {
		name := strings.ToLower(s.Name)
		other := strings.ToLower(s.Branch + " " + s.Repo + " " + s.Agent + " " + s.Profile)
		for _, a := range s.Subagents {
			other += " " + strings.ToLower(a.Type)
		}
		score := 0
		for _, w := range words {
			switch {
			case strings.Contains(name, w):
				score += 2
			case strings.Contains(other, w):
				score++
			default:
				score = -1
			}
			if score < 0 {
				break
			}
		}
		if score > bestScore {
			best, bestScore = i, score
		}
	}
	if best < 0 {
		return nil
	}
	return &sessions[best]
}

// Hows answers a status question in one line: the session's name, its
// state, what it is doing or needs, and its subagents.
func Hows(s Session) string {
	out := s.Name + ": " + strings.ReplaceAll(s.State, "_", " ")
	switch {
	case s.Reason != "":
		out += " · " + s.Reason
	case s.Detail != "":
		out += " · " + s.Detail
	}
	if s.ExitCode != nil && *s.ExitCode != 0 {
		out += fmt.Sprintf(" · exit %d", *s.ExitCode)
	}
	if n := len(s.Subagents); n > 0 {
		out += fmt.Sprintf(" · %d subagent", n)
		if n > 1 {
			out += "s"
		}
		for _, a := range s.Subagents {
			if a.NeedsYou {
				out += " (one needs you)"
				break
			}
		}
	}
	return out
}
