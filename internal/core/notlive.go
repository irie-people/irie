package core

// Work that is not live (DESIGN §4.5). A card's work has not landed while
// the branch of the worktree Irie made for it has commits whose work its
// repo's land branch lacks: dog.land, else live. Merges don't count, nor a
// commit the land branch has in another form, as a cherry-pick: git cherry
// compares them by patch, and a cherry-pick that had to be adapted keeps
// its author, author time and subject (worktree.ReadCommits). A card in a
// plain folder, with no worktree of its own, has none.
//
// Closing such a card keeps all of it, as every close keeps a worktree
// whose work has not landed, but nothing would bring it to mind again: the
// jot dog lets a closed card be. So a client asks, with unlanded, before it
// closes one, and says so first; a close itself never waits on the answer,
// since scripts close cards too, but reads it, says it in its reply and
// keeps it in its ui.closed, so that irie ctl dog lists the card for
// notLiveFor after (dog.go). A close in a hurry still leaves a trail.

import (
	"slices"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/worktree"
)

// notLiveFor is how long irie ctl dog lists a card closed with work not
// live: a week. The install lost on 15 Nov 2026 went unnoticed for six
// days, and a week covers that, a weekend away and a week's plans; a
// longer list would bury the fresh ones under what you chose to leave.
// Past it, git cherry live BRANCH still finds the work.
const notLiveFor = 7 * 24 * time.Hour

// notLiveGit bounds each git command of the look before a close: a client
// waits on it before the card goes.
const notLiveGit = 3 * time.Second

// notLiveAtOnce is how many cards' work one look reads at a time.
const notLiveAtOnce = 4

// cardLook is what a look at a card's work needs, copied from its session,
// or from its spec once it has closed, so that git runs without c.mu.
type cardLook struct {
	id, name string
	tree     jotTree
	created  time.Time // its own commits are those made since
	closed   time.Time // when it closed; zero while it is open
}

// cardLookOf is a look at x's work; false when x has no worktree of its own.
// The caller holds c.mu.
func (c *Core) cardLookOf(x *session) (cardLook, bool) {
	if x == nil || x.worktree == "" || x.repo == "" {
		return cardLook{}, false
	}
	return cardLook{id: x.id, name: x.name, tree: jotTree{path: x.worktree, repo: x.repo, branch: x.branch}, created: x.created}, true
}

// cardLooks are the looks at the work of the sessions ids names, in their
// order, those with no worktree of their own left out. The caller holds
// c.mu.
func (c *Core) cardLooks(ids []string) []cardLook {
	var looks []cardLook
	for _, id := range ids {
		if l, ok := c.cardLookOf(c.sessions[id]); ok {
			looks = append(looks, l)
		}
	}
	return looks
}

// liveOf is the branch a repo's work must reach to be live: dog.land,
// else live.
func (c *Core) liveOf(repo string) string {
	if land := c.landOf(repo); land != "" {
		return land
	}
	return "live"
}

// readNotLive reads what of a card's work has not landed; false when all
// of it has, or git cannot say, as when its repo has no land branch. A
// worktree that has gone, as Irie drops one once its branch has landed, is
// read from its branch. It runs git, so the caller does not hold c.mu.
func (c *Core) readNotLive(l cardLook, timeout time.Duration) (ipc.NotLive, bool) {
	land := c.liveOf(l.tree.repo)
	got, err := worktree.ReadCommits(l.tree.path, land, l.created, timeout)
	if err != nil && !exists(l.tree.path) && l.tree.branch != "" {
		got, err = worktree.ReadBranch(l.tree.repo, l.tree.branch, land, l.created, timeout)
	}
	if err != nil {
		if exists(l.tree.path) {
			c.log.Warn("not live", "session", l.id, "worktree", l.tree.path, "err", err)
		}
		return ipc.NotLive{}, false
	}
	if got.NoLand || got.Ahead == 0 {
		return ipc.NotLive{}, false
	}
	branch := got.Branch
	if branch == "" {
		branch = l.tree.branch
	}
	return ipc.NotLive{Session: l.id, Name: l.name, Path: l.tree.path, Branch: branch, Land: land, Ahead: got.Ahead, Commits: got.Commits}, true
}

// notLive reads what of the looks' work has not landed, notLiveAtOnce
// cards at a time, by session. It runs git, so the caller does not hold
// c.mu.
func (c *Core) notLive(looks []cardLook, timeout time.Duration) map[string]ipc.NotLive {
	out := map[string]ipc.NotLive{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	turns := make(chan struct{}, notLiveAtOnce)
	for _, l := range looks {
		wg.Add(1)
		turns <- struct{}{}
		go func() {
			defer func() { <-turns; wg.Done() }()
			if n, ok := c.readNotLive(l, timeout); ok {
				mu.Lock()
				out[l.id] = n
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return out
}

// inOrder is what of byID's work has not landed, in the order of ids.
func inOrder(ids []string, byID map[string]ipc.NotLive) []ipc.NotLive {
	out := []ipc.NotLive{}
	for _, id := range ids {
		if n, ok := byID[id]; ok {
			out = append(out, n)
		}
	}
	return out
}

// unlanded is the op unlanded: what of the work of the sessions a close of
// a would close has not landed. It closes nothing.
func (c *Core) unlanded(a ipc.CloseArgs) ipc.UnlandedResult {
	c.mu.Lock()
	ids := c.closeTargets(a)
	looks := c.cardLooks(ids)
	c.mu.Unlock()
	if ids == nil {
		ids = []string{}
	}
	return ipc.UnlandedResult{Sessions: ids, NotLive: inOrder(ids, c.notLive(looks, notLiveGit))}
}

// closedNotLive are the cards closed at your word in the last notLiveFor
// whose work had not landed when they closed, the last closed first: the
// dog reads them again for its report. Their worktrees come from their
// specs. The caller holds c.mu.
func (c *Core) closedNotLive(now time.Time) []cardLook {
	var looks []cardLook
	for id, p := range c.closes.past {
		if p.notLive == 0 || now.Sub(p.closed) >= notLiveFor || c.sessions[id] != nil {
			continue
		}
		spec, err := host.ReadSpec(c.paths.SpecFile(id))
		if err != nil || spec.Worktree == "" || spec.Repo == "" {
			continue
		}
		looks = append(looks, cardLook{id: id, name: p.name, created: p.created, closed: p.closed,
			tree: jotTree{path: spec.Worktree, repo: spec.Repo, branch: spec.Branch, dropped: c.wasDropped(spec.Worktree)}})
	}
	slices.SortFunc(looks, func(a, b cardLook) int { return b.closed.Compare(a.closed) })
	return looks
}
