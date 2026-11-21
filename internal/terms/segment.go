package terms

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// section is a heading of the document.
type section struct {
	title string
	level int // 1 to 6, as in HTML; a heading found in plain text is 2
}

// clause is a passage of the document: a paragraph, a list item, or part
// of a long paragraph.
type clause struct {
	sec  int    // its section's index, or -1 before the first heading
	text string // its words, normalised
}

// maxClause is the most characters a clause holds: a longer paragraph is
// cut between its sentences, so each point quotes a passage a person can
// read at a glance.
const maxClause = 700

// normalise makes text one shape to match and quote: straight quotes, plain
// spaces, no soft hyphens or zero-width characters, and Unix line ends.
var normaliser = strings.NewReplacer(
	"\r\n", "\n", "\r", "\n", "\t", " ", "\f", "\n", "\v", "\n",
	// Spaces that aren't plain: no-break, thin, narrow and the like, and
	// the line and paragraph separators.
	"\u00a0", " ", "\u2002", " ", "\u2003", " ", "\u2004", " ", "\u2005", " ",
	"\u2006", " ", "\u2007", " ", "\u2008", " ", "\u2009", " ", "\u200a", " ",
	"\u202f", " ", "\u205f", " ", "\u3000", " ", "\u2028", "\n", "\u2029", "\n\n",
	// Characters that show nothing: a soft hyphen, zero-width ones, a
	// byte-order mark.
	"\u00ad", "", "\u200b", "", "\u200c", "", "\u200d", "", "\u2060", "", "\ufeff", "",
	// Curly quotes and primes.
	"\u2018", "'", "\u2019", "'", "\u201a", "'", "\u201b", "'", "\u2032", "'",
	"\u201c", `"`, "\u201d", `"`, "\u201e", `"`, "\u201f", `"`, "\u00ab", `"`, "\u00bb", `"`,
	// Hyphens and dashes.
	"\u2010", "-", "\u2011", "-", "\u2012", "-", "\u2013", "-", "\u2014", "-", "\u2015", "-", "\u2212", "-",
	"\u2026", "...",
)

func normalise(s string) string {
	return strings.Map(shown, normaliser.Replace(strings.ToValidUTF8(s, "")))
}

// shown drops what a page may hide in its text: control characters, which
// a terminal acts on (an escape can rewrite a line, set the clipboard or
// the window's title), and the marks that reverse the order text shows in.
// Line ends stay.
func shown(r rune) rune {
	switch {
	case r == '\n':
		return r
	case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f:
		return -1
	case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069, r == 0x200e, r == 0x200f, r == 0x061c:
		return -1
	}
	return r
}

// Clean is text a page or a model gave made safe to print: without
// control characters or the marks that reverse text.
func Clean(s string) string { return strings.Map(shown, strings.ToValidUTF8(s, "")) }

var spaces = regexp.MustCompile(`[ ]{2,}`)

// squash collapses runs of spaces and trims the ends.
func squash(s string) string {
	return strings.TrimSpace(spaces.ReplaceAllString(s, " "))
}

var (
	// mdHeading is a heading as internal/htmltext writes one: # Title.
	mdHeading = regexp.MustCompile(`^(#{1,6})\s+(.+)$`)
	// numbered is a heading's number or label: 1., 12.3, (a), IV., A.,
	// Section 5, Article III.
	numbered = regexp.MustCompile(`^(?:\(?(?:\d{1,2}(?:\.\d{1,2}){0,3}|[A-Z]|[IVXL]{1,6})[.)]|\d{1,2}(?:\.\d{1,2}){1,3}|(?i:section|article|part|clause|chapter)\s+(?:\d{1,3}(?:\.\d{1,2})*|[IVXL]{1,6})[.:]?)\s+\S`)
	// runIn is a numbered paragraph that starts with its own short title:
	// "4. Your Content. You keep...".
	runIn = regexp.MustCompile(`^(\(?(?:\d{1,2}(?:\.\d{1,2}){0,3}|[A-Z])[.)]?\s+)([A-Z][A-Za-z'&,/ -]{2,70}?)[.:]\s+(\S.*)$`)
	// listItem is a line of a list.
	listItem = regexp.MustCompile(`^(?:[-*\x{2022}]\s+|\(?[a-z0-9]{1,3}[.)]\s+)`)
)

// segment cuts text into its headings and clauses.
func segment(text string) ([]section, []clause) {
	var secs []section
	var out []clause
	cur := -1
	add := func(s string) {
		s = squash(s)
		if words(s) < 3 {
			return
		}
		for _, part := range cut(s, maxClause) {
			out = append(out, clause{sec: cur, text: part})
		}
	}
	for _, block := range strings.Split(normalise(text), "\n\n") {
		for _, unit := range units(block) {
			if m := mdHeading.FindStringSubmatch(unit); m != nil {
				title := squash(strings.Trim(m[2], "#* "))
				if words(title) > 0 && len(title) <= 200 {
					secs = append(secs, section{title: title, level: len(m[1])})
					cur = len(secs) - 1
					continue
				}
				unit = m[2]
			}
			if isHeading(unit) {
				secs = append(secs, section{title: squash(strings.TrimRight(unit, ":")), level: 2})
				cur = len(secs) - 1
				continue
			}
			if m := runIn.FindStringSubmatch(unit); m != nil && titled(m[2]) {
				secs = append(secs, section{title: squash(m[1] + m[2]), level: 3})
				cur = len(secs) - 1
				add(m[3])
				continue
			}
			add(strings.TrimPrefix(strings.TrimPrefix(unit, "- "), "* "))
		}
	}
	return secs, out
}

// units are a block's paragraphs: each list item and heading on its own,
// and lines a plain-text file wrapped joined again.
func units(block string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			out = append(out, s)
		}
		cur.Reset()
	}
	lines := strings.Split(block, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			flush()
			continue
		}
		starts := mdHeading.MatchString(line) || listItem.MatchString(line) || numbered.MatchString(line) || isHeading(line)
		if starts && cur.Len() > 0 {
			flush()
		}
		if cur.Len() > 0 {
			cur.WriteByte(' ')
		}
		cur.WriteString(line)
		// A line that ends its sentence, or a heading, ends the unit
		// unless the next line goes on with the sentence.
		next := ""
		if i+1 < len(lines) {
			next = strings.TrimSpace(lines[i+1])
		}
		if mdHeading.MatchString(line) || isHeading(line) || !continues(line, next) {
			flush()
		}
	}
	flush()
	return out
}

// continues says whether next goes on with the sentence line ends, as in a
// text wrapped at a fixed width.
func continues(line, next string) bool {
	if next == "" {
		return false
	}
	last, _ := utf8.DecodeLastRuneInString(line)
	first, _ := utf8.DecodeRuneInString(next)
	if strings.ContainsRune(".:;!?\"')", last) {
		return false
	}
	return unicode.IsLower(first) || (len(line) >= 60 && !listItem.MatchString(next) && !numbered.MatchString(next))
}

// isHeading says whether a line reads as a heading in plain text: short,
// without a sentence's end, and numbered or in capitals.
func isHeading(line string) bool {
	n := words(line)
	if n == 0 || n > 14 || len(line) > 120 {
		return false
	}
	if strings.HasSuffix(line, ".") && !strings.HasSuffix(line, "..") {
		// "1. Acceptance of Terms." and "Governing Law and Venue." are
		// headings; a sentence is not, nor one in capitals, as terms
		// write what matters most: "YOU WAIVE ANY RIGHT TO A JURY TRIAL."
		rest := strings.TrimSuffix(numberedLabel.ReplaceAllString(line, ""), ".")
		if n > 10 || !titled(rest) || (capitals(rest) && words(rest) > 5) {
			return false
		}
		return true
	}
	if strings.ContainsAny(line, ";") || strings.Count(line, ",") > 2 {
		return false
	}
	if numbered.MatchString(line) {
		rest := numberedLabel.ReplaceAllString(line, "")
		return titled(rest) && words(rest) <= 10
	}
	return capitals(line) && n >= 1 && len(line) >= 4
}

var numberedLabel = regexp.MustCompile(`^(?:\(?(?:\d{1,2}(?:\.\d{1,2}){0,3}|[A-Z]|[IVXL]{1,6})[.)]|\d{1,2}(?:\.\d{1,2}){1,3}|(?i:section|article|part|clause|chapter)\s+(?:\d{1,3}(?:\.\d{1,2})*|[IVXL]{1,6})[.:]?)\s+`)

// smallWords may start in lower case in a title.
var smallWords = map[string]bool{"a": true, "an": true, "and": true, "as": true, "at": true, "by": true, "for": true, "from": true, "in": true, "into": true, "of": true, "on": true, "or": true, "the": true, "to": true, "with": true, "your": true, "our": true, "us": true, "you": true, "we": true, "its": true, "not": true, "this": true, "these": true, "that": true, "use": true}

// titled says whether s reads as a title: its words in capitals, or in
// title case but for the small words.
func titled(s string) bool {
	if capitals(s) {
		return true
	}
	fs := strings.Fields(s)
	if len(fs) == 0 || len(fs) > 10 {
		return false
	}
	first, _ := utf8.DecodeRuneInString(fs[0])
	if !unicode.IsUpper(first) && !unicode.IsDigit(first) {
		return false
	}
	low := 0
	for _, f := range fs[1:] {
		r, _ := utf8.DecodeRuneInString(f)
		if unicode.IsLower(r) && !smallWords[strings.ToLower(strings.Trim(f, ",:;.'\"()"))] {
			low++
		}
	}
	return low == 0 || (len(fs) >= 4 && low <= 1)
}

// capitals says whether s's letters are mostly capitals.
func capitals(s string) bool {
	up, letters := 0, 0
	for _, r := range s {
		if unicode.IsLetter(r) {
			letters++
			if unicode.IsUpper(r) {
				up++
			}
		}
	}
	return letters >= 3 && up*10 >= letters*8
}

// cut splits s at sentence ends into pieces of at most max characters,
// where its sentences allow.
func cut(s string, max int) []string {
	if len(s) <= max {
		return []string{s}
	}
	var out []string
	var cur strings.Builder
	for _, sent := range sentences(s) {
		if cur.Len() > 0 && cur.Len()+1+len(sent) > max {
			out = append(out, cur.String())
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteByte(' ')
		}
		cur.WriteString(sent)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// abbreviations end with a full stop without ending a sentence.
var abbreviations = map[string]bool{
	"e.g": true, "i.e": true, "etc": true, "inc": true, "ltd": true, "co": true, "corp": true,
	"llc": true, "l.l.c": true, "u.s": true, "u.k": true, "e.u": true, "no": true, "nos": true,
	"sec": true, "art": true, "v": true, "vs": true, "mr": true, "mrs": true, "ms": true, "dr": true,
	"st": true, "jr": true, "sr": true, "approx": true, "incl": true, "max": true, "min": true,
	"para": true, "cf": true, "fig": true, "p": true, "pp": true, "ch": true, "s.a": true, "b.v": true,
	"n.v": true, "a.g": true, "pty": true, "plc": true, "gmbh": true, "u.s.c": true, "c.f.r": true,
}

// sentences splits s into its sentences.
func sentences(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '.' && c != '!' && c != '?' {
			continue
		}
		j := i + 1
		for j < len(s) && (s[j] == '"' || s[j] == '\'' || s[j] == ')') {
			j++
		}
		if j >= len(s) || s[j] != ' ' {
			continue
		}
		k := j
		for k < len(s) && s[k] == ' ' {
			k++
		}
		if k >= len(s) {
			break
		}
		next, _ := utf8.DecodeRuneInString(s[k:])
		if !unicode.IsUpper(next) && !unicode.IsDigit(next) && next != '"' && next != '(' {
			continue
		}
		if c == '.' {
			// The word before the stop: an abbreviation or an initial
			// doesn't end a sentence.
			w := i
			for w > start && s[w-1] != ' ' && s[w-1] != '(' {
				w--
			}
			word := strings.ToLower(strings.Trim(s[w:i], "\"'("))
			if abbreviations[word] || (len(word) == 1 && unicode.IsLetter(rune(word[0]))) {
				continue
			}
		}
		out = append(out, strings.TrimSpace(s[start:j]))
		start = k
		i = k - 1
	}
	if rest := strings.TrimSpace(s[start:]); rest != "" {
		out = append(out, rest)
	}
	return out
}

// words counts s's words.
func words(s string) int {
	n := 0
	in := false
	for _, r := range s {
		w := unicode.IsLetter(r) || unicode.IsDigit(r)
		if w && !in {
			n++
		}
		in = w || (in && (r == '\'' || r == '-'))
	}
	return n
}
