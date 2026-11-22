// Package explained keeps the Teach tile's explanations as files: a
// library, a folder for each project, that a person reads anywhere and
// Irie reuses and learns from (docs/proposals/materialized-cards.md §4).
// Each question has two files beside each other: Markdown for the person,
// its picture drawn as Markdown with its diagram in Mermaid, so Obsidian,
// GitHub or an editor show it; and JSON for Irie, every version of the card
// as it was made, with what the person said to have each one made.
package explained

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/genui"
)

// Version is one card of an explanation: the first, made for its question,
// or one made from the one before it at the person's word.
type Version struct {
	Card  json.RawMessage `json:"card"`
	Made  time.Time       `json:"made"`
	By    string          `json:"by,omitempty"`    // the local model that made it
	Asked string          `json:"asked,omitempty"` // what the person said of the one before; "" for the first
}

// Explanation is a question's cards, in the order they were made.
type Explanation struct {
	Question string `json:"question"`
	Project  string `json:"project"`
	Topic    string `json:"topic,omitempty"` // the topic of the project's docs it explains, when asked as one
	// Kept says the person marked it as one that helped, which makes it an
	// example for the ones made after it.
	Kept     bool      `json:"kept,omitempty"`
	Versions []Version `json:"versions"`
}

// Latest is its newest version.
func (e Explanation) Latest() Version {
	if len(e.Versions) == 0 {
		return Version{}
	}
	return e.Versions[len(e.Versions)-1]
}

// Library is the folder that holds the explanations, a folder in it for
// each project.
type Library struct{ Dir string }

// most bounds a name's words, in bytes.
const most = 60

// Slug names a question's files: its words in lower case joined by hyphens,
// cut short, and a digest of the whole, so two questions never share one.
func Slug(question string) string {
	words := strings.FieldsFunc(strings.ToLower(question), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	s := ""
	for _, w := range words {
		if len(s)+len(w)+1 > most {
			break
		}
		if s != "" {
			s += "-"
		}
		s += w
	}
	sum := sha256.Sum256([]byte(question))
	if s == "" {
		s = "question"
	}
	return s + "-" + hex.EncodeToString(sum[:3])
}

// Folder is a project's name as a folder: its letters, digits and the marks
// a file name takes, "general" for none.
func Folder(project string) string {
	f := strings.Map(func(r rune) rune {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '-', r == '_', r == '.':
			return r
		}
		return '-'
	}, strings.TrimSpace(project))
	f = strings.Trim(f, ".-")
	if f == "" {
		return "general"
	}
	return f
}

// Files are the paths of a question's two files: Markdown and JSON.
func (l Library) Files(project, question string) (md, js string) {
	base := filepath.Join(l.Dir, Folder(project), Slug(question))
	return base + ".md", base + ".json"
}

// Load reads a question's explanation; nil, with no error, when none is
// kept.
func (l Library) Load(project, question string) (*Explanation, error) {
	_, js := l.Files(project, question)
	return read(js)
}

func read(path string) (*Explanation, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var e Explanation
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &e, nil
}

// Add keeps v as the newest version of the question's explanation, making
// it when it is the first, and gives it as kept, with its Markdown file.
func (l Library) Add(project, question, topic string, v Version) (*Explanation, string, error) {
	e, err := l.Load(project, question)
	if err != nil {
		return nil, "", err
	}
	if e == nil {
		e = &Explanation{Question: question, Project: project}
	}
	if topic != "" {
		e.Topic = topic
	}
	e.Versions = append(e.Versions, v)
	md, err := l.write(e)
	return e, md, err
}

// Keep marks the question's explanation as one that helped, or not, and
// gives it as kept; nil when none is kept.
func (l Library) Keep(project, question string, on bool) (*Explanation, error) {
	e, err := l.Load(project, question)
	if err != nil || e == nil {
		return e, err
	}
	e.Kept = on
	_, err = l.write(e)
	return e, err
}

// write writes e's two files, the JSON first: it is what Irie reads back.
func (l Library) write(e *Explanation) (string, error) {
	md, js := l.Files(e.Project, e.Question)
	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return "", err
	}
	if err := filetx.WriteFile(js, append(b, '\n'), 0o644); err != nil {
		return "", err
	}
	return md, filetx.WriteFile(md, []byte(Markdown(*e, filepath.Base(js))), 0o644)
}

// All are the explanations kept for project, or for every project when it
// is "", the newest first. A file that does not read is passed over.
func (l Library) All(project string) []Explanation {
	pattern := filepath.Join(l.Dir, "*", "*.json")
	if project != "" {
		pattern = filepath.Join(l.Dir, Folder(project), "*.json")
	}
	files, _ := filepath.Glob(pattern)
	var out []Explanation
	for _, f := range files {
		if e, err := read(f); err == nil && e != nil && len(e.Versions) > 0 {
			out = append(out, *e)
		}
	}
	slices.SortStableFunc(out, func(a, b Explanation) int { return b.Latest().Made.Compare(a.Latest().Made) })
	return out
}

// Markdown is the file a person reads: what it was asked and how, the
// newest card drawn as Markdown (genui.Markdown), and what was said to
// have each version made. data names the JSON file beside it.
func Markdown(e Explanation, data string) string {
	var b strings.Builder
	v := e.Latest()
	q, _ := json.Marshal(e.Question)
	p, _ := json.Marshal(e.Project)
	fmt.Fprintf(&b, "---\nquestion: %s\nproject: %s\n", q, p)
	if e.Topic != "" {
		t, _ := json.Marshal(e.Topic)
		fmt.Fprintf(&b, "topic: %s\n", t)
	}
	fmt.Fprintf(&b, "kept: %t\nversions: %d\nmade: %s\n---\n\n", e.Kept, len(e.Versions), v.Made.UTC().Format(time.RFC3339))
	b.WriteString("# " + strings.Join(strings.Fields(e.Question), " ") + "\n\n")
	if c, err := genui.Check(v.Card); err == nil {
		b.WriteString(genui.Markdown(c))
	}
	fmt.Fprintf(&b, "*Made by %s on this computer, version %d of %d.*\n", orLocal(v.By), len(e.Versions), len(e.Versions))
	if len(e.Versions) > 1 {
		b.WriteString("\n## How it was made\n\n1. Asked: " + oneLine(e.Question) + "\n")
		for i, x := range e.Versions[1:] {
			fmt.Fprintf(&b, "%d. Then: %s\n", i+2, oneLine(x.Asked))
		}
	}
	fmt.Fprintf(&b, "\nEvery version, as Irie made it, is in %s.\n", data)
	return b.String()
}

func orLocal(by string) string {
	if by == "" {
		return "a local model"
	}
	return by
}

// oneLine is s on one line, for a list item.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
