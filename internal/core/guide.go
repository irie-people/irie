package core

import (
	"os"
	"slices"

	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/skills"
)

// A guide is a Claude Code session that runs one of Irie's own skills, which
// the user starts from any task bar: `:setup switch` runs the dead man's
// switch guide (docs/proposals/extensions.md §2.2). For now a guide is the
// only session that gets Irie's plugin folder (§1.2); others get none.
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
	s, err := c.skill(g.skill)
	if err != nil {
		return ipc.Session{}, err
	}
	profile, ok := c.profile(g.profile)
	if !ok || profile.Harness != "claude-code" {
		return ipc.Session{}, ipc.Errorf(ipc.CodeNotFound, "The %s guide runs on +%s, a Claude Code profile, which your config has not got.", g.name, g.profile)
	}
	return c.spawnProfile(ipc.SpawnArgs{Task: s.Command(), Name: g.name}, true, g.profile, profile, nil, map[string]any{"guide": g.skill}, true)
}

// skillsOn reports whether Irie hands its skills to agents at all, and why
// not: [skills] enabled, or IRIE_SKILLS=off where the core started.
func (c *Core) skillsOn() (bool, string) {
	switch {
	case os.Getenv("IRIE_SKILLS") == "off":
		return false, "IRIE_SKILLS=off was set where the core started"
	case !c.cfg().Skills.Enabled:
		return false, "[skills] enabled = false in config.toml"
	}
	return true, ""
}

// skill returns Irie's own skill called name, when it is on.
func (c *Core) skill(name string) (skills.Skill, error) {
	all, err := skills.Builtin()
	if err != nil {
		return skills.Skill{}, err
	}
	s, ok := skills.Find(all, name)
	if !ok {
		return skills.Skill{}, ipc.Errorf(ipc.CodeNotFound, "This Irie has no skill called %s.", name)
	}
	if on, why := c.skillsOn(); !on {
		return skills.Skill{}, ipc.Errorf(ipc.CodeBadArgs, "Irie's skills are off: %s.", why)
	}
	if slices.Contains(c.cfg().Skills.Disabled, name) {
		return skills.Skill{}, ipc.Errorf(ipc.CodeBadArgs, "The %s skill is off: [skills] disabled names it in config.toml.", name)
	}
	return s, nil
}

// pluginDir makes the plugin folder that holds every skill that is on, and
// returns its path.
func (c *Core) pluginDir() (string, error) {
	all, err := skills.Builtin()
	if err != nil {
		return "", err
	}
	on, _ := c.skillsOn()
	set := skills.Enabled(all, on, c.cfg().Skills.Disabled)
	return skills.Folder(c.paths.SkillPlugins(), set)
}

// pruneSkills removes the plugin folders that no session in a pane uses
// (docs/proposals/skills.md §1.1). A closed guide that comes back gets the
// folder of the set of the day.
func (c *Core) pruneSkills() {
	c.mu.Lock()
	ids := make([]string, 0, len(c.sessions))
	for id := range c.sessions {
		ids = append(ids, id)
	}
	c.mu.Unlock()
	keep := map[string]bool{}
	for _, id := range ids {
		spec, err := host.ReadSpec(c.paths.SpecFile(id))
		if err != nil {
			continue
		}
		if i := slices.Index(spec.Argv, "--plugin-dir"); i >= 0 && i+1 < len(spec.Argv) {
			keep[spec.Argv[i+1]] = true
		}
	}
	if err := skills.Prune(c.paths.SkillPlugins(), keep); err != nil {
		c.log.Warn("skills", "err", err)
	}
}
