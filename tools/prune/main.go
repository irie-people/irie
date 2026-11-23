// Command prune takes the plugins a company's copy of Irie leaves out of
// its builds out of its source tree too, so that the copy it publishes
// carries none of their code. A copy leaves plugins out with go build
// tags, the Tags in internal/brand/brand.go (DESIGN §4.17); prune reads
// them, refuses a checkout with none, and with every one of them set
// removes, in turn:
//
//  1. Go files whose //go:build line is then false, whatever the other
//     tags, GOOS and GOARCH. A line they make true goes, with the blank
//     line after it; one that still depends on the rest is rewritten
//     without the copy's tags, as gofmt prints it.
//  2. The packages under internal/ that nothing left needs: outside go
//     list -deps -test of the main packages, those with only tests and
//     every package outside internal/, and imported by no file left,
//     whatever its constraint.
//  3. The files only the deleted Go files embedded (//go:embed).
//  4. The Mac app's code for those plugins. Under mac/Sources each tag noX
//     is the Swift condition NOX, and #if blocks of NO* conditions are
//     settled as the //go:build lines are; a file left with no code goes.
//  5. The modules only the removed code used, with go mod tidy.
//
// Then go build ./... and go vet ./... must pass with no tags at all.
package main

import (
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const usage = `usage: go run ./tools/prune [-n] [DIR]

Removes the plugins a company's copy leaves out, by the Tags in
DIR/internal/brand/brand.go, from DIR's source tree, then checks that
go build ./... and go vet ./... pass with no tags. DIR is the checkout,
the one holding the current folder by default. -n prints what it would
do and changes nothing.
`

func main() {
	dry := flag.Bool("n", false, "print what it would do and change nothing")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()
	if flag.NArg() > 1 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(flag.Arg(0), *dry, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "prune:", err)
		os.Exit(1)
	}
}

// run prunes the checkout at dir, or the one holding the current folder
// when dir is "", saying what it does on w and passing the go command's
// own output to goOut.
func run(dir string, dry bool, w, goOut io.Writer) error {
	root, err := checkout(dir)
	if err != nil {
		return err
	}
	tags, err := readTags(root)
	if err != nil {
		return err
	}
	p, err := makePlan(root, tags)
	if err != nil {
		return err
	}
	p.print(w)
	if dry {
		fmt.Fprintln(w, "\nthen go mod tidy, go build ./... and go vet ./...")
		fmt.Fprintln(w, "-n: nothing changed")
		return nil
	}
	if err := p.apply(); err != nil {
		return err
	}
	for _, args := range [][]string{{"mod", "tidy"}, {"build", "./..."}, {"vet", "./..."}} {
		fmt.Fprintln(w, "go", strings.Join(args, " "))
		cmd := goCommand(root, args...)
		cmd.Stdout, cmd.Stderr = goOut, goOut
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("go %s failed after pruning: %v", strings.Join(args, " "), err)
		}
	}
	fmt.Fprintln(w, "pruned; go build ./... and go vet ./... pass with no tags")
	return nil
}

// checkout is the checkout's folder, without symbolic links: dir, or the
// nearest folder holding a go.mod from the current one up.
func checkout(dir string) (string, error) {
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		for dir = wd; !exists(filepath.Join(dir, "go.mod")); dir = filepath.Dir(dir) {
			if filepath.Dir(dir) == dir {
				return "", fmt.Errorf("no go.mod in %s or above it", wd)
			}
		}
	}
	root, err := filepath.Abs(dir)
	if err == nil {
		root, err = filepath.EvalSymlinks(root)
	}
	if err != nil {
		return "", err
	}
	if !exists(filepath.Join(root, "go.mod")) {
		return "", fmt.Errorf("%s has no go.mod: it is no checkout", root)
	}
	return root, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// readTags reads the copy's build tags, the Tags constant in
// internal/brand/brand.go, comma-separated, and refuses none.
func readTags(root string) ([]string, error) {
	path := filepath.Join(root, "internal", "brand", "brand.go")
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST {
			continue
		}
		for _, s := range g.Specs {
			v := s.(*ast.ValueSpec)
			for i, n := range v.Names {
				if n.Name != "Tags" || i >= len(v.Values) {
					continue
				}
				lit, ok := v.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return nil, fmt.Errorf("%s: Tags is not a string", path)
				}
				s, err := strconv.Unquote(lit.Value)
				if err != nil {
					return nil, err
				}
				tags := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' })
				if len(tags) == 0 {
					return nil, fmt.Errorf("Tags in %s is empty: this checkout leaves no plugin out, so there is nothing to prune", path)
				}
				for _, t := range tags {
					if !validTag(t) {
						return nil, fmt.Errorf("%s: %q in Tags is not a build tag", path, t)
					}
				}
				return tags, nil
			}
		}
	}
	return nil, fmt.Errorf("%s has no Tags constant", path)
}

// validTag says whether t can be a build tag: letters, digits, _ and .
func validTag(t string) bool {
	for _, r := range t {
		if !(r == '_' || r == '.' || '0' <= r && r <= '9' || 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z') {
			return false
		}
	}
	return t != ""
}

// goCommand is the go command, run in root with GOFLAGS emptied, so that
// no tags reach it from the environment.
func goCommand(root string, args ...string) *exec.Cmd {
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOFLAGS=", "PWD="+root)
	return cmd
}

// A plan is everything prune does to a checkout, worked out before it
// changes anything. Paths are relative to root, with slashes.
type plan struct {
	root string
	tags []string

	goGone    []goChange // step 1: Go files deleted
	goKept    []goChange // step 1: Go files that stay with their line dropped or rewritten
	pkgs      []pkgGone  // step 2
	embedded  []embedGone
	swiftGone []string
	swiftKept []swiftChange
}

// goChange is one Go file step 1 deletes or changes: its constraint
// before, after, "" when the line goes, and the file's new text.
type goChange struct {
	path     string
	old, new string
	src      []byte
}

// pkgGone is a package folder step 2 deletes, with the files it takes:
// all those in it, not in a package inside it.
type pkgGone struct {
	dir   string
	files []string
}

// embedGone is what one deleted Go file embedded, and step 3 deletes: a
// file, or a folder, whole or the files in it nothing left embeds.
type embedGone struct {
	path  string // what the pattern matched
	by    string // the Go file that embedded it
	whole bool   // a folder, gone with everything in it
	files []string
}

// swiftChange is a Swift file step 4 rewrites, and how many of its #if
// blocks it settled.
type swiftChange struct {
	path   string
	blocks int
	src    []byte
}

// makePlan works out steps 1 to 4 for the checkout at root with tags set.
func makePlan(root string, tags []string) (*plan, error) {
	p := &plan{root: root, tags: tags}
	module, ignore, err := readGoMod(root)
	if err != nil {
		return nil, err
	}
	files, err := goTree(root, ignore)
	if err != nil {
		return nil, err
	}

	// Step 1. The text of every file that embeds anything is kept for step 3.
	set := map[string]bool{}
	for _, t := range tags {
		set[t] = true
	}
	gone := map[string]bool{}
	writes := map[string][]byte{}
	embedders := map[string][]byte{}
	for _, list := range files {
		for _, f := range list {
			src, err := os.ReadFile(p.abs(f))
			if err != nil {
				return nil, err
			}
			if strings.Contains(string(src), "//go:embed") {
				embedders[f] = src
			}
			e, err := pruneGo(src, set)
			if err != nil {
				return nil, fmt.Errorf("%s: %v", f, err)
			}
			c := goChange{path: f, old: e.old, new: e.new, src: e.src}
			switch e.action {
			case goDelete:
				gone[f] = true
				p.goGone = append(p.goGone, c)
			case goUnwrap, goRewrite:
				writes[f] = e.src
				p.goKept = append(p.goKept, c)
			}
		}
	}

	// Step 2.
	unused, err := unneeded(root, module, files, gone, writes)
	if err != nil {
		return nil, err
	}
	pkgDirs := map[string]bool{}
	for d := range files {
		pkgDirs[d] = true
	}
	removed := map[string]bool{}
	for _, d := range unused {
		removed[d] = true
	}
	// owner is the package folder a path is in, the innermost one.
	owner := func(path string) string {
		for d := path; ; d = filepath.ToSlash(filepath.Dir(d)) {
			if pkgDirs[d] || d == "." {
				return d
			}
		}
	}
	inRemoved := func(path string) bool { return removed[owner(path)] }
	for _, d := range unused {
		var taken []string
		err := filepath.WalkDir(p.abs(d), func(path string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel := p.rel(path)
			if e.IsDir() {
				if rel != d && (pkgDirs[rel] || exists(filepath.Join(path, "go.mod"))) {
					return fs.SkipDir // a package, or a module, of its own
				}
				return nil
			}
			taken = append(taken, rel)
			return nil
		})
		if err != nil {
			return nil, err
		}
		p.pkgs = append(p.pkgs, pkgGone{dir: d, files: taken})
	}
	// A file of a deleted package goes with it, whatever step 1 said.
	p.goGone = filterChanges(p.goGone, inRemoved)
	p.goKept = filterChanges(p.goKept, inRemoved)

	// Step 3. What the Go files left embed stays.
	left := func(f string) bool { return !gone[f] && !inRemoved(f) }
	live := map[string]bool{}
	for f, src := range embedders {
		if !left(f) {
			continue
		}
		ms, err := embeds(root, f, src)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", f, err)
		}
		for _, m := range ms {
			for _, x := range m.files {
				live[x] = true
			}
		}
	}
	goLeft := map[string]bool{}
	for _, list := range files {
		for _, f := range list {
			if left(f) {
				goLeft[f] = true
			}
		}
	}
	claimed := map[string]bool{}
	for _, g := range p.goGone {
		src, ok := embedders[g.path]
		if !ok {
			continue
		}
		ms, err := embeds(root, g.path, src)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", g.path, err)
		}
		for _, m := range ms {
			if inRemoved(m.path) || claimed[m.path] {
				continue
			}
			eg := embedGone{path: m.path, by: g.path}
			if m.dir && !holdsAny(m.path, live, goLeft, pkgDirs) && !exists(filepath.Join(p.abs(m.path), "go.mod")) {
				eg.whole = true
				err := filepath.WalkDir(p.abs(m.path), func(path string, e fs.DirEntry, err error) error {
					if err == nil && !e.IsDir() {
						eg.files = append(eg.files, p.rel(path))
					}
					return err
				})
				if err != nil {
					return nil, err
				}
			} else {
				for _, x := range m.files {
					if !live[x] && !goLeft[x] && !claimed[x] {
						eg.files = append(eg.files, x)
					}
				}
			}
			for _, x := range eg.files {
				claimed[x] = true
			}
			claimed[m.path] = true
			if len(eg.files) > 0 {
				p.embedded = append(p.embedded, eg)
			}
		}
	}

	// Step 4.
	if err := p.planSwift(); err != nil {
		return nil, err
	}
	return p, nil
}

// holdsAny says whether the folder dir holds a file of live, a Go file
// left or a package folder: then a deleted file's embedding of it takes
// only what nothing else needs.
func holdsAny(dir string, sets ...map[string]bool) bool {
	for _, s := range sets {
		for x := range s {
			if strings.HasPrefix(x, dir+"/") {
				return true
			}
		}
	}
	return false
}

func filterChanges(cs []goChange, drop func(string) bool) []goChange {
	var out []goChange
	for _, c := range cs {
		if !drop(c.path) {
			out = append(out, c)
		}
	}
	return out
}

// planSwift is step 4: each Swift file under mac/Sources that the copy's
// NO* conditions settle.
func (p *plan) planSwift() error {
	flags := swiftFlags(p.tags)
	top := filepath.Join(p.root, "mac", "Sources")
	if !exists(top) || len(flags) == 0 {
		return nil
	}
	return filepath.WalkDir(top, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			if path != top && strings.HasPrefix(e.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".swift") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out, blocks, err := pruneSwift(src, flags)
		if err != nil {
			return fmt.Errorf("%s: %v", p.rel(path), err)
		}
		switch {
		case blocks == 0:
		case !swiftHasCode(out):
			p.swiftGone = append(p.swiftGone, p.rel(path))
		default:
			p.swiftKept = append(p.swiftKept, swiftChange{path: p.rel(path), blocks: blocks, src: out})
		}
		return nil
	})
}

func (p *plan) abs(rel string) string { return filepath.Join(p.root, filepath.FromSlash(rel)) }

func (p *plan) rel(path string) string {
	r, err := filepath.Rel(p.root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(r)
}

// print says what the plan does, step by step, and counts it.
func (p *plan) print(w io.Writer) {
	fmt.Fprintf(w, "tags: %s\n", strings.Join(p.tags, ","))
	fmt.Fprintln(w, "\n1. Go files the tags leave out")
	changes := append(append([]goChange(nil), p.goGone...), p.goKept...)
	sort.Slice(changes, func(i, j int) bool { return changes[i].path < changes[j].path })
	var unwrapped, rewritten int
	for _, c := range changes {
		switch {
		case c.src == nil:
			fmt.Fprintf(w, "delete  %s (//go:build %s)\n", c.path, c.old)
		case c.new == "":
			unwrapped++
			fmt.Fprintf(w, "keep    %s without //go:build %s\n", c.path, c.old)
		default:
			rewritten++
			fmt.Fprintf(w, "rewrite %s: //go:build %s becomes //go:build %s\n", c.path, c.old, c.new)
		}
	}
	fmt.Fprintln(w, "\n2. Packages nothing left needs")
	var pkgFiles int
	for _, g := range p.pkgs {
		pkgFiles += len(g.files)
		fmt.Fprintf(w, "delete  %s/ (%d files)\n", g.dir, len(g.files))
	}
	fmt.Fprintln(w, "\n3. Files only those Go files embedded")
	var embedFiles int
	for _, e := range p.embedded {
		embedFiles += len(e.files)
		switch {
		case e.whole:
			fmt.Fprintf(w, "delete  %s/ (%d files), embedded by %s\n", e.path, len(e.files), e.by)
		default:
			for _, f := range e.files {
				fmt.Fprintf(w, "delete  %s, embedded by %s\n", f, e.by)
			}
		}
	}
	fmt.Fprintln(w, "\n4. Swift under mac/Sources")
	for _, f := range p.swiftGone {
		fmt.Fprintf(w, "delete  %s: no code left\n", f)
	}
	for _, c := range p.swiftKept {
		s := "s"
		if c.blocks == 1 {
			s = ""
		}
		fmt.Fprintf(w, "rewrite %s: %d #if block%s settled\n", c.path, c.blocks, s)
	}
	fmt.Fprintf(w, "\nGo files: %d deleted, %d without their //go:build line, %d with it rewritten\n",
		len(p.goGone), unwrapped, rewritten)
	var dirs []string
	for _, g := range p.pkgs {
		dirs = append(dirs, g.dir)
	}
	fmt.Fprintf(w, "packages: %d deleted, %d files: %s\n", len(p.pkgs), pkgFiles, strings.Join(dirs, " "))
	fmt.Fprintf(w, "embedded: %d files deleted\n", embedFiles)
	fmt.Fprintf(w, "Swift:    %d files deleted, %d rewritten\n", len(p.swiftGone), len(p.swiftKept))
}

// apply makes the plan's changes, then deletes the folders they leave
// empty.
func (p *plan) apply() error {
	for _, c := range p.goKept {
		if err := os.WriteFile(p.abs(c.path), c.src, 0o644); err != nil {
			return err
		}
	}
	for _, c := range p.swiftKept {
		if err := os.WriteFile(p.abs(c.path), c.src, 0o644); err != nil {
			return err
		}
	}
	var gone []string
	for _, c := range p.goGone {
		gone = append(gone, c.path)
	}
	for _, g := range p.pkgs {
		gone = append(gone, g.files...)
	}
	for _, e := range p.embedded {
		gone = append(gone, e.files...)
	}
	gone = append(gone, p.swiftGone...)
	emptied := map[string]bool{}
	for _, f := range gone {
		if err := os.Remove(p.abs(f)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		for d := filepath.ToSlash(filepath.Dir(f)); d != "."; d = filepath.ToSlash(filepath.Dir(d)) {
			emptied[d] = true
		}
	}
	// The deepest first; a folder that still holds anything stays.
	var dirs []string
	for d := range emptied {
		dirs = append(dirs, d)
	}
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, d := range dirs {
		os.Remove(p.abs(d))
	}
	return nil
}
