package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// readGoMod reads go.mod's module path and the folders its ignore
// directives leave out of the module (https://go.dev/ref/mod#go-mod-file-ignore).
func readGoMod(root string) (module string, ignore []string, err error) {
	b, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", nil, err
	}
	unquote := func(s string) string {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
		return s
	}
	block := false
	for _, l := range strings.Split(string(b), "\n") {
		if i := strings.Index(l, "//"); i >= 0 {
			l = l[:i]
		}
		f := strings.Fields(l)
		switch {
		case len(f) == 0:
		case block && f[0] == ")":
			block = false
		case block:
			ignore = append(ignore, unquote(f[0]))
		case f[0] == "module" && len(f) > 1:
			module = unquote(f[1])
		case f[0] == "ignore" && len(f) > 1 && f[1] == "(":
			block = true
		case f[0] == "ignore" && len(f) > 1:
			ignore = append(ignore, unquote(f[1]))
		}
	}
	if module == "" {
		return "", nil, fmt.Errorf("%s has no module line", filepath.Join(root, "go.mod"))
	}
	return module, ignore, nil
}

// ignored says whether go.mod's ignore directives leave out the folder
// rel: one starting with ./ names a folder from the module's root, any
// other a folder of that name at any depth.
func ignored(rel string, ignore []string) bool {
	for _, ig := range ignore {
		if r, ok := strings.CutPrefix(ig, "./"); ok {
			if rel == strings.TrimSuffix(r, "/") {
				return true
			}
		} else if ig = strings.TrimSuffix(ig, "/"); rel == ig || strings.HasSuffix(rel, "/"+ig) {
			return true
		}
	}
	return false
}

// goTree lists the checkout's Go files by folder, both relative to root
// with slashes, where the go command looks for packages: not in testdata
// or vendor, folders whose names begin with . or _, another module, or a
// folder go.mod ignores.
func goTree(root string, ignore []string) (map[string][]string, error) {
	files := map[string][]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		r, _ := filepath.Rel(root, path)
		rel := filepath.ToSlash(r)
		name := d.Name()
		if d.IsDir() {
			if rel != "." && (name[0] == '.' || name[0] == '_' || name == "testdata" || name == "vendor" ||
				exists(filepath.Join(path, "go.mod")) || ignored(rel, ignore)) {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(name, ".go") && d.Type().IsRegular() {
			dir := filepath.ToSlash(filepath.Dir(rel))
			files[dir] = append(files[dir], rel)
		}
		return nil
	})
	return files, err
}

func inInternal(dir string) bool { return dir == "internal" || strings.HasPrefix(dir, "internal/") }

// unneeded is step 2: the package folders under internal/ that nothing
// needs once step 1 has deleted gone and rewritten writes. A package is
// needed when go list -deps -test of the roots lists it, the roots being
// the main packages, those with only tests and every package outside
// internal/; and when a Go file left imports it, whatever that file's
// constraint, so that a file for another system keeps what it uses.
func unneeded(root, module string, files map[string][]string, gone map[string]bool, writes map[string][]byte) ([]string, error) {
	left := map[string][]string{}
	for d, list := range files {
		for _, f := range list {
			if !gone[f] {
				left[d] = append(left[d], f)
			}
		}
	}
	var roots []string
	for d, list := range left {
		if !inInternal(d) {
			roots = append(roots, d)
			continue
		}
		main, testsOnly := false, true
		for _, f := range list {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			testsOnly = false
			pf, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, filepath.FromSlash(f)), nil, parser.PackageClauseOnly)
			if err != nil {
				return nil, err
			}
			if pf.Name.Name == "main" {
				main = true
				break
			}
		}
		if main || testsOnly {
			roots = append(roots, d)
		}
	}
	sort.Strings(roots)
	needed, err := listDeps(root, roots, gone, writes)
	if err != nil {
		return nil, err
	}
	for _, r := range roots {
		needed[r] = true
	}
	unused := map[string]bool{}
	for d := range files {
		if inInternal(d) && !needed[d] {
			unused[d] = true
		}
	}
	// What a file left imports is needed, and what that imports in turn.
	var queue []string
	for d, list := range left {
		if !unused[d] {
			queue = append(queue, list...)
		}
	}
	for len(queue) > 0 {
		f := queue[0]
		queue = queue[1:]
		pf, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, filepath.FromSlash(f)), nil, parser.ImportsOnly)
		if err != nil {
			return nil, err
		}
		for _, imp := range pf.Imports {
			p, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return nil, fmt.Errorf("%s: %v", f, err)
			}
			if d, ok := strings.CutPrefix(p, module+"/"); ok && unused[d] {
				delete(unused, d)
				queue = append(queue, left[d]...)
			}
		}
	}
	var out []string
	for d := range unused {
		out = append(out, d)
	}
	sort.Strings(out)
	return out, nil
}

// listDeps runs go list -e -deps -test on the folders roots, with gone
// deleted and writes in place through an overlay, so that nothing on disk
// changes, and says which folders, relative to root, hold the packages
// it lists.
func listDeps(root string, roots []string, gone map[string]bool, writes map[string][]byte) (map[string]bool, error) {
	tmp, err := os.MkdirTemp("", "prune")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	replace := map[string]string{}
	for f := range gone {
		replace[filepath.Join(root, filepath.FromSlash(f))] = ""
	}
	n := 0
	for f, src := range writes {
		n++
		backing := filepath.Join(tmp, strconv.Itoa(n)+".go")
		if err := os.WriteFile(backing, src, 0o600); err != nil {
			return nil, err
		}
		replace[filepath.Join(root, filepath.FromSlash(f))] = backing
	}
	overlay, err := json.Marshal(map[string]any{"Replace": replace})
	if err != nil {
		return nil, err
	}
	overlayFile := filepath.Join(tmp, "overlay.json")
	if err := os.WriteFile(overlayFile, overlay, 0o600); err != nil {
		return nil, err
	}
	args := []string{"list", "-e", "-deps", "-test", "-overlay=" + overlayFile, "-f", "{{.Dir}}"}
	for _, r := range roots {
		if r == "." {
			args = append(args, ".")
		} else {
			args = append(args, "./"+r)
		}
	}
	var stdout, stderr bytes.Buffer
	cmd := goCommand(root, args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go list -deps -test: %v\n%s", err, stderr.Bytes())
	}
	needed := map[string]bool{}
	for _, dir := range strings.Split(stdout.String(), "\n") {
		if rel, ok := strings.CutPrefix(dir, root+string(filepath.Separator)); ok {
			needed[filepath.ToSlash(rel)] = true
		}
	}
	return needed, nil
}
