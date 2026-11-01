// Package learn holds what Irie teaches with (docs/proposals/learning.md):
// the tips it adds under Claude Code's spinner, and the learning resources
// and glossary that the Help menu shows. They live in learn.toml, one file
// that the core serves to every view.
package learn

import (
	_ "embed"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

//go:embed learn.toml
var data string

// Tip is one line for Claude Code's spinner. Claude Code keeps each tip's
// history by its id.
type Tip struct {
	ID   string `toml:"id" json:"id"`
	Text string `toml:"text" json:"text"`
}

// Resource is a free resource to learn from. Irie links to it and copies
// nothing of it.
type Resource struct {
	Group string `toml:"group" json:"group"`
	Title string `toml:"title" json:"title"`
	URL   string `toml:"url" json:"url"`
	About string `toml:"about" json:"about"`         // what it teaches, in a sentence
	Note  string `toml:"note" json:"note,omitempty"` // what it needs or allows, such as an account or a license
	Lang  string `toml:"lang" json:"lang,omitempty"` // the language it teaches, for a language's own tutorial
	Yours bool   `toml:"-" json:"yours,omitempty"`   // one of the user's own links (learn.links)
	Here  bool   `toml:"-" json:"here,omitempty"`    // a tutorial of a language the session's project is written in
}

// Term is one word of the glossary, in plain words.
type Term struct {
	Term  string `toml:"term" json:"term"`
	Means string `toml:"means" json:"means"`
}

// Set is the whole of learn.toml.
type Set struct {
	Tips      []Tip      `toml:"tip"`
	Resources []Resource `toml:"resource"`
	Glossary  []Term     `toml:"term"`
}

// Claude Code's limits on a spinner tip (spinnerTipsOverride).
const (
	MaxTipText = 500
	MaxTips    = 200
)

var tipID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

var set, setErr = parse(data)

// Get returns learn.toml, which the tests check.
func Get() Set {
	if setErr != nil {
		panic(setErr)
	}
	return set
}

func parse(s string) (Set, error) {
	var out Set
	md, err := toml.Decode(s, &out)
	if err != nil {
		return Set{}, fmt.Errorf("learn.toml: %w", err)
	}
	if keys := md.Undecoded(); len(keys) > 0 {
		return Set{}, fmt.Errorf("learn.toml: unknown key %s", keys[0])
	}
	return out, out.check()
}

// check says what in s Claude Code or a view would refuse or show badly.
func (s Set) check() error {
	var errs []error
	if len(s.Tips) > MaxTips {
		errs = append(errs, fmt.Errorf("%d tips: Claude Code reads at most %d", len(s.Tips), MaxTips))
	}
	ids := map[string]bool{}
	for _, t := range s.Tips {
		switch {
		case !tipID.MatchString(t.ID):
			errs = append(errs, fmt.Errorf("tip %q: an id is 1 to 64 letters, digits, dots, underscores or hyphens", t.ID))
		case ids[t.ID]:
			errs = append(errs, fmt.Errorf("tip %q: the id is taken", t.ID))
		case t.Text == "" || len(t.Text) > MaxTipText || strings.ContainsAny(t.Text, "\n\r\t"):
			errs = append(errs, fmt.Errorf("tip %q: one line of at most %d bytes", t.ID, MaxTipText))
		}
		ids[t.ID] = true
	}
	titles := map[string]bool{}
	for _, r := range s.Resources {
		switch {
		case r.Group == "" || r.Title == "" || r.About == "":
			errs = append(errs, fmt.Errorf("resource %q: needs a group, a title and what it teaches", r.Title))
		case !strings.HasPrefix(r.URL, "https://"):
			errs = append(errs, fmt.Errorf("resource %q: its link must be https", r.Title))
		case titles[r.Title]:
			errs = append(errs, fmt.Errorf("resource %q: the title is taken", r.Title))
		case r.Lang != "" && !slices.ContainsFunc(markers, func(m marker) bool { return m.lang == r.Lang }):
			errs = append(errs, fmt.Errorf("resource %q: Irie cannot tell a %s project", r.Title, r.Lang))
		}
		titles[r.Title] = true
	}
	terms := map[string]bool{}
	for _, t := range s.Glossary {
		k := strings.ToLower(t.Term)
		switch {
		case t.Term == "" || t.Means == "":
			errs = append(errs, fmt.Errorf("term %q: needs what it means", t.Term))
		case terms[k]:
			errs = append(errs, fmt.Errorf("term %q: twice", t.Term))
		}
		terms[k] = true
	}
	return errors.Join(errs...)
}

// marker is a file at the top of a project that says which language it is
// written in.
type marker struct{ file, lang string }

var markers = []marker{
	{"go.mod", "go"},
	{"Package.swift", "swift"},
	{"Cargo.toml", "rust"},
	{"pyproject.toml", "python"},
	{"requirements.txt", "python"},
	{"setup.py", "python"},
	{"tsconfig.json", "typescript"},
	{"package.json", "javascript"},
}

// Languages names the languages the project in dir is written in, read from
// the files at its top, such as go.mod for Go. It runs no model and reads no
// file's contents.
func Languages(dir string) []string {
	if dir == "" {
		return nil
	}
	var out []string
	for _, m := range markers {
		if slices.Contains(out, m.lang) {
			continue
		}
		if fi, err := os.Stat(filepath.Join(dir, m.file)); err == nil && !fi.IsDir() {
			out = append(out, m.lang)
		}
	}
	return out
}

// Resources are the resources to show: the user's own links first, then the
// tutorials of the languages in langs, then the rest in learn.toml's order,
// with the other languages' tutorials last.
func Resources(links map[string]string, langs []string) []Resource {
	var yours, here, rest, other []Resource
	for _, title := range slices.Sorted(maps.Keys(links)) {
		yours = append(yours, Resource{Group: "Your links", Title: title, URL: links[title], Yours: true})
	}
	for _, r := range Get().Resources {
		switch {
		case r.Lang == "":
			rest = append(rest, r)
		case slices.Contains(langs, r.Lang):
			r.Here = true
			here = append(here, r)
		default:
			other = append(other, r)
		}
	}
	return slices.Concat(yours, here, rest, other)
}
