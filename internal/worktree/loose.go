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
	Head   string // HEAD's commit
	Branch string // HEAD's branch; "" when it is detached
	Locked bool   // git worktree lock holds it
}

// Trees lists the repo's worktrees whose directories are there, the main
// checkout first.
func Trees(repo string, timeout time.Duration) ([]Tree, error) {
	all, err := list(repo, timeout)
	if err != nil {
		return nil, err
	}
	var trees []Tree
	for _, t := range all {
		if exists(t.Path) {
			trees = append(trees, t)
		}
	}
	return trees, nil
}

// list lists every worktree git records for the repo, the main checkout
// first, those whose directories have gone included.
func list(repo string, timeout time.Duration) ([]Tree, error) {
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
			} else if h, ok := strings.CutPrefix(line, "HEAD "); ok {
				t.Head = h
			} else if b, ok := strings.CutPrefix(line, "branch "); ok {
				t.Branch = strings.TrimPrefix(b, "refs/heads/")
			} else if line == "locked" || strings.HasPrefix(line, "locked ") {
				t.Locked = true
			}
		}
		if t.Path != "" {
			trees = append(trees, t)
		}
	}
	return trees, nil
}

// Loose is what a worktree holds that has not landed.
type Loose struct {
	Branch string   // HEAD's branch; "" when it is detached
	Dirty  int      // files not committed
	Files  []string // the first of them, relative to the worktree
	// Ahead counts the commits whose work the land branch lacks, as
	// ahead reads them: merges are not counted, nor a commit the land
	// branch has a copy of.
	Ahead   int
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
	return l, ahead(&l, dir, land, "HEAD", since, timeout)
}

// ReadBranch reads what a branch whose worktree Irie dropped (drop.go)
// holds that has not landed: as ReadLoose does, from the branch alone,
// since a worktree is dropped only with nothing uncommitted in it.
func ReadBranch(repo, branch, land string, since time.Time, timeout time.Duration) (Loose, error) {
	l := Loose{Branch: branch}
	if _, err := git(timeout, repo, nil, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch+"^{commit}"); err != nil {
		return l, err
	}
	return l, ahead(&l, repo, land, "refs/heads/"+branch, since, timeout)
}

// ReadCommits reads the commits of the worktree at dir whose work land
// lacks, as ReadLoose does, without looking at its files: what a close
// says has not landed (DESIGN §4.5).
func ReadCommits(dir, land string, since time.Time, timeout time.Duration) (Loose, error) {
	var l Loose
	out, err := git(timeout, dir, nil, "symbolic-ref", "--quiet", "--short", "HEAD")
	switch {
	case err == nil:
		l.Branch = strings.TrimSpace(out)
	case exitCode(err) != 1: // 1 is a detached HEAD
		return l, err
	}
	return l, ahead(&l, dir, land, "HEAD", since, timeout)
}

// ahead counts into l the commits of rev, run in dir, whose work land
// lacks: those not in land, committed since since, but merges, which bring
// in what is on a branch already. A commit counts as landed when land has
// a copy of it in another commit: one with the same patch, as git cherry
// finds a cherry-pick or a rebase, or with the same author, author time
// and subject, which a cherry-pick keeps when it had to be adapted. When
// git cannot read the copies in time, every commit not in land counts.
// With no land it counts none.
func ahead(l *Loose, dir, land, rev string, since time.Time, timeout time.Duration) error {
	if land == "" {
		return nil
	}
	if _, err := git(timeout, dir, nil, "rev-parse", "--verify", "--quiet", land+"^{commit}"); err != nil {
		if exitCode(err) == 1 {
			l.NoLand = true
			return nil
		}
		return err
	}
	out, err := git(timeout, dir, nil, "log", "--no-merges", "--format=%H%x09%ct%x09%ae%x09%at%x09%h%x09%s", land+".."+rev)
	if err != nil {
		return err
	}
	type commit struct{ hash, made, short, subject string }
	var mine []commit
	for _, line := range lines(out) {
		f := strings.SplitN(line, "\t", 6)
		if len(f) < 6 {
			continue
		}
		secs, err := strconv.ParseInt(f[1], 10, 64)
		if err != nil || time.Unix(secs, 0).Before(since.Truncate(time.Second)) {
			continue
		}
		mine = append(mine, commit{hash: f[0], made: madeKey(f[2], f[3], f[5]), short: f[4], subject: f[5]})
	}
	if len(mine) == 0 {
		return nil
	}
	copied := landedCopies(dir, land, rev, mine[len(mine)-1].hash, timeout)
	for _, c := range mine {
		if copied[c.hash] || copied[c.made] {
			continue
		}
		l.Ahead++
		if len(l.Commits) < named {
			l.Commits = append(l.Commits, c.short+" "+c.subject)
		}
	}
	return nil
}

// landedCopies are the commits of rev that land has a copy of, keyed both
// ways ahead looks for one: by hash, those git cherry marks as having the
// same patch in land, and by author, author time and subject (madeKey),
// those of land's commits since the branch's oldest commit not in it,
// oldest, parted from it. A copy that also changed its author time, behind
// a merge of land into rev since, is found by neither: git cherry looks
// only at the land commits rev lacks, so that commit counts as not landed,
// which says too much rather than too little. What git cannot read, as
// when it runs out of time, is left out.
func landedCopies(dir, land, rev, oldest string, timeout time.Duration) map[string]bool {
	copied := map[string]bool{}
	if out, err := git(timeout, dir, nil, "cherry", land, rev); err == nil {
		for _, line := range lines(out) {
			if hash, ok := strings.CutPrefix(line, "- "); ok {
				copied[strings.TrimSpace(hash)] = true
			}
		}
	}
	// Where the branch parted from land, so that a merge of land into it
	// since, as tools/live.sh makes, hides none of the copies.
	parted := rev
	if base, err := git(timeout, dir, nil, "merge-base", land, oldest+"^"); err == nil && strings.TrimSpace(base) != "" {
		parted = strings.TrimSpace(base)
	}
	if out, err := git(timeout, dir, nil, "log", "--no-merges", "--format=%ae%x09%at%x09%s", parted+".."+land); err == nil {
		for _, line := range lines(out) {
			if f := strings.SplitN(line, "\t", 3); len(f) == 3 {
				copied[madeKey(f[0], f[1], f[2])] = true
			}
		}
	}
	return copied
}

// madeKey names a commit by what a cherry-pick keeps of it: its author's
// email, the author time in seconds, and its subject.
func madeKey(email, at, subject string) string {
	return "made\x00" + email + "\x00" + at + "\x00" + subject
}

// lines are out's lines, but blank ones.
func lines(out string) []string {
	var ls []string
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) != "" {
			ls = append(ls, line)
		}
	}
	return ls
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
