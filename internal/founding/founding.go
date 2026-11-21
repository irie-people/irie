// Package founding holds the charters, the founding plugin's (DESIGN
// §4.17): texts of rights kept word for word, from the bodies that keep
// them. They began as the Declaration of Independence, the Constitution and
// the Bill of Rights, as the National Archives transcribes them, and the
// Freedom of Information Act, as the United States Code sets it out; the
// texts each of those grew from or into joined them, Magna Carta first. The
// texts are the founding-documents skill's own files, so agents and the Mac
// app's Charters tab read the same words. This reads them into the parts
// the tab draws, with each file's SHA-512, finds passages in them, says how
// they bear on one another (links.go) and keeps their chain (chain.go).
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

// Document is one of the texts as this build carries it: its id, which the
// tab, the links and the chain name it by, its file in the skill, its short
// name for the tab's choice, the date it is of, and its kind.
type Document struct {
	ID    string
	File  string
	Short string
	// When is the date the text is of, as RFC 3339 writes a date, or a
	// year alone where the day isn't the point.
	When string
	// Kind is charter, declaration, articles, constitution, amendments or
	// statute: for a graph to tell its nodes apart by.
	Kind string
}

// Documents are the texts in the order they show: by date, so the tab reads
// as the lineage the links trace, the oldest first.
var Documents = []Document{
	{ID: "magna-carta", File: "magna-carta.md", Short: "Magna Carta", When: "1215-06-15", Kind: "charter"},
	{ID: "english-rights", File: "english-bill-of-rights.md", Short: "English Bill of Rights", When: "1689-12-16", Kind: "statute"},
	{ID: "virginia", File: "virginia-declaration-of-rights.md", Short: "Virginia Declaration", When: "1776-06-12", Kind: "declaration"},
	{ID: "declaration", File: "declaration.md", Short: "Declaration", When: "1776-07-04", Kind: "declaration"},
	{ID: "articles", File: "articles-of-confederation.md", Short: "Articles", When: "1777-11-15", Kind: "articles"},
	{ID: "constitution", File: "constitution.md", Short: "Constitution", When: "1787-09-17", Kind: "constitution"},
	{ID: "rights", File: "bill-of-rights.md", Short: "Bill of Rights", When: "1791-12-15", Kind: "amendments"},
	{ID: "udhr", File: "universal-declaration-of-human-rights.md", Short: "Universal Declaration", When: "1948-12-10", Kind: "declaration"},
	{ID: "foia", File: "freedom-of-information-act.md", Short: "FOIA", When: "1966-07-04", Kind: "statute"},
	{ID: "privacy", File: "privacy-act.md", Short: "Privacy Act", When: "1974-12-31", Kind: "statute"},
}

// ByID is the document named, if there is one.
func ByID(id string) (Document, bool) {
	for _, d := range Documents {
		if d.ID == id {
			return d, true
		}
	}
	return Document{}, false
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

// files are the skill's files, by name, as this build carries them.
func files(all []skills.Skill) (map[string][]byte, error) {
	for _, s := range all {
		if s.Name == Skill {
			return s.Files, nil
		}
	}
	return nil, fmt.Errorf("this build has no %s skill", Skill)
}

func load(all []skills.Skill) ([]ipc.FoundingDoc, error) {
	fs, err := files(all)
	if err != nil {
		return nil, err
	}
	var docs []ipc.FoundingDoc
	for _, f := range Documents {
		b, ok := fs[f.File]
		if !ok {
			return nil, fmt.Errorf("the %s skill has no %s", Skill, f.File)
		}
		d, err := Parse(f.ID, b)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.File, err)
		}
		d.Short, d.When, d.Kind = f.Short, f.When, f.Kind
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
