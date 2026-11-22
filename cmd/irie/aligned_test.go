package main

import (
	"strings"
	"testing"

	"github.com/irie-people/irie/internal/brand"
)

// TestHelpLinesUpForAnyName: whatever a copy's command is called, a
// command's description in the help starts at the column of the lines
// under it (DESIGN §2.10).
func TestHelpLinesUpForAnyName(t *testing.T) {
	texts := map[string]string{"usage": usage, "ctl": ctlUsage, "remote": remoteUsage, "peer": peerUsage, "plugins": pluginsUsage}
	for _, slug := range []string{"ab", brand.Slug, "pesto", "acme-workbench"} {
		for name, text := range texts {
			lines := strings.Split(align(strings.ReplaceAll(text, brand.Slug, slug), slug), "\n")
			for i, l := range lines[:len(lines)-1] {
				in := len(l) - len(strings.TrimLeft(l, " "))
				if in == 0 || in > 8 || !strings.HasPrefix(l[in:], slug+" ") {
					continue
				}
				gap := strings.Index(l[in+len(slug):], "  ")
				if gap < 0 {
					continue
				}
				col := in + len(slug) + gap
				for col < len(l) && l[col] == ' ' {
					col++
				}
				next := lines[i+1]
				under := len(next) - len(strings.TrimLeft(next, " "))
				if under <= 8 || col == len(l) {
					continue // a description of one line, or none
				}
				if col != under {
					t.Errorf("%s help as %q: the description starts at column %d, the line under it at %d:\n%s\n%s", name, slug, col, under, l, next)
				}
			}
		}
	}
}

// TestAlignMovesOnlyTheGap: align narrows or widens the gap before a
// description, and moves one whose gap would close to a line of its own.
func TestAlignMovesOnlyTheGap(t *testing.T) {
	sp := strings.Repeat
	// Laid out for a command of four letters, with the descriptions at
	// column 20, then given a command of five.
	laid := func(cmd string) string {
		return "usage: " + cmd + " [command]\n\n" +
			"  " + cmd + " up" + sp(" ", 20-2-len("irie up")) + "start it\n" +
			sp(" ", 20) + "and more\n" +
			"  " + cmd + " attach [-x]" + sp(" ", 20-2-len("irie attach [-x]")) + "attach\n" +
			sp(" ", 20) + "this terminal\n"
	}
	want := "usage: pesto [command]\n\n" +
		"  pesto up" + sp(" ", 20-len("  pesto up")) + "start it\n" +
		sp(" ", 20) + "and more\n" +
		"  pesto attach [-x]\n" +
		sp(" ", 20) + "attach\n" +
		sp(" ", 20) + "this terminal\n"
	if got := align(laid("pesto"), "pesto"); got != want {
		t.Errorf("align =\n%s\nwant\n%s", got, want)
	}
	if in := laid("irie"); align(in, "irie") != in {
		t.Errorf("align for a command of four letters changed the text:\n%s", align(in, "irie"))
	}
}
