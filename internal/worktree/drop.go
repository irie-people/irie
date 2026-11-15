package worktree

// Dropping a worktree whose work has landed, and adding it back (DESIGN
// §4.5). A closed session whose branch is in its repo's land branch, and
// whose worktree holds nothing more, needs only its branch: Irie removes
// the checkout with git worktree remove, which keeps the branch and its
// reflog, and reopen adds it back at the same path from the branch, as a
// new worktree is made. The worktree's own HEAD reflog goes with it. The
// checks read with no optional locks, and the removal never forces, so git
// refuses whatever it can see changed since they looked.

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

// Look is a worktree a drop looks at.
type Look struct {
	Dir    string // the worktree
	Repo   string // the repo's main checkout
	Branch string // the session's branch, which must be checked out there
	Land   string // the branch its work lands on (dog.land)
	Copied []string
	// Copy are worktree.copy's paths: what a worktree holds under them that
	// git does not track is a local file, as are its ignored files that the
	// repo's .worktreeinclude names.
	Copy []string
	// Scan walks its files too: for another checkout inside it, a folder
	// git could not remove, and the room it takes.
	Scan bool
}

// Landed is what CheckDrop found of a worktree.
type Landed struct {
	Gone   bool   // there is no worktree at the path, so nothing to drop
	Branch string // the branch it has checked out
	Commit string // that branch's tip
	// Local are its local files, each the repo's own copy, which a drop
	// takes out first and a restore copies in again.
	Local []string
	Bytes int64 // the room it takes on the disk, when it was walked
	// Keep says why the worktree must stay, in plain words; "" when it can
	// go.
	Keep string
}

// CheckDrop reads whether the worktree k.Dir can be removed and later added
// back from k.Branch alone: it is one of k.Repo's linked worktrees that git
// has not locked, with that branch checked out and no submodules; the
// branch's tip is in k.Land; nothing in it is uncommitted, untracked files
// included, nor hidden from git by assume-unchanged or skip-worktree; and
// its local files are the repo's own (Look.Copy). Other files git ignores,
// such as build output, do not count. With k.Scan, no other checkout is
// inside it, as Claude Code's worktrees and a clone in an ignored folder
// are, and git can remove every folder of it.
func CheckDrop(k Look, timeout time.Duration) (Landed, error) {
	var l Landed
	dir, repo := k.Dir, k.Repo
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
	// It is one of repo's linked worktrees, as git lists them, and none
	// of the others is inside it.
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
	for j, t := range trees {
		if j != i && within(realish(t.Path), realish(dir)) {
			l.Keep = "another worktree is inside it: " + t.Path
			return l, nil
		}
	}
	t := trees[i]
	l.Branch, l.Commit = t.Branch, t.Head
	switch {
	case t.Locked:
		l.Keep = "git has it locked (git worktree lock)"
	case t.Branch == "":
		l.Keep = "its HEAD is detached, at " + short(t.Head)
	case t.Branch != k.Branch:
		l.Keep = fmt.Sprintf("it has %s checked out, not %s", t.Branch, k.Branch)
	case exists(filepath.Join(dir, ".gitmodules")):
		l.Keep = "it has submodules"
	}
	if l.Keep != "" {
		return l, nil
	}
	if l.Keep, err = notLanded(repo, l.Commit, k.Land, timeout); err != nil || l.Keep != "" {
		return l, err
	}
	if l.Local, l.Keep, err = localCopies(k, timeout); err != nil || l.Keep != "" {
		return l, err
	}
	if l.Keep, err = notCommitted(dir, l.Local, timeout); err != nil || l.Keep != "" {
		return l, err
	}
	if l.Keep, err = skipped(dir, timeout); err != nil || l.Keep != "" {
		return l, err
	}
	if k.Scan {
		l.Bytes, l.Keep = scan(dir)
	}
	return l, nil
}

// within reports whether path is dir or below it.
func within(path, dir string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
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

// localCopies lists the worktree's local files, those Irie copied in and
// those the worktree holds where a new worktree gets them, and says which
// one is not the repo's own copy: one the session changed, or made where
// the repo has none, such as a settings file an agent writes. Restore
// could not give such a file back.
func localCopies(k Look, timeout time.Duration) ([]string, string, error) {
	found, err := localFiles(k.Dir, k.Copy)
	if err != nil {
		return nil, "", err
	}
	// What Irie copied in counts while git does not track it: a copy the
	// session committed comes back with its branch.
	if len(k.Copied) > 0 {
		args := []string{"ls-files", "-z", "--"}
		for _, f := range k.Copied {
			args = append(args, ":(literal)"+f)
		}
		out, err := git(timeout, k.Dir, nil, args...)
		if err != nil {
			return nil, "", err
		}
		tracked := split0(out)
		for _, f := range k.Copied {
			if _, err := os.Lstat(filepath.Join(k.Dir, f)); err == nil && !slices.Contains(tracked, f) {
				found = append(found, f)
			}
		}
	}
	slices.Sort(found)
	found = slices.Compact(found)
	for _, f := range found {
		if _, err := os.Lstat(filepath.Join(k.Repo, f)); err != nil {
			return found, "its local file " + f + " is not in the repo, so it would not come back", nil
		}
		if !sameCopy(k.Repo, k.Dir, f) {
			return found, "its local file " + f + " differs from the repo's", nil
		}
	}
	return found, "", nil
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
	return filesWords(files, "not committed"), nil
}

// filesWords says how many files are what, naming the first three.
func filesWords(files []string, what string) string {
	switch n := len(files); {
	case n == 0:
		return ""
	case n == 1:
		return "1 file is " + what + ": " + files[0]
	case n > 3:
		return fmt.Sprintf("%d files are %s: %s and more", n, what, strings.Join(files[:3], ", "))
	default:
		return fmt.Sprintf("%d files are %s: %s", n, what, strings.Join(files, ", "))
	}
}

// skipped says which tracked files git is told not to look at, with
// assume-unchanged or skip-worktree, whose changes status would not show.
// ls-files -v tags the first with a lower-case letter and the second S.
func skipped(dir string, timeout time.Duration) (string, error) {
	out, err := git(timeout, dir, nil, "ls-files", "-v", "-z")
	if err != nil {
		return "", err
	}
	var files []string
	for _, e := range strings.Split(out, "\x00") {
		if len(e) > 2 && e[1] == ' ' && (e[0] >= 'a' && e[0] <= 'z' || e[0] == 'S') {
			files = append(files, e[2:])
		}
	}
	return filesWords(files, "hidden from git's status (assume-unchanged or skip-worktree)"), nil
}

// scan walks the worktree at dir: the room its files take, and why it must
// stay when it must, the first of another checkout inside it, as a
// worktree or a clone in an ignored folder is, which git worktree remove
// would delete with whatever it holds; a folder it cannot read; or a
// read-only folder, such as a Go module cache's, which git could not empty,
// leaving half a worktree.
func scan(dir string) (int64, string) {
	var n int64
	var nested, unread, readOnly string
	top := filepath.Join(dir, ".git")
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		rel := mustRel(dir, p)
		if err != nil {
			if unread == "" {
				unread = rel
			}
			return nil
		}
		if d.Name() == ".git" && p != top && nested == "" {
			nested = filepath.Dir(rel)
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		// git must write and search a folder to empty it (W_OK|X_OK).
		if d.IsDir() && readOnly == "" && syscall.Access(p, 0o2|0o1) != nil {
			readOnly = rel
		}
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			n += int64(st.Blocks) * 512
		} else {
			n += fi.Size()
		}
		return nil
	})
	switch {
	case nested != "":
		return n, "another checkout is inside it, at " + nested
	case unread != "":
		return n, "a folder in it cannot be read: " + unread
	case readOnly != "":
		return n, "a folder in it is read-only, so git could not remove it: " + readOnly
	}
	return n, ""
}

// sameCopy reports whether the worktree's copy of the local file rel is
// the repo's: the same bytes, a link to the same place, or a link to the
// repo's file itself, as a repo's post-checkout hook may make. One the
// worktree no longer has is nothing to lose.
func sameCopy(repo, wt, rel string) bool {
	mine, err := os.Lstat(filepath.Join(wt, rel))
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	theirs, err2 := os.Lstat(filepath.Join(repo, rel))
	if err != nil || err2 != nil {
		return false
	}
	if mine.Mode()&fs.ModeSymlink != 0 {
		a, err1 := os.Stat(filepath.Join(wt, rel))
		b, err2 := os.Stat(filepath.Join(repo, rel))
		if err1 == nil && err2 == nil && os.SameFile(a, b) {
			return true
		}
	}
	if mine.Mode().Type() != theirs.Mode().Type() {
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
// worktree remove: its branch stays. Its local files (Landed.Local) that
// git counts as untracked go first, while they are the repo's own copies;
// git itself refuses a worktree with anything else not committed, and then
// they are put back. When git removed its record of the worktree but could
// not delete all of it, partial is true: what is left is no worktree, and
// Restore moves it aside.
func Drop(dir, repo string, local []string, timeout time.Duration) (partial bool, err error) {
	out, err := git(timeout, dir, nil, "status", "--porcelain=v2", "-z", "--untracked-files=all")
	if err != nil {
		return false, err
	}
	untracked := map[string]bool{}
	for _, f := range strings.Split(out, "\x00") {
		if p, ok := strings.CutPrefix(f, "? "); ok {
			untracked[p] = true
		}
	}
	var gone []string
	for _, f := range local {
		p := filepath.Join(dir, f)
		if _, err := os.Lstat(p); err != nil || !untracked[f] {
			continue
		}
		if !sameCopy(repo, dir, f) {
			putBack(repo, dir, gone)
			return false, fmt.Errorf("its local file %s changed", f)
		}
		if err := os.Remove(p); err != nil {
			putBack(repo, dir, gone)
			return false, err
		}
		gone = append(gone, f)
	}
	if _, err := git(timeout, repo, nil, "worktree", "remove", dir); err != nil {
		// git forgets the worktree even when it could not delete it all.
		reg, rerr := registered(repo)
		partial = rerr == nil && !reg[realish(dir)] && exists(dir)
		if exists(dir) {
			putBack(repo, dir, gone)
		}
		return partial, err
	}
	return false, nil
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
// hook, then the local files and the submodules. When the branch is gone,
// as a merged branch is often deleted, it is made again at commit, its tip
// when it was dropped. What a drop that git could not finish left at dir,
// which git no longer counts as a worktree, is moved aside first, and a
// path git still lists, as one removed by hand leaves it, is cleared.
func Restore(repo, dir, branch, commit string, o Options) (Result, error) {
	if !filepath.IsAbs(dir) {
		return Result{}, fmt.Errorf("worktree: %q is not an absolute path", dir)
	}
	res := Result{Dir: dir, Slug: filepath.Base(dir), Branch: branch}
	reg, err := registered(repo)
	if err != nil {
		return Result{}, err
	}
	if exists(dir) {
		if reg[realish(dir)] || isDir(filepath.Join(dir, ".git")) {
			return Result{}, fmt.Errorf("%s is there already", dir)
		}
		aside := dir + ".left"
		for n := 2; exists(aside); n++ {
			aside = dir + ".left-" + strconv.Itoa(n)
		}
		if err := os.Rename(dir, aside); err != nil {
			return Result{}, fmt.Errorf("what a drop left at %s could not be moved aside: %v", dir, err)
		}
		res.Warnings = append(res.Warnings, "what was left of its old worktree is in "+aside)
	}
	if reg[realish(dir)] {
		if _, err := git(gitTimeout, repo, nil, "worktree", "remove", dir); err != nil {
			return Result{}, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return Result{}, err
	}
	add := []string{"worktree", "add", "--quiet", dir, branch}
	if _, err := git(gitTimeout, repo, nil, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err != nil {
		if exitCode(err) != 1 {
			return Result{}, err
		}
		if commit == "" {
			return Result{}, fmt.Errorf("its branch %s is gone from %s", branch, repo)
		}
		if _, err := git(gitTimeout, repo, nil, "cat-file", "-e", commit+"^{commit}"); err != nil {
			return Result{}, fmt.Errorf("its branch %s is gone from %s, and so is its commit %s", branch, repo, short(commit))
		}
		add = []string{"worktree", "add", "--quiet", "--no-track", "-b", branch, dir, commit}
		res.Warnings = append(res.Warnings, fmt.Sprintf("its branch %s was gone, so it starts again at %s", branch, short(commit)))
	}
	if _, err := git(gitTimeout, repo, nil, add...); err != nil {
		return Result{}, err
	}
	fill(repo, &res, o)
	return res, nil
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
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
	n, _ := scan(dir)
	return n
}
