package core

import (
	"maps"
	"slices"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/skills"
)

// skillsFolder is the folder of Irie's skills that a session of profile p
// gets (internal/skills), or "" for none. A folder that cannot be made
// leaves the session without skills, and its warnings say why. The caller
// holds c.mu.
func (c *Core) skillsFolder(p config.Profile, x *session) string {
	cfg := c.cfg().Skills
	on := skills.On(skills.Choose(skills.Bundled(), skills.Settings{
		Enabled: cfg.Enabled, Disabled: cfg.Disabled, Profile: p.TakesSkills(),
	}))
	if len(on) == 0 {
		return ""
	}
	dir, err := skills.Build(c.paths.Skills(), on, c.version)
	if err != nil {
		c.log.Warn("skills", "session", x.id, "err", err)
		x.warnings = append(x.warnings, "Irie's skills left out: "+err.Error())
		return ""
	}
	return dir
}

// pruneSkills removes the skills folders that no session uses: what a
// session uses is the --plugin-dir of its spec's command line. It runs as
// the core starts, before the socket serves, so no launch makes a folder
// meanwhile. A spec it cannot read stops it, since that session's folder
// would go too.
func (c *Core) pruneSkills() {
	c.mu.Lock()
	ids := slices.Sorted(maps.Keys(c.sessions))
	c.mu.Unlock()
	var keep []string
	for _, id := range ids {
		spec, err := host.ReadSpec(c.paths.SpecFile(id))
		if err != nil {
			c.log.Warn("skills: not pruned", "session", id, "err", err)
			return
		}
		keep = append(keep, pluginDirs(spec.Argv)...)
	}
	removed, err := skills.Prune(c.paths.Skills(), keep)
	if len(removed) > 0 {
		c.log.Info("skills: pruned", "folders", len(removed))
	}
	if err != nil {
		c.log.Warn("skills: prune", "err", err)
	}
}

// pluginDirs are the folders a command line passes with --plugin-dir.
func pluginDirs(argv []string) []string {
	var out []string
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == "--plugin-dir" {
			out = append(out, argv[i+1])
			i++
		}
	}
	return out
}
