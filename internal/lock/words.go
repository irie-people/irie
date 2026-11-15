package lock

import (
	"fmt"

	"github.com/irie-people/irie/internal/ipc"
)

// Words say what the last check found for p, for a card, the part and its
// version first so that a narrow card keeps them: "Claude Code 2.1.250: 1
// advisory, fixed in 2.1.251", "MCP server github 0.6.2: 2 advisories, no
// fix yet", "… 3 advisories, 2 fixed in 2.1.251". "" when none affects it.
func Words(p ipc.LockPackage) string {
	if p.Advisories == 0 {
		return ""
	}
	return fmt.Sprintf("%s %s: %s, %s", partName(p), p.Version, Advisories(p.Advisories), fixWords(p))
}

// Advisories is n advisories, in words: "1 advisory", "3 advisories".
func Advisories(n int) string {
	if n == 1 {
		return "1 advisory"
	}
	return fmt.Sprintf("%d advisories", n)
}

// fixWords say which version fixes p's advisories.
func fixWords(p ipc.LockPackage) string {
	switch {
	case p.Unfixed == p.Advisories:
		return "no fix yet"
	case p.Unfixed == 0:
		return "fixed in " + p.Fixed
	}
	return fmt.Sprintf("%d fixed in %s", p.Advisories-p.Unfixed, p.Fixed)
}

// partName is what p is in its session, or its own name.
func partName(p ipc.LockPackage) string {
	if p.Part != "" {
		return p.Part
	}
	return p.Name
}

// Found says what a check found for one version, for irie lock: "2
// advisories, fixed in 2.1.251", "no known advisory", or "not checked
// yet".
func Found(p ipc.LockPackage) string {
	switch {
	case !p.Checked:
		return "not checked yet"
	case p.Advisories == 0:
		return "no known advisory"
	}
	return Advisories(p.Advisories) + ", " + fixWords(p)
}
