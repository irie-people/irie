package main

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/skills"
)

// skillsCmd is `irie skills`: each of Irie's own skills, whether it is on
// and why, and how to start it (docs/proposals/extensions.md §1.2). It reads
// the config and needs no core.
func skillsCmd(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("irie skills takes no arguments; turn one off with [skills] disabled in %s", configPathHint())
	}
	cfg, _, err := load()
	if err != nil {
		return err
	}
	all, err := skills.Builtin()
	if err != nil {
		return err
	}
	return listSkills(os.Stdout, all, cfg.Skills, os.Getenv("IRIE_SKILLS"))
}

func listSkills(w io.Writer, all []skills.Skill, cfg config.Skills, env string) error {
	for _, s := range all {
		state := "on"
		switch {
		case env == "off":
			state = "off: IRIE_SKILLS=off"
		case !cfg.Enabled:
			state = "off: [skills] enabled = false"
		case slices.Contains(cfg.Disabled, s.Name):
			state = "off: [skills] disabled names it"
		}
		start := s.Command()
		if s.Name == "dead-mans-switch" {
			start = ":setup switch, or " + start + " in a guide's session"
		}
		desc, _, _ := strings.Cut(s.Description, ". ")
		fmt.Fprintf(w, "%s (%s)\n  %s.\n  Built into Irie. Start it with %s.\n", s.Name, state, strings.TrimSuffix(desc, "."), start)
	}
	return nil
}

func configPathHint() string {
	_, p, err := load()
	if err != nil {
		return "config.toml"
	}
	return p.ConfigFile()
}
