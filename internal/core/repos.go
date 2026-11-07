package core

import (
	"errors"
	"os"
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

// place finds where a task runs (DESIGN §4.2): with no repo, in a.Cwd, in
// spawn.default_cwd or in a new scratch dir; with one, in a new worktree of
// it, or in the directory itself for in_place or a directory outside git.
// A new scratch dir or worktree, and its branch, are named after base, the
// first free of base, base-2 and so on. An @name that fits no repo or
// several fails, and nothing is spawned. The place has no name yet: the
// spawn gives it one. The caller holds c.spawnMu and not c.mu.
func (c *Core) place(a ipc.SpawnArgs, base string) (place, error) {
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
	res, err := worktree.Create(worktree.Request{Repo: r.Top, Name: filepath.Base(r.Main), Slug: base}, o)
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
