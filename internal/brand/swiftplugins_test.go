package brand

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The Mac app leaves a plugin's Swift code out as the go build leaves out
// its Go code (DESIGN §4.17): for each go build tag noX a build has,
// mac/build.sh compiles with the Swift condition NOX, and the plugin's code
// is inside #if !NOX … #endif. A copy's source tree loses that code by
// those lines, so they keep one shape.

// pluginSwift names the Mac app's files that are one plugin's alone, each
// with the condition that leaves it out.
var pluginSwift = map[string]string{
	"Mail.swift":         "NOMAIL",
	"MailViews.swift":    "NOMAIL",
	"Jammin.swift":       "NOJAMMIN",
	"JamminSignIn.swift": "NOJAMMIN",
	"JamminViews.swift":  "NOJAMMIN",
	"VariorumTab.swift":  "NOWRITING",
	"Founding.swift":     "NOFOUNDING",
	"Yoink.swift":        "NOYOINK",
	"YoinkViews.swift":   "NOYOINK",
	"Markets.swift":      "NOMARKETS",
}

// directive is a line's compiler directive, such as "#if" or "#endif", and
// what follows it; "" for any other line.
func directive(line string) (string, string) {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "#") {
		return "", ""
	}
	d, rest, _ := strings.Cut(t, " ")
	switch d {
	case "#if", "#elseif", "#else", "#endif":
		return d, strings.TrimSpace(rest)
	}
	return "", ""
}

// TestSwiftPluginFiles: a file that is one plugin's is its condition's
// from its first line of code, after the file's leading comment, to its
// last: #if !NOX first and the #endif that closes it last. A copy that
// leaves the plugin out has no such file.
func TestSwiftPluginFiles(t *testing.T) {
	for name, cond := range pluginSwift {
		src, err := os.ReadFile(filepath.Join(root, "mac", "Sources", "Irie", name))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Error(err)
			continue
		}
		var lines []string
		for _, l := range strings.Split(string(src), "\n") {
			if s := strings.TrimSpace(l); s != "" && (len(lines) > 0 || !strings.HasPrefix(s, "//")) {
				lines = append(lines, l)
			}
		}
		if len(lines) < 2 || lines[0] != "#if !"+cond || lines[len(lines)-1] != "#endif" {
			t.Errorf("%s: its first line of code is not #if !%s, or its last is not #endif", name, cond)
			continue
		}
		depth := 0
		for i, l := range lines {
			switch d, _ := directive(l); d {
			case "#if":
				depth++
			case "#endif":
				depth--
			}
			if depth == 0 && i < len(lines)-1 {
				t.Errorf("%s:%q: the #if !%s on its first line closes before its last", name, l, cond)
				break
			}
		}
	}
}

// TestSwiftPluginConditions: every #if or #elseif in the Mac app that names
// a plugin's condition names only those, each the capitals of a go build
// tag a plugin's Go code has, with !, &&, || and parentheses, and nothing
// else on its line, such as os() or a comment: another condition goes in
// an #if of its own around or inside it.
func TestSwiftPluginConditions(t *testing.T) {
	tags := map[string]bool{}
	goBuild := regexp.MustCompile(`(?m)^//go:build (.*)$`)
	noTag := regexp.MustCompile(`\bno[a-z]+\b`)
	for _, dir := range []string{"internal", "cmd", "core"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range goBuild.FindAllStringSubmatch(string(src), -1) {
				for _, tag := range noTag.FindAllString(m[1], -1) {
					tags[strings.ToUpper(tag)] = true
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	files, err := filepath.Glob(filepath.Join(root, "mac", "Sources", "Irie", "*.swift"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no Swift files: %v", err)
	}
	word := regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)
	plugin := regexp.MustCompile(`^NO[A-Z0-9_]+$`)
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for i, l := range strings.Split(string(src), "\n") {
			d, cond := directive(l)
			if d != "#if" && d != "#elseif" {
				continue
			}
			words := word.FindAllString(cond, -1)
			named := false
			for _, w := range words {
				named = named || plugin.MatchString(w)
			}
			if !named {
				continue
			}
			for _, w := range words {
				if !tags[w] {
					t.Errorf("%s:%d: %q: %s is no plugin's condition, the capitals of a go build tag such as NOMAIL for nomail; another condition takes an #if of its own", filepath.Base(path), i+1, l, w)
				}
			}
			if rest := strings.Trim(word.ReplaceAllString(cond, ""), "!&|() "); rest != "" {
				t.Errorf("%s:%d: %q: a plugin's condition takes only !, &&, || and parentheses", filepath.Base(path), i+1, l)
			}
		}
	}
}
