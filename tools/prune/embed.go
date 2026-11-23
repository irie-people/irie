package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// embedMatch is what one //go:embed pattern names: a file, or a folder,
// with the files the go command embeds from it.
type embedMatch struct {
	path  string // relative to the checkout, with slashes
	dir   bool
	files []string
}

// embeds is what the Go file file, src, embeds: each match of each of its
// //go:embed patterns.
func embeds(root, file string, src []byte) ([]embedMatch, error) {
	pats, err := embedPatterns(file, src)
	if err != nil {
		return nil, err
	}
	var out []embedMatch
	for _, pat := range pats {
		ms, err := resolveEmbed(root, path.Dir(file), pat)
		if err != nil {
			return nil, err
		}
		out = append(out, ms...)
	}
	return out, nil
}

// embedPatterns reads the patterns of src's //go:embed lines.
func embedPatterns(name string, src []byte) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var pats []string
	for _, g := range f.Comments {
		for _, c := range g.List {
			args, ok := strings.CutPrefix(c.Text, "//go:embed")
			if !ok || args == "" || args[0] != ' ' && args[0] != '\t' {
				continue
			}
			ps, err := splitEmbed(args)
			if err != nil {
				return nil, fmt.Errorf("%s: %v", fset.Position(c.Pos()), err)
			}
			pats = append(pats, ps...)
		}
	}
	return pats, nil
}

// splitEmbed splits a //go:embed line's patterns: words, or Go strings in
// double quotes or back quotes, as go/build reads them.
func splitEmbed(args string) ([]string, error) {
	var out []string
	for {
		args = strings.TrimLeft(args, " \t")
		if args == "" {
			return out, nil
		}
		var p string
		switch args[0] {
		case '`':
			end := strings.IndexByte(args[1:], '`')
			if end < 0 {
				return nil, fmt.Errorf("invalid quoted string in //go:embed: %s", args)
			}
			p, args = args[1:1+end], args[2+end:]
		case '"':
			i := 1
			for ; i < len(args) && args[i] != '"'; i++ {
				if args[i] == '\\' {
					i++
				}
			}
			if i >= len(args) {
				return nil, fmt.Errorf("invalid quoted string in //go:embed: %s", args)
			}
			q, err := strconv.Unquote(args[:i+1])
			if err != nil {
				return nil, fmt.Errorf("invalid quoted string in //go:embed: %s", args[:i+1])
			}
			p, args = q, args[i+1:]
		default:
			i := strings.IndexAny(args, " \t")
			if i < 0 {
				i = len(args)
			}
			p, args = args[:i], args[i:]
		}
		if args != "" && args[0] != ' ' && args[0] != '\t' {
			return nil, fmt.Errorf("invalid quoted string in //go:embed: %s", args)
		}
		out = append(out, p)
	}
}

// resolveEmbed finds what pattern embeds for a file in the folder pkg, as
// the go command does (https://pkg.go.dev/embed): the files it matches,
// and for a folder the files in it, without those whose names, or whose
// folders' names, begin with . or _ unless the pattern starts with all:,
// and none from a folder holding another module.
func resolveEmbed(root, pkg, pattern string) ([]embedMatch, error) {
	all := false
	if p, ok := strings.CutPrefix(pattern, "all:"); ok {
		all, pattern = true, p
	}
	if _, err := path.Match(pattern, ""); err != nil || !fs.ValidPath(pattern) || pattern == "." {
		return nil, fmt.Errorf("invalid //go:embed pattern %q", pattern)
	}
	dir := filepath.Join(root, filepath.FromSlash(pkg))
	matches, err := filepath.Glob(quoteGlob(dir) + string(filepath.Separator) + filepath.FromSlash(pattern))
	if err != nil {
		return nil, err
	}
	rel := func(p string) string {
		r, _ := filepath.Rel(root, p)
		return filepath.ToSlash(r)
	}
	var out []embedMatch
	for _, m := range matches {
		info, err := os.Lstat(m)
		if err != nil {
			return nil, err
		}
		switch {
		case info.Mode().IsRegular():
			out = append(out, embedMatch{path: rel(m), files: []string{rel(m)}})
		case info.IsDir():
			em := embedMatch{path: rel(m), dir: true}
			err := filepath.WalkDir(m, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				name := d.Name()
				if p != m && (badEmbedName(name) || !all && (name[0] == '.' || name[0] == '_')) {
					if d.IsDir() {
						return fs.SkipDir
					}
					return nil
				}
				if d.IsDir() {
					if p != m && exists(filepath.Join(p, "go.mod")) {
						return fs.SkipDir
					}
					return nil
				}
				if d.Type().IsRegular() {
					em.files = append(em.files, rel(p))
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
			out = append(out, em)
		}
	}
	return out, nil
}

// badEmbedName is a name the go command never embeds from a folder:
// version control's.
func badEmbedName(name string) bool {
	switch name {
	case "", ".bzr", ".hg", ".git", ".svn":
		return true
	}
	return false
}

// quoteGlob escapes the characters filepath.Glob reads in s.
func quoteGlob(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(`*?[\`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
