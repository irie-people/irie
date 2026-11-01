package main

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/irie-people/irie/internal/skills"
	"github.com/irie-people/irie/internal/version"
)

// skillsCmd is `irie skills`: the skills that come with Irie, whether
// sessions get each, and why not (docs/proposals/skills.md §5.2).
func skillsCmd(args []string) error {
	if len(args) > 0 {
		return errors.New("usage: irie skills")
	}
	cfg, _, err := load()
	if err != nil {
		return err
	}
	fmt.Printf("Skills that come with Irie. Each Claude Code session gets them as a plugin\nfor that session only, and runs them as /%s:<name>.\n\n", skills.Plugin)
	choices := skills.Choose(skills.Bundled(), skills.Settings{Enabled: cfg.Skills.Enabled, Disabled: cfg.Skills.Disabled, Profile: true})
	width := 0
	for _, c := range choices {
		width = max(width, len(skills.Plugin)+len(c.Name)+2)
	}
	for _, c := range choices {
		state := "on"
		if !c.On {
			state = "off: " + c.Why
		}
		fmt.Printf("%-*s  %s\n", width, "/"+skills.Plugin+":"+c.Name, state)
		fmt.Printf("%-*s  %s\n", width, "", c.Description)
		fmt.Printf("%-*s  %s %s\n", width, "", c.Source, version.String())
	}
	var none []string
	for _, name := range slices.Sorted(maps.Keys(cfg.Profiles)) {
		if p := cfg.Profiles[name]; p.Harness == "claude-code" && !p.TakesSkills() {
			none = append(none, name)
		}
	}
	if len(none) > 0 {
		fmt.Printf("\nProfiles that take none (skills = false): %s.\n", strings.Join(none, ", "))
	}
	fmt.Println("\nA session gets them only when its Claude Code loads a plugin passed with\n--plugin-dir; when it does not, the session's warnings say why.")
	return nil
}
