// Package jot tells a jot that adds to the one sent just before it from a
// jot that is a task of its own (DESIGN §4.2, "More for the last jot").
//
// It reads only the words: whether the jot before it is recent, alive and
// in the same place is the core's to say. It costs no tokens and is the
// same every time, so a user can learn it: a jot that goes on from the one
// before ("also", "oh and", "PS", "make sure"), points back at it ("it",
// "that", "these", "^") or is only a link, a path or an error message adds
// to it. Anything else, such as a new ask ("deep research …", "can we …"),
// is a task of its own, as every jot was before.
package jot

import (
	"regexp"
	"strings"
)

// openers are the first words of a jot that goes on from the one before
// it, or points back at it. A phrase matches whole words at the start.
var openers = []string{
	// It goes on from the jot before.
	"also", "and", "plus", "but", "or", "oh", "ah", "oops", "whoops", "btw", "by the way", "fyi",
	"ps", "p.s", "p.p.s", "pps", "nb", "n.b",
	"actually", "sorry", "correction", "instead", "nvm", "nevermind", "never mind",
	"i forgot", "forgot", "i meant", "i mean", "to clarify", "to add", "in addition", "additionally",
	"on top of that", "one more thing", "another thing", "last thing", "same", "then", "after that",
	"afterwards", "afterward", "once done", "once that's done", "once thats done", "once it's done",
	"when done", "when you're done", "when youre done", "when finished", "except", "unless", "otherwise",
	"make sure", "be sure", "remember to", "don't forget", "dont forget", "do not forget",
	"don't", "dont", "do not", "never",
	"more context", "some context", "extra context", "additional context", "added context",
	"for context", "for reference",
	// It points back at the jot before.
	"it", "it's", "its", "it'll", "itll", "it'd", "that", "that's", "thats", "this", "these", "those",
	"they", "they're", "theyre", "them", "here's", "heres", "here is", "here are",
	"for that", "for this", "for it", "for those", "for these", "for them", "on that", "on this",
	"in that", "in this", "with that", "with this", "about that", "about this", "re",
}

// labels are words that are a jot's own ask unless a colon follows them:
// "update the README" asks for work, "update: it is the web repo" adds.
var labels = []string{"update", "edit", "context", "note", "addendum", "clarification"}

// also is "also" among a jot's first words, as in "can you also add tests".
const alsoWithin = 4

// errorLine is the first line of an error message or a stack trace pasted
// alone: "TypeError: …", "error[E0308]: …", "panic: …", "fatal: …",
// "Traceback (most recent call last):".
var errorLine = regexp.MustCompile(`^(?:[\w.$]*(?:error|exception)(?:\[[^\]]*\])?|panic|fatal|traceback \(most recent call last\)):`)

// Follows reports whether text, typed in a task bar just after another
// jot, reads as more for that jot rather than a task of its own. why says
// what in it does, in a few words.
func Follows(text string) (why string, ok bool) {
	first, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	first = strings.ToLower(strings.TrimSpace(first))
	// Curly apostrophes, as macOS types them, count as straight ones.
	first = strings.NewReplacer("’", "'", "‘", "'").Replace(first)
	if first == "" {
		return "", false
	}
	switch {
	case strings.HasPrefix(first, "^"), strings.HasPrefix(first, "↑"):
		return "it points up at the jot before", true
	case hasAnyPrefix(first, "http://", "https://", "www.", "file://"):
		return "it is a link", true
	case hasAnyPrefix(first, "/", "~/", "./", "../"):
		return "it is a path", true
	case errorLine.MatchString(first):
		return "it is an error message", true
	}
	// Leading marks such as "- ", "* " or "… " do not count.
	first = strings.TrimLeft(first, "-*•…. \t")
	words := strings.Fields(first)
	if len(words) == 0 {
		return "", false
	}
	if w := words[0]; strings.HasSuffix(w, ":") && contains(labels, strings.TrimSuffix(w, ":")) {
		return "it starts with “" + w + "”", true
	}
	// Each word without the marks after it, so "btw," and "oh, and" match;
	// "p.s." and "n.b." keep their inner dots.
	plain := make([]string, len(words))
	for i, w := range words {
		plain[i] = strings.TrimRight(w, ",:;.!?—–-)\"'")
		if strings.Count(w, ".") > 1 {
			plain[i] = strings.TrimSuffix(strings.TrimRight(w, ",:;!?"), ".")
		}
	}
	for _, o := range openers {
		if ow := strings.Fields(o); len(ow) <= len(plain) && equal(plain[:len(ow)], ow) {
			return "it starts with “" + o + "”", true
		}
	}
	for _, w := range plain[:min(alsoWithin, len(plain))] {
		if w == "also" {
			return "it says “also”", true
		}
	}
	return "", false
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func equal(a, b []string) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
