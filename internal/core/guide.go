package core

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/harness"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/plugins"
	"github.com/irie-people/irie/internal/skills"
)

// A guide is a Claude Code session that runs one of Irie's own skills, which
// the user starts from any task bar: `:setup switch` runs the dead man's
// switch guide (docs/proposals/extensions.md §2.2), and `:write` a writing
// session in Variorum (docs/proposals/writers.md). It gets Irie's plugin as
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

// guideWrite runs on +claude too: it reads what the writer shares with it
// in Variorum, which is theirs to choose, and writes beside their words.
var guideWrite = guideSpec{skill: "variorum", name: "Variorum", profile: "claude"}

// guide starts g's session, with the skill's command as its task, and after
// it what the person wants done, when they said. Such a task names the
// session until Claude Code titles it, as a typed task does.
func (c *Core) guide(g guideSpec, want string) (ipc.Session, error) {
	profile, ok := c.profile(g.profile)
	if !ok || profile.Harness != "claude-code" {
		return ipc.Session{}, ipc.Errorf(ipc.CodeNotFound, "The %s guide runs on +%s, a Claude Code profile, which your config has not got.", g.name, g.profile)
	}
	if err := c.skillCheck("The "+g.name+" guide", profile, g.skill); err != nil {
		return ipc.Session{}, err
	}
	// A folder of its own, the same every time, so Claude Code asks once
	// whether to trust it rather than at every guide; what the guide makes
	// goes in the folder the person picks.
	dir := c.guideDir(g)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ipc.Session{}, err
	}
	task, name, named := "/"+skills.Plugin+":"+g.skill, g.name, true
	if want = strings.TrimSpace(want); want != "" {
		task, name, named = task+" "+want, title(want), false
	}
	return c.spawnProfile(ipc.SpawnArgs{Task: task, Name: name, Cwd: dir}, named, g.profile, profile, nil, map[string]any{"guide": g.skill})
}

// guideDir is where g's sessions run: <data>/guides/<skill>.
func (c *Core) guideDir(g guideSpec) string { return filepath.Join(c.paths.Data, "guides", g.skill) }

// skillCheck says why a session of profile, a Claude Code profile, could
// not run skill, one of Irie's own: no such skill, the skill is off, or the
// profile's Claude Code loads no plugin. who is what wants the skill, as
// the error names it, such as "The Variorum guide" or "+chaos". It probes
// the CLI, so the caller does not hold c.mu.
func (c *Core) skillCheck(who string, profile config.Profile, skill string) error {
	var choice *skills.Choice
	for _, ch := range skills.Choose(skills.Bundled(), plugins.SkillSettings(*c.cfg(), profile.TakesSkills())) {
		if ch.Name == skill {
			choice = &ch
		}
	}
	switch {
	case choice == nil:
		return ipc.Errorf(ipc.CodeNotFound, "This "+brand.Name+" has no skill called %s.", skill)
	case !choice.On:
		return ipc.Errorf(ipc.CodeBadArgs, "%s runs "+brand.Name+"'s skill %s, which is off here: %s.", who, skill, choice.Why)
	}
	// The CLI must load Irie's plugin, which its probe checks once per
	// binary; the probe's warning says why not.
	h, err := harnessFor(profile.Harness)
	if err != nil {
		return err
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
			return ipc.Errorf(ipc.CodeBadArgs, "%s needs Claude Code to load "+brand.Name+"'s skills, and it doesn't here: %s. `claude update` may help.", who, why)
		}
	}
	return nil
}

// onSkill is the task a session of profile starts with, when the profile
// starts on one of Irie's skills (profiles.<name>.skill, DESIGN §4.18): the
// skill's command, then the task, which Claude Code hands the skill. A task
// that already runs the skill, as one carried on from such a session does,
// is kept as it is.
func onSkill(profile config.Profile, task string) string {
	if profile.Skill == "" {
		return task
	}
	cmd := "/" + skills.Plugin + ":" + profile.Skill
	if t := strings.TrimSpace(task); t == cmd || strings.HasPrefix(t, cmd+" ") || strings.HasPrefix(t, cmd+"\n") {
		return task
	}
	return cmd + " " + task
}
