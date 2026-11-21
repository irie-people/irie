// Command code finds code and prints it by its declarations, so that
// finding a thing and reading it take one call rather than a grep and
// then a sed for each hit (AGENTS.md, "Stack"). tools/code.sh runs it.
//
// Every call an agent makes is a round trip that reads its whole context
// again, so a call saved is worth more than the bytes it prints. Irie's
// journal for November 2026 had more than half of all greps followed,
// within two calls, by a read of the code they found.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const usage = `usage:
  tools/code.sh def NAME... [PATH...]   each declaration named NAME, whole, with its
                                        doc comment; NAME may be TYPE.NAME, for a
                                        method or a member of a type
  tools/code.sh outline PATH...         a file's declarations with their lines; for
                                        a folder, each file with what it declares
  tools/code.sh grep [-i] [-F] [-w] [--show[=LINES]] PATTERN [PATH...]
                                        the lines that match, under the declaration
                                        each sits in; --show prints those
                                        declarations whole, as many as fit in
                                        LINES (300), and lists the rest
  tools/code.sh refs [--show[=LINES]] NAME [PATH...]
                                        where NAME is used in code, never in a
                                        comment or a string, under the declaration
                                        each use sits in

PATH is a file or a folder in the checkout; the whole checkout by default.
PATTERN is a Go regular expression, as grep -E takes. Go is read with
go/parser, Swift with a lexer that knows its comments and strings, and
Markdown by its headings, so a hit in a doc comes under its section.
It exits 1 when nothing is found, as grep does.
`

// defaultBudget is how many lines --show prints whole when no number
// is given: about what a sed of a few windows would have printed.
const defaultBudget = 300

// maxHitLines caps the matching lines a report lists; past it, a line
// counts what is left in each file.
const maxHitLines = 150

// maxDefs is how many declarations def prints whole; past it, it lists
// them, so that a common name such as String is narrowed first.
const maxDefs = 6

// notFound is the error for a search that found nothing: exit status 1.
type notFound string

func (e notFound) Error() string { return string(e) }

func main() {
	err := run(os.Args[1:], os.Stdout)
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, "code:", err)
	var nf notFound
	if errors.As(err, &nf) {
		os.Exit(1)
	}
	os.Exit(2)
}

func run(args []string, w io.Writer) error {
	if len(args) == 0 {
		return errors.New("no command\n" + usage)
	}
	if a := args[0]; a == "-h" || a == "--help" || a == "help" {
		fmt.Fprint(w, usage)
		return nil
	}
	root, err := top()
	if err != nil {
		return err
	}
	switch args[0] {
	case "def":
		return cmdDef(root, args[1:], w)
	case "outline":
		return cmdOutline(root, args[1:], w)
	case "grep":
		return cmdGrep(root, args[1:], w)
	case "refs":
		return cmdRefs(root, args[1:], w)
	}
	return fmt.Errorf("no command %q\n%s", args[0], usage)
}

// top is the checkout's top folder.
func top() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", errors.New("not in a git checkout")
	}
	return strings.TrimSpace(string(out)), nil
}

// splitArgs takes each argument that names a file or a folder for a
// path, relative to the checkout's top, and the rest for names.
func splitArgs(root string, args []string) (names, paths []string, err error) {
	for _, a := range args {
		if _, statErr := os.Stat(a); statErr != nil {
			names = append(names, a)
			continue
		}
		rel, err := inRoot(root, a)
		if err != nil {
			return nil, nil, err
		}
		paths = append(paths, rel)
	}
	return names, paths, nil
}

// inRoot is path p relative to the checkout's top.
func inRoot(root, p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	if a, err := filepath.EvalSymlinks(abs); err == nil {
		abs = a
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("%s is outside the checkout", p)
	}
	return rel, nil
}

// listFiles lists the checkout's tracked files and its new ones that
// are not ignored, under the paths, relative to its top.
func listFiles(root string, paths []string) ([]string, error) {
	args := []string{"-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--"}
	for _, p := range paths {
		if p != "." {
			args = append(args, p)
		}
	}
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %v", err)
	}
	seen := map[string]bool{}
	var fs []string
	for _, f := range strings.Split(string(out), "\x00") {
		if f == "" || strings.HasSuffix(f, "/") || seen[f] {
			continue
		}
		seen[f] = true
		if st, err := os.Stat(filepath.Join(root, f)); err == nil && st.Mode().IsRegular() {
			fs = append(fs, f)
		}
	}
	sort.Strings(fs)
	return fs, nil
}

// readText reads a file, or nothing when it is too big to be source or
// holds a NUL byte in its first 8000, as a binary file does.
func readText(root, f string) []byte {
	b, err := os.ReadFile(filepath.Join(root, f))
	if err != nil || len(b) > 4<<20 {
		return nil
	}
	if bytes.IndexByte(b[:min(len(b), 8000)], 0) >= 0 {
		return nil
	}
	return b
}

// isCode says whether def and refs read a file: Go or Swift.
func isCode(f string) bool { return strings.HasSuffix(f, ".go") || strings.HasSuffix(f, ".swift") }

// cmdDef prints each declaration with one of the names, whole.
func cmdDef(root string, args []string, w io.Writer) error {
	names, paths, err := splitArgs(root, args)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return errors.New("def: no NAME\n" + usage)
	}
	docs := false // a section of a doc only when a doc is named
	for _, p := range paths {
		docs = docs || strings.HasSuffix(p, ".md")
	}
	fs, err := listFiles(root, paths)
	if err != nil {
		return err
	}
	type match struct {
		file  string
		lines []string
		d     decl
	}
	var ms []match
	for _, f := range fs {
		if !isCode(f) && !(docs && strings.HasSuffix(f, ".md")) {
			continue
		}
		src := readText(root, f)
		if src == nil || !mentionsAny(src, names) {
			continue
		}
		lines := splitLines(src)
		for _, d := range declsOf(f, src) {
			if namedAny(d, names) {
				ms = append(ms, match{f, lines, d})
			}
		}
	}
	if len(ms) == 0 {
		return notFound("no declaration named " + strings.Join(names, " or "))
	}
	if len(ms) > maxDefs {
		fmt.Fprintf(w, "%d declarations; name a file or folder, or TYPE.NAME, to print them:\n", len(ms))
		for _, m := range ms {
			fmt.Fprintf(w, "%s:%s  %s\n", m.file, span(m.d), m.d.label)
		}
		return nil
	}
	for i, m := range ms {
		if i > 0 {
			fmt.Fprintln(w)
		}
		printWhole(w, m.file, m.lines, m.d, nil)
	}
	return nil
}

// mentionsAny says whether src holds any of the names' last parts, a
// cheap test before a file is parsed.
func mentionsAny(src []byte, names []string) bool {
	for _, n := range names {
		if i := strings.LastIndex(n, "."); i >= 0 {
			n = n[i+1:]
		}
		if bytes.Contains(src, []byte(n)) {
			return true
		}
	}
	return false
}

// namedAny says whether d is declared with one of the names: NAME, or
// TYPE.NAME for a member of a type.
func namedAny(d decl, names []string) bool {
	for _, n := range names {
		if d.name == n {
			return true
		}
		if i := strings.LastIndex(n, "."); i > 0 && d.parent == n[:i] && d.name == n[i+1:] {
			return true
		}
	}
	return false
}

// printWhole prints a declaration with its line numbers, a > after the
// number of each line in mark.
func printWhole(w io.Writer, file string, lines []string, d decl, mark map[int]bool) {
	fmt.Fprintf(w, "%s:%s  %s\n", file, span(d), d.label)
	for n := d.start; n <= d.end && n <= len(lines); n++ {
		sep := "  "
		if mark[n] {
			sep = "> "
		}
		fmt.Fprintf(w, "%5d%s%s\n", n, sep, lines[n-1])
	}
}

// cmdOutline prints each file's declarations, or a line for each file
// of a folder with the names it declares.
func cmdOutline(root string, args []string, w io.Writer) error {
	names, paths, err := splitArgs(root, args)
	if err != nil {
		return err
	}
	if len(names) > 0 {
		return fmt.Errorf("outline: no file or folder %s", strings.Join(names, ", "))
	}
	if len(paths) == 0 {
		return errors.New("outline: no PATH\n" + usage)
	}
	for _, p := range paths {
		st, err := os.Stat(filepath.Join(root, p))
		if err != nil {
			return err
		}
		if !st.IsDir() {
			src := readText(root, p)
			ds := declsOf(p, src)
			fmt.Fprintf(w, "%s (%d lines)\n", p, len(splitLines(src)))
			for i, d := range ds {
				fmt.Fprintf(w, "%s%s  %s\n", strings.Repeat("  ", 1+depth(ds, i)), span(d), d.label)
			}
			continue
		}
		fs, err := listFiles(root, []string{p})
		if err != nil {
			return err
		}
		for _, f := range fs {
			src := readText(root, f)
			if src == nil {
				continue
			}
			var ns []string
			for _, d := range declsOf(f, src) {
				if d.name != "" && d.parent == "" || strings.HasSuffix(f, ".go") && d.name != "" {
					ns = append(ns, d.name)
				}
			}
			line := fmt.Sprintf("%s (%d lines)", f, len(splitLines(src)))
			if len(ns) > 0 {
				line += ": " + cut(strings.Join(ns, ", "), 160)
			}
			fmt.Fprintln(w, line)
		}
	}
	return nil
}

// span is a declaration's lines: 21, or 22-24.
func span(d decl) string {
	if d.start == d.end {
		return strconv.Itoa(d.start)
	}
	return fmt.Sprintf("%d-%d", d.start, d.end)
}

// depth is how many of the file's declarations hold declaration i.
func depth(ds []decl, i int) int {
	n := 0
	for j, d := range ds {
		if j != i && d.start <= ds[i].start && ds[i].end <= d.end && d.end-d.start > ds[i].end-ds[i].start {
			n++
		}
	}
	return n
}

// cut shortens s to at most n bytes, saying so.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + " …"
}

// A group is the matching lines in one declaration of a file, or in a
// file outside any declaration, where d is nil.
type group struct {
	file  string
	lines []string
	d     *decl
	hits  []int
}

// size is how many lines printing the group's declaration whole takes.
func (g *group) size() int {
	if g.d == nil {
		return 0
	}
	return g.d.end - g.d.start + 1
}

// pick chooses, of the declarations that hold a match, the ones --show
// prints whole within budget lines; it lists the others by their lines
// and matches, so that a later call can print the one wanted. Every group
// goes into exactly one of the two lists, in the order given (by file,
// then by line), and the sizes of those printed whole add up to no more
// than budget.
//
// The ones with the most matches for their length go first, the smaller
// first among equals, each taken if it still fits: a long switch with
// one match gives way to the short functions it would crowd out, and a
// single long function that fits is printed, which is what a second
// call would have fetched.
func pick(gs []*group, budget int) (whole, listed []*group) {
	order := make([]int, len(gs))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		x, y := gs[order[a]], gs[order[b]]
		// x.hits/x.size > y.hits/y.size, without dividing.
		if dx, dy := len(x.hits)*y.size(), len(y.hits)*x.size(); dx != dy {
			return dx > dy
		}
		return x.size() < y.size()
	})
	take := make([]bool, len(gs))
	left := budget
	for _, i := range order {
		if s := gs[i].size(); s <= left {
			take[i] = true
			left -= s
		}
	}
	for i, g := range gs {
		if take[i] {
			whole = append(whole, g)
		} else {
			listed = append(listed, g)
		}
	}
	return whole, listed
}

// report prints the groups: with show, as many of their declarations
// whole as pick lets, then the others' matching lines.
func report(w io.Writer, gs []*group, show bool, budget int) {
	var listed []*group
	if show {
		var decls, outside []*group
		for _, g := range gs {
			if g.d != nil {
				decls = append(decls, g)
			} else {
				outside = append(outside, g)
			}
		}
		whole, rest := pick(decls, budget)
		for _, g := range whole {
			mark := map[int]bool{}
			for _, n := range g.hits {
				mark[n] = true
			}
			printWhole(w, g.file, g.lines, *g.d, mark)
		}
		if len(whole) > 0 && len(rest)+len(outside) > 0 {
			fmt.Fprintf(w, "\nNot printed whole (tools/code.sh def NAME FILE prints one):\n")
		}
		listed = append(rest, outside...)
		sort.SliceStable(listed, func(i, j int) bool { return listed[i].file < listed[j].file })
	} else {
		listed = gs
	}
	shown := 0
	left := map[string]int{}
	var order []string
	for _, g := range listed {
		if shown >= maxHitLines {
			if left[g.file] == 0 {
				order = append(order, g.file)
			}
			left[g.file] += len(g.hits)
			continue
		}
		if g.d != nil {
			fmt.Fprintf(w, "%s:%s  %s\n", g.file, span(*g.d), g.d.label)
		} else {
			fmt.Fprintf(w, "%s\n", g.file)
		}
		for _, n := range g.hits {
			fmt.Fprintf(w, "%5d  %s\n", n, cut(strings.TrimSpace(g.lines[n-1]), 160))
			shown++
		}
	}
	if len(order) > 0 {
		n := 0
		parts := make([]string, 0, len(order))
		for _, f := range order {
			n += left[f]
			parts = append(parts, fmt.Sprintf("%s (%d)", f, left[f]))
		}
		fmt.Fprintf(w, "… %d more matching lines in %d files: %s\n", n, len(order), cut(strings.Join(parts, ", "), 600))
	}
}

// groupHits puts a file's matching lines under the innermost declaration
// each sits in.
func groupHits(file string, src []byte, hits []int) []*group {
	lines := splitLines(src)
	ds := declsOf(file, src)
	var gs []*group
	byDecl := map[*decl]*group{}
	var outside *group
	for _, n := range hits {
		d := innermost(ds, n)
		if d == nil {
			if outside == nil {
				outside = &group{file: file, lines: lines}
				gs = append(gs, outside)
			}
			outside.hits = append(outside.hits, n)
			continue
		}
		g := byDecl[d]
		if g == nil {
			g = &group{file: file, lines: lines, d: d}
			byDecl[d] = g
			gs = append(gs, g)
		}
		g.hits = append(g.hits, n)
	}
	return gs
}

// showFlag reads --show or --show=LINES.
func showFlag(a string) (bool, int, error) {
	if a == "--show" || a == "-s" {
		return true, defaultBudget, nil
	}
	if v, ok := strings.CutPrefix(a, "--show="); ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return false, 0, fmt.Errorf("--show=%s: not a number of lines", v)
		}
		return true, n, nil
	}
	return false, 0, nil
}

// cmdGrep prints the lines that match a pattern, under their
// declarations.
func cmdGrep(root string, args []string, w io.Writer) error {
	var fold, fixed, word, show bool
	budget := defaultBudget
	for len(args) > 0 && strings.HasPrefix(args[0], "-") && len(args[0]) > 1 {
		a := args[0]
		args = args[1:]
		if a == "--" {
			break
		}
		if s, n, err := showFlag(a); err != nil {
			return err
		} else if s {
			show, budget = true, n
			continue
		}
		for _, c := range strings.TrimPrefix(a, "-") {
			switch c {
			case 'i':
				fold = true
			case 'F':
				fixed = true
			case 'w':
				word = true
			case 'E', 'n', 'r':
				// grep's habits, which this does anyway
			default:
				return fmt.Errorf("grep: no flag -%c\n%s", c, usage)
			}
		}
	}
	if len(args) == 0 {
		return errors.New("grep: no PATTERN\n" + usage)
	}
	pat := args[0]
	if fixed {
		pat = regexp.QuoteMeta(pat)
	} else {
		pat = strings.ReplaceAll(pat, `\|`, `|`) // grep's basic syntax for or
	}
	if word {
		pat = `\b(?:` + pat + `)\b`
	}
	if fold {
		pat = `(?i)` + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return fmt.Errorf("grep: %v", err)
	}
	names, paths, err := splitArgs(root, args[1:])
	if err != nil {
		return err
	}
	if len(names) > 0 {
		return fmt.Errorf("grep: no file or folder %s", strings.Join(names, ", "))
	}
	fs, err := listFiles(root, paths)
	if err != nil {
		return err
	}
	var gs []*group
	for _, f := range fs {
		src := readText(root, f)
		if src == nil {
			continue
		}
		idx := re.FindAllIndex(src, -1)
		if idx == nil {
			continue
		}
		starts := lineStarts(src)
		var hits []int
		for _, x := range idx {
			if n := lineOf(starts, x[0]); len(hits) == 0 || hits[len(hits)-1] != n {
				hits = append(hits, n)
			}
		}
		gs = append(gs, groupHits(f, src, hits)...)
	}
	if len(gs) == 0 {
		return notFound(fmt.Sprintf("no match for %s in %d files", args[0], len(fs)))
	}
	report(w, gs, show, budget)
	return nil
}

// cmdRefs prints where a name is used in Go and Swift code.
func cmdRefs(root string, args []string, w io.Writer) error {
	show, budget := false, defaultBudget
	if len(args) > 0 {
		s, n, err := showFlag(args[0])
		if err != nil {
			return err
		}
		if s {
			show, budget, args = true, n, args[1:]
		}
	}
	if len(args) == 0 {
		return errors.New("refs: no NAME\n" + usage)
	}
	name := args[0]
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	names, paths, err := splitArgs(root, args[1:])
	if err != nil {
		return err
	}
	if len(names) > 0 {
		return fmt.Errorf("refs: no file or folder %s", strings.Join(names, ", "))
	}
	fs, err := listFiles(root, paths)
	if err != nil {
		return err
	}
	var gs []*group
	for _, f := range fs {
		if !isCode(f) {
			continue
		}
		src := readText(root, f)
		if src == nil || !bytes.Contains(src, []byte(name)) {
			continue
		}
		var hits []int
		if strings.HasSuffix(f, ".go") {
			hits = dedupe(goIdents(f, src, name))
		} else {
			hits = wordLines(swiftMask(src), name)
		}
		if len(hits) > 0 {
			gs = append(gs, groupHits(f, src, hits)...)
		}
	}
	if len(gs) == 0 {
		return notFound("no use of " + name + " in code")
	}
	report(w, gs, show, budget)
	return nil
}

// dedupe sorts line numbers and drops repeats.
func dedupe(ns []int) []int {
	sort.Ints(ns)
	out := ns[:0]
	for i, n := range ns {
		if i == 0 || n != ns[i-1] {
			out = append(out, n)
		}
	}
	return out
}
