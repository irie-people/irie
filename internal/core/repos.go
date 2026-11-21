package core

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/journal"
	"github.com/irie-people/irie/internal/repo"
	"github.com/irie-people/irie/internal/worktree"
)

// maxRecent is how many recently used repos the core keeps.
const maxRecent = 20

// place is where a new session runs, and what the user should know about it.
type place struct {
	name     string // the session's name
	named    bool   // the user chose it (title.go)
	cwd      string
	repo     string // the main checkout, when the task runs in a git repo
	worktree string
	branch   string
	copied   []string
	warnings []string
}

func home() string {
	h, _ := os.UserHomeDir()
	return h
}

// repoOptions are where @names find repos. The caller must not hold c.mu.
func (c *Core) repoOptions() repo.Options {
	c.mu.Lock()
	recent := slices.Clone(c.recent)
	c.mu.Unlock()
	return repo.Options{Aliases: c.cfg().Repos.Aliases, Roots: c.cfg().Repos.Roots, Recent: recent, Home: home()}
}

func (c *Core) worktreeOptions() worktree.Options {
	root := c.paths.Worktrees()
	if c.cfg().Worktree.Root != "" {
		root = repo.Expand(c.cfg().Worktree.Root, home())
	}
	return worktree.Options{
		Root: root, Base: c.cfg().Worktree.Base, BranchPrefix: c.cfg().Worktree.BranchPrefix,
		Copy: c.cfg().Worktree.Copy, Submodules: c.cfg().Worktree.Submodules,
	}
}

// place finds where a task runs (DESIGN §4.2): from a repo (a.From), in a
// new scratch dir holding a copy of its last commit; with no repo, in
// a.Cwd, in spawn.default_cwd or in a new scratch dir; with one, in a new worktree of
// it, or in the directory itself for in_place or a directory outside git.
// A new scratch dir or worktree, and its branch, are named after base, the
// first free of base, base-2 and so on. An @name that fits no repo or
// several fails, and nothing is spawned. The place has no name yet: the
// spawn gives it one. The caller holds c.spawnMu and not c.mu.
func (c *Core) place(a ipc.SpawnArgs, base string) (place, error) {
	if a.From != "" {
		if a.Cwd != "" || a.Repo != "" {
			return place{}, ipc.Errorf(ipc.CodeBadArgs, "a task starts from a copy of a repo, or runs in a directory or repo, not both")
		}
		cwd, err := c.copyFrom(a.From, base)
		return place{cwd: cwd}, err
	}
	var dir string
	if a.Repo != "" {
		var err error
		if dir, err = repo.Find(a.Repo, c.repoOptions()); err != nil {
			var amb *repo.AmbiguousError
			var nf *repo.NotFoundError
			switch {
			case errors.As(err, &amb):
				return place{}, ipc.Errorf(ipc.CodeAmbiguous, "%v", err)
			case errors.As(err, &nf):
				return place{}, ipc.Errorf(ipc.CodeNotFound, "%v", err)
			}
			return place{}, ipc.Errorf(ipc.CodeBadArgs, "%v", err)
		}
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			return place{}, ipc.Errorf(ipc.CodeBadArgs, "@%s: no such directory: %s", a.Repo, repo.Tilde(dir, home()))
		}
	}
	if a.Repo == "" {
		cwd, err := c.workdir(a.Cwd, base)
		return place{cwd: cwd}, err
	}
	r, inGit, err := worktree.Find(dir)
	if err != nil {
		return place{}, err
	}
	if !inGit || a.InPlace {
		p := place{cwd: dir}
		if inGit {
			p.repo = r.Main
		}
		return p, nil
	}
	o := c.worktreeOptions()
	o.Land = c.landOf(r.Main)
	res, err := worktree.Create(worktree.Request{Repo: r.Top, Name: filepath.Base(r.Main), Slug: base, Taken: c.dropTaken(o.Root, filepath.Base(r.Main))}, o)
	if err != nil {
		return place{}, err
	}
	// The agent starts where the @path pointed, in the worktree's copy of it.
	cwd := res.Dir
	if r.Sub != "" {
		if fi, err := os.Stat(filepath.Join(res.Dir, r.Sub)); err == nil && fi.IsDir() {
			cwd = filepath.Join(res.Dir, r.Sub)
		}
	}
	return place{
		cwd: cwd, repo: r.Main, worktree: res.Dir, branch: res.Branch,
		copied: res.Copied, warnings: res.Warnings,
	}, nil
}

// copyTimeout bounds the copy of a repo a task starts from.
const copyTimeout = 2 * time.Minute

// fromRepo is the git repo a task starts from, from as given: an absolute
// or ~/ path to a checkout, here.
func fromRepo(from string) (string, error) {
	if !repo.IsPath(from) {
		return "", ipc.Errorf(ipc.CodeBadArgs, "start from a repo by its path, /… or ~/…, not %q", from)
	}
	dir := repo.Expand(from, home())
	if out, err := exec.Command("git", "-C", dir, "rev-parse", "--verify", "--quiet", "HEAD^{commit}").CombinedOutput(); err != nil {
		why, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
		if why != "" {
			why = ": " + why
		}
		return "", ipc.Errorf(ipc.CodeBadArgs, "%s is not a git repo with a commit to start from%s", repo.Tilde(dir, home()), why)
	}
	return dir, nil
}

// copyFrom makes a task's folder a copy of the files of the git repo
// from, as of its last commit, in a new scratch folder named after base,
// as a bench's runs start with --from (DESIGN §6.6): each starts from the
// same files, and comparing its folder with the repo shows what it
// changed. The copy is no git repo of its own. Claude Code asks before it
// works in a git repo whose own folder it was not told to trust, which an
// unattended run cannot answer, while a plain folder in a trusted one, as
// the scratch folder may be, needs no answer. A copy that fails leaves no
// folder behind.
func (c *Core) copyFrom(from, base string) (string, error) {
	src, err := fromRepo(from)
	if err != nil {
		return "", err
	}
	dir, err := c.newScratch(base)
	if err != nil {
		return "", err
	}
	if err := gitExport(src, dir); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("copying %s: %v", repo.Tilde(src, home()), err)
	}
	return dir, nil
}

// gitExport writes the files of the last commit of the git repo src into
// dir, and nothing outside it.
func gitExport(src, dir string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), copyTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", src, "archive", "--format=tar", "HEAD")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	werr := untar(root, out)
	if werr != nil {
		cancel() // stops git, which may still be writing
	}
	if err := cmd.Wait(); err != nil && werr == nil {
		werr = errors.New(firstNonEmpty(strings.TrimSpace(stderr.String()), err.Error()))
	}
	return werr
}

// untar writes the files, folders and links of the tar stream r into
// root. The global header git archive starts with, naming the commit, is
// no file.
func untar(root *os.Root, r io.Reader) error {
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.FromSlash(strings.TrimSuffix(h.Name, "/"))
		if parent := filepath.Dir(name); parent != "." {
			if err := root.MkdirAll(parent, 0o700); err != nil {
				return err
			}
		}
		switch h.Typeflag {
		case tar.TypeDir:
			err = root.MkdirAll(name, 0o700)
		case tar.TypeReg:
			var f *os.File
			if f, err = root.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, h.FileInfo().Mode().Perm()|0o600); err == nil {
				_, err = io.Copy(f, tr)
				if cerr := f.Close(); err == nil {
					err = cerr
				}
			}
		case tar.TypeSymlink:
			err = root.Symlink(h.Linkname, name)
		}
		if err != nil {
			return err
		}
	}
}

// loadRecent rebuilds the recently used repos from the journal's spawns, so
// they are derived like everything else, and survive a restart.
func (c *Core) loadRecent() {
	evs, err := journal.ReadKind(c.paths.Journal(), time.Time{}, event.SpawnRequested)
	if err != nil {
		c.log.Warn("journal", "err", err)
	}
	for i := len(evs) - 1; i >= 0 && len(c.recent) < maxRecent; i-- {
		if r := evs[i].Str("repo"); r != "" && !slices.Contains(c.recent, r) {
			c.recent = append(c.recent, r)
		}
	}
}

// addRecent puts r first among the recently used repos. The caller holds
// c.mu.
func (c *Core) addRecent(r string) {
	c.recent = slices.Insert(slices.DeleteFunc(c.recent, func(x string) bool { return x == r }), 0, r)
	if len(c.recent) > maxRecent {
		c.recent = c.recent[:maxRecent]
	}
}

// complete returns the tokens that Tab offers for a command-bar token: the
// profiles for +, the repo names or directories for @.
func (c *Core) complete(text string) (items, notes []string) {
	items = []string{}
	switch {
	case strings.HasPrefix(text, "+"):
		return c.completeProfiles(text)
	case strings.HasPrefix(text, "@") && !strings.HasSuffix(text, "!"):
		for _, v := range repo.Complete(text[1:], c.repoOptions()) {
			items = append(items, "@"+v)
		}
	}
	return items, nil
}
