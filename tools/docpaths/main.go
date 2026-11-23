// Command docpaths checks that every repository path the docs name in
// backquotes exists, so that the docs can't drift from the code unseen
// (DESIGN §2.12). tools/check.sh runs it, through tools/docpaths.sh. It
// reads docs/*.md and the Markdown files at the top of the repository,
// but PROVENANCE.md and CHANGELOG.md, which record the past; the plans in
// docs/proposals it leaves alone.
//
// A path is a span in backquotes with no space in it, under one of the
// repository's top folders: `internal/core/dog.go`, `tools/check.sh`,
// `mac/`. It may end in a line, `internal/core/pane.go:12-20`, a heading,
// `docs/MAC.md#code-cards`, or a Go pattern, `./internal/...`; it may be a
// glob, `internal/core/*_off.go`, or offer choices,
// `site/media/intro.{webm,mp4}`, each of which must exist. Names must
// match in case, as on Linux, even where the disk ignores it. A path with
// a placeholder in it, such as `<name>` or `$VAR`, isn't checked, nor one
// marked as not real: "for example" or "e.g." just before it, or
// "(planned)" just after it. Nor is a path git ignores, such as a build's
// output (`mac/build/Irie.app`), which a fresh checkout lacks.
//
// A company's copy whose brand leaves plugins out (the Tags in
// internal/brand, DESIGN §2.10) keeps Irie's docs, which name those
// plugins' code, and tools/prune takes that code out of the copy's source.
// There a path the checkout's own history deleted is no finding: Irie's
// check holds the same docs to Irie's code.
//
// The idea is Blitz's tools/specs (PROVENANCE.md). It uses the standard
// library alone, so tools/gotool.sh builds it once for each version.
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// roots are the folders at the top of the repository a doc's path may
// start with, and e2e/, which DESIGN plans. TestRootsAreTheTopFolders
// keeps the list whole.
var roots = []string{".github", "cmd", "core", "docs", "e2e", "internal", "ios", "mac", "site", "tools"}

// past are the Markdown files at the top that record the past rather than
// describe the code as it is: a path they name may be long gone.
var past = []string{"CHANGELOG.md", "PROVENANCE.md"}

// A finding is a path a doc names that isn't in the repository.
type finding struct {
	file string
	line int
	path string
}

func (f finding) String() string {
	return fmt.Sprintf("%s:%d: `%s` names nothing in the repository", f.file, f.line, f.path)
}

func main() {
	if len(os.Args) > 1 {
		fmt.Fprintln(os.Stderr, "usage: tools/docpaths.sh (from anywhere in the repository)")
		os.Exit(2)
	}
	files, err := docs(".")
	if err != nil {
		fail(err)
	}
	r := newRepo(".")
	var found []finding
	for _, f := range files {
		text, err := os.ReadFile(f)
		if err != nil {
			fail(err)
		}
		found = append(found, scan(f, string(text), r.exists)...)
	}
	found = unignored(found)
	if len(found) > 0 && leavesPluginsOut(".") {
		found = undeleted(".", found)
	}
	if len(found) == 0 {
		return
	}
	for _, f := range found {
		fmt.Fprintln(os.Stderr, "docpaths: "+f.String())
	}
	fmt.Fprintln(os.Stderr, `docpaths: fix each path, or mark it: "(planned)" after a path not built yet, "for example" before an example`)
	os.Exit(1)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "docpaths:", err)
	os.Exit(2)
}

// docs lists the Markdown files to check under root, relative to it.
func docs(root string) ([]string, error) {
	var out []string
	for _, pattern := range []string{"docs/*.md", "*.md"} {
		matches, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			rel, err := filepath.Rel(root, m)
			if err != nil {
				return nil, err
			}
			if !slices.Contains(past, rel) {
				out = append(out, filepath.ToSlash(rel))
			}
		}
	}
	return out, nil
}

var (
	// span is a code span with no space in it.
	span = regexp.MustCompile("`([^`\\s]+)`")
	// atLine is a path followed by a line or lines: pane.go:12-20.
	atLine = regexp.MustCompile(`^(.+?):[0-9]+(?:[-–][0-9]+)?$`)
)

// scan returns the paths a doc's text names that don't exist; name is the
// doc's file.
func scan(name, text string, exists func(string) bool) []finding {
	var out []finding
	fence := ""
	for i, line := range strings.Split(text, "\n") {
		if f := fenceOf(line); f != "" {
			switch {
			case fence == "":
				fence = f
			case f[0] == fence[0] && len(f) >= len(fence):
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		for _, loc := range span.FindAllStringSubmatchIndex(line, -1) {
			p, ok := repoPath(line[loc[2]:loc[3]])
			if !ok || example(line[:loc[0]]) || planned(line[loc[1]:]) {
				continue
			}
			if !exists(p) {
				out = append(out, finding{name, i + 1, p})
			}
		}
	}
	return out
}

// fenceOf is the fence a line of Markdown opens or closes a code block
// with, ``` or ~~~ and longer, or "".
func fenceOf(line string) string {
	line = strings.TrimLeft(line, " \t")
	for _, c := range []string{"`", "~"} {
		if n := len(line) - len(strings.TrimLeft(line, c)); n >= 3 {
			return line[:n]
		}
	}
	return ""
}

// repoPath is the path a code span names, without the line, heading or
// Go pattern after it, when it is one this check can look for.
func repoPath(s string) (string, bool) {
	s = strings.TrimRight(s, ".,;:)")
	if m := atLine.FindStringSubmatch(s); m != nil {
		s = m[1]
	}
	if i := strings.IndexByte(s, '#'); i > 0 {
		s = s[:i]
	}
	// A Go pattern, ./internal/..., has lost its dots with the
	// punctuation: what is left is its folder.
	s = strings.TrimPrefix(s, "./")
	// A placeholder, a home, an address, a range or a URL.
	if strings.ContainsAny(s, "<>$~@…:") || strings.Contains(s, "//") || strings.Contains(s, "..") {
		return "", false
	}
	first, _, sub := strings.Cut(s, "/")
	if !sub || !slices.Contains(roots, first) {
		return "", false
	}
	return s, true
}

// example says whether the text just before a path offers it as an
// example.
func example(before string) bool {
	tail := strings.ToLower(before[max(0, len(before)-24):])
	return strings.Contains(tail, "e.g.") || strings.Contains(tail, "example")
}

// planned says whether the text just after a path says it isn't built yet.
func planned(after string) bool {
	return strings.HasPrefix(strings.TrimLeft(after, " "), "(planned")
}

// repo looks for paths in a checkout, by the names its folders list, so
// that a name must match in case even on a disk that ignores case.
type repo struct {
	root  string
	names map[string]map[string]bool
}

func newRepo(root string) *repo {
	return &repo{root: root, names: map[string]map[string]bool{}}
}

// list is the names in the folder dir, relative to the root.
func (r *repo) list(dir string) map[string]bool {
	if names, ok := r.names[dir]; ok {
		return names
	}
	names := map[string]bool{}
	entries, _ := os.ReadDir(filepath.Join(r.root, dir))
	for _, e := range entries {
		names[e.Name()] = true
	}
	r.names[dir] = names
	return names
}

// exists says whether p is in the checkout: a file or folder, a glob that
// matches one, or, for {a,b}, each choice.
func (r *repo) exists(p string) bool {
	if i := strings.IndexByte(p, '{'); i >= 0 {
		if j := strings.IndexByte(p[i:], '}'); j > 0 {
			for _, alt := range strings.Split(p[i+1:i+j], ",") {
				if !r.exists(p[:i] + alt + p[i+j+1:]) {
					return false
				}
			}
			return true
		}
	}
	return r.match(".", strings.Split(strings.TrimSuffix(p, "/"), "/"))
}

// match says whether the path parts lead from dir to something in the
// checkout, each part a name or a glob of names.
func (r *repo) match(dir string, parts []string) bool {
	if len(parts) == 0 {
		return true
	}
	names := r.list(dir)
	if !strings.ContainsAny(parts[0], `*?[\`) {
		return names[parts[0]] && r.match(path.Join(dir, parts[0]), parts[1:])
	}
	for name := range names {
		if ok, _ := path.Match(parts[0], name); ok && r.match(path.Join(dir, name), parts[1:]) {
			return true
		}
	}
	return false
}

// tagsLine is the Tags line of internal/brand/brand.go, as tools/brand.sh
// reads it.
var tagsLine = regexp.MustCompile(`(?m)^[ \t]*Tags[ \t]*=[ \t]*"([^"]*)"`)

// leavesPluginsOut says whether the checkout at root is a copy whose brand
// leaves plugins out of its builds, and so perhaps out of its source.
func leavesPluginsOut(root string) bool {
	b, err := os.ReadFile(filepath.Join(root, "internal", "brand", "brand.go"))
	if err != nil {
		return false
	}
	m := tagsLine.FindSubmatch(b)
	return m != nil && len(bytes.TrimSpace(m[1])) > 0
}

// undeleted drops the findings for paths the history of the checkout at
// root deleted: a file, a folder it deleted files from, or a glob that
// matches one, as when tools/prune took a plugin's code out of a copy.
func undeleted(root string, found []finding) []finding {
	cmd := exec.Command("git", "log", "--diff-filter=D", "--name-only", "--format=", "HEAD")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return found
	}
	var gone []string
	for _, p := range strings.Split(string(out), "\n") {
		if p != "" {
			gone = append(gone, p)
		}
	}
	var kept []finding
	for _, f := range found {
		if !deletedIn(f.path, gone) {
			kept = append(kept, f)
		}
	}
	return kept
}

// deletedIn says whether p names something among the deleted paths gone.
func deletedIn(p string, gone []string) bool {
	p = strings.TrimSuffix(p, "/")
	for _, d := range gone {
		if d == p || strings.HasPrefix(d, p+"/") {
			return true
		}
		if ok, _ := path.Match(p, d); ok {
			return true
		}
	}
	return false
}

// unignored drops the findings for paths git ignores, such as a build's
// output, which a fresh checkout lacks and a built one has.
func unignored(found []finding) []finding {
	if len(found) == 0 {
		return nil
	}
	var in bytes.Buffer
	for _, f := range found {
		in.WriteString(f.path + "\n")
	}
	cmd := exec.Command("git", "check-ignore", "--stdin")
	cmd.Stdin = &in
	// 0: some are ignored; 1: none are.
	out, err := cmd.Output()
	if err != nil && cmd.ProcessState == nil {
		return found
	}
	ignored := map[string]bool{}
	for _, p := range strings.Split(string(out), "\n") {
		ignored[p] = true
	}
	var kept []finding
	for _, f := range found {
		if !ignored[f.path] {
			kept = append(kept, f)
		}
	}
	return kept
}
