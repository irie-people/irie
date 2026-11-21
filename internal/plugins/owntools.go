package plugins

import (
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/irie-people/irie/internal/brand"
)

// Tools of your own (DESIGN §4.17): a manifest's [tools.<id>] tables. Tools
// of yours are an MCP server of yours, a program that speaks MCP over its
// standard input and output, which Irie hands agents' sessions with its own
// servers while its plugin is on. The session's agent starts it as the
// session starts, and Claude Code names its tools mcp__<id>__<tool>. An
// anonymous research session never gets one, as it gets no server but
// Irie's (DESIGN §4.16). Every session runs it, so a manifest with tools is
// off until you turn its plugin on.

// toolsFile is one of a manifest's [tools.<id>] tables.
type toolsFile struct {
	Title string   `toml:"title"`
	Run   []string `toml:"run"`
}

// OwnTools are tools of yours, as their manifest names them.
type OwnTools struct {
	Name     string   // its table's id, the server's name, as tools are mcp__<name>__…
	Plugin   string   // the plugin it comes with
	Title    string   // its name in Settings
	Run      []string // the server's program and its arguments
	Manifest string   // the manifest it comes from
	// Problem says why it can't go to sessions, in a sentence: a table
	// that doesn't read, or a name another server has. "" = it can, once
	// its program is there.
	Problem string
	On      bool // its plugin is on (Tools)
}

// toolsPart is the part a [tools.<id>] table makes.
func toolsPart(p Plugin, id string, f toolsFile) Part {
	title := line(f.Title, 40)
	if title == "" {
		title = id
	}
	t := &OwnTools{Name: id, Plugin: p.Name, Title: title, Manifest: p.Manifest}
	x := Part{Name: "tools." + id, Title: title, Brings: []string{"tools for agents' sessions, mcp__" + id + "__…"}, own: always, Tools: t}
	x.Summary = "Tools of yours, an MCP server, that agents' sessions get while this plugin is on, as mcp__" + id + "__…."
	fails := func(why string) Part {
		t.Problem = why
		x.check = func(Env, bool) (bool, string) { return false, why }
		return x
	}
	switch {
	case !manifestName.MatchString(id) || len(id) > 64:
		return fails("[tools." + id + "] isn't a server's name: lowercase letters, digits and single hyphens, at most 64, such as [tools." + p.Name + "].")
	case id == brand.Slug || strings.HasPrefix(id, brand.Slug+"-"):
		return fails(brand.Name + "'s own servers have names that start " + brand.Slug + ": give [tools." + id + "] another name.")
	case len(f.Run) == 0:
		return fails("[tools." + id + "] has no run: run = [\"~/bin/" + id + "\", \"mcp\"] names the program that serves its tools.")
	}
	run, why := runWords(f.Run, `run = ["~/bin/`+id+`", "mcp"]`)
	if why != "" {
		return fails(why)
	}
	t.Run = run
	x.Summary = "Tools of yours, an MCP server, that agents' sessions get while this plugin is on, as mcp__" + id + "__…, from " + filepath.Base(run[0]) + "."
	x.check = func(Env, bool) (bool, string) { return checkTools(*t) }
	return x
}

// checkTools says whether tools of yours can go to sessions: their table
// reads and their program is on this computer.
func checkTools(t OwnTools) (bool, string) {
	if t.Problem != "" {
		return false, t.Problem
	}
	if _, err := ToolsProgram(t); err != nil {
		return false, err.Error()
	}
	return true, "Sessions that start now get its tools, mcp__" + t.Name + "__…, from " + filepath.Base(t.Run[0]) + "."
}

// ToolsProgram is the program tools of yours run, found as a shell would
// find it: a full path, or a name on the PATH.
func ToolsProgram(t OwnTools) (string, error) {
	if len(t.Run) == 0 {
		return "", helperError("The tools have no program to run.")
	}
	path, err := exec.LookPath(t.Run[0])
	if err != nil {
		return "", helperError(strconv.Quote(t.Run[0]) + " isn't a program on this computer, so sessions can't start its tools: install it, or give its full path in run.")
	}
	return path, nil
}

// Tools are your tools, from the manifests in the plugins folder, in the
// order of their files and then their names, each with whether its plugin
// is on. A manifest that doesn't read brings none.
func Tools(e Env) []OwnTools {
	mine, _ := Mine(e.Paths.Plugins())
	var out []OwnTools
	for _, p := range mine {
		if p.problem != "" {
			continue
		}
		on := p.on(e.Config)
		for _, x := range p.Parts {
			if x.Tools == nil {
				continue
			}
			t := *x.Tools
			t.On = on
			t.Run = slices.Clone(t.Run)
			out = append(out, t)
		}
	}
	return out
}

// firstNames gives each skill's name, each server's and each word's to the
// first of your plugins that has it, in the order of their files: a later
// one with the same name can't go to sessions, or start one, and its part
// says why.
func firstNames(ps []Plugin) {
	skillsBy, toolsBy, wordsBy := map[string]string{}, map[string]string{}, map[string]string{}
	for i := range ps {
		for j := range ps[i].Parts {
			x := &ps[i].Parts[j]
			switch {
			case x.Skill != nil && x.Skill.Problem == "":
				if first, taken := skillsBy[x.Skill.Name]; taken {
					why := "Your plugin " + first + " has a skill named " + x.Skill.Name + " first: give [skills." + x.Skill.Name + "] another name."
					s := *x.Skill
					s.Problem = why
					x.Skill = &s
					x.check = func(Env, bool) (bool, string) { return false, why }
					continue
				}
				skillsBy[x.Skill.Name] = ps[i].Name
			case x.Tools != nil && x.Tools.Problem == "":
				if first, taken := toolsBy[x.Tools.Name]; taken {
					why := "Your plugin " + first + " has tools named " + x.Tools.Name + " first: give [tools." + x.Tools.Name + "] another name."
					t := *x.Tools
					t.Problem = why
					x.Tools = &t
					x.check = func(Env, bool) (bool, string) { return false, why }
					continue
				}
				toolsBy[x.Tools.Name] = ps[i].Name
			case x.Word != nil && x.Word.Problem == "":
				if first, taken := wordsBy[x.Word.Name]; taken {
					why := "Your plugin " + first + " has the word :" + x.Word.Name + " first: give [words." + x.Word.Name + "] another name."
					w := *x.Word
					w.Problem = why
					x.Word = &w
					x.check = func(Env, bool) (bool, string) { return false, why }
					continue
				}
				wordsBy[x.Word.Name] = ps[i].Name
			}
		}
	}
}
