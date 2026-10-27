// Package worktree makes the git worktree that an @repo task runs in (DESIGN
// §4.2): a new branch, checked out under worktree.root outside the repo, with
// the repo's tracked files at worktree.base and the untracked local files
// that the repo's .worktreeinclude and worktree.copy name. Uncommitted
// changes stay in the main checkout. Irie never removes a worktree.
package worktree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Options are the [worktree] config, with its root resolved.
type Options struct {
	Root         string   // worktrees go in Root/<repo>/<slug>
	Base         string   // what a new branch starts from
	BranchPrefix string   // a branch is BranchPrefix + slug
	Copy         []string // untracked paths copied in when present
	Submodules   bool     // check out the repo's submodules too
}

// Repo is the git checkout a directory is in.
type Repo struct {
	Top  string // the checkout's top directory, in the form the caller gave
	Main string // the repo's main checkout, whose name the repo goes by
	Sub  string // the directory's path below Top; "" at the top
}

// Request asks for a worktree of one repo.
type Request struct {
	Repo  string            // the checkout the new branch starts in
	Name  string            // the repo's name: its worktrees go in Root/Name
	Slug  string            // the slug wanted; -2, -3 … when it is taken
	Taken func(string) bool // slugs taken elsewhere, such as by a session's name
}

// Result is a new worktree.
type Result struct {
	Dir      string
	Slug     string
	Branch   string
	Copied   []string // local files copied in, relative to Dir, sorted
	Warnings []string // what the user should know, such as changes left behind
}

const (
	gitTimeout       = 2 * time.Minute
	submoduleTimeout = 5 * time.Minute
)

// repoEnv are the variables that point git at another repo than the one it
// runs in (git rev-parse --local-env-vars). Irie drops them, so git always
// works on the repo it names with -C.
var repoEnv = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_IMPLICIT_WORK_TREE", "GIT_GRAFT_FILE", "GIT_NO_REPLACE_OBJECTS",
	"GIT_REPLACE_REF_BASE", "GIT_PREFIX", "GIT_SHALLOW_FILE",
}

// gitError is a git command that failed. msg is what git said.
type gitError struct {
	args []string
	code int
	msg  string
}

func (e *gitError) Error() string { return "git " + strings.Join(e.args, " ") + ": " + e.msg }

// exitCode is the exit code of a failed git command, or -1.
func exitCode(err error) int {
	var ge *gitError
	if errors.As(err, &ge) {
		return ge.code
	}
	return -1
}

// git runs git -C dir args and returns what it printed. Git never prompts,
// since nobody could answer, and takes no optional locks, so looking at the
// main checkout never rewrites its index. It runs in its own process group,
// so a timeout stops everything it started.
func git(timeout time.Duration, dir string, stdin []byte, args ...string) (string, error) {
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("git: %q is not an absolute path", dir)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(without(os.Environ(), repoEnv), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		code := -1
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		}
		msg := strings.TrimSpace(stderr.String())
		switch {
		case ctx.Err() != nil:
			msg = fmt.Sprintf("no answer within %v", timeout)
		case msg == "":
			msg = err.Error()
		}
		return stdout.String(), &gitError{args: args, code: code, msg: msg}
	}
	return stdout.String(), nil
}

func without(env, names []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if !slices.Contains(names, k) {
			out = append(out, kv)
		}
	}
	return out
}

// Find reports the git checkout that dir, an absolute path, is in. Outside
// any checkout, ok is false.
func Find(dir string) (r Repo, ok bool, err error) {
	if !filepath.IsAbs(dir) {
		return Repo{}, false, fmt.Errorf("worktree: %q is not an absolute path", dir)
	}
	out, err := git(gitTimeout, dir, nil, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-common-dir")
	if exitCode(err) == 128 {
		// Not in a work tree: outside git, or in a bare repo.
		return Repo{}, false, nil
	}
	if err != nil {
		return Repo{}, false, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		return Repo{}, false, fmt.Errorf("git rev-parse printed %q", out)
	}
	top, common := lines[0], lines[1]
	r = Repo{Top: top, Main: top}
	if filepath.Base(common) == ".git" {
		r.Main = filepath.Dir(common)
	}
	// Git prints real paths. Where it can, Find keeps the caller's own form,
	// which may lead through a symlink.
	given := filepath.Clean(dir)
	real, err := filepath.EvalSymlinks(given)
	if err != nil {
		return r, true, nil
	}
	rel, err := filepath.Rel(top, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return r, true, nil
	}
	givenTop := given
	if rel != "." {
		r.Sub = rel
		givenTop = ""
		if strings.HasSuffix(given, string(filepath.Separator)+rel) {
			givenTop = strings.TrimSuffix(given, string(filepath.Separator)+rel)
		}
	}
	if givenTop != "" {
		if r.Main == top {
			r.Main = givenTop
		}
		r.Top = givenTop
	}
	return r, true, nil
}

func numbered(slug string, n int) string {
	if n == 1 {
		return slug
	}
	return slug + "-" + strconv.Itoa(n)
}

// Create makes a worktree of r.Repo at Root/Name/<slug> on the branch
// BranchPrefix + <slug>, taking the first slug that no session, directory,
// branch or registered worktree holds.
func Create(r Request, o Options) (Result, error) {
	if r.Slug == "" || r.Name == "" {
		return Result{}, errors.New("worktree: no slug or repo name")
	}
	if r.Taken == nil {
		r.Taken = func(string) bool { return false }
	}
	if _, err := git(gitTimeout, r.Repo, nil, "rev-parse", "--verify", "--quiet", "--end-of-options", o.Base+"^{commit}"); err != nil {
		if exitCode(err) != 1 {
			return Result{}, err
		}
		if o.Base == "HEAD" {
			return Result{}, fmt.Errorf("%s has no commits yet: commit once, or add ! to run in the checkout itself", r.Repo)
		}
		return Result{}, fmt.Errorf("worktree.base %q is not a commit in %s", o.Base, r.Repo)
	}
	registered, err := registered(r.Repo)
	if err != nil {
		return Result{}, err
	}
	parent := filepath.Join(o.Root, r.Name)
	var res Result
	for n := 1; res.Dir == ""; n++ {
		if n > 1000 {
			return Result{}, fmt.Errorf("worktree: no free slug for %s in %s", r.Slug, parent)
		}
		slug := numbered(r.Slug, n)
		dir := filepath.Join(parent, slug)
		if r.Taken(slug) || exists(dir) || registered[realish(dir)] {
			continue
		}
		branch := o.BranchPrefix + slug
		_, err := git(gitTimeout, r.Repo, nil, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
		if err == nil {
			continue // the branch exists
		}
		if exitCode(err) != 1 {
			return Result{}, err
		}
		res = Result{Dir: dir, Slug: slug, Branch: branch}
	}
	for _, d := range []string{o.Root, parent} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return Result{}, err
		}
	}
	// --no-track: the agent's branch follows no upstream, whatever it starts from.
	if _, err := git(gitTimeout, r.Repo, nil, "worktree", "add", "--quiet", "--no-track", "-b", res.Branch, res.Dir, o.Base); err != nil {
		return Result{}, err
	}

	files, err := localFiles(r.Repo, o.Copy)
	if err != nil {
		res.Warnings = append(res.Warnings, "no local files copied: "+err.Error())
	}
	for _, f := range files {
		switch err := copyFile(r.Repo, res.Dir, f); {
		case err == nil:
			res.Copied = append(res.Copied, f)
		case !errors.Is(err, fs.ErrExist):
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s not copied: %v", f, err))
		}
	}
	if o.Submodules && exists(filepath.Join(res.Dir, ".gitmodules")) {
		if _, err := git(submoduleTimeout, res.Dir, nil, "submodule", "update", "--init", "--recursive"); err != nil {
			res.Warnings = append(res.Warnings, "submodules not checked out: "+err.Error())
		}
	}
	if n := uncommitted(r.Repo, res.Copied); n > 0 {
		was := "changes in the main checkout were"
		if n == 1 {
			was = "change in the main checkout was"
		}
		res.Warnings = append(res.Warnings, fmt.Sprintf("%d uncommitted %s not carried over", n, was))
	}
	return res, nil
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// realish resolves the symlinks of p's deepest existing ancestor, so a path
// that does not exist yet compares with the real paths git prints.
func realish(p string) string {
	rest := ""
	for dir := filepath.Clean(p); ; dir = filepath.Dir(dir) {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(real, rest)
		}
		if dir == filepath.Dir(dir) {
			return filepath.Clean(p)
		}
		rest = filepath.Join(filepath.Base(dir), rest)
	}
}

// registered returns the paths of the repo's worktrees, as git records
// them, including ones whose directory has gone.
func registered(repo string) (map[string]bool, error) {
	out, err := git(gitTimeout, repo, nil, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	paths := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if p, ok := strings.CutPrefix(line, "worktree "); ok {
			paths[p], paths[realish(p)] = true, true
		}
	}
	return paths, nil
}

func split0(s string) []string {
	var out []string
	for _, f := range strings.Split(s, "\x00") {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// localFiles lists the repo's untracked files that a worktree gets, relative
// to the repo: those that match a pattern of its .worktreeinclude and are
// gitignored, the convention Claude Code documents, and those under the
// paths of copy, ignored or not. Git does the matching.
func localFiles(repo string, copy []string) ([]string, error) {
	var files []string
	if include := filepath.Join(repo, ".worktreeinclude"); exists(include) {
		out, err := git(gitTimeout, repo, nil, "ls-files", "-z", "--others", "--ignored", "--exclude-from="+include)
		if err != nil {
			return nil, err
		}
		if matched := split0(out); len(matched) > 0 {
			// check-ignore exits 1 when it finds none ignored.
			out, err := git(gitTimeout, repo, []byte(strings.Join(matched, "\x00")+"\x00"), "check-ignore", "-z", "--stdin")
			if err != nil && exitCode(err) != 1 {
				return nil, err
			}
			files = append(files, split0(out)...)
		}
	}
	if len(copy) > 0 {
		args := []string{"ls-files", "-z", "--others", "--"}
		for _, p := range copy {
			args = append(args, ":(literal)"+p)
		}
		out, err := git(gitTimeout, repo, nil, args...)
		if err != nil {
			return nil, err
		}
		files = append(files, split0(out)...)
	}
	slices.Sort(files)
	return slices.Compact(files), nil
}

// copyFile copies the file rel from the repo into the worktree, keeping its
// mode, or the link itself for a symlink. It makes missing directories as
// the repo has them, but never writes through a symlink in the worktree,
// such as one HEAD holds where the main checkout has a directory. A file
// the worktree already has, from HEAD, is left alone (fs.ErrExist).
func copyFile(repo, wt, rel string) error {
	src, dst := filepath.Join(repo, rel), filepath.Join(wt, rel)
	dir := wt
	for _, part := range strings.Split(filepath.Dir(rel), string(filepath.Separator)) {
		if part == "." {
			break
		}
		dir = filepath.Join(dir, part)
		fi, err := os.Lstat(dir)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			perm := fs.FileMode(0o700)
			if sfi, err := os.Stat(filepath.Join(repo, mustRel(wt, dir))); err == nil {
				perm = sfi.Mode().Perm()
			}
			if err := os.Mkdir(dir, perm); err != nil {
				return err
			}
		case err != nil:
			return err
		case fi.Mode()&fs.ModeSymlink != 0:
			return fmt.Errorf("%s is a symlink in the worktree", mustRel(wt, dir))
		case !fi.IsDir():
			return fmt.Errorf("%s is a file in the worktree", mustRel(wt, dir))
		}
	}
	if exists(dst) {
		return fs.ErrExist
	}
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	switch {
	case fi.Mode()&fs.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	case !fi.Mode().IsRegular():
		return fmt.Errorf("not a regular file")
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fi.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func mustRel(base, p string) string {
	rel, err := filepath.Rel(base, p)
	if err != nil {
		return p
	}
	return rel
}

// uncommitted counts the changes in the checkout that a worktree does not
// get: changed tracked files and untracked files that git does not ignore,
// leaving out the ones that were copied.
func uncommitted(repo string, copied []string) int {
	out, err := git(gitTimeout, repo, nil, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return 0
	}
	n := 0
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 {
			continue
		}
		xy, path := f[:2], f[3:]
		if strings.ContainsAny(xy, "RC") {
			i++ // the next field is the path it came from
		}
		if !slices.Contains(copied, path) {
			n++
		}
	}
	return n
}
