package plugins

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/skills"
)

// Skills of your own (DESIGN §4.17): a manifest's [skills.<id>] tables. A
// skill of yours is a folder of yours that holds a SKILL.md, which Irie
// hands agents' sessions with its own skills while its plugin is on, as
// /irie:<id>. It is copied whole into the sealed folder a session gets as
// it starts (internal/skills), so a change to it reaches the sessions that
// start after. Its words go to every agent, so a manifest with a skill is
// off until you turn its plugin on.

// skillFile is one of a manifest's [skills.<id>] tables.
type skillFile struct {
	Title string `toml:"title"`
	Path  string `toml:"path"`
}

// OwnSkill is a skill of yours, as its manifest names it.
type OwnSkill struct {
	Name     string // its table's id, the skill's name: /irie:<name> in a session
	Plugin   string // the plugin it comes with
	Dir      string // its folder, which holds its SKILL.md
	Manifest string // the manifest it comes from
	// Problem says why it can't go to sessions, in a sentence: a table
	// that doesn't read, or a name another skill has. "" = it can, once
	// its folder reads.
	Problem string
}

// Source is where a session's SOURCES.md, and irie skills, say the skill
// comes from.
func (s OwnSkill) Source() string { return "your plugin " + s.Plugin + ", from " + s.Dir }

// command is how a session runs the skill.
func (s OwnSkill) command() string { return "/" + skills.Plugin + ":" + s.Name }

// skillPart is the part a [skills.<id>] table makes.
func skillPart(p Plugin, id string, f skillFile) Part {
	title := line(f.Title, 40)
	if title == "" {
		title = id
	}
	s := &OwnSkill{Name: id, Plugin: p.Name, Manifest: p.Manifest}
	x := Part{Name: "skills." + id, Title: title, Brings: []string{"a skill for agents' sessions, " + s.command()}, own: always, Skill: s}
	x.Summary = "A skill of yours that agents' sessions get while this plugin is on, as " + s.command() + "."
	fails := func(why string) Part {
		s.Problem = why
		x.check = func(Env, bool) (bool, string) { return false, why }
		return x
	}
	if !manifestName.MatchString(id) || len(id) > 64 {
		return fails("[skills." + id + "] isn't a skill's name: lowercase letters, digits and single hyphens, at most 64, such as [skills.grants].")
	}
	if ownSkill(id) {
		return fails("One of " + brand.Name + "'s own skills is named " + id + ": give [skills." + id + "] another name.")
	}
	dir, ok := helperPath(f.Path)
	switch {
	case !ok:
		return fails("path = " + strconv.Quote(f.Path) + " isn't a full path to a folder: name it as ~/skills/" + id + ".")
	case dir == "":
		return fails("[skills." + id + "] has no path: path = \"~/skills/" + id + "\" names the folder that holds its SKILL.md.")
	}
	s.Dir = dir
	x.Summary = "A skill of yours that agents' sessions get while this plugin is on, as " + s.command() + ", from " + dir + "."
	x.check = func(Env, bool) (bool, string) { return checkSkill(*s) }
	return x
}

// ownSkill says whether one of Irie's own skills, in this build, is named
// name.
func ownSkill(name string) bool {
	for _, s := range skills.Bundled() {
		if s.Name == name {
			return true
		}
	}
	return false
}

// checkSkill says whether a skill of yours can go to sessions: its table
// reads and its folder holds a skill.
func checkSkill(s OwnSkill) (bool, string) {
	if s.Problem != "" {
		return false, s.Problem
	}
	if _, err := skills.Read(s.Dir, s.Name, s.Source()); err != nil {
		return false, "Sessions get it once its folder holds a skill: " + err.Error() + "."
	}
	return true, "Sessions that start now get " + s.command() + "."
}

// OwnSkills are your skills, from the manifests in the plugins folder, in
// the order of their files and then their names, each with whether its
// plugin is on. A manifest that doesn't read brings none.
func OwnSkills(e Env) []OwnSkillOn {
	mine, _ := Mine(e.Paths.Plugins())
	var out []OwnSkillOn
	for _, p := range mine {
		if p.problem != "" {
			continue
		}
		on := p.on(e.Config)
		for _, x := range p.Parts {
			if x.Skill != nil {
				out = append(out, OwnSkillOn{OwnSkill: *x.Skill, On: on, key: p.key})
			}
		}
	}
	return out
}

// OwnSkillOn is a skill of yours with whether its plugin is on.
type OwnSkillOn struct {
	OwnSkill
	On  bool
	key string // its plugin's switch
}

// SessionSkills are the skills a session may get, Irie's own then yours,
// and the settings that choose among them for a session whose profile
// takes skills or not (skills.Choose). A skill of yours goes in whole, as
// its folder holds it now; one whose plugin is off, whose table doesn't
// read or whose folder holds no skill is left out, and says why.
func SessionSkills(e Env, profile bool) ([]skills.Skill, skills.Settings) {
	all := skills.Bundled()
	st := SkillSettings(e.Config, profile)
	for _, s := range OwnSkills(e) {
		if s.Problem != "" {
			// A name another skill has first stays that skill's: leaving
			// this one out by name would leave the other out too.
			if s.Name != "" && !ownSkill(s.Name) && !hasSkill(all, s.Name) {
				all = append(all, skills.Skill{Name: s.Name, Source: s.Source(), Mine: true})
				st.Left[s.Name] = strings.TrimSuffix(lower(s.Problem), ".")
			}
			continue
		}
		if !s.On {
			all = append(all, skills.Skill{Name: s.Name, Source: s.Source(), Mine: true})
			st.Left[s.Name] = fmt.Sprintf("the %s plugin is off (%s)", s.Plugin, s.key)
			continue
		}
		sk, err := skills.Read(s.Dir, s.Name, s.Source())
		if err != nil {
			all = append(all, skills.Skill{Name: s.Name, Source: s.Source(), Mine: true})
			st.Left[s.Name] = "its folder holds no skill: " + err.Error()
			continue
		}
		all = append(all, sk)
	}
	return all, st
}

func hasSkill(all []skills.Skill, name string) bool {
	for _, s := range all {
		if s.Name == name {
			return true
		}
	}
	return false
}

// lower is a sentence with its first letter made small, to follow a colon.
func lower(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}
