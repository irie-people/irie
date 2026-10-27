// Package repo finds the directory that an @ token of the command bar names
// (DESIGN §4.2). A path names itself. A name resolves through repos.aliases,
// then the git repos found two levels under repos.roots, then the repos used
// recently. The first of these that knows the name answers; when it knows
// two repos by that name, the name is ambiguous and the error lists them
// instead of guessing.
package repo

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// Options say where repos are found.
type Options struct {
	Aliases map[string]string // repos.aliases: name → absolute or ~ path
	Roots   []string          // repos.roots: absolute or ~ paths
	Recent  []string          // repos used recently, newest first
	Home    string            // what ~ stands for
}

// IsPath reports whether an @ token's value is a path rather than a name.
func IsPath(v string) bool { return filepath.IsAbs(v) || v == "~" || strings.HasPrefix(v, "~/") }

// Expand returns p, cleaned, with a leading ~ standing for home.
func Expand(p, home string) string {
	switch {
	case p == "~":
		return filepath.Clean(home)
	case strings.HasPrefix(p, "~/"):
		return filepath.Join(home, p[2:])
	}
	return filepath.Clean(p)
}

// Tilde shortens p to a ~ path when it lies under home.
func Tilde(p, home string) string {
	if home != "" && home != "/" && (p == home || strings.HasPrefix(p, home+"/")) {
		return "~" + p[len(home):]
	}
	return p
}

// NotFoundError is a name that no repo answers to.
type NotFoundError struct {
	Name    string
	Closest []string // the nearest names Irie knows, nearest first
}

func (e *NotFoundError) Error() string {
	if len(e.Closest) == 0 {
		return fmt.Sprintf("no repo named @%s, and no repos are known: set repos.roots or repos.aliases in config.toml, or give a path", e.Name)
	}
	return fmt.Sprintf("no repo named @%s; closest: @%s", e.Name, strings.Join(e.Closest, ", @"))
}

// AmbiguousError is a name that fits more than one repo.
type AmbiguousError struct {
	Name  string
	Paths []string // ~ paths
}

func (e *AmbiguousError) Error() string {
	return fmt.Sprintf("@%s fits %d repos: %s; give the path of the one you mean", e.Name, len(e.Paths), strings.Join(e.Paths, ", "))
}

// known is a repo and the names it answers to.
type known struct {
	path  string
	names []string // its directory's name, then its path under a root
}

// Find resolves an @ token's value to a directory. It does not check that
// a path exists.
func Find(v string, o Options) (string, error) {
	switch {
	case v == "":
		return "", errors.New("@ needs a repo name or a path")
	case IsPath(v):
		return Expand(v, o.Home), nil
	case strings.HasPrefix(v, "~"):
		return "", fmt.Errorf("@%s: write a path as /… or ~/…", v)
	case v == "." || v == ".." || strings.HasPrefix(v, "./") || strings.HasPrefix(v, "../"):
		return "", fmt.Errorf("@%s: give an absolute path, a ~/ path or a repo name", v)
	}
	tiers := [][]known{aliases(o), roots(o), recent(o)}
	for _, tier := range tiers {
		switch hits := match(tier, v); len(hits) {
		case 0:
		case 1:
			return hits[0], nil
		default:
			for i, p := range hits {
				hits[i] = Tilde(p, o.Home)
			}
			return "", &AmbiguousError{Name: v, Paths: hits}
		}
	}
	return "", &NotFoundError{Name: v, Closest: closest(v, tiers)}
}

// match returns the paths of the repos in tier that answer to v: by that
// very name, or else by one that differs from it only in case.
func match(tier []known, v string) []string {
	var exact, folded []string
	for _, r := range tier {
		for _, n := range r.names {
			switch {
			case n == v && !slices.Contains(exact, r.path):
				exact = append(exact, r.path)
			case strings.EqualFold(n, v) && !slices.Contains(folded, r.path):
				folded = append(folded, r.path)
			}
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return folded
}

func aliases(o Options) []known {
	var out []known
	for _, name := range slices.Sorted(maps.Keys(o.Aliases)) {
		out = append(out, known{path: Expand(o.Aliases[name], o.Home), names: []string{name}})
	}
	return out
}

// roots scans each root two levels deep. A repo found at the first level is
// not looked into; at the second, it also answers to its path under the
// root, such as clients/api.
func roots(o Options) []known {
	var out []known
	for _, root := range o.Roots {
		dir := Expand(root, o.Home)
		for _, e := range subdirs(dir) {
			p := filepath.Join(dir, e)
			if isRepo(p) {
				out = append(out, known{path: p, names: []string{e}})
				continue
			}
			for _, f := range subdirs(p) {
				if q := filepath.Join(p, f); isRepo(q) {
					out = append(out, known{path: q, names: []string{f, e + "/" + f}})
				}
			}
		}
	}
	return out
}

// recent lists the recently used repos that still exist.
func recent(o Options) []known {
	var out []known
	for _, p := range o.Recent {
		if isRepo(p) {
			out = append(out, known{path: p, names: []string{filepath.Base(p)}})
		}
	}
	return out
}

// subdirs lists the directories in dir by name, following symlinks and
// leaving out hidden ones.
func subdirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if e.IsDir() || e.Type()&fs.ModeSymlink != 0 && isDir(filepath.Join(dir, e.Name())) {
			out = append(out, e.Name())
		}
	}
	return out
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// isRepo reports whether dir is a git checkout: it holds .git, which is a
// directory, or a file in a linked worktree or a submodule.
func isRepo(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil && isDir(dir)
}

// closest returns up to three known names nearest to v by edit distance,
// ignoring case.
func closest(v string, tiers [][]known) []string {
	type scored struct {
		name string
		d    int
	}
	seen := map[string]bool{}
	var all []scored
	for _, tier := range tiers {
		for _, r := range tier {
			for _, n := range r.names {
				if !seen[n] {
					seen[n] = true
					all = append(all, scored{n, distance(strings.ToLower(v), strings.ToLower(n))})
				}
			}
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].d != all[j].d {
			return all[i].d < all[j].d
		}
		return all[i].name < all[j].name
	})
	var out []string
	for _, s := range all[:min(3, len(all))] {
		out = append(out, s.name)
	}
	return out
}

// distance is the Levenshtein distance between a and b, in runes.
func distance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev, cur := make([]int, len(rb)+1), make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

// Complete returns, sorted, the @ token values that start with prefix: the
// directories under a path, or the names of the repos Irie knows, matched
// regardless of case.
func Complete(prefix string, o Options) []string {
	if prefix == "~" {
		return []string{"~/"}
	}
	if IsPath(prefix) {
		return completePath(prefix, o.Home)
	}
	lower := strings.ToLower(prefix)
	seen := map[string]bool{}
	var out []string
	for _, tier := range [][]known{aliases(o), roots(o), recent(o)} {
		for _, r := range tier {
			for _, n := range r.names {
				if strings.HasPrefix(strings.ToLower(n), lower) && !seen[n] {
					seen[n] = true
					out = append(out, n)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// completePath lists the directories that complete a path's last element,
// each ending in a slash. Hidden ones come only for a prefix that starts
// with a dot.
func completePath(prefix, home string) []string {
	cut := strings.LastIndex(prefix, "/") + 1
	dir, part := prefix[:cut], prefix[cut:]
	entries, err := os.ReadDir(Expand(dir, home))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, part) || strings.HasPrefix(name, ".") && !strings.HasPrefix(part, ".") {
			continue
		}
		if isDir(filepath.Join(Expand(dir, home), name)) {
			out = append(out, dir+name+"/")
		}
	}
	return out
}
