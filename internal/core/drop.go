package core

// Dropping a closed session's worktree once its work has landed (DESIGN
// §4.5; docs/proposals/reversible.md, step 5). A closed session whose
// branch is in its repo's land branch (dog.land), and whose worktree holds
// nothing more, needs only its branch: once its agent has ended, the core
// removes the checkout with git and keeps the branch, and reopen adds the
// worktree back at the same path. Nothing that runs there is disturbed: a
// worktree an open session works in, or one that any process holds, by
// its working directory, its program or a file it has open, stays, and is
// looked at again a little later. The journal records each removal before
// it happens, with the path, branch and commit that put it back by hand,
// and `irie ctl worktrees` sweeps the sessions closed before.

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

// droppedFor is the worktree Irie dropped that a closed session's folder is
// in: its own worktree, or, for a session that worked in another's folder,
// as a delegated subtask does, the dropped worktree its folder is inside.
// The caller holds c.mu.
func (c *Core) droppedFor(cwd, wt string) (string, droppedTree, bool) {
	if wt != "" {
		if d, ok := c.closes.dropped[filepath.Clean(wt)]; ok {
			return filepath.Clean(wt), d, true
		}
	}
	for _, p := range slices.Sorted(maps.Keys(c.closes.dropped)) {
		if cwd != "" && inside(cwd, p) {
			return p, c.closes.dropped[p], true
		}
	}
	return "", droppedTree{}, false
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
	spec     host.Spec
	land     string   // the branch its repo's work lands on (dog.land)
	copy     []string // worktree.copy
	keep     string   // why Irie keeps it before git looks, or ""
	busy     bool     // what keeps it may end
}

// dropLookAt is closed session id's worktree as a drop looks at it; ok is
// false when the session is open or Irie made it no worktree. A worktree
// an open session works in, as a fork made with --here or a delegated
// subtask does, stays, as does one whose agent is still ending or that a
// drop is removing now. The caller holds c.mu.
func (c *Core) dropLookAt(id string) (dropLook, bool) {
	if c.sessions[id] != nil || !c.closes.done[id] {
		return dropLook{}, false
	}
	spec, err := host.ReadSpec(c.paths.SpecFile(id))
	if err != nil || spec.Worktree == "" || spec.Repo == "" || spec.Branch == "" || !filepath.IsAbs(spec.Worktree) || !filepath.IsAbs(spec.Repo) {
		return dropLook{}, false
	}
	spec.Worktree = filepath.Clean(spec.Worktree)
	l := dropLook{id: id, name: id, spec: spec, land: c.landOf(spec.Repo), copy: slices.Clone(c.cfg().Worktree.Copy)}
	if r, ok := c.closedRecord(id); ok {
		l.name = r.Name
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
	if c.closes.dropping[spec.Worktree] != nil {
		l.keep, l.busy = "it is being dropped", true
	}
	return l, true
}

// lookedTree is a closed session's worktree as lookDrop found it, with its
// local files, which a drop takes out first.
type lookedTree struct {
	ipc.ClosedTree
	look  dropLook
	local []string
}

// lookDrop reads whether each of ls can go, with what it takes on the
// disk; one whose worktree is gone already has none. Git reads dropWorkers
// of them at once, and one look over the processes finds what runs in any
// of them. It runs git, so the caller does not hold c.mu.
func (c *Core) lookDrop(ls []dropLook) []lookedTree {
	trees := make([]*lookedTree, len(ls))
	var wg sync.WaitGroup
	next := make(chan int)
	for range min(dropWorkers, len(ls)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				trees[i] = lookOne(ls[i])
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
			if t != nil && t.Keep == "" {
				t.Keep, t.Busy = heldKeep(held[t.Path], err)
			}
		}
	}
	var out []lookedTree
	for _, t := range trees {
		if t != nil {
			out = append(out, *t)
		}
	}
	return out
}

// heldKeep says why what holds a worktree keeps it, or "" when nothing
// does; a look that failed keeps it too, as what runs there is not known.
func heldKeep(hs []proc.Holder, err error) (string, bool) {
	switch {
	case err != nil:
		return "cannot tell what runs in it: " + err.Error(), true
	case len(hs) > 0:
		return heldWords(hs), true
	}
	return "", false
}

// lookOne is what git says of one closed session's worktree, or nil when
// it is gone already.
func lookOne(l dropLook) *lookedTree {
	t := &lookedTree{ClosedTree: ipc.ClosedTree{Session: l.id, Name: l.name, Path: l.spec.Worktree, Branch: l.spec.Branch, Keep: l.keep, Busy: l.busy}, look: l}
	got, err := worktree.CheckDrop(lookOf(l, true), dropGit)
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
	t.Commit, t.local, t.Bytes = got.Commit, got.Local, got.Bytes
	if t.Bytes == 0 {
		t.Bytes = worktree.DiskUsage(l.spec.Worktree)
	}
	return t
}

// lookOf is the worktree package's look at l's worktree; scan walks its
// files too.
func lookOf(l dropLook, scan bool) worktree.Look {
	return worktree.Look{Dir: l.spec.Worktree, Repo: l.spec.Repo, Branch: l.spec.Branch, Land: l.land, Copied: l.spec.Copied, Copy: l.copy, Scan: scan}
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

// dropOne drops closed session id's worktree when it can go. The long look,
// git's and the walk of its files, runs first with no lock but its own, or
// is prior, a sweep's. Then, with a spawn's turn, so that no reopen, spawn
// or fork starts meanwhile, it looks again at what can change in a moment,
// what runs there last, and records the drop in the journal before
// anything goes; a reopen that comes during the removal waits for it. ok is
// false when the session is open, or has no worktree there.
func (c *Core) dropOne(id string, prior *lookedTree) (ipc.ClosedTree, bool) {
	var t lookedTree
	if prior != nil {
		t = *prior
	} else {
		c.mu.Lock()
		l, ok := c.dropLookAt(id)
		c.mu.Unlock()
		if !ok {
			return ipc.ClosedTree{}, false
		}
		trees := c.lookDrop([]dropLook{l})
		if len(trees) == 0 {
			return ipc.ClosedTree{}, false
		}
		t = trees[0]
	}
	if t.Keep != "" {
		return t.ClosedTree, true
	}
	path := t.Path

	c.spawnMu.Lock()
	unlock := c.spawnMu.Unlock
	defer func() { unlock() }()
	c.mu.Lock()
	l, ok := c.dropLookAt(id)
	c.mu.Unlock()
	if !ok {
		return ipc.ClosedTree{}, false
	}
	if l.keep != "" {
		t.Keep, t.Busy = l.keep, l.busy
		return t.ClosedTree, true
	}
	got, err := worktree.CheckDrop(lookOf(l, false), dropGit)
	switch {
	case err != nil:
		t.Keep = "git could not read it: " + err.Error()
	case got.Gone:
		return ipc.ClosedTree{}, false
	case got.Keep != "":
		t.Keep = got.Keep
	case got.Commit != t.Commit:
		t.Keep = "its branch moved while Irie looked"
	}
	if t.Keep != "" {
		return t.ClosedTree, true
	}
	held, err := proc.Holding([]string{path})
	if t.Keep, t.Busy = heldKeep(held[path], err); t.Keep != "" {
		return t.ClosedTree, true
	}

	// The journal first: no worktree goes unrecorded.
	harness := c.harnessOfSpec(l.spec)
	data := map[string]any{"path": path, "repo": l.spec.Repo, "branch": t.Branch, "commit": t.Commit, "land": l.land, "bytes": t.Bytes}
	c.mu.Lock()
	if !c.appendEvent(event.Event{Session: id, Source: event.SourceIrie, Harness: harness, Kind: event.WorktreeDropped, Data: data}) {
		c.mu.Unlock()
		t.Keep = "the journal could not record it"
		return t.ClosedTree, true
	}
	c.noteDropped(path, droppedTree{session: id, repo: l.spec.Repo, branch: t.Branch, commit: t.Commit, at: time.Now()})
	if c.closes.dropping == nil {
		c.closes.dropping = map[string]chan struct{}{}
	}
	done := make(chan struct{})
	c.closes.dropping[path] = done
	c.mu.Unlock()
	// Spawns go on while the files go: the path is taken (dropTaken), and a
	// reopen waits for done.
	unlock()
	unlock = func() {}

	partial, err := worktree.Drop(path, l.spec.Repo, got.Local, dropGit)
	c.mu.Lock()
	delete(c.closes.dropping, path)
	close(done)
	switch {
	case err == nil:
		t.Dropped = true
	case partial:
		// git forgot it but could not delete it all: reopen moves what is
		// left aside and adds it back.
		t.Dropped, t.Keep = true, ""
		c.log.Warn("worktree drop: git did not delete it all", "session", id, "path", path, "err", err)
	default:
		// git refused, and it is all there still.
		delete(c.closes.dropped, path)
		c.appendEvent(event.Event{Session: id, Source: event.SourceIrie, Harness: harness, Kind: event.WorktreeRestored, Data: map[string]any{
			"path": path, "repo": l.spec.Repo, "branch": t.Branch, "commit": t.Commit, "kept": err.Error(),
		}})
		t.Keep = "git did not remove it: " + err.Error()
	}
	c.mu.Unlock()
	if t.Dropped {
		c.log.Info("worktree dropped", "session", id, "name", l.name, "path", path, "branch", t.Branch, "commit", t.Commit, "land", l.land, "mb", mb(t.Bytes))
	} else {
		c.log.Warn("worktree drop", "session", id, "path", path, "err", err)
	}
	return t.ClosedTree, true
}

// appendEvent writes e, about a session that may have no row, to the
// journal and tells the subscribers that watch events; false when the
// journal could not take it. The caller holds c.mu.
func (c *Core) appendEvent(e event.Event) bool {
	e, err := c.journal.Append(e)
	if err != nil {
		c.log.Warn("journal", "kind", e.Kind, "err", err)
		return false
	}
	for s := range c.subs {
		s.event(e)
	}
	return true
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
	t, ok := c.dropOne(id, nil)
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
// that can go, which only the person may ask for. worktree.drop is about
// closes; a sweep is the person's own word.
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

	looked := c.lookDrop(ls)
	res := ipc.WorktreesResult{Trees: []ipc.ClosedTree{}}
	trees := make([]ipc.ClosedTree, len(looked))
	for i := range looked {
		t := looked[i].ClosedTree
		if a.Drop && t.Keep == "" {
			got, ok := c.dropOne(t.Session, &looked[i])
			if !ok {
				got = t
				got.Keep = "it went meanwhile"
			}
			got.Bytes = cmp.Or(got.Bytes, t.Bytes)
			t = got
			if t.Dropped {
				res.Dropped++
			}
		}
		trees[i] = t
	}
	slices.SortStableFunc(trees, func(x, y ipc.ClosedTree) int { return cmp.Compare(goes(x), goes(y)) })
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
// describes, if gone, is in a worktree Irie dropped, which it adds back as
// the session reopens. A worktree removed some other way stays gone. The
// caller holds c.mu.
func (c *Core) restorable(spec host.Spec) bool {
	_, d, ok := c.droppedFor(spec.Cwd, spec.Worktree)
	return ok && exists(cmp.Or(d.repo, spec.Repo))
}

// restoreFolder adds back the worktree Irie dropped that session id's folder
// is in, when the folder is gone or what a drop left: at the same path,
// from its branch, as a new worktree is made, running the repo's hooks;
// the journal says so. It waits first for a drop of it that is under way.
// For the session's own worktree it returns the local files copied in.
// The caller holds c.spawnMu and not c.mu.
func (c *Core) restoreFolder(id, name, harness, cwd, wt string) (copied, warnings []string, ok bool, err error) {
	c.mu.Lock()
	path, d, found := c.droppedFor(cwd, wt)
	done := c.closes.dropping[path]
	c.mu.Unlock()
	if done != nil {
		<-done
		c.mu.Lock()
		path, d, found = c.droppedFor(cwd, wt)
		c.mu.Unlock()
	}
	if !found {
		return nil, nil, false, nil
	}
	// Put back by hand, or never gone after all: it is there as a checkout.
	if r, ok, err := worktree.Find(path); err == nil && ok && samePath(r.Top, path) {
		c.mu.Lock()
		delete(c.closes.dropped, path)
		c.mu.Unlock()
		return nil, nil, false, nil
	}
	res, err := worktree.Restore(d.repo, path, d.branch, d.commit, c.worktreeOptions())
	if err != nil {
		c.log.Warn("worktree restore", "session", id, "path", path, "branch", d.branch, "err", err)
		return nil, nil, false, ipc.Errorf(ipc.CodeBadArgs, "%s ran in %s, whose worktree %s could not be added back: %v", name, cwd, path, err)
	}
	commit, _ := worktree.Git(10*time.Second, res.Dir, nil, "rev-parse", "HEAD")
	commit = strings.TrimSpace(commit)
	c.mu.Lock()
	c.appendEvent(event.Event{Session: id, Source: event.SourceIrie, Harness: cmp.Or(harness, "unknown"), Kind: event.WorktreeRestored, Data: map[string]any{
		"path": path, "repo": d.repo, "branch": d.branch, "commit": commit,
	}})
	delete(c.closes.dropped, path)
	c.mu.Unlock()
	c.log.Info("worktree restored", "session", id, "name", name, "path", path, "branch", d.branch, "commit", commit)
	if fi, err := os.Stat(cwd); err != nil || !fi.IsDir() {
		return nil, nil, true, ipc.Errorf(ipc.CodeBadArgs, "%s ran in %s, which its branch %s does not have", name, cwd, d.branch)
	}
	if wt != "" && samePath(wt, path) {
		copied = res.Copied
	}
	return copied, res.Warnings, true, nil
}

// restoreWorktree adds back the worktree of closed session r, which j
// reopens, when Irie dropped it (restoreFolder). The caller holds
// c.spawnMu and not c.mu.
func (c *Core) restoreWorktree(r closedRecord, j *job) error {
	pl := &j.place
	copied, warnings, ok, err := c.restoreFolder(r.ID, r.Name, j.h.ID(), pl.cwd, pl.worktree)
	if err != nil || !ok {
		return err
	}
	if pl.worktree != "" && copied != nil {
		pl.copied = copied
	}
	pl.warnings = append(pl.warnings, warnings...)
	return nil
}
