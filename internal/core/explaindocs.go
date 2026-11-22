package core

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/irie-people/irie/internal/search"
)

// A card that explains, asked in a session, reads what the session's own
// project says of the question first: the passages of its docs that match
// it best. A small local model knows nothing of the project, and without
// them it explains its words from what such words usually mean.

const (
	explainDocs = 7 << 10 // the most of a card's context the docs take, in bytes
	docPassage  = 1500    // the most of one passage
	docFileMost = 2 << 20 // a file past this is skipped
	docHits     = 8       // the most passages a card reads
)

// passage is a paragraph, a list item or a table row of a project's docs,
// with where it is: the file and the heading it sits under.
type passage struct {
	where, text string
}

// docsOf reads the passages of dir's docs: its README.md and the Markdown
// files in its docs folder. Proposals are left out: they say what might be
// built, and a card should say what is.
func docsOf(dir string) []passage {
	if dir == "" {
		return nil
	}
	files := []string{filepath.Join(dir, "README.md")}
	if more, err := filepath.Glob(filepath.Join(dir, "docs", "*.md")); err == nil {
		slices.Sort(more)
		files = append(files, more...)
	}
	var out []passage
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil || info.Size() > docFileMost {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		rel, _ := filepath.Rel(dir, f)
		out = append(out, passagesOf(filepath.ToSlash(rel), string(b))...)
	}
	return out
}

// passagesOf cuts a Markdown file into passages: each paragraph, each item
// of a list at its left edge with what is indented under it, and each row
// of a table, under the heading before it. Code blocks are left out, and
// so is a passage too short to say anything.
func passagesOf(name, text string) []passage {
	var out []passage
	heading := ""
	var cur []string
	flush := func() {
		t := strings.Join(cur, " ")
		cur = cur[:0]
		t = strings.NewReplacer("**", "", "__", "").Replace(strings.TrimSpace(t))
		if utf8.RuneCountInString(t) < 40 || !strings.ContainsFunc(t, unicode.IsLetter) {
			return
		}
		where := name
		if heading != "" {
			where += ", " + heading
		}
		out = append(out, passage{where: where, text: cutText(t, docPassage)})
	}
	fence := false
	for _, l := range strings.Split(text, "\n") {
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(t, "```"):
			flush()
			fence = !fence
		case fence:
		case strings.HasPrefix(t, "#"):
			flush()
			heading = strings.TrimSpace(strings.TrimLeft(t, "#"))
		case t == "":
			flush()
		case strings.HasPrefix(l, "- "), strings.HasPrefix(l, "* "), strings.HasPrefix(t, "|"), listItem(l):
			flush()
			cur = append(cur, t)
		default:
			cur = append(cur, t)
		}
	}
	flush()
	return out
}

// listItem reports whether l starts an item of a numbered list.
func listItem(l string) bool {
	i := 0
	for i < len(l) && l[i] >= '0' && l[i] <= '9' {
		i++
	}
	return i > 0 && strings.HasPrefix(l[i:], ". ")
}

// docsFor is what dir's docs say of topic: the passages that match it
// best, by BM25 (internal/search), best first, each under where it is, in
// at most budget bytes; "" when none match.
func docsFor(dir, topic string, budget int) string {
	q := search.Tokens(topic, nil)
	if len(q) == 0 {
		return ""
	}
	ps := docsOf(dir)
	if len(ps) == 0 {
		return ""
	}
	toks := make([][]string, len(ps))
	for i, p := range ps {
		toks[i] = search.Tokens(p.where+" "+p.text, nil)
	}
	var b strings.Builder
	for _, h := range search.NewIndex(toks).Search(q, docHits) {
		p := ps[h.Doc]
		entry := "From " + p.where + ":\n" + p.text + "\n\n"
		if b.Len()+len(entry) > budget {
			continue
		}
		b.WriteString(entry)
	}
	return strings.TrimSpace(b.String())
}

// cutText is s at most max bytes long, cut where a rune starts, with … where
// it was cut.
func cutText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max] + "…"
}
