package core

// Dropping a closed session's worktree once its work has landed (DESIGN
// §4.5; docs/proposals/reversible.md, step 5). A closed session whose
// branch is in its repo's land branch (dog.land), and whose worktree holds
// nothing more, needs only its branch: once its agent has ended, the core
// removes the checkout with git and keeps the branch, and reopen adds the
// worktree back at the same path. Nothing that runs there is disturbed: a
// worktree an open session works in, or one that any process holds, by
// its working directory, its program or a file it has open, stays, and is
// looked at again a little later. The journal records each removal, with
// the path, branch and commit that put it back by hand, and
// `irie ctl worktrees` sweeps the sessions closed before.

import (
	"cmp"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/journal"
	"github.com/irie-people/irie/internal/proc"
	"github.com/irie-people/irie/internal/worktree"
)

// dropSettle is how long after a closed session's pane goes the core first
// looks at its worktree, so that what the close ended has exited.
const dropSettle = 2 * time.Second

// dropAgain are how long the core waits to look again at a closed
// session's worktree that something still ran in, once each.
var dropAgain = []time.Duration{time.Minute, 10 * time.Minute}

// dropGit bounds each git command of a look.
const dropGit = 30 * time.Second

// dropWorkers is how many worktrees a sweep reads at once.
const dropWorkers = 4

// droppedTree is a worktree Irie dropped, which its session's reopen adds
// back: from its worktree.dropped.
type droppedTree struct {
	session, repo, branch, commit string
	at                            time.Time
}

// loadDrops rebuilds from the journal the worktrees Irie dropped that no
// reopen has added back, so that no new worktree takes one's path.
func (c *Core) loadDrops() {
	evs, err := journal.ReadKinds(c.paths.Journal(), time.Time{}, event.WorktreeDropped, event.WorktreeRestored)
	if err != nil {
		c.log.Warn("journal", "err", err)
	}
	for _, e := range evs {
		p := filepath.Clean(e.Str("path"))
		switch e.Kind {
		case event.WorktreeDropped:
			c.noteDropped(p, droppedTree{session: e.Session, repo: e.Str("repo"), branch: e.Str("branch"), commit: e.Str("commit"), at: e.Time()})
		case event.WorktreeRestored:
			delete(c.closes.dropped, p)
		}
	}
}

// noteDropped keeps that Irie dropped the worktree at p. The caller holds
// c.mu, or the core has not started.
func (c *Core) noteDropped(p string, d droppedTree) {
	if c.closes.dropped == nil {
		c.closes.dropped = map[string]droppedTree{}
	}
	c.closes.dropped[p] = d
}

// wasDropped reports whether Irie dropped the worktree at p and no reopen
// has added it back. The caller holds c.mu.
func (c *Core) wasDropped(p string) bool {
	_, ok := c.closes.dropped[filepath.Clean(p)]
	return ok
}

// dropTaken is the Taken of a new worktree at root/name: the paths of the
// worktrees Irie dropped are their sessions' still, which reopen adds back.
// It takes c.mu.
func (c *Core) dropTaken(root, name string) func(string) bool {
	return func(slug string) bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.wasDropped(filepath.Join(root, name, slug))
	}
}

// dropLook is a closed session's worktree as a drop looks at it, copied so
// that git runs without c.mu.
type dropLook struct {
	id, name string
	closed   time.Time
	spec     host.Spec
	land     string // the branch its repo's work lands on (dog.land)
	keep     string // why Irie keeps it before git looks, or ""
	busy     bool   // what keeps it may end
}

// dropLookAt is closed session id's worktree as a drop looks at it; ok is
// false when the session is open or Irie made it no worktree. A worktree
// an open session works in, as a fork made with --here does, stays, as
// does one whose agent is still ending. The caller holds c.mu.
func (c *Core) dropLookAt(id string) (dropLook, bool) {
	if c.sessions[id] != nil || !c.closes.done[id] {
		return dropLook{}, false
	}
	spec, err := host.ReadSpec(c.paths.SpecFile(id))
	if err != nil || spec.Worktree == "" || spec.Repo == "" || spec.Branch == "" || !filepath.IsAbs(spec.Worktree) || !filepath.IsAbs(spec.Repo) {
		return dropLook{}, false
	}
	spec.Worktree = filepath.Clean(spec.Worktree)
	l := dropLook{id: id, name: id, spec: spec, land: c.landOf(spec.Repo)}
	if r, ok := c.closedRecord(id); ok {
		l.name, l.closed = r.Name, r.Closed
	}
	for _, oid := range slices.Sorted(maps.Keys(c.sessions)) {
		x := c.sessions[oid]
		if x.worktree != "" && filepath.Clean(x.worktree) == spec.Worktree || inside(x.cwd, spec.Worktree) {
			l.keep = fmt.Sprintf("%q works in it", x.name)
			return l, true
		}
	}
	for _, e := range c.closes.ending {
		if e.session == id {
			l.keep, l.busy = "its agent is still ending", true
		}
	}
	return l, true
}

// lookDrop reads whether each of ls can go, as a ClosedTree each, with what
// it takes on the disk; one whose worktree is gone already has none. Git
// reads dropWorkers of them at once, and one look over the processes finds
// what runs in any of them. It runs git, so the caller does not hold c.mu.
func (c *Core) lookDrop(ls []dropLook) []ipc.ClosedTree {
	trees := make([]*ipc.ClosedTree, len(ls))
	var wg sync.WaitGroup
	next := make(chan int)
	for range min(dropWorkers, len(ls)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				trees[i] = c.lookOne(ls[i])
			}
		}()
	}
	for i := range ls {
		next <- i
	}
	close(next)
	wg.Wait()

	// What runs in those that could go keeps them.
	var free []string
	for _, t := range trees {
		if t != nil && t.Keep == "" {
			free = append(free, t.Path)
		}
	}
	if len(free) > 0 {
		held, err := proc.Holding(free)
		for _, t := range trees {
			if t == nil || t.Keep != "" {
				continue
			}
			switch {
			case err != nil:
				t.Keep, t.Busy = "cannot tell what runs in it: "+err.Error(), true
			case len(held[t.Path]) > 0:
				t.Keep, t.Busy = heldWords(held[t.Path]), true
			}
		}
	}
	var out []ipc.ClosedTree
	for _, t := range trees {
		if t != nil {
			out = append(out, *t)
		}
	}
	return out
}

// lookOne is what git says of one closed session's worktree, or nil when
// it is gone already.
func (c *Core) lookOne(l dropLook) *ipc.ClosedTree {
	t := &ipc.ClosedTree{Session: l.id, Name: l.name, Path: l.spec.Worktree, Branch: l.spec.Branch, Keep: l.keep, Busy: l.busy}
	got, err := worktree.CheckDrop(l.spec.Worktree, l.spec.Repo, l.spec.Branch, l.land, l.spec.Copied, dropGit)
	switch {
	case err != nil:
		if !exists(l.spec.Worktree) {
			return nil
		}
		if t.Keep == "" {
			t.Keep = "git could not read it: " + err.Error()
		}
	case got.Gone:
		return nil
	case t.Keep == "":
		t.Keep = got.Keep
	}
	if got.Commit != "" {
		t.Commit = got.Commit
	}
	t.Bytes = worktree.DiskUsage(l.spec.Worktree)
	return t
}

// heldWords says what holds a worktree: its first holder, and how many
// more.
func heldWords(hs []proc.Holder) string {
	h := hs[0]
	what := "has a file open in it"
	switch h.What {
	case "cwd":
		what = "works in it"
	case "txt":
		what = "runs a program from it"
	}
	s := fmt.Sprintf("%s (pid %d) %s", cmp.Or(h.Name, "a process"), h.PID, what)
	pids := map[int]bool{}
	for _, x := range hs {
		pids[x.PID] = true
	}
	if n := len(pids) - 1; n == 1 {
		s += ", and 1 more process"
	} else if n > 1 {
		s += fmt.Sprintf(", and %d more processes", n)
	}
	return s
}

// dropOne drops closed session id's worktree when it can go, with a spawn's
// turn, so that no reopen, spawn or fork runs meanwhile: every check again,
// git's own last. ok is false when the session is open, or has no worktree
// there.
func (c *Core) dropOne(id string) (t ipc.ClosedTree, ok bool) {
	c.spawnMu.Lock()
	defer c.spawnMu.Unlock()
	c.mu.Lock()
	l, ok := c.dropLookAt(id)
	c.mu.Unlock()
	if !ok {
		return t, false
	}
	trees := c.lookDrop([]dropLook{l})
	if len(trees) == 0 {
		return t, false
	}
	t = trees[0]
	if t.Keep != "" {
		return t, true
	}
	if err := worktree.Drop(l.spec.Worktree, l.spec.Repo, l.spec.Copied, dropGit); err != nil {
		t.Keep = "git did not remove it: " + err.Error()
		c.log.Warn("worktree drop", "session", id, "path", t.Path, "err", err)
		return t, true
	}
	t.Dropped = true
	c.mu.Lock()
	e := event.Event{Session: id, Source: event.SourceIrie, Harness: c.harnessOfSpec(l.spec), Kind: event.WorktreeDropped, Data: map[string]any{
		"path": t.Path, "repo": l.spec.Repo, "branch": t.Branch, "commit": t.Commit, "land": l.land, "bytes": t.Bytes,
	}}
	if e, err := c.journal.Append(e); err != nil {
		c.log.Warn("journal", "kind", e.Kind, "err", err)
	} else {
		for s := range c.subs {
			s.event(e)
		}
	}
	c.noteDropped(t.Path, droppedTree{session: id, repo: l.spec.Repo, branch: t.Branch, commit: t.Commit, at: time.Now()})
	c.mu.Unlock()
	c.log.Info("worktree dropped", "session", id, "name", l.name, "path", t.Path, "branch", t.Branch, "commit", t.Commit, "land", l.land, "mb", mb(t.Bytes))
	return t, true
}

// harnessOfSpec is the harness a session ran on: its spec's, else its
// profile's, as an older spec names none.
func (c *Core) harnessOfSpec(spec host.Spec) string {
	if spec.Harness != "" {
		return spec.Harness
	}
	return cmp.Or(c.cfg().Profiles[spec.Profile].Harness, "unknown")
}

// mb is a size in megabytes, as du counts them, to one place.
func mb(bytes int64) string { return fmt.Sprintf("%.1f", float64(bytes)/(1<<20)) }

// dropAfterClose drops closed session id's worktree once its agent has
// ended, while worktree.drop is on; a worktree something still runs in is
// looked at again after each of dropAgain. It runs in the background.
func (c *Core) dropAfterClose(id string, try int) {
	if !c.cfg().Worktree.Drop {
		return
	}
	t, ok := c.dropOne(id)
	switch {
	case !ok || t.Dropped:
	case t.Busy && try < len(dropAgain):
		c.log.Info("worktree kept for now", "session", id, "path", t.Path, "why", t.Keep, "again", dropAgain[try])
		time.AfterFunc(dropAgain[try], func() { c.dropAfterClose(id, try+1) })
	default:
		c.log.Info("worktree kept", "session", id, "path", t.Path, "why", t.Keep)
	}
}

// dropOnEnd looks at closed session id's worktree soon after its pane has
// gone. The caller holds c.mu.
func (c *Core) dropOnEnd(id string) {
	if id == "" || c.sessions[id] != nil || !c.closes.done[id] || !c.cfg().Worktree.Drop {
		return
	}
	time.AfterFunc(dropSettle, func() { c.dropAfterClose(id, 0) })
}

// worktreesOp is the sweep of the closed sessions' worktrees (OpWorktrees),
// from the process peer: a dry run, or with a.Drop the removal of each
// that can go, which only the person may ask for.
func (c *Core) worktreesOp(peer int, a ipc.WorktreesArgs) (ipc.WorktreesResult, error) {
	if a.Drop && c.fromAgent(peer) {
		return ipc.WorktreesResult{}, ipc.Errorf(ipc.CodeBadArgs, "An agent can't sweep the closed sessions' worktrees: that is for your user to do, with `"+brand.Slug+" ctl worktrees --drop` in their own terminal.")
	}
	c.mu.Lock()
	// The newest closed first: those reopen can reach, then the rest.
	var ids []string
	for _, r := range c.closes.list {
		ids = append(ids, r.ID)
	}
	var older []string
	for id := range c.closes.done {
		if !slices.Contains(ids, id) {
			older = append(older, id)
		}
	}
	slices.Sort(older)
	slices.Reverse(older)
	ids = append(ids, older...)
	var ls []dropLook
	seen := map[string]bool{}
	for _, id := range ids {
		if l, ok := c.dropLookAt(id); ok && !seen[l.spec.Worktree] {
			seen[l.spec.Worktree] = true
			ls = append(ls, l)
		}
	}
	c.mu.Unlock()

	trees := c.lookDrop(ls)
	res := ipc.WorktreesResult{Trees: []ipc.ClosedTree{}}
	for i := range trees {
		t := &trees[i]
		if !a.Drop || t.Keep != "" {
			continue
		}
		size := t.Bytes
		got, ok := c.dropOne(t.Session)
		if !ok {
			t.Keep = "it went meanwhile"
			continue
		}
		*t = got
		t.Bytes = cmp.Or(got.Bytes, size)
		if t.Dropped {
			res.Dropped++
		}
	}
	slices.SortStableFunc(trees, func(x, y ipc.ClosedTree) int {
		return cmp.Compare(goes(x), goes(y))
	})
	for _, t := range trees {
		if t.Keep == "" || t.Dropped {
			res.Can++
			res.Bytes += t.Bytes
		}
		res.Trees = append(res.Trees, t)
	}
	c.log.Info("worktrees swept", "drop", a.Drop, "trees", len(trees), "can", res.Can, "mb", mb(res.Bytes), "dropped", res.Dropped)
	return res, nil
}

// goes orders a sweep's worktrees: those that went or can go, then the rest.
func goes(t ipc.ClosedTree) int {
	if t.Keep == "" || t.Dropped {
		return 0
	}
	return 1
}

// restorable reports whether the folder of the closed session spec
// describes, gone, is in its worktree, which Irie can add back from its
// branch: the worktree is gone and its repo is there.
func restorable(spec host.Spec) bool {
	return spec.Worktree != "" && spec.Repo != "" && spec.Branch != "" && filepath.IsAbs(spec.Worktree) &&
		inside(spec.Cwd, spec.Worktree) && !exists(spec.Worktree) && exists(spec.Repo)
}

// restoreWorktree adds back the worktree of closed session r, which j
// reopens, when it is gone: at the same path, from its branch, as a new
// worktree is made, running the repo's hooks; then the journal says so.
// The caller holds c.spawnMu and not c.mu.
func (c *Core) restoreWorktree(r closedRecord, j *job) error {
	pl := &j.place
	if !restorable(host.Spec{Cwd: pl.cwd, Repo: pl.repo, Worktree: pl.worktree, Branch: pl.branch}) {
		return nil
	}
	res, err := worktree.Restore(pl.repo, pl.worktree, pl.branch, c.worktreeOptions())
	if err != nil {
		c.log.Warn("worktree restore", "session", r.ID, "path", pl.worktree, "branch", pl.branch, "err", err)
		return ipc.Errorf(ipc.CodeBadArgs, "%s ran in %s, which is gone, and its worktree could not be added back: %v", r.Name, pl.worktree, err)
	}
	pl.copied = res.Copied
	pl.warnings = append(pl.warnings, res.Warnings...)
	commit, _ := worktree.Git(10*time.Second, res.Dir, nil, "rev-parse", "HEAD")
	commit = strings.TrimSpace(commit)
	c.mu.Lock()
	e := event.Event{Session: r.ID, Source: event.SourceIrie, Harness: j.h.ID(), Kind: event.WorktreeRestored, Data: map[string]any{
		"path": pl.worktree, "repo": pl.repo, "branch": pl.branch, "commit": commit,
	}}
	if e, err := c.journal.Append(e); err != nil {
		c.log.Warn("journal", "kind", e.Kind, "err", err)
	} else {
		for s := range c.subs {
			s.event(e)
		}
	}
	delete(c.closes.dropped, filepath.Clean(pl.worktree))
	c.mu.Unlock()
	c.log.Info("worktree restored", "session", r.ID, "name", r.Name, "path", pl.worktree, "branch", pl.branch, "commit", commit)
	if fi, err := os.Stat(pl.cwd); err != nil || !fi.IsDir() {
		return ipc.Errorf(ipc.CodeBadArgs, "%s ran in %s, which its branch %s does not have", r.Name, pl.cwd, pl.branch)
	}
	return nil
}
