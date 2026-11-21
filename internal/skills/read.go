package skills

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// A skill of yours is a folder on this computer that holds a SKILL.md, as
// a plugin of your own names one (DESIGN §4.17). Read copies it whole into
// a Skill, so a session gets it in Irie's sealed folder as it was when the
// session started, whatever happens to the folder after.

// The most a skill of yours may hold, so a stray folder can neither slow a
// launch nor fill Irie's skills folder.
const (
	MaxFiles = 64
	MaxBytes = 1 << 20
)

// maxDescription is the most a skill's description may hold, as the Agent
// Skills spec has it.
const maxDescription = 1024

// grants are the frontmatter fields that change what an agent may do, which
// no skill of Irie's sets (docs/proposals/skills.md §4.1), and no skill of
// yours may: a skill is words.
var grants = []string{"allowed-tools", "disallowed-tools", "hooks", "shell", "model", "effort", "context", "agent"}

// injected says whether Markdown holds a command Claude Code runs as the
// skill loads, an exclamation mark and a backtick, before the model sees it.
func injected(text string) bool {
	return strings.Contains(text, "!`") || strings.Contains(text, "```!")
}

// Read reads the skill in dir, a folder holding its SKILL.md, as the skill
// named name, from source. It takes plain files and folders, leaves out
// hidden ones (.git, .DS_Store) and refuses a link, so a skill brings only
// what its own folder holds. As Irie's own, a skill of yours is words: its
// frontmatter grants nothing and changes no model, and no Markdown of it
// runs a command as it loads. What is wrong is the error's sentence.
func Read(dir, name, source string) (Skill, error) {
	// The folder itself may be a link, as to a checkout's; what it holds
	// may not.
	root, err := filepath.EvalSymlinks(dir)
	var st os.FileInfo
	if err == nil {
		st, err = os.Stat(root)
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Skill{}, fmt.Errorf("%s isn't there", dir)
	case err != nil:
		return Skill{}, fmt.Errorf("%s doesn't read: %v", dir, err)
	case !st.IsDir():
		return Skill{}, fmt.Errorf("%s isn't a folder: a skill is a folder holding its SKILL.md", dir)
	}
	s := Skill{Name: name, Source: source, Files: map[string][]byte{}, Mine: true}
	total := 0
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		switch {
		case strings.HasPrefix(d.Name(), "."):
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		case d.Type()&fs.ModeSymlink != 0:
			return fmt.Errorf("%s is a link: a skill holds its own files", rel)
		case d.IsDir():
			return nil
		case !d.Type().IsRegular():
			return fmt.Errorf("%s isn't a plain file", rel)
		case len(s.Files) == MaxFiles:
			return fmt.Errorf("it holds more than %d files", MaxFiles)
		}
		b, err := readAtMost(p, MaxBytes-total)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		if strings.HasSuffix(rel, ".md") && injected(string(b)) {
			return fmt.Errorf("%s runs a command as the skill loads (an exclamation mark before a backtick): a skill of yours is words, and its scripts run only when an agent runs them", rel)
		}
		total += len(b)
		s.Files[rel] = b
		return nil
	})
	if err != nil {
		return Skill{}, err
	}
	md, ok := s.Files["SKILL.md"]
	if !ok {
		return Skill{}, fmt.Errorf("%s has no SKILL.md", dir)
	}
	fm := Frontmatter(md)
	s.Description = strings.TrimSpace(fm["description"])
	switch n := strings.TrimSpace(fm["name"]); {
	case fm == nil:
		return Skill{}, errors.New("its SKILL.md opens with no frontmatter: a --- line, then name and description, then ---")
	case s.Description == "":
		return Skill{}, errors.New("its SKILL.md has no description, which tells an agent when to use it")
	case len(s.Description) > maxDescription:
		return Skill{}, fmt.Errorf("its SKILL.md's description is over %d characters", maxDescription)
	case n != "" && n != name:
		return Skill{}, fmt.Errorf("its SKILL.md names it %s, and the manifest %s: give them one name", n, name)
	}
	for _, k := range grants {
		if _, ok := fm[k]; ok {
			return Skill{}, fmt.Errorf("its SKILL.md sets %s, which changes what an agent may do: a skill of yours is words, so leave it out", k)
		}
	}
	return s, nil
}

// errTooBig is what readAtMost says of a file past the skill's room.
var errTooBig = fmt.Errorf("the skill holds more than %d KB", MaxBytes>>10)

// readAtMost reads the file at p, refusing it when it holds more than n
// bytes, without reading more than that.
func readAtMost(p string, n int) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, int64(n)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > n {
		return nil, errTooBig
	}
	return b, nil
}
