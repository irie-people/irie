package main

import (
	"bytes"
	"go/format"
	"strings"
	"testing"
)

// copyTags are the tags the tests prune by.
var copyTags = map[string]bool{"noprivacy": true, "nowriting": true}

func TestPruneGoSettlesEachConstraint(t *testing.T) {
	const body = "\npackage p\n\nvar x = 1\n"
	for _, c := range []struct {
		build  string
		action goAction
		want   string // the new //go:build expression, for goRewrite
	}{
		{"linux && !noprivacy", goDelete, ""},
		{"novpn || noprivacy", goUnwrap, ""},
		{"!novpn && noprivacy", goRewrite, "!novpn"},
		{"!(noprivacy && novpn)", goRewrite, "!novpn"},
		{"!(noprivacy || novpn)", goDelete, ""},
		{"(darwin || linux) && !noprivacy || nochat && nowriting", goRewrite, "nochat"},
		{"(darwin || linux) && noprivacy && !novpn", goRewrite, "(darwin || linux) && !novpn"},
		{"!novpn && !noprivacy && !nowriting && !nochat", goDelete, ""},
		// What the tags leave can still be sure either way.
		{"linux || !linux || !noprivacy", goUnwrap, ""},
		{"linux && !linux || !noprivacy", goDelete, ""},
		// The copy's tags absent: as it was.
		{"linux || darwin", goKeep, ""},
		{"!novpn", goKeep, ""},
	} {
		src := "//go:build " + c.build + "\n" + body
		e, err := pruneGo([]byte(src), copyTags)
		if err != nil {
			t.Errorf("%s: %v", c.build, err)
			continue
		}
		if e.action != c.action {
			t.Errorf("%s: action %d, want %d", c.build, e.action, c.action)
			continue
		}
		var want string
		switch c.action {
		case goUnwrap:
			want = strings.TrimPrefix(body, "\n")
		case goRewrite:
			want = "//go:build " + c.want + "\n" + body
		default:
			if e.src != nil {
				t.Errorf("%s: new text %q for a file it does not keep changed", c.build, e.src)
			}
			continue
		}
		if string(e.src) != want {
			t.Errorf("%s: got\n%s\nwant\n%s", c.build, e.src, want)
		}
		if f, err := format.Source(e.src); err != nil || !bytes.Equal(f, e.src) {
			t.Errorf("%s: gofmt would change\n%s\nto\n%s (%v)", c.build, e.src, f, err)
		}
	}
}

func TestPruneGoFindsTheLineWhereGoBuildDoes(t *testing.T) {
	for _, c := range []struct{ name, src, want string }{
		{"after a comment and a blank line",
			"// Copyright\n\n//go:build noprivacy\n\npackage p\n",
			"// Copyright\n\npackage p\n"},
		{"right under a comment, which must not become the package's doc",
			"// Copyright\n//go:build noprivacy\n\npackage p\n",
			"// Copyright\n\npackage p\n"},
		{"after a block comment",
			"/* Copyright\n   2026 */\n//go:build noprivacy\n\npackage p\n",
			"/* Copyright\n   2026 */\n\npackage p\n"},
		{"with no blank line after it",
			"//go:build noprivacy\npackage p\n",
			"package p\n"},
	} {
		e, err := pruneGo([]byte(c.src), copyTags)
		if err != nil || e.action != goUnwrap {
			t.Errorf("%s: action %d, %v", c.name, e.action, err)
			continue
		}
		if string(e.src) != c.want {
			t.Errorf("%s: got\n%s\nwant\n%s", c.name, e.src, c.want)
		}
	}
	for _, src := range []string{
		"/*\n//go:build noprivacy\n*/\n\npackage p\n",
		"package p\n\n//go:build noprivacy\n",
		"package p\n\nconst s = `\n//go:build noprivacy\n`\n",
		"package p\n",
	} {
		e, err := pruneGo([]byte(src), copyTags)
		if err != nil || e.action != goKeep {
			t.Errorf("%q: action %d, %v; want it kept as it is", src, e.action, err)
		}
	}
	for _, src := range []string{
		"//go:build noprivacy\n//go:build linux\n\npackage p\n",
		"// +build noprivacy\n\npackage p\n",
		"//go:build noprivacy &&\n\npackage p\n",
	} {
		if _, err := pruneGo([]byte(src), copyTags); err == nil {
			t.Errorf("%q: no error", src)
		}
	}
}
