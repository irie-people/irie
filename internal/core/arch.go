package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/irie-people/irie/internal/arch"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/harness"
)

// archFor is what Irie's arch agents bring to a session of profile p (DESIGN
// §4.11), which the adapter gives as far as its CLI can take it. Only
// Claude Code sessions have them yet. With arch.mikael.on, Mikael goes with
// every one; with arch.mikael.door too, he weighs the calls at its door,
// except for a session on a local model, where the small model that would
// weigh them is the local one, and one that runs as an arch agent itself.
func (c *Core) archFor(p config.Profile, harnessID string) harness.Arch {
	if harnessID != "claude-code" {
		return harness.Arch{}
	}
	m := c.cfg().Arch.Mikael
	a := harness.Arch{As: p.Arch, Good: slices.Clone(m.Good)}
	if m.On {
		a.With = []string{"mikael"}
		a.Door = m.Door && p.Local == nil && p.Arch == ""
	}
	return a
}

// archWord is what the arch agents with a session say as it, or one of its
// subagents, starts (DESIGN §4.11), by what the session took
// (harness.ArchFile); "" for any other event. A session or subagent that is
// an arch agent itself hears nothing, and a subagent, which cannot start
// agents of its own, is not told to call on them. It reads the file, so the
// caller does not hold c.mu.
func (c *Core) archWord(id string, evs []event.Event) string {
	start, sub, agentType := false, false, ""
	for _, e := range evs {
		switch {
		case e.Kind == event.SessionStarted:
			start = true
		case e.Kind == event.SubagentStarted:
			sub, agentType = true, e.Str("agent_type")
		}
	}
	if !start && !sub {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(c.paths.Session(id), harness.ArchFile))
	if err != nil {
		return ""
	}
	var a harness.Arch
	if err := json.Unmarshal(b, &a); err != nil {
		c.log.Warn("arch", "session", id, "err", err)
		return ""
	}
	if _, isArch := arch.Get(agentType); start && a.As != "" || sub && isArch {
		return ""
	}
	var words []string
	for _, name := range a.With {
		ag, ok := arch.Get(name)
		if !ok {
			continue
		}
		w := ag.Word(arch.WordData{OnCall: a.OnCall && !sub, Door: a.Door && ag.Door != nil, Good: a.Good})
		if w != "" {
			words = append(words, w)
		}
	}
	return strings.Join(words, "\n\n")
}
