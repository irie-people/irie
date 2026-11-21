package plugins

import (
	"fmt"
	"os"
	"slices"
	"strconv"

	irie "github.com/irie-people/irie/core"
	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/skills"
)

// Words of your own (DESIGN §4.17): a manifest's [words.<id>] tables. A
// word of yours is :<id> in a task bar. It starts a session for its
// plugin's app: on a skill of the plugin's when it names one, so :<id>
// alone opens such a session and :<id> TASK hands the skill the task, and
// in a folder of yours when it names one, in place, as :markets runs in the
// map's folder. A word runs nothing by itself, so it doesn't keep its
// plugin off as a skill or a server does.

// wordFile is one of a manifest's [words.<id>] tables.
type wordFile struct {
	Title string `toml:"title"`
	Skill string `toml:"skill"`
	Dir   string `toml:"dir"`
}

// OwnWord is a word of yours, as its manifest names it.
type OwnWord struct {
	Name     string // its table's id: the word, typed as :<name>
	Plugin   string // the plugin it comes with
	Title    string // its part's name, and the name of a session it starts with no task
	Skill    string // a skill of its plugin's that its sessions start on; "" = none
	Dir      string // the folder its sessions run in, in place; "" = where the task says
	Manifest string // the manifest it comes from
	// Problem says why it can't start a session, in a sentence: a table
	// that doesn't read, or a name another word has. "" = it can.
	Problem string
	// Off says why typing it does nothing now, in a sentence: its plugin is
	// off, or its table doesn't read (Words). "" = it starts a session.
	Off string
}

// wordPart is the part a [words.<id>] table makes. skills are the ids of
// the plugin's own [skills.<id>] tables, one of which the word may start
// its sessions on.
func wordPart(p Plugin, id string, f wordFile, skillIDs []string) Part {
	title := line(f.Title, 40)
	if title == "" {
		title = ":" + id
	}
	w := &OwnWord{Name: id, Plugin: p.Name, Title: title, Manifest: p.Manifest}
	x := Part{Name: "words." + id, Title: title, Brings: []string{":" + id + " [TASK] in a task bar"}, own: always, Word: w}
	x.Summary = "A word of yours, :" + id + ", that starts a session for this plugin from a task bar."
	fails := func(why string) Part {
		w.Problem = why
		x.check = func(Env, bool) (bool, string) { return false, why }
		return x
	}
	switch {
	case !manifestName.MatchString(id) || len(id) > 40:
		return fails("[words." + id + "] isn't a word: lowercase letters, digits and single hyphens, at most 40, such as [words." + p.Name + "].")
	case irie.IsCommand(id):
		return fails(brand.Name + " has a :" + id + " of its own: give [words." + id + "] another name.")
	}
	if f.Skill != "" {
		if !slices.Contains(skillIDs, f.Skill) {
			return fails("skill = " + strconv.Quote(f.Skill) + " isn't one of this plugin's skills: name a [skills.<id>] table of this manifest, or leave skill out.")
		}
		w.Skill = f.Skill
	}
	dir, ok := helperPath(f.Dir)
	if !ok {
		return fails("dir = " + strconv.Quote(f.Dir) + " isn't a full path to a folder: name it as ~/Documents/" + p.Title + ".")
	}
	w.Dir = dir
	// Its words in the Plugins menu and Settings: Start <title>… types
	// them, as :write starts Writing.
	if w.Skill != "" {
		x.Setup = ":" + id
	}
	x.Summary = "A word of yours, :" + id + ", that starts a session for this plugin from a task bar" + where(*w) + "."
	x.check = func(Env, bool) (bool, string) { return checkWord(*w) }
	return x
}

// where says where a word's sessions start and on what, as a sentence's
// end: " on /irie:grants, in ~/Documents/Grants".
func where(w OwnWord) string {
	s := ""
	if w.Skill != "" {
		s += " on /" + skills.Plugin + ":" + w.Skill
	}
	if w.Dir != "" {
		if s != "" {
			s += ","
		}
		s += " in " + w.Dir
	}
	return s
}

// checkWord says whether a word of yours can start a session: its table
// reads and its folder is there. Its skill's part says how the skill
// stands.
func checkWord(w OwnWord) (bool, string) {
	if w.Problem != "" {
		return false, w.Problem
	}
	if w.Dir != "" {
		if st, err := os.Stat(w.Dir); err != nil || !st.IsDir() {
			return false, "Its folder, " + w.Dir + ", isn't there: make it, or change dir in its manifest."
		}
	}
	if w.Skill != "" {
		return true, "Type :" + w.Name + " in a task bar to start a session on /" + skills.Plugin + ":" + w.Skill + ", or :" + w.Name + " and a task to hand it the task."
	}
	return true, "Type :" + w.Name + " and a task in a task bar to start a session for it" + where(w) + "."
}

// Words are your words, from the manifests in the plugins folder, in the
// order of their files and then their names, each with why it does nothing
// now, if it doesn't. A manifest that doesn't read brings none.
func Words(e Env) []OwnWord {
	mine, _ := Mine(e.Paths.Plugins())
	var out []OwnWord
	for _, p := range mine {
		if p.problem != "" {
			continue
		}
		on := p.on(e.Config)
		for _, x := range p.Parts {
			if x.Word == nil {
				continue
			}
			w := *x.Word
			switch {
			case w.Problem != "":
				w.Off = ":" + w.Name + " can't start a session: " + lower(w.Problem)
			case !on:
				w.Off = fmt.Sprintf(":%s is off, as the %s plugin is: `%s plugins on %s` turns it on.", w.Name, p.Name, brand.Slug, p.Name)
			}
			out = append(out, w)
		}
	}
	return out
}
