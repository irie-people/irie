package core

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/irie-people/irie/internal/ipc"
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
	var out []passage
	eachDoc(dir, func(name, text string) { out = append(out, passagesOf(name, text)...) })
	return out
}

// eachDoc hands each of dir's docs to f, by its name in dir and its text:
// README.md, then the docs folder's Markdown files by name.
func eachDoc(dir string, f func(name, text string)) {
	if dir == "" {
		return
	}
	files := []string{filepath.Join(dir, "README.md")}
	if more, err := filepath.Glob(filepath.Join(dir, "docs", "*.md")); err == nil {
		slices.Sort(more)
		files = append(files, more...)
	}
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil || info.Size() > docFileMost {
			continue
		}
		b, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		rel, _ := filepath.Rel(dir, file)
		f(filepath.ToSlash(rel), string(b))
	}
}

// boldItem is an item of a list at its left edge that leads with its name
// in bold, as "- **Your groups.** New Group in a card's menu…".
var boldItem = regexp.MustCompile(`^[-*] \*\*([^*]{2,80})\*\*`)

// The most topics one doc gives the Teach tile, and all of them.
const (
	topicsPerDoc = 120
	topicsMost   = 400
)

// docTopics are what dir's docs teach, doc by doc, each in its order: each
// item of a list that leads with its name in bold, its name without the
// stop after it, once in a doc. The Teach tile lists them by doc, and steps
// through one doc's as a tour; which doc is the guide, a person knows
// better than a rule, so the tile asks.
func docTopics(dir string) []ipc.LearnTopic {
	var out []ipc.LearnTopic
	eachDoc(dir, func(name, text string) {
		heading := ""
		seen := map[string]bool{}
		n := 0
		for _, l := range strings.Split(text, "\n") {
			if t := strings.TrimSpace(l); strings.HasPrefix(t, "#") {
				heading = strings.TrimSpace(strings.TrimLeft(t, "#"))
				continue
			}
			m := boldItem.FindStringSubmatch(l)
			if m == nil || n >= topicsPerDoc || len(out) >= topicsMost {
				continue
			}
			title := strings.TrimRight(strings.TrimSpace(m[1]), ".:,;")
			if title == "" || seen[strings.ToLower(title)] {
				continue
			}
			seen[strings.ToLower(title)] = true
			n++
			out = append(out, ipc.LearnTopic{Title: title, File: name, Heading: heading})
		}
	})
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
