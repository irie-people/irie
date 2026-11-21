//go:build !nochat

package chat

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// What a service sends is shown as text, never as markup, and without what
// could rewrite a terminal's lines or hide where a link goes: control
// characters, and the marks that reverse text.

// Clean is a message's text as a person reads it: whole lines, with tabs,
// and nothing that moves a terminal's cursor or turns text around.
func Clean(s string) string {
	s = strings.ToValidUTF8(s, "�")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case hides(r):
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// Line is a name or a title on one line, at most n characters.
func Line(s string, n int) string {
	s = strings.Join(strings.Fields(Clean(s)), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

// hides says whether r is a character no message should carry to a
// screen: a control character, or a mark that turns text around.
func hides(r rune) bool {
	switch {
	case r < ' ', r == 0x7f, r >= 0x80 && r < 0xa0:
		return true
	case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069, r == 0x200e, r == 0x200f, r == 0x061c:
		return true
	case r == 0x2028 || r == 0x2029:
		return true
	}
	return unicode.Is(unicode.Cs, r)
}

// count is how many characters s has, as the services count them.
func count(s string) int { return utf8.RuneCountInString(s) }
