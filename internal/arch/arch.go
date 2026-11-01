// Package arch holds Irie's arch agents (DESIGN §4.11): helpers that go
// with every session Irie starts. The first is Mikael, who stands with
// the user against evil and for the good they want to see in the world.
//
// Each arch agent is a few markdown files in agents/, which anyone can read
// and change without knowing Go:
//
//   - <name>.md: a few fields, then his charge, the prompt he works by when
//     an agent calls on him or a session runs as him;
//   - <name>.word.md: what every session hears from him as it starts, a Go
//     template of WordData;
//   - <name>.door.md: the calls he weighs before they run, as permission
//     rules, then the prompt he weighs each one by.
package arch

import (
	"bytes"
	"cmp"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"text/template"
)

//go:embed agents/*.md
var files embed.FS

// Agent is one arch agent.
type Agent struct {
	Name        string   // such as mikael
	Rank        int      // the order arch agents come in, Mikael first
	Title       string   // his work in a few words, such as a helper against evil
	Description string   // when an agent should call on him, as Claude Code's agent list shows it
	Tools       []string // the tools he may use; none means every tool
	Color       string   // his colour in Claude Code's task list
	Charge      string   // the prompt he works by
	Door        *Door    // nil: he keeps no door

	word *template.Template // nil: he says nothing as a session starts
}

// Door is what an arch agent weighs before it runs. Each tool call that
// matches one of its rules goes, with the prompt, to the small model Claude
// Code keeps for background work, which lets the call run or stops it with
// a reason the agent reads (a prompt hook).
type Door struct {
	Rules   []string // permission rules, such as Bash(curl *), Read(~/.ssh/**) or WebFetch
	Prompt  string   // the prompt, with $ARGUMENTS where the call goes
	Status  string   // what Claude Code's spinner says while he weighs it
	Timeout int      // seconds; a judgment that takes longer lets the call run
}

// WordData is what a word can say about the session it goes to.
type WordData struct {
	OnCall bool     // the agent can call on him
	Door   bool     // he weighs calls at the session's door
	Good   []string // the good the user wants to see in the world, in their words
}

// Word is what the agent says to a session as it starts; "" when he says
// nothing.
func (a Agent) Word(d WordData) string {
	if a.word == nil {
		return ""
	}
	var b strings.Builder
	if err := a.word.Execute(&b, d); err != nil {
		return ""
	}
	return strings.TrimSpace(b.String())
}

var (
	loadOnce sync.Once
	loaded   []Agent
	loadErr  error
)

// All returns every arch agent, by rank. The files are part of Irie, so one
// that does not read is a bug, which the package's tests catch.
func All() []Agent {
	loadOnce.Do(func() { loaded, loadErr = load(files) })
	if loadErr != nil {
		panic(loadErr)
	}
	return slices.Clone(loaded)
}

// Get returns the arch agent called name.
func Get(name string) (Agent, bool) {
	for _, a := range All() {
		if a.Name == name {
			return a, true
		}
	}
	return Agent{}, false
}

// Names returns every arch agent's name, by rank.
func Names() []string {
	var names []string
	for _, a := range All() {
		names = append(names, a.Name)
	}
	return names
}

// Definitions is the value of Claude Code's --agents flag that puts the
// named arch agents on call, by name. When the user wrote the good they
// want to see in the world, each charge ends with it, in their words.
func Definitions(names []string, good []string) (string, error) {
	type definition struct {
		Description string   `json:"description"`
		Prompt      string   `json:"prompt"`
		Tools       []string `json:"tools,omitempty"`
		Color       string   `json:"color,omitempty"`
	}
	defs := map[string]definition{}
	for _, name := range names {
		a, ok := Get(name)
		if !ok {
			return "", fmt.Errorf("arch: no arch agent %q", name)
		}
		prompt := a.Charge
		if len(good) > 0 {
			prompt += "\n\n## The good your user wants to see in the world\n"
			for _, g := range good {
				prompt += "\n- " + g
			}
		}
		defs[name] = definition{Description: a.Description, Prompt: prompt, Tools: a.Tools, Color: a.Color}
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(defs); err != nil {
		return "", err
	}
	return strings.TrimSpace(b.String()), nil
}

// names an arch agent may have: they are Claude Code agent names and
// profile names too.
var agentName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// rule is the shape of a permission rule: a tool name, and its pattern in
// parentheses.
var rule = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\(.+\))?$`)

// load reads every arch agent from fsys.
func load(fsys fs.FS) ([]Agent, error) {
	paths, err := fs.Glob(fsys, "agents/*.md")
	if err != nil {
		return nil, err
	}
	var agents []Agent
	for _, p := range paths {
		base := strings.TrimSuffix(strings.TrimPrefix(p, "agents/"), ".md")
		if strings.Contains(base, ".") {
			continue // a part of an agent, read with it
		}
		a, err := loadAgent(fsys, base)
		if err != nil {
			return nil, fmt.Errorf("arch: %s: %w", p, err)
		}
		agents = append(agents, a)
	}
	for _, p := range paths {
		base := strings.TrimSuffix(strings.TrimPrefix(p, "agents/"), ".md")
		name, part, ok := strings.Cut(base, ".")
		if !ok {
			continue
		}
		if !slices.ContainsFunc(agents, func(a Agent) bool { return a.Name == name }) || part != "word" && part != "door" {
			return nil, fmt.Errorf("arch: %s: no such arch agent or part; parts are <name>.word.md and <name>.door.md", p)
		}
	}
	slices.SortFunc(agents, func(a, b Agent) int { return cmp.Or(cmp.Compare(a.Rank, b.Rank), cmp.Compare(a.Name, b.Name)) })
	return agents, nil
}

func loadAgent(fsys fs.FS, name string) (Agent, error) {
	data, err := fs.ReadFile(fsys, "agents/"+name+".md")
	if err != nil {
		return Agent{}, err
	}
	fields, _, body, err := frontmatter(string(data))
	if err != nil {
		return Agent{}, err
	}
	a := Agent{Name: fields["name"], Title: fields["title"], Description: fields["description"], Color: fields["color"], Charge: body}
	switch {
	case a.Name != name:
		return Agent{}, fmt.Errorf("name %q must be the file's name, %q", a.Name, name)
	case !agentName.MatchString(a.Name):
		return Agent{}, fmt.Errorf("name %q: use lower-case letters, digits and dashes", a.Name)
	case a.Description == "" || a.Charge == "":
		return Agent{}, fmt.Errorf("an arch agent needs a description and a charge")
	}
	if r := fields["rank"]; r != "" {
		if a.Rank, err = strconv.Atoi(r); err != nil {
			return Agent{}, fmt.Errorf("rank %q: must be a whole number", r)
		}
	}
	for _, t := range strings.Split(fields["tools"], ",") {
		if t = strings.TrimSpace(t); t != "" {
			a.Tools = append(a.Tools, t)
		}
	}
	if data, err := fs.ReadFile(fsys, "agents/"+name+".word.md"); err == nil {
		if a.word, err = template.New(name + ".word.md").Option("missingkey=error").Parse(string(data)); err != nil {
			return Agent{}, err
		}
	}
	if data, err := fs.ReadFile(fsys, "agents/"+name+".door.md"); err == nil {
		fields, lists, body, err := frontmatter(string(data))
		if err != nil {
			return Agent{}, fmt.Errorf("%s.door.md: %w", name, err)
		}
		d := &Door{Rules: lists["rules"], Prompt: body, Status: fields["status"], Timeout: 30}
		if t := fields["timeout"]; t != "" {
			if d.Timeout, err = strconv.Atoi(t); err != nil || d.Timeout < 1 {
				return Agent{}, fmt.Errorf("%s.door.md: timeout %q: must be a number of seconds", name, t)
			}
		}
		if len(d.Rules) == 0 || !strings.Contains(d.Prompt, "$ARGUMENTS") {
			return Agent{}, fmt.Errorf("%s.door.md: a door needs rules and a prompt with $ARGUMENTS", name)
		}
		for _, r := range d.Rules {
			if !rule.MatchString(r) {
				return Agent{}, fmt.Errorf("%s.door.md: rule %q: write it as a permission rule, such as Bash(curl *) or WebFetch", name, r)
			}
		}
		a.Door = d
	}
	return a, nil
}

// frontmatter splits a file into its fields and its body. The fields sit
// between two lines of ---: `key: value`, or `key:` and then lines of
// `  - item` for a list. Blank lines, and lines that start with #, say
// nothing.
func frontmatter(text string) (fields map[string]string, lists map[string][]string, body string, err error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	head, rest, ok := strings.Cut(strings.TrimPrefix(text, "---\n"), "\n---\n")
	if !strings.HasPrefix(text, "---\n") || !ok {
		return nil, nil, "", fmt.Errorf("the file must start with fields between two lines of ---")
	}
	fields, lists = map[string]string{}, map[string][]string{}
	list := ""
	for _, line := range strings.Split(head, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
		case strings.HasPrefix(trimmed, "- "):
			if list == "" {
				return nil, nil, "", fmt.Errorf("%q: a list item needs a key before it", line)
			}
			lists[list] = append(lists[list], strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
		default:
			k, v, ok := strings.Cut(line, ":")
			if !ok || strings.TrimSpace(k) != k || k == "" {
				return nil, nil, "", fmt.Errorf("%q: write a field as key: value", line)
			}
			if v = strings.TrimSpace(v); v == "" {
				list = k
				lists[k] = nil
			} else {
				list = ""
				fields[k] = v
			}
		}
	}
	return fields, lists, strings.TrimSpace(rest), nil
}
