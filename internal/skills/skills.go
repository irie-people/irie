// Package skills holds Irie's own skills and makes the plugin folder that
// hands them to Claude Code for one session (docs/proposals/extensions.md
// §1.2; docs/proposals/skills.md §1.1). Nothing here writes outside Irie's
// own folders.
//
// A skill is a folder with a SKILL.md, in the Agent Skills format. Irie's own
// are compiled into irie from builtin/, so a release carries them and they
// need no download.
package skills

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
)

//go:embed all:builtin
var builtin embed.FS

// Plugin is the name of Irie's plugin: its skills run as /irie:<name>.
const Plugin = "irie"

// Skill is one skill folder.
type Skill struct {
	Name        string            // its name, which is also its folder's
	Description string            // what it does, from its frontmatter
	UserOnly    bool              // disable-model-invocation: only the user starts it, and it costs no context until then
	Files       map[string][]byte // its files, by slash-separated path inside the folder
}

// Command is how a person starts the skill in Claude Code.
func (s Skill) Command() string { return "/" + Plugin + ":" + s.Name }

var (
	once    sync.Once
	loaded  []Skill
	loadErr error
)

// Builtin returns Irie's own skills, sorted by name. An embedded skill that
// is not well formed is an error, which the tests catch before a release.
func Builtin() ([]Skill, error) {
	once.Do(func() { loaded, loadErr = load(builtin, "builtin") })
	return slices.Clone(loaded), loadErr
}

// Find returns the skill called name in all.
func Find(all []Skill, name string) (Skill, bool) {
	for _, s := range all {
		if s.Name == name {
			return s, true
		}
	}
	return Skill{}, false
}

// Enabled returns the skills of all that are on: none when on is false, and
// none of those that off names.
func Enabled(all []Skill, on bool, off []string) []Skill {
	if !on {
		return nil
	}
	var out []Skill
	for _, s := range all {
		if !slices.Contains(off, s.Name) {
			out = append(out, s)
		}
	}
	return out
}

// nameRE is the Agent Skills rule for a name: lowercase letters, digits and
// hyphens, at most 64 characters.
var nameRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ValidName reports whether name may name a skill.
func ValidName(name string) bool { return len(name) <= 64 && nameRE.MatchString(name) }

// load reads every skill folder under dir in fsys.
func load(fsys fs.FS, dir string) ([]Skill, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	var out []Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		s, err := loadOne(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b Skill) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

func loadOne(fsys fs.FS, dir string) (Skill, error) {
	name := path.Base(dir)
	s := Skill{Name: name, Files: map[string][]byte{}}
	err := fs.WalkDir(fsys, dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		s.Files[strings.TrimPrefix(p, dir+"/")] = b
		return nil
	})
	if err != nil {
		return Skill{}, err
	}
	doc, ok := s.Files["SKILL.md"]
	if !ok {
		return Skill{}, fmt.Errorf("skill %s has no SKILL.md", name)
	}
	fm, err := Frontmatter(doc)
	if err != nil {
		return Skill{}, fmt.Errorf("skill %s: %v", name, err)
	}
	if fm["name"] != name {
		return Skill{}, fmt.Errorf("skill %s: its SKILL.md is named %q", name, fm["name"])
	}
	if !ValidName(name) {
		return Skill{}, fmt.Errorf("skill %s: a name is lowercase letters, digits and hyphens", name)
	}
	s.Description = fm["description"]
	if s.Description == "" {
		return Skill{}, fmt.Errorf("skill %s has no description", name)
	}
	s.UserOnly = fm["disable-model-invocation"] == "true"
	return s, nil
}

// Frontmatter reads the YAML frontmatter of a SKILL.md as flat `key: value`
// lines, which is all Irie's own skills use. A value may be quoted.
func Frontmatter(doc []byte) (map[string]string, error) {
	text := strings.ReplaceAll(string(doc), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return nil, errors.New("SKILL.md must start with --- and its frontmatter")
	}
	body, _, ok := strings.Cut(text[4:], "\n---\n")
	if !ok {
		return nil, errors.New("SKILL.md's frontmatter has no closing ---")
	}
	out := map[string]string{}
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok || k != strings.TrimSpace(k) || k == "" {
			return nil, fmt.Errorf("frontmatter line %q is not key: value", line)
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
		out[k] = v
	}
	return out, nil
}

// manifest is the plugin's .claude-plugin/plugin.json. Only its name is
// required; it has no version, so the folder's digest changes only with its
// skills.
func manifest() []byte {
	b, _ := json.MarshalIndent(map[string]string{
		"name":        Plugin,
		"description": "Irie's own skills, for this session only.",
	}, "", "  ")
	return append(b, '\n')
}

// Contents is every file of the plugin folder for set, by slash-separated
// path: the manifest and each skill under skills/<name>/.
func Contents(set []Skill) map[string][]byte {
	out := map[string][]byte{".claude-plugin/plugin.json": manifest()}
	for _, s := range set {
		for p, b := range s.Files {
			out["skills/"+s.Name+"/"+p] = b
		}
	}
	return out
}

// Digest names the plugin folder that holds files: a hash of every path and
// its contents.
func Digest(files map[string][]byte) string {
	h := sha256.New()
	for _, p := range slices.Sorted(maps.Keys(files)) {
		sum := sha256.Sum256(files[p])
		fmt.Fprintf(h, "%s\x00%x\n", p, sum)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// Folder makes, under root, the plugin folder that holds set, and returns
// its path. The folder is named by its digest, so the same set always gets
// the same folder, and a running session never sees its skills change.
// Files are read-only. A folder that is already there is checked file by
// file, and made again when anything in it changed: Claude Code loads a
// plugin folder in place, and an agent may edit a skill where it finds it.
func Folder(root string, set []Skill) (string, error) {
	files := Contents(set)
	dir := filepath.Join(root, Digest(files))
	if Check(dir, files) == nil {
		return dir, nil
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	tmp := filepath.Join(root, ".new-"+random())
	if err := write(tmp, files); err != nil {
		removeAll(tmp)
		return "", err
	}
	if _, err := os.Lstat(dir); err == nil {
		removeAll(dir)
	}
	if err := os.Rename(tmp, dir); err != nil {
		removeAll(tmp)
		// Another spawn may have made the same folder meanwhile.
		if Check(dir, files) == nil {
			return dir, nil
		}
		return "", err
	}
	return dir, nil
}

// write puts files into dir, which must not exist yet.
func write(dir string, files map[string][]byte) error {
	if err := os.Mkdir(dir, 0o700); err != nil {
		return err
	}
	for _, p := range slices.Sorted(maps.Keys(files)) {
		f := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(f, files[p], 0o444); err != nil {
			return err
		}
		// WriteFile's mode is cut by the umask; read-only is the point.
		if err := os.Chmod(f, 0o444); err != nil {
			return err
		}
	}
	return os.Chmod(dir, 0o755)
}

// Check reports whether dir holds exactly files: no file missing, changed,
// writable or added.
func Check(dir string, files map[string][]byte) error {
	seen := 0
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		want, ok := files[filepath.ToSlash(rel)]
		if !ok {
			return fmt.Errorf("%s is not Irie's", rel)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a plain file", rel)
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		if fi.Mode().Perm()&0o222 != 0 {
			return fmt.Errorf("%s is writable", rel)
		}
		got, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if !bytes.Equal(got, want) {
			return fmt.Errorf("%s changed", rel)
		}
		seen++
		return nil
	})
	if err != nil {
		return err
	}
	if seen != len(files) {
		return fmt.Errorf("%d of %d files", seen, len(files))
	}
	return nil
}

// Prune removes the plugin folders under root whose paths keep doesn't
// name, such as folders no live session uses. It is Irie's cache, not the
// user's data.
func Prune(root string, keep map[string]bool) error {
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		dir := filepath.Join(root, e.Name())
		if keep[dir] {
			continue
		}
		if err := removeAll(dir); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// removeAll removes dir, whose files are read-only.
func removeAll(dir string) error {
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil {
			os.Chmod(p, 0o700)
		}
		return nil
	})
	return os.RemoveAll(dir)
}

func random() string {
	var b [6]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
