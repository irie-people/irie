// Package founding holds the founding documents, the founding plugin's
// (DESIGN §4.17): the Declaration of Independence, the Constitution and the
// Bill of Rights, as the National Archives transcribes them. The texts are
// the founding-documents skill's own files, so agents and the Mac app's
// Founding tab read the same words. This reads them into the parts the tab
// draws, with each file's SHA-512, and finds passages in them.
package founding

import (
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/skills"
)

// Skill is the skill whose files are the documents.
const Skill = "founding-documents"

// Files are the documents' files in the skill, by document, in the order
// they show.
var Files = []struct{ ID, File string }{
	{"declaration", "declaration.md"},
	{"constitution", "constitution.md"},
	{"rights", "bill-of-rights.md"},
}

var (
	loadOnce sync.Once
	loaded   []ipc.FoundingDoc
	loadErr  error
)

// Load gives the documents, read once from the skill this build carries:
// the texts are built into the binary and never change while it runs.
func Load() ([]ipc.FoundingDoc, error) {
	loadOnce.Do(func() { loaded, loadErr = load(skills.Bundled()) })
	return loaded, loadErr
}

func load(all []skills.Skill) ([]ipc.FoundingDoc, error) {
	var files map[string][]byte
	for _, s := range all {
		if s.Name == Skill {
			files = s.Files
		}
	}
	if files == nil {
		return nil, fmt.Errorf("this build has no %s skill", Skill)
	}
	var docs []ipc.FoundingDoc
	for _, f := range Files {
		b, ok := files[f.File]
		if !ok {
			return nil, fmt.Errorf("the %s skill has no %s", Skill, f.File)
		}
		d, err := Parse(f.ID, b)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.File, err)
		}
		docs = append(docs, d)
	}
	return docs, nil
}

// Digest is the SHA-512 of a file's bytes, in hex: its fingerprint.
func Digest(b []byte) string {
	sum := sha512.Sum512(b)
	return hex.EncodeToString(sum[:])
}

// Parse reads one document's Markdown: a "# " title, an editorial line in
// italics under it, "## " to "#### " headings, and paragraphs between blank
// lines. A paragraph wholly in italics is a note of ours; one whose every
// line starts "- " is a list of names; any other is the document's words,
// its line breaks kept.
func Parse(id string, b []byte) (ipc.FoundingDoc, error) {
	d := ipc.FoundingDoc{ID: id, SHA512: Digest(b)}
	if strings.Contains(string(b), "\r") {
		return d, fmt.Errorf("a carriage return: each line ends with a line feed alone")
	}
	for _, para := range strings.Split(strings.TrimSpace(string(b)), "\n\n") {
		para = strings.TrimSpace(para)
		lines := strings.Split(para, "\n")
		switch {
		case para == "":
			continue
		case strings.HasPrefix(para, "# "):
			if d.Title != "" {
				return d, fmt.Errorf("a second title, %q", para)
			}
			d.Title = strings.TrimPrefix(para, "# ")
		case strings.HasPrefix(para, "#### "):
			d.Blocks = append(d.Blocks, ipc.FoundingBlock{Kind: "h4", Text: strings.TrimPrefix(para, "#### ")})
		case strings.HasPrefix(para, "### "):
			d.Blocks = append(d.Blocks, ipc.FoundingBlock{Kind: "h3", Text: strings.TrimPrefix(para, "### ")})
		case strings.HasPrefix(para, "## "):
			d.Blocks = append(d.Blocks, ipc.FoundingBlock{Kind: "h2", Text: strings.TrimPrefix(para, "## ")})
		case len(lines) == 1 && len(para) > 2 && strings.HasPrefix(para, "_") && strings.HasSuffix(para, "_"):
			note := para[1 : len(para)-1]
			if d.About == "" && len(d.Blocks) == 0 {
				d.About = note
				continue
			}
			d.Blocks = append(d.Blocks, ipc.FoundingBlock{Kind: "note", Text: note})
		case allItems(lines):
			items := make([]string, len(lines))
			for i, l := range lines {
				items[i] = strings.TrimPrefix(l, "- ")
			}
			d.Blocks = append(d.Blocks, ipc.FoundingBlock{Kind: "list", Items: items})
		default:
			d.Blocks = append(d.Blocks, ipc.FoundingBlock{Kind: "text", Text: para})
		}
	}
	if d.Title == "" {
		return d, fmt.Errorf("no title")
	}
	if len(d.Blocks) == 0 {
		return d, fmt.Errorf("no text")
	}
	return d, nil
}

func allItems(lines []string) bool {
	for _, l := range lines {
		if !strings.HasPrefix(l, "- ") {
			return false
		}
	}
	return true
}
