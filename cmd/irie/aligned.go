package main

import (
	"strings"

	"github.com/irie-people/irie/internal/brand"
)

// aligned is a help text with its columns put right for this product's
// command (DESIGN §2.10). The texts are laid out for a command of four
// letters: each command's description starts at the column of the lines
// under it. A command of another length moves that start, so aligned
// narrows or widens the gap before it by the difference, and where the gap
// would close, starts the description on a line of its own.
func aligned(s string) string { return align(s, brand.Slug) }

// laidOut is the length of the command the help texts are laid out for:
// Irie's, which has four letters.
const laidOut = 4

// align is aligned for a command called slug.
func align(s, slug string) string {
	d := len(slug) - laidOut
	if d == 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		// A command's first line: indented a little, then the command, then
		// its arguments, a gap of two spaces or more, and its description.
		in := len(l) - len(strings.TrimLeft(l, " "))
		rest := l[in:]
		if in == 0 || in > 8 || !(rest == slug || strings.HasPrefix(rest, slug+" ")) {
			out = append(out, l)
			continue
		}
		gap := strings.Index(l[in+len(slug):], "  ")
		if gap < 0 {
			out = append(out, l)
			continue
		}
		gap += in + len(slug)
		end := gap
		for end < len(l) && l[end] == ' ' {
			end++
		}
		if end == len(l) {
			out = append(out, l)
			continue
		}
		// Where the description starts for a command of four letters.
		col := end - d
		if col-gap >= 2 {
			out = append(out, l[:gap]+strings.Repeat(" ", col-gap)+l[end:])
		} else {
			out = append(out, l[:gap], strings.Repeat(" ", col)+l[end:])
		}
	}
	return strings.Join(out, "\n")
}
