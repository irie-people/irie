package core

// The jot dog (DESIGN §4.8) sees each jot through until its work has landed.
// A jot is a task given to Irie: a session with a task that no other session
// delegated. When its agent ends a turn, the dog reads the jot's own
// worktrees with git: the files not committed, and the jot's own commits
// that its repo's land branch (dog.land), such as live, lacks. While work is
// left, it sends the agent back to finish, unless a stop hook sent it on
// already, work of its own in the background will wake it, it asks the user
// something, or the dog has sent it back dog.nudges times since the user's
// last prompt. A jot whose turn ends with work left says so in its row, and
// the dog looks again every dogEvery seconds, so the row clears once the
// work lands, however it lands. A jot you close is done: the dog never sends
// it back, lists it or brings it back, whatever it left. The dog only reads:
// it never commits, merges or removes anything.

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/repo"
	"github.com/irie-people/irie/internal/worktree"
)

// dogEvery is how often, in the core's ticks of a second, the dog looks
// again at the finished jots it found work left in.
const dogEvery = 30

// dogGit bounds each git command of a look at a turn's end, where the agent
// waits for the answer; dogSweepGit those of a look in the background.
const (
	dogGit      = 2 * time.Second
	dogSweepGit = 10 * time.Second
)

// dogStopBudget is how long a look at a turn's end may take for the dog to
// send the agent back. The shim waits stopReplyTimeout, 8 s, for the core's
// answer, and a nudge the agent never hears must not stand in for the end
// of its turn, so a slower look lets the turn end.
const dogStopBudget = 5 * time.Second

// placesFor is how long the worktrees found for a scratch jot are kept
// before the dog looks for them again.
const placesFor = 2 * time.Minute

// dogClosedFor is how far back irie ctl dog looks among the jots that
// closed on their own.
const dogClosedFor = 24 * time.Hour

// dogState is what the dog knows of a jot (session.dog). c.mu guards it.
type dogState struct {
	loose    []looseIn // the jot's worktrees with work left, by its last look
	checked  time.Time // when it last looked; zero when it has not
	places   []jotTree // the jot's own worktrees, as last found
	placesAt time.Time
}

// jotTree is a worktree that holds a jot's own work, and its repo.
type jotTree struct {
	path, repo string
}

// looseIn is what one worktree of a jot holds that has not landed.
type looseIn struct {
	jotTree
	land string
	worktree.Loose
}

// jotLook is what one look at a jot needs, copied from its session so that
// git runs without c.mu.
type jotLook struct {
	id, cwd, repo, worktree string
	created                 time.Time
	copied                  []string
	vendors                 []string  // the conversations its agent ran as
	places                  []jotTree // a scratch jot's worktrees, as last found
	fresh                   bool      // look for a scratch jot's worktrees again
}

// isJot reports whether the dog watches x: a task given to Irie, which no
// other session delegated and no tool runs. The caller holds c.mu.
func (c *Core) isJot(x *session) bool {
	if !c.cfg().Dog.Watch || x.harness == "tool" || x.out != nil && x.out.parent != "" {
		return false
	}
	_, err := os.Stat(c.paths.TaskFile(x.id))
	return err == nil
}

// lookAt is what a look at jot x needs. The caller holds c.mu.
func (c *Core) lookAt(x *session, fresh bool) jotLook {
	l := jotLook{id: x.id, cwd: x.cwd, repo: x.repo, worktree: x.worktree, created: x.created, copied: x.copied, places: x.dog.places}
	for _, v := range []string{x.vendorID, x.track.vendorID} {
		if v != "" && !slices.Contains(l.vendors, v) {
			l.vendors = append(l.vendors, v)
		}
	}
	l.fresh = fresh || time.Since(x.dog.placesAt) > placesFor
	return l
}

// knownRepos are the repos whose worktrees may hold a scratch jot's work:
// the aliases, the repos used lately and those of open sessions. The caller
// holds c.mu.
func (c *Core) knownRepos() []string {
	home, _ := os.UserHomeDir()
	var out []string
	add := func(r string) {
		if r != "" && !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(c.cfg().Repos.Aliases)) {
		add(repo.Expand(c.cfg().Repos.Aliases[name], home))
	}
	for _, r := range c.recent {
		add(r)
	}
	for _, id := range slices.Sorted(maps.Keys(c.sessions)) {
		add(c.sessions[id].repo)
	}
	return out
}

// placesOf finds a jot's own worktrees: the one Irie made for it; or, for a
// jot that runs in a scratch folder of its own, the worktrees of the repos
// Irie knows that its agent made in that folder, or under a folder named
// for its conversation, as Claude Code's scratchpad is. A jot that runs in
// a repo's main checkout has none: that checkout is shared, so what is in
// it may be anyone's.
func (c *Core) placesOf(l jotLook, repos []string, timeout time.Duration) []jotTree {
	switch {
	case l.worktree != "":
		return []jotTree{{path: l.worktree, repo: l.repo}}
	case l.repo != "":
		return nil
	case !l.fresh:
		return l.places
	}
	scratch := inside(l.cwd, c.paths.Scratch())
	var out []jotTree
	for _, r := range repos {
		trees, err := worktree.Trees(r, timeout)
		if err != nil || len(trees) == 0 {
			continue
		}
		// The first is the main checkout, which is shared.
		for _, t := range trees[1:] {
			mine := scratch && inside(t.Path, l.cwd)
			for _, v := range l.vendors {
				mine = mine || strings.Contains(t.Path, string(filepath.Separator)+v+string(filepath.Separator))
			}
			if mine && !slices.ContainsFunc(out, func(p jotTree) bool { return samePath(p.path, t.Path) }) {
				out = append(out, jotTree{path: t.Path, repo: r})
			}
		}
	}
	return out
}

// landOf is the branch a repo's work must reach to be through (dog.land),
// set by the repo's path, its alias or its folder's name; "" when none is.
func (c *Core) landOf(r string) string {
	if r == "" || len(c.cfg().Dog.Land) == 0 {
		return ""
	}
	home, _ := os.UserHomeDir()
	keys := slices.Sorted(maps.Keys(c.cfg().Dog.Land))
	for _, k := range keys {
		if repo.IsPath(k) && samePath(repo.Expand(k, home), r) {
			return c.cfg().Dog.Land[k]
		}
	}
	for _, k := range keys {
		if a, ok := c.cfg().Repos.Aliases[k]; ok && samePath(repo.Expand(a, home), r) {
			return c.cfg().Dog.Land[k]
		}
	}
	return c.cfg().Dog.Land[filepath.Base(r)]
}

// readJot reads what a jot left that has not landed, in each of its own
// worktrees, and the worktrees it looked in. It runs git, so the caller
// does not hold c.mu.
func (c *Core) readJot(l jotLook, repos []string, timeout time.Duration) ([]jotTree, []looseIn) {
	places := c.placesOf(l, repos, timeout)
	var loose []looseIn
	for _, p := range places {
		land := c.landOf(p.repo)
		got, err := worktree.ReadLoose(p.path, land, l.created, l.copied, timeout)
		switch {
		case err != nil && !exists(p.path):
			// The agent removed it once its work was done.
		case err != nil:
			c.log.Warn("dog", "session", l.id, "worktree", p.path, "err", err)
		case got.Left():
			if got.NoLand {
				land = ""
			}
			loose = append(loose, looseIn{jotTree: p, land: land, Loose: got})
		}
	}
	return places, loose
}

// noteLoose keeps what the dog found in jot id, for its row. The caller
// holds c.mu.
func (c *Core) noteLoose(id string, l jotLook, places []jotTree, loose []looseIn) {
	x := c.sessions[id]
	if x == nil {
		return
	}
	x.dog.loose, x.dog.checked = loose, time.Now()
	if l.fresh {
		x.dog.places, x.dog.placesAt = places, x.dog.checked
	}
}

// dogAtStop is the jot dog at the end of a turn (DESIGN §4.8). It reads what
// the jot left and keeps it for the jot's row. When the agent should go on to
// finish, it returns what to tell it, and what was left. The agent waits for
// the answer, so git runs without c.mu and within dogGit a command.
func (c *Core) dogAtStop(id string, evs []event.Event) (string, []looseIn) {
	end := slices.IndexFunc(evs, func(e event.Event) bool { return e.Kind == event.TurnEnded && e.Agent == event.Main })
	if end < 0 {
		return "", nil
	}
	c.mu.Lock()
	x := c.sessions[id]
	if x == nil || !c.isJot(x) {
		c.mu.Unlock()
		return "", nil
	}
	l, repos, nudged, teach := c.lookAt(x, true), c.knownRepos(), x.track.nudged, x.tutor == config.TutorTeach
	c.mu.Unlock()

	start := time.Now()
	places, loose := c.readJot(l, repos, dogGit)
	c.mu.Lock()
	c.noteLoose(id, l, places, loose)
	c.mu.Unlock()

	e := evs[end]
	waiting, _ := e.Int("waiting")
	switch {
	case len(loose) == 0, nudged >= c.cfg().Dog.Nudges, time.Since(start) > dogStopBudget:
		return "", nil
	case e.Data["stop_hook_active"] == true, waiting > 0, asksUser(e.Str("message")):
		// A stop hook sent it on already, so it has had its say; work of
		// its own in the background will wake it; or it waits for you.
		return "", nil
	case teach && turnAsked(e.Str("message")):
		// In teach mode it left a part for you to write, which is the
		// TODO(human) the dog would count (docs/proposals/learning.md §4.4).
		return "", nil
	}
	return unfinished(loose), loose
}

// nudgeEvent stands in for a turn's end when the dog sends the agent back to
// finish: what was left, and what the agent had said.
func nudgeEvent(end event.Event, loose []looseIn) event.Event {
	l := wireLoose(loose, 0)
	if l == nil {
		l = &ipc.Loose{}
	}
	data := map[string]any{"dirty": l.Dirty, "ahead": l.Ahead}
	if l.Land != "" {
		data["land"] = l.Land
	}
	if msg := end.Str("message"); msg != "" {
		data["message"] = msg
	}
	return event.Event{Source: event.SourceIrie, Agent: event.Main, Kind: event.DogNudged, Vendor: end.Vendor, Data: data}
}

// asksUser reports whether a turn's last message ends by asking the user
// something, so its agent waits for an answer.
func asksUser(msg string) bool {
	return strings.HasSuffix(strings.TrimRight(msg, " \t\r\n*_`)\"'"), "?")
}

// unfinished is what the dog tells an agent whose jot is not through.
func unfinished(loose []looseIn) string {
	var b strings.Builder
	b.WriteString("[Irie] The jot dog: this task is not through yet.")
	for _, in := range loose {
		where := in.path
		if in.Branch != "" {
			where += " (branch " + in.Branch + ")"
		}
		if in.Dirty > 0 {
			fmt.Fprintf(&b, "\n- Not committed in %s: %s%s.", where, strings.Join(in.Files, ", "), andMore(in.Dirty, len(in.Files)))
		}
		if in.Ahead > 0 {
			fmt.Fprintf(&b, "\n- Not in %s yet, from %s: %s%s.", in.land, where, strings.Join(in.Commits, "; "), andMore(in.Ahead, len(in.Commits)))
		}
	}
	b.WriteString("\nFinish it now: commit your work, then land it the way this repo's AGENTS.md or CLAUDE.md says. " +
		"If an install or another session holds you up, wait for it and try again. " +
		"If you stopped on purpose, because your task says not to land it or only the user can decide what comes next, say so in one line.")
	return b.String()
}

func andMore(all, named int) string {
	if all > named {
		return fmt.Sprintf(" and %d more", all-named)
	}
	return ""
}

// wireLoose is what a jot's row says is left; nil when nothing is.
func wireLoose(loose []looseIn, nudged int) *ipc.Loose {
	if len(loose) == 0 {
		return nil
	}
	l := &ipc.Loose{Where: []ipc.LoosePlace{}, Nudged: nudged}
	for _, in := range loose {
		l.Dirty += in.Dirty
		l.Ahead += in.Ahead
		if l.Land == "" {
			l.Land = in.land
		}
		l.Where = append(l.Where, ipc.LoosePlace{Path: in.path, Branch: in.Branch, Dirty: in.Dirty, Ahead: in.Ahead})
	}
	return l
}

// dogShows reports whether a row of this state says what its jot left: its
// turn is over, so the work is as it will stay until someone acts.
func dogShows(state string) bool {
	return state == ipc.StateDone || state == ipc.StateIdle || state == ipc.StateExited
}

// dogSweep looks again at the finished jots the dog found work left in, and
// at those it has not looked at since the core started, so a row says what
// is left and clears once the work lands, however it lands. One sweep runs
// at a time, and git runs without c.mu.
func (c *Core) dogSweep() {
	if !c.cfg().Dog.Watch || !c.sweeping.CompareAndSwap(false, true) {
		return
	}
	defer c.sweeping.Store(false)
	c.mu.Lock()
	var looks []jotLook
	for _, id := range slices.Sorted(maps.Keys(c.sessions)) {
		x := c.sessions[id]
		if dogShows(x.track.state(x.dead)) && c.isJot(x) && (x.dog.checked.IsZero() || len(x.dog.loose) > 0) {
			looks = append(looks, c.lookAt(x, false))
		}
	}
	repos := c.knownRepos()
	c.mu.Unlock()
	for _, l := range looks {
		places, loose := c.readJot(l, repos, dogSweepGit)
		c.mu.Lock()
		c.noteLoose(l.id, l, places, loose)
		frames := c.changes()
		c.mu.Unlock()
		c.publish(frames)
	}
}

// dogList is the dog's report (irie ctl dog): the jots that are not through,
// looked at afresh. The open ones come in the list's order, then those that
// closed on their own (spawn.close_when_done) in the last day, the last
// closed first. A jot you closed is done, so it is never on the report.
func (c *Core) dogList() (ipc.DogResult, error) {
	type closedLook struct {
		jotLook
		r closedRecord
	}
	c.mu.Lock()
	var open []jotLook
	for _, id := range slices.Sorted(maps.Keys(c.sessions)) {
		if x := c.sessions[id]; dogShows(x.track.state(x.dead)) && c.isJot(x) {
			open = append(open, c.lookAt(x, true))
		}
	}
	var closed []closedLook
	for _, r := range c.closes.list {
		if r.Auto && c.sessions[r.ID] == nil && time.Since(r.Closed) < dogClosedFor && c.cfg().Dog.Watch {
			if l, ok := c.closedJot(r); ok {
				closed = append(closed, closedLook{l, r})
			}
		}
	}
	repos := c.knownRepos()
	c.mu.Unlock()

	for _, l := range open {
		places, loose := c.readJot(l, repos, dogSweepGit)
		c.mu.Lock()
		c.noteLoose(l.id, l, places, loose)
		c.mu.Unlock()
	}
	out := ipc.DogResult{Jots: []ipc.Jot{}}
	c.mu.Lock()
	for _, s := range c.list() {
		if s.Loose != nil {
			out.Jots = append(out.Jots, ipc.Jot{ID: s.ID, Name: s.Name, State: s.State, Loose: *s.Loose})
		}
	}
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	for _, cl := range closed {
		if _, loose := c.readJot(cl.jotLook, repos, dogSweepGit); len(loose) > 0 {
			at := cl.r.Closed
			out.Jots = append(out.Jots, ipc.Jot{ID: cl.r.ID, Name: cl.r.Name, State: "closed", Closed: &at, Loose: *wireLoose(loose, 0)})
		}
	}
	return out, nil
}

// closedJot is a look at a closed session that was a jot, from its spec.
// The caller holds c.mu.
func (c *Core) closedJot(r closedRecord) (jotLook, bool) {
	spec, err := host.ReadSpec(c.paths.SpecFile(r.ID))
	if err != nil || spec.Harness == "tool" {
		return jotLook{}, false
	}
	if o := c.outcomes[r.ID]; o != nil && o.parent != "" {
		return jotLook{}, false
	}
	if _, err := os.Stat(c.paths.TaskFile(r.ID)); err != nil {
		return jotLook{}, false
	}
	l := jotLook{id: r.ID, cwd: spec.Cwd, repo: spec.Repo, worktree: spec.Worktree, created: r.Created, copied: spec.Copied, fresh: true}
	if r.VendorID != "" {
		l.vendors = []string{r.VendorID}
	}
	return l, true
}

// inside reports whether path is dir or below it.
func inside(path, dir string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// samePath reports whether a and b name one directory, whatever symlinks
// lead there.
func samePath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
