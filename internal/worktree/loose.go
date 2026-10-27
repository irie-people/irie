package worktree

// What a worktree holds that has not landed, for the jot dog (DESIGN §4.8).
// It only reads: git status and git log, with no optional locks, so looking
// never rewrites the index of a checkout an agent works in.

import (
	"slices"
	"strconv"
	"strings"
	"time"
)

// named is how many of the files and commits Loose names; it counts them all.
const named = 5

// Tree is one worktree of a repo, as git lists it.
type Tree struct {
	Path   string
	Branch string // HEAD's branch; "" when it is detached
}

// Trees lists the repo's worktrees whose directories are there, the main
// checkout first.
func Trees(repo string, timeout time.Duration) ([]Tree, error) {
	out, err := git(timeout, repo, nil, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var trees []Tree
	for _, block := range strings.Split(out, "\n\n") {
		var t Tree
		for _, line := range strings.Split(block, "\n") {
			if p, ok := strings.CutPrefix(line, "worktree "); ok {
				t.Path = p
			} else if b, ok := strings.CutPrefix(line, "branch "); ok {
				t.Branch = strings.TrimPrefix(b, "refs/heads/")
			}
		}
		if t.Path != "" && exists(t.Path) {
			trees = append(trees, t)
		}
	}
	return trees, nil
}

// Loose is what a worktree holds that has not landed.
type Loose struct {
	Branch  string   // HEAD's branch; "" when it is detached
	Dirty   int      // files not committed
	Files   []string // the first of them, relative to the worktree
	Ahead   int      // commits that the land branch lacks
	Commits []string // the first of them, as "abc1234 subject", newest first
	NoLand  bool     // the land branch is not in the repo
}

// Left reports whether anything has not landed.
func (l Loose) Left() bool { return l.Dirty > 0 || l.Ahead > 0 }

// ReadLoose reads what the worktree at dir holds that has not landed: the
// files not committed, untracked ones included but those in skip, which Irie
// copied in; and when land names a branch, the commits of HEAD that it
// lacks, of those committed since since, so that what the branch started
// from does not count.
func ReadLoose(dir, land string, since time.Time, skip []string, timeout time.Duration) (Loose, error) {
	var l Loose
	out, err := git(timeout, dir, nil, "status", "--porcelain=v2", "--branch", "-z", "--untracked-files=all")
	if err != nil {
		return l, err
	}
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		var path string
		switch {
		case strings.HasPrefix(f, "# branch.head "):
			if b := strings.TrimPrefix(f, "# branch.head "); b != "(detached)" {
				l.Branch = b
			}
			continue
		case strings.HasPrefix(f, "1 "):
			path = nth(f, 8)
		case strings.HasPrefix(f, "2 "):
			path = nth(f, 9)
			i++ // the next field is the path it came from
		case strings.HasPrefix(f, "u "):
			path = nth(f, 10)
		case strings.HasPrefix(f, "? "):
			path = f[2:]
		}
		if path == "" || slices.Contains(skip, path) {
			continue
		}
		l.Dirty++
		if len(l.Files) < named {
			l.Files = append(l.Files, path)
		}
	}
	if land == "" {
		return l, nil
	}
	if _, err := git(timeout, dir, nil, "rev-parse", "--verify", "--quiet", land+"^{commit}"); err != nil {
		if exitCode(err) == 1 {
			l.NoLand = true
			return l, nil
		}
		return l, err
	}
	out, err = git(timeout, dir, nil, "log", "--format=%h%x09%ct%x09%s", land+"..HEAD")
	if err != nil {
		return l, err
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		hash, rest, ok := strings.Cut(line, "\t")
		at, subject, ok2 := strings.Cut(rest, "\t")
		secs, err := strconv.ParseInt(at, 10, 64)
		if !ok || !ok2 || err != nil || time.Unix(secs, 0).Before(since.Truncate(time.Second)) {
			continue
		}
		l.Ahead++
		if len(l.Commits) < named {
			l.Commits = append(l.Commits, hash+" "+subject)
		}
	}
	return l, nil
}

// nth is what follows the first n space-separated fields of an entry of
// git status --porcelain=v2: its path, which may hold spaces.
func nth(entry string, n int) string {
	parts := strings.SplitN(entry, " ", n+1)
	if len(parts) <= n {
		return ""
	}
	return parts[n]
}
