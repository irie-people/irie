package core

import (
	"strings"

	"github.com/irie-people/irie/internal/harness"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/skills"
)

// A guide is a Claude Code session that runs one of Irie's own skills, which
// the user starts from any task bar: `:setup switch` runs the dead man's
// switch guide (docs/proposals/extensions.md §2.2). It gets Irie's plugin as
// every Claude Code session does (skills.go); the guide first checks that
// its skill will be in it, and says why when it won't.
type guideSpec struct {
	skill   string // the skill it runs, by name
	name    string // the session's name
	profile string // the profile it runs on
}

// guideSwitch runs on +claude, the most careful profile: the session
// decides who can reach everything a person owns.
var guideSwitch = guideSpec{skill: "dead-mans-switch", name: "Dead Man's Switch", profile: "claude"}

// guide starts g's session, with the skill's command as its task.
func (c *Core) guide(g guideSpec) (ipc.Session, error) {
	profile, ok := c.profile(g.profile)
	if !ok || profile.Harness != "claude-code" {
		return ipc.Session{}, ipc.Errorf(ipc.CodeNotFound, "The %s guide runs on +%s, a Claude Code profile, which your config has not got.", g.name, g.profile)
	}
	cfg := c.cfg().Skills
	var choice *skills.Choice
	for _, ch := range skills.Choose(skills.Bundled(), skills.Settings{Enabled: cfg.Enabled, Disabled: cfg.Disabled, Profile: profile.TakesSkills()}) {
		if ch.Name == g.skill {
			choice = &ch
		}
	}
	switch {
	case choice == nil:
		return ipc.Session{}, ipc.Errorf(ipc.CodeNotFound, "This Irie has no skill called %s.", g.skill)
	case !choice.On:
		return ipc.Session{}, ipc.Errorf(ipc.CodeBadArgs, "The %s guide is Irie's skill %s, which is off here: %s.", g.name, g.skill, choice.Why)
	}
	// The CLI must load Irie's plugin, which its probe checks once per
	// binary; the probe's warning says why not.
	h, err := harnessFor(profile.Harness)
	if err != nil {
		return ipc.Session{}, err
	}
	if p, ok := h.(harness.Prober); ok {
		probe := p.Probe(profile, c.agentEnv(profile))
		if !probe.Flags["--plugin-dir"] {
			why := "this Claude Code takes no --plugin-dir"
			for _, w := range probe.Warnings {
				if strings.Contains(w, "skills") || strings.Contains(w, "--plugin-dir") {
					why = w
				}
			}
			return ipc.Session{}, ipc.Errorf(ipc.CodeBadArgs, "The %s guide needs Claude Code to load Irie's skills, and it doesn't here: %s. `claude update` may help.", g.name, why)
		}
	}
	task := "/" + skills.Plugin + ":" + g.skill
	return c.spawnProfile(ipc.SpawnArgs{Task: task, Name: g.name}, true, g.profile, profile, nil, map[string]any{"guide": g.skill})
}
