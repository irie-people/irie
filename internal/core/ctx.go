package core

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/anon"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/ctxgit"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/harness"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/worktree"
)

// Contexts in git (DESIGN §4.13): the core stores each session's
// conversation in the context store as it goes, a commit per turn, and
// forks a context from any of its turns into a session of its own.

// contexts is the store, opened on first use, and the sessions waiting to
// be synced into it. It has locks of its own, taken after mu when at all.
type contexts struct {
	once    sync.Once
	st      *ctxgit.Store
	err     error
	mu      sync.Mutex
	queue   []string           // session ids, oldest first
	queued  map[string]ctxSnap // what each queued session was when its turn or its life ended
	running bool
}

// ctxSnap is what the store needs of a session, taken when the event that
// asks for a sync is recorded: a session that closes itself as its turn
// ends is gone from the list by the time the sync runs.
type ctxSnap struct {
	s     ctxgit.Session
	code  string // the checkout whose commit its turns record
	ended bool   // its agent has ended or it was closed: forget it after
}

// contextOps are the ops of contexts in git (ipc/context.go).
var contextOps = []string{ipc.OpContext, ipc.OpFork}

// contextOp serves the context ops; ok is false for any other op.
func (c *Core) contextOp(op string, raw json.RawMessage) (result any, ok bool, err error) {
	switch op {
	case ipc.OpContext:
		var a ipc.ContextArgs
		if err := decode(raw, &a); err != nil {
			return nil, true, err
		}
		r, err := c.context(a)
		return r, true, err
	case ipc.OpFork:
		var a ipc.ForkArgs
		if err := decode(raw, &a); err != nil {
			return nil, true, err
		}
		r, err := c.fork(a)
		return r, true, err
	}
	return nil, false, nil
}

// ctxStore is the context store, opened the first time it is needed.
func (c *Core) ctxStore() (*ctxgit.Store, error) {
	c.ctxs.once.Do(func() {
		c.ctxs.st, c.ctxs.err = ctxgit.Open(c.paths.ContextStore())
		if c.ctxs.err != nil {
			c.log.Warn("contexts", "err", c.ctxs.err)
		}
	})
	return c.ctxs.st, c.ctxs.err
}

// ctxOnEvent queues a sync of x when e ends a turn, the agent or the
// session: a commit a turn, made off the hook's path. The caller holds
// c.mu.
func (c *Core) ctxOnEvent(x *session, e event.Event) {
	if x.harness != "claude-code" {
		return
	}
	ended := false
	switch {
	case e.Agent == event.Main && (e.Kind == event.TurnEnded || e.Kind == event.TurnFailed):
	case e.Kind == event.SessionEnded || e.Kind == event.PaneExited || e.Kind == event.UIClosed:
		ended = true
	default:
		return
	}
	snap := c.ctxSnapOf(x)
	snap.ended = snap.ended || ended
	go c.ctxSettled(snap)
}

// After an event, a sync waits for the session's transcript to settle:
// Claude Code writes a turn's last reply, and its bookkeeping, once the
// turn's Stop hooks have run, so a sync at the hook would store the turn
// without its reply, and store it again once the reply came. It waits
// until the newest transcript has gone ctxSettle without growing, and
// ctxSettleMost at most.
const (
	ctxSettle     = time.Second
	ctxSettleMost = 30 * time.Second
)

// ctxSettled queues a sync of snap once its newest transcript has settled.
// A session that has closed meanwhile is synced as ended.
func (c *Core) ctxSettled(snap ctxSnap) {
	if ts := snap.s.Transcripts; len(ts) > 0 {
		settleFile(ts[len(ts)-1], ctxSettle, ctxSettleMost)
	}
	c.mu.Lock()
	if c.sessions[snap.s.ID] == nil {
		snap.ended = true
	}
	c.mu.Unlock()
	c.ctxSoon(snap)
}

// settleFile returns once the file at path has gone quiet without changing
// size, or after most.
func settleFile(path string, quiet, most time.Duration) {
	end := time.Now().Add(most)
	size, since := int64(-1), time.Now()
	for time.Now().Before(end) {
		n := int64(-1)
		if fi, err := os.Stat(path); err == nil {
			n = fi.Size()
		}
		if n != size {
			size, since = n, time.Now()
		} else if time.Since(since) >= quiet {
			return
		}
		time.Sleep(quiet / 4)
	}
}

// ctxSnapOf is what the store needs of x now. The caller holds c.mu.
func (c *Core) ctxSnapOf(x *session) ctxSnap {
	return ctxSnap{
		s: ctxgit.Session{
			ID: x.id, Name: x.name, Conversation: x.vendorID, Model: x.model, Profile: x.profile,
			Working: x.track.live() == ipc.StateWorking, Transcripts: c.transcriptsOf(x),
		},
		code:  codeDir(x),
		ended: x.track.exited != nil || x.track.ended != nil,
	}
}

// ctxSoon queues a sync, once a session however often it is asked before
// the sync runs: the newest snapshot of it is the one synced.
func (c *Core) ctxSoon(snap ctxSnap) {
	c.ctxs.mu.Lock()
	defer c.ctxs.mu.Unlock()
	if c.ctxs.queued == nil {
		c.ctxs.queued = map[string]ctxSnap{}
	}
	if old, ok := c.ctxs.queued[snap.s.ID]; ok {
		snap.ended = snap.ended || old.ended
	} else {
		c.ctxs.queue = append(c.ctxs.queue, snap.s.ID)
	}
	c.ctxs.queued[snap.s.ID] = snap
	if !c.ctxs.running {
		c.ctxs.running = true
		go c.ctxWork()
	}
}

// ctxWork syncs the queued sessions one at a time, until none wait. A
// store that will not open has said why once; the queue is let go.
func (c *Core) ctxWork() {
	if _, err := c.ctxStore(); err != nil {
		c.ctxs.mu.Lock()
		c.ctxs.queue, c.ctxs.queued, c.ctxs.running = nil, nil, false
		c.ctxs.mu.Unlock()
		return
	}
	for {
		c.ctxs.mu.Lock()
		if len(c.ctxs.queue) == 0 {
			c.ctxs.running = false
			c.ctxs.mu.Unlock()
			return
		}
		id := c.ctxs.queue[0]
		c.ctxs.queue = c.ctxs.queue[1:]
		snap := c.ctxs.queued[id]
		delete(c.ctxs.queued, id)
		c.ctxs.mu.Unlock()
		if _, err := c.ctxSyncSnap(snap); err != nil {
			c.log.Warn("context sync", "session", id, "err", err)
		}
	}
}

// ctxSync stores what is new of session id's conversation and returns its
// branch's head. A session that has ended is then forgotten from memory;
// its branch stays.
func (c *Core) ctxSync(id string) (string, error) {
	c.mu.Lock()
	x := c.sessions[id]
	if x == nil {
		c.mu.Unlock()
		return "", ipc.Errorf(ipc.CodeNotFound, "no open session %s", id)
	}
	snap := c.ctxSnapOf(x)
	c.mu.Unlock()
	return c.ctxSyncSnap(snap)
}

// ctxSyncSnap stores what is new of a session as a snapshot of it says,
// open or not, and returns its branch's head.
func (c *Core) ctxSyncSnap(snap ctxSnap) (string, error) {
	st, err := c.ctxStore()
	if err != nil {
		return "", err
	}
	s := snap.s
	s.Final = snap.ended
	s.Code, s.Dirty = codeState(snap.code)
	head, err := st.Sync(s)
	if snap.ended {
		st.Forget(s)
	}
	return head, err
}

// codeDir is the checkout whose commit a session's turns record: its
// worktree, or the folder it runs in when that is in a repo.
func codeDir(x *session) string {
	switch {
	case x.worktree != "":
		return x.worktree
	case x.repo != "":
		return x.cwd
	}
	return ""
}

// codeState is the commit dir's checkout is at, and how many of its files
// are not committed; "" outside git.
func codeState(dir string) (string, int) {
	if dir == "" {
		return "", 0
	}
	head, err := worktree.Git(5*time.Second, dir, nil, "rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		return "", 0
	}
	out, err := worktree.Git(10*time.Second, dir, nil, "status", "--porcelain", "-z", "--untracked-files=normal")
	if err != nil {
		return strings.TrimSpace(head), 0
	}
	n := 0
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 3 {
			continue
		}
		n++
		if f[0] == 'R' || f[0] == 'C' {
			i++ // a rename's or copy's second path
		}
	}
	return strings.TrimSpace(head), n
}

// ctxTarget finds the branch a name stands for: an open session's id or
// name, then a closed session's id, then any branch of the store as it
// is. open says it is an open session's. The caller holds c.mu.
func (c *Core) ctxTarget(name string) (id string, open bool) {
	if x := c.sessionNamed(name); x != nil {
		return x.id, true
	}
	return name, false
}

// sessionNamed is the open session with the id, or else the name. The
// caller holds c.mu.
func (c *Core) sessionNamed(s string) *session {
	if x := c.sessions[s]; x != nil {
		return x
	}
	for _, x := range c.sessions {
		if x.name == s {
			return x
		}
	}
	return nil
}

// context serves the context op.
func (c *Core) context(a ipc.ContextArgs) (ipc.ContextResult, error) {
	st, err := c.ctxStore()
	if err != nil {
		return ipc.ContextResult{}, ipc.Errorf(ipc.CodeInternal, "the context store: %v", err)
	}
	r := ipc.ContextResult{Repo: st.Dir()}
	switch a.Action {
	case ipc.ContextWhere:
		return r, nil
	case ipc.ContextClean:
		r.KeepDays = c.cfg().Context.KeepDays
		gone, err := c.ctxExpire()
		r.Removed = gone
		if err != nil {
			return r, ipc.Errorf(ipc.CodeInternal, "%v", err)
		}
		if len(gone) > 0 {
			// Deleting their objects can take a minute, so it is left to go
			// on after the answer.
			go c.ctxPack()
		}
		return r, nil
	}
	if a.Session == "" {
		return r, ipc.Errorf(ipc.CodeBadArgs, "%s needs a session", a.Action)
	}
	c.mu.Lock()
	id, open := c.ctxTarget(a.Session)
	c.mu.Unlock()
	r.Session, r.Branch = id, ctxgit.Branch(id)
	// What an open session said last is stored first, so every read is
	// of its conversation as it stands.
	if open {
		if r.Commit, err = c.ctxSync(id); err != nil {
			return r, ipc.Errorf(ipc.CodeInternal, "syncing %s: %v", a.Session, err)
		}
	}
	switch a.Action {
	case ipc.ContextSync:
		if !open {
			return r, ipc.Errorf(ipc.CodeNotFound, "no open session %s", a.Session)
		}
		return r, nil
	case ipc.ContextLog:
		var turns []ctxgit.Turn
		if a.At != "" {
			commit, _, err := st.Resolve(id, a.At)
			if err != nil {
				return r, ipc.Errorf(ipc.CodeNotFound, "%v", err)
			}
			turns, err = st.LogRev(commit, a.Limit)
		} else if _, rerr := st.Rev(r.Branch); rerr == nil {
			turns, err = st.Log(id, a.Limit)
		} else if commit, rerr := st.Rev(a.Session); rerr == nil {
			// A tag or a commit, which has no session of its own.
			r.Session, r.Branch = "", ""
			turns, err = st.LogRev(commit, a.Limit)
		} else {
			return r, ipc.Errorf(ipc.CodeNotFound, "%s has no context stored", a.Session)
		}
		if err != nil {
			return r, ipc.Errorf(ipc.CodeInternal, "%v", err)
		}
		for _, t := range turns {
			r.Turns = append(r.Turns, wireTurn(t))
		}
		return r, nil
	case ipc.ContextResolve, ipc.ContextTag:
		commit, cut, err := st.Resolve(id, a.At)
		if err != nil {
			if rev, rerr := st.Rev(a.Session); rerr == nil && a.At == "" {
				commit, cut, err = rev, "", nil
			} else {
				return r, ipc.Errorf(ipc.CodeNotFound, "%v", err)
			}
		}
		r.Commit, r.Cut = commit, cut
		if a.Action == ipc.ContextTag {
			if cut != "" {
				if r.Commit, err = st.Cut(commit, cut); err != nil {
					return r, ipc.Errorf(ipc.CodeInternal, "%v", err)
				}
				r.Cut = ""
			}
			if err := st.Tag(a.Name, r.Commit); err != nil {
				return r, ipc.Errorf(ipc.CodeBadArgs, "%v", err)
			}
		}
		return r, nil
	}
	return r, ipc.Errorf(ipc.CodeBadArgs, "no context action %q: log, sync, resolve, tag, clean or where", a.Action)
}

// How often the core looks after the store: a little after it starts,
// then every hour.
const (
	ctxSweepFirst = 2 * time.Minute
	ctxSweepEvery = time.Hour
)

// ctxSweep lets go of the contexts kept past context.keep_days and packs
// what syncs wrote, over and over for as long as the core runs.
func (c *Core) ctxSweep() {
	time.Sleep(ctxSweepFirst)
	for {
		if _, err := c.ctxExpire(); err != nil {
			c.log.Warn("contexts", "expire", err)
		}
		c.ctxPack()
		time.Sleep(ctxSweepEvery)
	}
}

// ctxPack packs the store's loose objects, and deletes from the disk what
// the contexts let go of held (ctxgit.Store.Pack). With no store yet there
// is nothing to pack.
func (c *Core) ctxPack() {
	if _, err := os.Stat(c.paths.ContextStore()); err != nil {
		return
	}
	st, err := c.ctxStore()
	if err == nil {
		err = st.Pack()
	}
	if err != nil {
		c.log.Warn("contexts", "pack", err)
	}
}

// ctxExpire lets go of the contexts kept past context.keep_days: a closed
// session's once its newest turn is that old (DESIGN §4.13). With 0 every
// context is kept, and with no store yet there is nothing to do.
func (c *Core) ctxExpire() ([]string, error) {
	days := c.cfg().Context.KeepDays
	if days <= 0 {
		return nil, nil
	}
	if _, err := os.Stat(c.paths.ContextStore()); err != nil {
		return nil, nil
	}
	st, err := c.ctxStore()
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	open := make(map[string]bool, len(c.sessions))
	for id := range c.sessions {
		open[id] = true
	}
	c.mu.Unlock()
	gone, err := st.Expire(time.Duration(days)*24*time.Hour, func(id string) bool { return open[id] }, time.Now())
	if len(gone) > 0 {
		c.log.Info("contexts let go", "sessions", len(gone), "keep_days", days)
	}
	return gone, err
}

func wireTurn(t ctxgit.Turn) ipc.ContextTurn {
	return ipc.ContextTurn{
		Commit: t.Commit, Time: t.Time, Turn: t.Turn, Prompt: t.Prompt, Said: t.Said, Session: t.Session, Name: t.Name,
		Conversation: t.Conversation, Leaf: t.Leaf, Model: t.Model, Code: t.Code, Dirty: t.Dirty, Partial: t.Partial, Cut: t.Cut,
	}
}

// fork starts a session that carries an open session's context on from a
// point of it (DESIGN §4.13): the conversation up to that turn, written as
// a Claude Code transcript of its own that a resume carries on, in a new
// worktree of the session's repo at the code that turn recorded.
func (c *Core) fork(a ipc.ForkArgs) (ipc.ForkResult, error) {
	st, err := c.ctxStore()
	if err != nil {
		return ipc.ForkResult{}, ipc.Errorf(ipc.CodeInternal, "the context store: %v", err)
	}
	c.mu.Lock()
	x := c.sessionNamed(a.Session)
	if x == nil {
		c.mu.Unlock()
		return ipc.ForkResult{}, ipc.Errorf(ipc.CodeNotFound, "no open session %s", a.Session)
	}
	src := *x
	c.mu.Unlock()
	if src.harness != "claude-code" {
		return ipc.ForkResult{}, ipc.Errorf(ipc.CodeBadArgs, "%s runs %s: only a Claude Code conversation can be forked", src.name, src.harness)
	}
	head, err := c.ctxSync(src.id)
	if err != nil {
		return ipc.ForkResult{}, ipc.Errorf(ipc.CodeInternal, "syncing %s: %v", src.name, err)
	}
	if head == "" {
		return ipc.ForkResult{}, ipc.Errorf(ipc.CodeBusy, "%s has no turn to fork yet", src.name)
	}
	commit, cut, err := st.Resolve(src.id, a.At)
	if err != nil {
		return ipc.ForkResult{}, ipc.Errorf(ipc.CodeNotFound, "%v", err)
	}
	tip := commit == head && cut == ""
	if cut != "" {
		if commit, err = st.Cut(commit, cut); err != nil {
			return ipc.ForkResult{}, ipc.Errorf(ipc.CodeInternal, "%v", err)
		}
	}
	turns, err := st.LogRev(commit, 0)
	if err != nil || len(turns) == 0 {
		return ipc.ForkResult{}, ipc.Errorf(ipc.CodeInternal, "reading %s: %v", commit, err)
	}
	at := turns[0]
	lines, err := st.Transcript(commit, "")
	if err != nil {
		return ipc.ForkResult{}, ipc.Errorf(ipc.CodeInternal, "%v", err)
	}

	// The fork runs as its source did, unless asked otherwise.
	profileName := a.Profile
	if profileName == "" {
		profileName = src.profile
	}
	profileName, profile, err := c.resumeProfile(profileName)
	if err != nil {
		return ipc.ForkResult{}, err
	}
	switch {
	case a.Model != "" && profile.Local != nil:
		return ipc.ForkResult{}, ipc.Errorf(ipc.CodeBadArgs, "+%s takes no model for one fork", profileName)
	case a.Model != "":
		profile.Model = a.Model
	case a.Profile == "" && src.model != "" && profile.Local == nil:
		profile.Model = src.model
	}
	h, err := harnessFor(profile.Harness)
	if err != nil {
		return ipc.ForkResult{}, ipc.Errorf(ipc.CodeBadArgs, "profile %s: %v", profileName, err)
	}
	var warnings []string
	if profile.Local != nil {
		if warnings, err = c.prepareLocal(profileName, &profile, false); err != nil {
			return ipc.ForkResult{}, err
		}
	}
	var probe harness.Probe
	if p, ok := h.(harness.Prober); ok {
		probe = p.Probe(profile, c.agentEnv(profile))
	}
	probe.Warnings = append(warnings, probe.Warnings...)
	// A fork of an anonymous session stays in its compartment (DESIGN
	// §4.16).
	var comp *anon.Compartment
	if src.anon != "" {
		got, notes, err := c.prepareAnon(src.anon, "")
		if err != nil {
			return ipc.ForkResult{}, err
		}
		comp, probe.Warnings = &got, append(probe.Warnings, notes...)
	}

	name := cleanText(a.Name, maxName)
	if name == "" {
		name = src.name + " · fork"
	}
	c.spawnMu.LockAt(c.levelOf(src.id, ""))
	defer c.spawnMu.Unlock()
	pl, err := c.forkPlace(&src, turns, name, a.Here, tip)
	if err != nil {
		return ipc.ForkResult{}, err
	}
	// The fork's own conversation, where Claude Code looks for it.
	vendor := harness.NewUUID()
	from, to := forkRoot(&src), pl.worktree
	if to == "" {
		to = pl.cwd
	}
	file := filepath.Join(claudeProjects(profile, pl.cwd), vendor+".jsonl")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return ipc.ForkResult{}, err
	}
	if err := os.WriteFile(file, ctxgit.ForkLines(lines, vendor, from, to), 0o600); err != nil {
		return ipc.ForkResult{}, err
	}

	task := strings.TrimSpace(a.Task)
	if task != "" {
		task = forkNote(&src, at, pl) + "\n\n" + task
	}
	c.mu.Lock()
	pl.name, pl.named = unique(name, c.names(nil)), true
	nx, err := c.launch(job{
		place: pl, profile: profileName, run: profile, h: h, probe: probe, next: task, resume: vendor, anon: comp,
		data: map[string]any{"resume": vendor, "kind": "fork", "fork_of": src.id, "fork_at": commit, "fork_turn": at.Turn},
	})
	if err == nil {
		if berr := st.StartBranch(nx.id, commit); berr != nil {
			c.log.Warn("fork branch", "session", nx.id, "err", berr)
		}
		if pl.repo != "" {
			c.addRecent(pl.repo)
		}
	}
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	if err != nil {
		os.Remove(file)
		c.log.Warn("fork failed", "session", src.id, "err", err)
		return ipc.ForkResult{}, err
	}
	c.log.Info("forked", "session", nx.id, "from", src.id, "commit", commit, "turn", at.Turn, "cwd", nx.cwd)
	return ipc.ForkResult{Session: nx.wire(), From: commit, Turn: at.Turn}, nil
}

// forkRoot is the folder a session's conversation names its files under:
// its worktree, else its repo's checkout when it runs in it, else its
// folder.
func forkRoot(x *session) string {
	switch {
	case x.worktree != "":
		return x.worktree
	case x.repo != "" && inside(x.cwd, x.repo):
		return x.repo
	}
	return x.cwd
}

// forkPlace is where a fork works: a new worktree of its source's repo at
// the code of the turn it forks from (the newest turn back that recorded
// any), carrying the source's uncommitted work when the fork is from its
// newest turn. A source outside git, or here, shares its source's folder.
func (c *Core) forkPlace(src *session, turns []ctxgit.Turn, name string, here, tip bool) (place, error) {
	if here || src.repo == "" {
		p := place{cwd: src.cwd, repo: src.repo, worktree: src.worktree, branch: src.branch}
		p.warnings = append(p.warnings, "works in "+src.name+"'s own folder, sharing its files")
		return p, nil
	}
	base := ""
	for _, t := range turns {
		if t.Code != "" {
			base = t.Code
			break
		}
	}
	root := forkRoot(src)
	if base == "" {
		base, _ = codeState(root)
	}
	if base == "" {
		return place{}, ipc.Errorf(ipc.CodeBadArgs, "%s's code has no commit to fork from", src.name)
	}
	if _, err := worktree.Git(10*time.Second, src.repo, nil, "cat-file", "-e", base+"^{commit}"); err != nil {
		return place{}, ipc.Errorf(ipc.CodeNotFound, "%s's code at that turn, %s, is gone from %s", src.name, shortID(base), src.repo)
	}
	opts := c.worktreeOptions()
	opts.Base = base
	res, err := worktree.Create(worktree.Request{Repo: src.repo, Name: filepath.Base(src.repo), Slug: slug(name)}, opts)
	if err != nil {
		return place{}, err
	}
	cwd := res.Dir
	if rel, err := filepath.Rel(root, src.cwd); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		if fi, err := os.Stat(filepath.Join(res.Dir, rel)); err == nil && fi.IsDir() {
			cwd = filepath.Join(res.Dir, rel)
		}
	}
	p := place{cwd: cwd, repo: src.repo, worktree: res.Dir, branch: res.Branch, copied: res.Copied, warnings: res.Warnings}
	if tip {
		p.warnings = append(p.warnings, carryWork(root, res.Dir)...)
	} else if t := turns[0]; t.Dirty > 0 {
		p.warnings = append(p.warnings, fmt.Sprintf("%d files %s had not committed at turn %d are not in the fork", t.Dirty, src.name, t.Turn))
	}
	return p, nil
}

// carryMax is the largest untracked file a fork carries over.
const carryMax = 10 << 20

// carryWork copies the work not committed in src to dst, a new worktree at
// src's commit: the changes to tracked files as a patch, and the untracked
// files that are not ignored. What it could not carry it says.
func carryWork(src, dst string) []string {
	var warnings []string
	diff, err := worktree.Git(time.Minute, src, nil, "diff", "--binary", "HEAD")
	if err == nil && strings.TrimSpace(diff) != "" {
		if _, err := worktree.Git(time.Minute, dst, []byte(diff), "apply", "--binary", "--whitespace=nowarn", "-"); err != nil {
			warnings = append(warnings, "uncommitted changes not carried over: "+err.Error())
		}
	}
	out, err := worktree.Git(time.Minute, src, nil, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return append(warnings, "untracked files not carried over: "+err.Error())
	}
	skipped := 0
	for _, rel := range strings.Split(out, "\x00") {
		if rel == "" {
			continue
		}
		if !copyRegular(filepath.Join(src, rel), filepath.Join(dst, rel)) {
			skipped++
		}
	}
	if skipped > 0 {
		warnings = append(warnings, strconv.Itoa(skipped)+" untracked files not carried over (too big, not regular, or already there)")
	}
	return warnings
}

// copyRegular copies a regular file of at most carryMax bytes to a path
// that does not exist yet.
func copyRegular(from, to string) bool {
	fi, err := os.Lstat(from)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > carryMax {
		return false
	}
	if _, err := os.Lstat(to); err == nil {
		return false
	}
	in, err := os.Open(from)
	if err != nil {
		return false
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return false
	}
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fi.Mode().Perm())
	if err != nil {
		return false
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err == nil
}

// claudeProjects is the folder where Claude Code keeps the transcripts of a
// session of profile p that runs in cwd (§4.7). A folder's name past 200
// characters gets a hash Irie cannot know; Claude Code finds a session by
// its id in any project's folder, so the name cut at 200 serves.
func claudeProjects(p config.Profile, cwd string) string {
	dir := p.Env["CLAUDE_CONFIG_DIR"]
	if dir == "" {
		dir = os.Getenv("CLAUDE_CONFIG_DIR")
	}
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".claude")
	}
	name := notAlphanumeric.ReplaceAllString(cwd, "-")
	if len(name) > 200 {
		name = name[:200]
	}
	return filepath.Join(dir, "projects", name)
}

// forkNote is what a fork's agent is told before its task: where it came
// from, and where it works now.
func forkNote(src *session, at ctxgit.Turn, pl place) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[Irie] This conversation is a fork of %q, from its turn %d.", src.name, at.Turn)
	if pl.worktree != "" && pl.worktree != src.worktree {
		fmt.Fprintf(&b, " You now work in %s, a worktree of your own on branch %s", pl.cwd, pl.branch)
		if at.Code != "" {
			fmt.Fprintf(&b, ", started at commit %s", shortID(at.Code))
		}
		b.WriteString("; the paths in the conversation above were moved there. Edit only files under it.")
	} else {
		fmt.Fprintf(&b, " You work in the same folder as %q, so its files are shared with it.", src.name)
	}
	return b.String()
}

// shortID is a commit id cut to 12 characters.
func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
