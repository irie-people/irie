package core

import (
	"slices"
	"strings"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/learn"
)

// tutorFor is the tutor mode a spawn runs in (learn.tutor,
// docs/proposals/learning.md): its own, else the config's. A subtask an
// agent delegates, and a tool, run off unless they say otherwise: their
// work is not the user's to learn from. "" is off.
func (c *Core) tutorFor(a ipc.SpawnArgs, harnessID string) (string, error) {
	t := a.Tutor
	switch {
	case t != "" && !slices.Contains(config.TutorModes, t):
		return "", ipc.Errorf(ipc.CodeBadArgs, "tutor %q: must be off, explain or teach", t)
	case t == "" && (a.Delegate || harnessID == "tool"):
		t = config.TutorOff
	case t == "":
		t = c.cfg().Learn.Tutor
	}
	if t == config.TutorOff {
		return "", nil
	}
	return t, nil
}

// tutorNotes are what the + list says of the tutor words, beside the
// profiles: each sets the tutor mode of one task.
var tutorNotes = map[string]string{
	config.TutorExplain: "tutor mode for one task: it explains why it does what it does · longer replies",
	config.TutorTeach:   "tutor mode for one task: it leaves the key parts for you to write · longer replies, and your time",
}

// maxTurnLine bounds the request a "your turn" card shows.
const maxTurnLine = 160

// yourTurn reports whether a teach-mode turn's last message asks the user
// to write a part at a TODO(human), and what it asks, in a line: the
// request's "Your Task" line when it has one (Claude Code's Learning style,
// docs/proposals/learning.md §4.4).
func yourTurn(msg string) (string, bool) {
	if !strings.Contains(msg, "TODO(human)") {
		return "", false
	}
	plain := strings.NewReplacer("**", "", "__", "", "`", "")
	for _, l := range strings.Split(msg, "\n") {
		l = plain.Replace(l)
		if k := strings.Index(strings.ToLower(l), "your task:"); k >= 0 {
			if t := cleanText(l[k+len("your task:"):], maxTurnLine); t != "" {
				return t, true
			}
		}
	}
	return "write the part it left for you at TODO(human)", true
}

// learnOp answers the learn op: what the Help menu shows. With a session,
// the tutorials of its project's languages come first.
func (c *Core) learnOp(a ipc.LearnArgs) (ipc.LearnResult, error) {
	var dir string
	if a.Session != "" {
		c.mu.Lock()
		x := c.sessions[a.Session]
		if x != nil {
			dir = x.cwd
		}
		c.mu.Unlock()
		if x == nil {
			return ipc.LearnResult{}, ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Session)
		}
	}
	return ipc.LearnResult{
		Resources: learn.Resources(c.cfg().Learn.Links, learn.Languages(dir)),
		Glossary:  learn.Get().Glossary,
	}, nil
}

// turnAsked reports whether a teach-mode turn's last message asks the user
// to write a part (yourTurn).
func turnAsked(msg string) bool {
	_, ok := yourTurn(msg)
	return ok
}
