// Package skills hands agents the skills that come with Irie
// (docs/proposals/skills.md, DESIGN §3.3). Claude Code gets them as a
// session-only plugin named irie: a folder under Irie's data dir, passed with
// --plugin-dir, whose skills run as /irie:<name>. The folder is named after a
// digest of what it holds, its files are read-only, and it never changes while
// a session uses it. Irie never writes into the user's own skills, settings
// or repositories.
//
// The skills come from inside the irie binary for now. The pinned skills of
// other repositories that the proposal lists will come through the same
// folder.
package skills

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// Plugin is the plugin's name, which Claude Code puts before each skill's:
// /irie:present.
const Plugin = "irie"

// Skill is one skill Irie can hand an agent.
type Skill struct {
	Name        string            // its folder's name, and its command after the plugin's
	Description string            // from its frontmatter
	Source      string            // where it comes from
	Files       map[string][]byte // its files, by slash path inside its folder
}

//go:embed bundled
var bundled embed.FS

// Bundled returns the skills built into this binary, by name. Each is a
// folder of bundled/ with a SKILL.md whose frontmatter names it.
func Bundled() []Skill {
	entries, err := fs.ReadDir(bundled, "bundled")
	if err != nil {
		panic(err) // the embed holds the folder
	}
	var out []Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		s := Skill{Name: e.Name(), Source: "bundled with Irie", Files: map[string][]byte{}}
		root := "bundled/" + e.Name()
		err := fs.WalkDir(bundled, root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			b, err := bundled.ReadFile(p)
			if err != nil {
				return err
			}
			s.Files[strings.TrimPrefix(p, root+"/")] = b
			return nil
		})
		if err != nil {
			panic(err)
		}
		s.Description = Frontmatter(s.Files["SKILL.md"])["description"]
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b Skill) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// Frontmatter reads the plain key: value lines between the --- lines that
// open a SKILL.md, as Claude Code reads them only when the first line is
// ---. Nested values are left out. It is nil when the file has none.
func Frontmatter(b []byte) map[string]string {
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil
	}
	out := map[string]string{}
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) == "---" {
			return out
		}
		if l == "" || l[0] == ' ' || l[0] == '\t' || l[0] == '#' {
			continue
		}
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
		out[strings.TrimSpace(k)] = v
	}
	return nil
}

// Settings decide which skills a session gets.
type Settings struct {
	Enabled  bool     // skills.enabled
	Disabled []string // skills.disabled
	Profile  bool     // the session's profile takes skills
}

// Choice is whether one skill goes to a session, and why not.
type Choice struct {
	Skill
	On  bool
	Why string // why it is left out; "" when it goes in
}

// Choose decides, for each skill, whether it goes to the session.
func Choose(all []Skill, s Settings) []Choice {
	out := make([]Choice, 0, len(all))
	for _, sk := range all {
		c := Choice{Skill: sk}
		switch {
		case !s.Enabled:
			c.Why = "skills are off (skills.enabled)"
		case !s.Profile:
			c.Why = "this profile takes no skills"
		case slices.Contains(s.Disabled, sk.Name):
			c.Why = "turned off (skills.disabled)"
		default:
			c.On = true
		}
		out = append(out, c)
	}
	return out
}

// On returns the skills that go to the session.
func On(choices []Choice) []Skill {
	var out []Skill
	for _, c := range choices {
		if c.On {
			out = append(out, c.Skill)
		}
	}
	return out
}

// Layout is every file of the plugin folder that holds set, by slash path:
// the manifest Irie writes, a SOURCES.md, and each skill under skills/.
func Layout(set []Skill, version string) map[string][]byte {
	files := map[string][]byte{}
	type author struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	manifest, _ := json.MarshalIndent(struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Version     string `json:"version"`
		Author      author `json:"author"`
		License     string `json:"license"`
	}{
		Name:        Plugin,
		Description: "Skills that come with Irie, handed to this session only",
		Version:     version,
		Author:      author{Name: "Irie", URL: "https://github.com/irie-people/irie"},
		License:     "Apache-2.0",
	}, "", "  ")
	files[".claude-plugin/plugin.json"] = append(manifest, '\n')
	var src strings.Builder
	src.WriteString("# Sources\n\nIrie made this folder for its sessions, and never changes it while one uses it. Each skill and where it comes from:\n\n| Skill | Source |\n|---|---|\n")
	for _, s := range set {
		fmt.Fprintf(&src, "| %s | %s %s |\n", s.Name, s.Source, version)
		for p, b := range s.Files {
			files["skills/"+s.Name+"/"+p] = b
		}
	}
	files["SOURCES.md"] = []byte(src.String())
	return files
}

// Digest names a plugin folder by what it holds.
func Digest(files map[string][]byte) string {
	h := sha256.New()
	for _, p := range sortedKeys(files) {
		fmt.Fprintf(h, "%s\x00%d\x00", p, len(files[p]))
		h.Write(files[p])
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func sortedKeys(m map[string][]byte) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// Build returns the plugin folder that holds set, under root, Irie's
// <data>/skills: plugins/<digest>. It makes the folder when it is missing,
// and makes it again when what it holds differs, as when an agent edited a
// skill where it found it. Its files are read-only. version goes into the
// manifest.
func Build(root string, set []Skill, version string) (string, error) {
	files := Layout(set, version)
	plugins := filepath.Join(root, "plugins")
	dir := filepath.Join(plugins, Digest(files))
	if Same(dir, files) {
		return dir, nil
	}
	if err := os.MkdirAll(plugins, 0o700); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(plugins, ".new-")
	if err != nil {
		return "", err
	}
	if err := write(tmp, files); err != nil {
		remove(tmp)
		return "", err
	}
	if _, err := os.Lstat(dir); err == nil {
		if err := remove(dir); err != nil {
			remove(tmp)
			return "", err
		}
	}
	if err := os.Rename(tmp, dir); err != nil {
		remove(tmp)
		if Same(dir, files) {
			return dir, nil // another launch made it first
		}
		return "", err
	}
	// Sealed only now: macOS renames a folder only while it is writable.
	if err := os.Chmod(dir, 0o555); err != nil {
		return "", err
	}
	return dir, nil
}

// write puts files under dir, read-only, and seals each folder below dir
// after what is in it.
func write(dir string, files map[string][]byte) error {
	var dirs []string
	for _, p := range sortedKeys(files) {
		if strings.Contains(p, "..") || path.IsAbs(p) {
			return fmt.Errorf("skills: bad path %q", p)
		}
		full := filepath.Join(dir, filepath.FromSlash(p))
		for d := filepath.Dir(full); d != dir && !slices.Contains(dirs, d); d = filepath.Dir(d) {
			dirs = append(dirs, d)
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, files[p], 0o444); err != nil {
			return err
		}
	}
	slices.SortFunc(dirs, func(a, b string) int { return len(b) - len(a) })
	for _, d := range dirs {
		if err := os.Chmod(d, 0o555); err != nil {
			return err
		}
	}
	return nil
}

// Same reports whether dir holds exactly files: the same paths, the same
// bytes, and nothing else.
func Same(dir string, files map[string][]byte) bool {
	seen := 0
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return errors.New("not a regular file")
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		want, ok := files[filepath.ToSlash(rel)]
		if !ok {
			return errors.New("extra file")
		}
		got, err := os.ReadFile(p)
		if err != nil || !bytes.Equal(got, want) {
			return errors.New("changed")
		}
		seen++
		return nil
	})
	return err == nil && seen == len(files)
}

// remove deletes a folder Build made, read-only as it is.
func remove(dir string) error {
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			os.Chmod(p, 0o700)
		}
		return nil
	})
	return os.RemoveAll(dir)
}

// Prune removes the plugin folders under root that keep does not name, and
// what builds that stopped half way left. It returns what it removed.
func Prune(root string, keep []string) ([]string, error) {
	plugins := filepath.Join(root, "plugins")
	entries, err := os.ReadDir(plugins)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	kept := map[string]bool{}
	for _, k := range keep {
		kept[filepath.Clean(k)] = true
	}
	var removed []string
	var errs []error
	for _, e := range entries {
		p := filepath.Join(plugins, e.Name())
		if kept[p] {
			continue
		}
		if err := remove(p); err != nil {
			errs = append(errs, err)
			continue
		}
		removed = append(removed, p)
	}
	return removed, errors.Join(errs...)
}
