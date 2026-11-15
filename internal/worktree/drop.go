package worktree

// Dropping a worktree whose work has landed, and adding it back (DESIGN
// §4.5). A closed session whose branch is in its repo's land branch, and
// whose worktree holds nothing more, needs only its branch: Irie removes
// the checkout with git worktree remove, which keeps the branch and its
// reflog, and reopen adds it back at the same path from the branch, as a
// new worktree is made. The worktree's own HEAD reflog goes with it. Only
// git decides: the checks read with no optional locks, and the removal
// never forces, so git refuses whatever changed since they looked.

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Landed is what CheckDrop found of a worktree.
type Landed struct {
	Gone   bool   // there is no worktree at the path, so nothing to drop
	Branch string // the branch it has checked out
	Commit string // that branch's tip
	// Keep says why the worktree must stay, in plain words; "" when it can
	// go.
	Keep string
}

// CheckDrop reads whether the worktree at dir, of the repo whose main
// checkout is repo, can be removed and later added back from branch alone:
// it is a linked worktree of repo that git has not locked, with branch
// checked out and no submodules; branch's tip is in land; and nothing is
// left in it that is not committed, untracked files included, but the
// local files Irie copied in (copied) while they match the repo's own.
// Files git ignores, such as build output, do not count.
func CheckDrop(dir, repo, branch, land string, copied []string, timeout time.Duration) (Landed, error) {
	var l Landed
	if fi, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
		l.Gone = true
		return l, nil
	} else if err != nil {
		return l, err
	} else if !fi.IsDir() {
		l.Keep = "it is not a folder"
		return l, nil
	}
	if !filepath.IsAbs(dir) || !filepath.IsAbs(repo) {
		return l, fmt.Errorf("worktree: %q or %q is not an absolute path", dir, repo)
	}
	// It is one of repo's linked worktrees, as git lists them.
	trees, err := list(repo, timeout)
	if err != nil {
		return l, err
	}
	i := slices.IndexFunc(trees, func(t Tree) bool { return realish(t.Path) == realish(dir) })
	switch {
	case i < 0:
		l.Keep = "git does not list it as a worktree of " + repo
		return l, nil
	case i == 0:
		l.Keep = "it is the repo's own checkout"
		return l, nil
	}
	t := trees[i]
	l.Branch, l.Commit = t.Branch, t.Head
	switch {
	case t.Locked:
		l.Keep = "git has it locked (git worktree lock)"
	case t.Branch == "":
		l.Keep = "its HEAD is detached, at " + short(t.Head)
	case t.Branch != branch:
		l.Keep = fmt.Sprintf("it has %s checked out, not %s", t.Branch, branch)
	case exists(filepath.Join(dir, ".gitmodules")):
		l.Keep = "it has submodules"
	}
	if l.Keep != "" {
		return l, nil
	}
	if l.Keep, err = notLanded(repo, l.Commit, land, timeout); err != nil || l.Keep != "" {
		return l, err
	}
	if l.Keep, err = notCommitted(dir, copied, timeout); err != nil || l.Keep != "" {
		return l, err
	}
	for _, f := range copied {
		if !sameCopy(repo, dir, f) {
			l.Keep = "its copy of " + f + " is not the repo's"
			return l, nil
		}
	}
	return l, nil
}

// notLanded says how commit is not in land, or "" when it is.
func notLanded(repo, commit, land string, timeout time.Duration) (string, error) {
	if land == "" {
		return "its repo has no land branch (dog.land)", nil
	}
	if _, err := git(timeout, repo, nil, "rev-parse", "--verify", "--quiet", "--end-of-options", land+"^{commit}"); err != nil {
		if exitCode(err) == 1 {
			return "its repo has no branch " + land, nil
		}
		return "", err
	}
	_, err := git(timeout, repo, nil, "merge-base", "--is-ancestor", commit, land)
	if err == nil {
		return "", nil
	}
	if exitCode(err) != 1 {
		return "", err
	}
	out, err := git(timeout, repo, nil, "rev-list", "--count", land+".."+commit)
	if err != nil {
		return "", err
	}
	n, _ := strconv.Atoi(strings.TrimSpace(out))
	if n == 1 {
		return "1 commit is not in " + land, nil
	}
	return fmt.Sprintf("%d commits are not in %s", n, land), nil
}

// notCommitted says what the worktree at dir holds that is not committed,
// untracked files included but those in skip, or "" when it holds none.
func notCommitted(dir string, skip []string, timeout time.Duration) (string, error) {
	out, err := git(timeout, dir, nil, "status", "--porcelain=v2", "-z", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return "", err
	}
	var files []string
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		var path string
		switch {
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
		if path != "" && !slices.Contains(skip, path) {
			files = append(files, path)
		}
	}
	switch n := len(files); {
	case n == 0:
		return "", nil
	case n == 1:
		return "1 file is not committed: " + files[0], nil
	case n > 3:
		return fmt.Sprintf("%d files are not committed: %s and more", n, strings.Join(files[:3], ", ")), nil
	default:
		return fmt.Sprintf("%d files are not committed: %s", n, strings.Join(files, ", ")), nil
	}
}

// sameCopy reports whether the worktree's copy of the local file rel is
// the repo's: the same bytes, or a link to the same place. One the
// worktree no longer has is nothing to lose.
func sameCopy(repo, wt, rel string) bool {
	mine, err := os.Lstat(filepath.Join(wt, rel))
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	theirs, err2 := os.Lstat(filepath.Join(repo, rel))
	if err != nil || err2 != nil || mine.Mode().Type() != theirs.Mode().Type() {
		return false
	}
	switch {
	case mine.Mode()&fs.ModeSymlink != 0:
		a, err1 := os.Readlink(filepath.Join(wt, rel))
		b, err2 := os.Readlink(filepath.Join(repo, rel))
		return err1 == nil && err2 == nil && a == b
	case mine.Mode().IsRegular():
		if mine.Size() != theirs.Size() {
			return false
		}
		a, err1 := os.ReadFile(filepath.Join(wt, rel))
		b, err2 := os.ReadFile(filepath.Join(repo, rel))
		return err1 == nil && err2 == nil && bytes.Equal(a, b)
	}
	return false
}

// Drop removes the worktree at dir, which CheckDrop found can go, with git
// worktree remove: its branch stays. The local files Irie copied in that
// git counts as untracked go first, while they match the repo's; git
// itself refuses a worktree with anything else not committed, and then
// they are put back.
func Drop(dir, repo string, copied []string, timeout time.Duration) error {
	out, err := git(timeout, dir, nil, "status", "--porcelain=v2", "-z", "--untracked-files=all")
	if err != nil {
		return err
	}
	untracked := map[string]bool{}
	for _, f := range strings.Split(out, "\x00") {
		if p, ok := strings.CutPrefix(f, "? "); ok {
			untracked[p] = true
		}
	}
	var gone []string
	for _, f := range copied {
		p := filepath.Join(dir, f)
		if _, err := os.Lstat(p); err != nil || !untracked[f] {
			continue
		}
		if !sameCopy(repo, dir, f) {
			putBack(repo, dir, gone)
			return fmt.Errorf("its copy of %s changed", f)
		}
		if err := os.Remove(p); err != nil {
			putBack(repo, dir, gone)
			return err
		}
		gone = append(gone, f)
	}
	if _, err := git(timeout, repo, nil, "worktree", "remove", dir); err != nil {
		putBack(repo, dir, gone)
		return err
	}
	return nil
}

// putBack copies the local files rel back into the worktree, after a drop
// that did not happen.
func putBack(repo, wt string, rel []string) {
	for _, f := range rel {
		copyFile(repo, wt, f)
	}
}

// Restore adds back the worktree of branch that Drop removed, at dir, as
// Create makes one: git worktree add, which runs the repo's post-checkout
// hook, then the local files and the submodules. A path git still lists,
// as one removed by hand leaves it, is cleared first.
func Restore(repo, dir, branch string, o Options) (Result, error) {
	if !filepath.IsAbs(dir) {
		return Result{}, fmt.Errorf("worktree: %q is not an absolute path", dir)
	}
	if exists(dir) {
		return Result{}, fmt.Errorf("%s is there already", dir)
	}
	if _, err := git(gitTimeout, repo, nil, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err != nil {
		if exitCode(err) == 1 {
			return Result{}, fmt.Errorf("its branch %s is gone from %s", branch, repo)
		}
		return Result{}, err
	}
	registered, err := registered(repo)
	if err != nil {
		return Result{}, err
	}
	if registered[realish(dir)] {
		if _, err := git(gitTimeout, repo, nil, "worktree", "remove", dir); err != nil {
			return Result{}, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return Result{}, err
	}
	if _, err := git(gitTimeout, repo, nil, "worktree", "add", "--quiet", dir, branch); err != nil {
		return Result{}, err
	}
	res := Result{Dir: dir, Slug: filepath.Base(dir), Branch: branch}
	fill(repo, &res, o)
	return res, nil
}

// short is a commit id cut to 12 characters.
func short(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// DiskUsage is what the files under dir take on the disk, as du counts it:
// each file's blocks, links not followed.
func DiskUsage(dir string) int64 {
	var n int64
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			n += int64(st.Blocks) * 512
		} else {
			n += fi.Size()
		}
		return nil
	})
	return n
}
