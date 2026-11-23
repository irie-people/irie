package core

// Closing and reopening sessions (DESIGN §4.5, M4). Close swaps the session's
// pane out of the slot and marks it closing, so its row goes at once, then
// ends the agent gently: SIGCONT to all it runs, in case it is frozen, then
// SIGTERM through its host, and SIGKILL after closeWait. A conversation its
// agent handed to Claude Code's background ends too. The session's files
// stay, and so does its worktree, unless its work has landed: then the
// checkout goes once the agent has ended, and its branch stays (drop.go). A
// session you close is done: nothing brings it back but you, and the jot
// dog lets it be. Reopen, close's undo, brings the session back in the same
// row, its worktree added back if it went: it carries the conversation on,
// or starts the task again when the agent never got that far. Which
// sessions are closed, and which closed on their own, is derived from the
// journal's ui.closed and ui.reopened events, so it survives a restart. A
// session lost with Irie's tmux server is recorded as closed on its own
// when the core next starts (lost.go).

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/harness"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/journal"
	"github.com/irie-people/irie/internal/rebuild"
	"github.com/irie-people/irie/internal/tmux"
)

// closeWait is how long a closed session's agent has to exit after SIGTERM
// before it is killed. Claude Code gives its SessionEnd hooks 1.5 s.
const closeWait = 5 * time.Second

// maxClosed is how many closed sessions reopen can reach.
const maxClosed = 100

// closes is what the core knows of closed sessions. c.mu guards it.
type closes struct {
	list   []closedRecord     // newest first
	done   map[string]bool    // every session closed now, past maxClosed too, by id
	ending map[string]*ending // closing panes whose agents are still ending, by pane id
	// past are every session closed now, past maxClosed too, by id: its
	// name, when it closed and whether it was lost, which find shows
	// (find.go).
	past map[string]pastClose
	// dropped are the worktrees Irie dropped that no reopen has added back,
	// and dropping those whose files go now, which a reopen waits for, by
	// path (drop.go).
	dropped  map[string]droppedTree
	dropping map[string]chan struct{}
	// cold is when the next closed session's prompt cache runs out, after
	// which the closed list says what reopening it costs (rebuilds.go).
	cold time.Time
}

// closedRecord is a closed session, from its ui.closed event: what reopen
// needs that its spec does not hold.
type closedRecord struct {
	ID       string
	Name     string
	Named    bool // the user chose the name (title.go)
	Old      bool // closed before Irie recorded named: reopen settles it (oldName)
	Created  time.Time
	Closed   time.Time
	VendorID string // the conversation reopen carries on
	Resume   bool   // the agent got as far as a conversation; else its task starts again
	// Auto: it closed on its own, by spawn.close_when_done, as a ui.closed
	// from irie says. Else you closed it, or an agent did at your word, and
	// it is done.
	Auto bool
	// Lost: its agent ended with Irie's tmux server, as when the Mac
	// restarts, and the core found it gone as it started (lost.go). Reopen
	// brings it back pinned, parked or frozen, as it was.
	Lost                   bool
	Pinned, Parked, Frozen bool
	// Working: a lost session that was at work when it was lost, or
	// boosted. A restore starts its agent again, and brings back the rest
	// resting (§4.5, rest.go).
	Working bool
	// Space and Group are the space and the group it was closed in, which
	// reopen brings it back to (comeHome).
	Space, Group string
	// Wake is what its next turn costs in its prompt cache, once reopened:
	// its context when it closed, and when its cache runs out (DESIGN
	// §6.3). Zero when Irie did not know.
	Wake rebuild.Wake
	// NotLive is how many commits of its work had not landed when you
	// closed it, which irie ctl dog lists for notLiveFor (notlive.go).
	NotLive int
}

// pastClose is what find shows of a closed session, and what the dog
// needs of one closed with work not live (notlive.go).
type pastClose struct {
	name    string
	closed  time.Time
	lost    bool
	created time.Time
	notLive int
}

// ending is a closing pane's agent on its way out.
type ending struct {
	session string
	done    chan struct{} // closed once the pane is gone
}

// loadClosed rebuilds the closed sessions from the journal: those whose last
// ui.closed came after their last ui.reopened. Rows tmux still has are open.
func (c *Core) loadClosed() {
	evs, err := journal.ReadKinds(c.paths.Journal(), time.Time{}, event.UIClosed, event.UIReopened)
	if err != nil {
		c.log.Warn("journal", "err", err)
	}
	for _, e := range evs {
		switch e.Kind {
		case event.UIClosed:
			c.addClosed(closedFrom(e))
		case event.UIReopened:
			c.dropClosed(e.Session)
		}
	}
}

func closedFrom(e event.Event) closedRecord {
	r := closedRecord{ID: e.Session, Name: e.Str("name"), Closed: e.Time(), VendorID: e.Str("vendor_id"), Auto: e.Source == event.SourceIrie}
	r.Created, _ = time.Parse(time.RFC3339Nano, e.Str("created"))
	r.Resume, _ = e.Data["resume"].(bool)
	r.Lost, r.Pinned, r.Parked, r.Frozen = e.Data["lost"] == true, e.Data["pinned"] == true, e.Data["parked"] == true, e.Data["frozen"] == true
	r.Working = e.Data["working"] == true
	r.Space, r.Group = e.Str("space"), e.Str("group")
	r.NotLive, _ = e.Int("not_live")
	named, ok := e.Data["named"].(bool)
	r.Named, r.Old = named, !ok
	r.Wake = wakeFrom(e)
	return r
}

// wakeData adds w, a closing session's context, to its ui.closed's data.
func wakeData(data map[string]any, w rebuild.Wake) {
	if w.Last.IsZero() || w.Tokens == 0 {
		return
	}
	data["context"], data["cache_last"], data["cache_ttl"], data["model"] = w.Tokens, w.Last.UTC().Format(time.RFC3339Nano), ttlWord(w.TTL), w.Model
	if w.Priced {
		data["context_usd"] = roundCents(w.USD)
	}
}

// wakeFrom is the context a ui.closed says its session had.
func wakeFrom(e event.Event) rebuild.Wake {
	var w rebuild.Wake
	last, err := time.Parse(time.RFC3339Nano, e.Str("cache_last"))
	if err != nil {
		return w
	}
	w.Last, w.TTL, w.Model = last, rebuild.DefaultTTL, e.Str("model")
	if e.Str("cache_ttl") == "1h" {
		w.TTL = time.Hour
	}
	w.Cold = last.Add(w.TTL)
	if n, ok := e.Data["context"].(float64); ok {
		w.Tokens = int64(n)
	} else if n, ok := e.Int("context"); ok {
		w.Tokens = int64(n)
	}
	switch v := e.Data["context_usd"].(type) {
	case float64:
		w.USD, w.Priced = v, true
	case int:
		w.USD, w.Priced = float64(v), true
	}
	return w
}

// addClosed puts r first among the closed sessions. The caller holds c.mu.
func (c *Core) addClosed(r closedRecord) {
	c.dropClosed(r.ID)
	if c.closes.done == nil {
		c.closes.done = map[string]bool{}
	}
	c.closes.done[r.ID] = true
	if c.closes.past == nil {
		c.closes.past = map[string]pastClose{}
	}
	c.closes.past[r.ID] = pastClose{name: r.Name, closed: r.Closed, lost: r.Lost, created: r.Created, notLive: r.NotLive}
	c.closes.list = slices.Insert(c.closes.list, 0, r)
	if len(c.closes.list) > maxClosed {
		c.closes.list = c.closes.list[:maxClosed]
	}
}

// dropClosed forgets that a session is closed. The caller holds c.mu.
func (c *Core) dropClosed(id string) {
	delete(c.closes.done, id)
	delete(c.closes.past, id)
	c.closes.list = slices.DeleteFunc(c.closes.list, func(r closedRecord) bool { return r.ID == id })
}

// closedList is what clients hear of the closed sessions, the last closed
// first. A session that runs again is not closed. The caller holds c.mu.
func (c *Core) closedList() []ipc.ClosedSession {
	out := []ipc.ClosedSession{}
	now := time.Now()
	c.closes.cold = time.Time{}
	for _, r := range c.closes.list {
		if c.sessions[r.ID] == nil {
			cs := ipc.ClosedSession{ID: r.ID, Name: r.Name, Closed: r.Closed, Auto: r.Auto, Lost: r.Lost, Working: r.Working}
			if w := r.Wake; c.cfg().Usage.Rebuilds && w.Priced && w.Tokens >= rebuild.MinTokens {
				if w.IsCold(now) {
					cs.Cold = coldOf(w)
				} else if c.closes.cold.IsZero() || w.Cold.Before(c.closes.cold) {
					c.closes.cold = w.Cold
				}
			}
			out = append(out, cs)
		}
	}
	return out
}

// closedGoesCold tells the clients that see the closed sessions once one
// of their prompt caches runs out, so that the closed list says what
// reopening it costs.
func (c *Core) closedGoesCold(now time.Time) {
	c.mu.Lock()
	due := !c.closes.cold.IsZero() && !now.Before(c.closes.cold)
	if due {
		c.closes.cold = time.Time{}
	}
	c.mu.Unlock()
	if due {
		c.publishClosed()
	}
}

// publishClosed tells every subscriber that sees the whole list that the
// closed sessions changed.
func (c *Core) publishClosed() {
	c.mu.Lock()
	subs := slices.Collect(maps.Keys(c.subs))
	c.mu.Unlock()
	for _, s := range subs {
		s.noteClosed()
	}
}

// close closes a session at your word (DESIGN §4.5), which makes it done:
// its row goes at once, its pane leaves the slot before anything ends it,
// and its agent ends in the background. What of its work had not landed
// it says, and keeps in its ui.closed (notlive.go); it never waits for you
// on it.
func (c *Core) close(a ipc.CloseArgs) ([]ipc.NotLive, error) {
	c.mu.Lock()
	looks := c.cardLooks([]string{a.Session})
	c.mu.Unlock()
	notLive := c.notLive(looks, notLiveGit)
	c.mu.Lock()
	err := c.closeWith(a.Session, false, notLive)
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	if err != nil {
		c.log.Warn("close", "session", a.Session, "err", err)
		return nil, err
	}
	c.publishClosed()
	return inOrder([]string{a.Session}, notLive), nil
}

// closeMany closes several sessions at your word, as close closes one: the
// sessions a names, and with a.Finished every session whose work is
// through (doneWith). It returns those it closed, in the list's order, and
// what of their work had not landed; it passes over one closed or gone
// meanwhile, and the first other error stops it, with what closed so far.
func (c *Core) closeMany(a ipc.CloseArgs) ([]string, []ipc.NotLive, error) {
	c.mu.Lock()
	looks := c.cardLooks(c.closeTargets(a))
	c.mu.Unlock()
	notLive := c.notLive(looks, notLiveGit)
	c.mu.Lock()
	var closed []string
	var err error
	for _, id := range c.closeTargets(a) {
		if c.sessions[id] == nil {
			continue
		}
		if err = c.closeWith(id, false, notLive); err != nil {
			c.log.Warn("close", "session", id, "err", err)
			break
		}
		closed = append(closed, id)
	}
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	if len(closed) > 0 {
		c.publishClosed()
	}
	return closed, inOrder(closed, notLive), err
}

// closeTargets are the sessions a close of a closes, in the list's order:
// those a names, and with a.Finished every session whose work is through
// (doneWith), of one space when a.Space names it. The caller holds c.mu.
func (c *Core) closeTargets(a ipc.CloseArgs) []string {
	want := map[string]bool{}
	for _, id := range append([]string{a.Session}, a.Sessions...) {
		if id != "" {
			want[id] = true
		}
	}
	var ids []string
	for _, id := range c.fullOrder() {
		x := c.sessions[id]
		// Finished takes one space's, when a.Space names it (spaces.go).
		inSpace := a.Space == nil || c.spaceOf(x) == c.spaceKey(*a.Space)
		if x != nil && (want[id] || a.Finished && inSpace && c.doneWith(x)) {
			ids = append(ids, id)
		}
	}
	return ids
}

// doneWith reports whether Close All Finished closes x: its turn is over,
// done, idle or exited, and nothing of its work goes on (finished), no
// helper or teammate at work; and it is neither pinned nor parked, which
// you keep on purpose. Another machine's sessions are never in c.sessions.
// The caller holds c.mu.
func (c *Core) doneWith(x *session) bool {
	if x.track.pinned || x.track.parked || x.track.frozen != nil || !finished(x) {
		return false
	}
	if _, waits := x.subsAtWork(); waits {
		return false
	}
	switch x.wire().State {
	case ipc.StateDone, ipc.StateIdle, ipc.StateExited, ipc.StateResting:
		return true
	}
	return false
}

// closeLocked closes session id: at your word, or on its own (auto) when
// spawn.close_when_done closes it. The caller holds c.mu.
func (c *Core) closeLocked(id string, auto bool) error {
	return c.closeWith(id, auto, nil)
}

// closeWith closes session id as closeLocked does, keeping in its
// ui.closed how many commits of its work had not landed, by notLive's
// look (notlive.go). The caller holds c.mu.
func (c *Core) closeWith(id string, auto bool, notLive map[string]ipc.NotLive) error {
	if err := c.refresh(); err != nil {
		return err
	}
	x := c.sessions[id]
	if x == nil {
		return ipc.Errorf(ipc.CodeNotFound, "no session %q", id)
	}
	p := c.snap.Pane(x.pane)
	if p == nil {
		return ipc.Errorf(ipc.CodeNotFound, "session %s has no pane", x.name)
	}
	// One command list: the pane goes home, and the closing mark takes it out
	// of the registry, before anything ends what it runs.
	cmds, err := tmux.SwapOut(c.snap, p.ID)
	if err != nil {
		return err
	}
	cmds = append(cmds, []string{"set-option", "-p", "-t", p.ID, tmux.OptRole, tmux.RoleClosing})
	if _, err := c.tmux.RunList(cmds); err != nil {
		return err
	}
	r := c.reopenable(x)
	r.Closed, r.Auto = time.Now().UTC().Truncate(time.Millisecond), auto
	r.Space, r.Group = c.spaceOf(x), c.groupOf(x)
	// Who closed it is the event's source: you, or Irie on its own.
	source := event.SourceUser
	if auto {
		source = event.SourceIrie
	}
	data := map[string]any{
		"name": x.name, "named": r.Named, "created": x.created.Format(time.RFC3339Nano), "vendor_id": r.VendorID, "resume": r.Resume,
	}
	for k, v := range map[string]string{"space": r.Space, "group": r.Group} {
		if v != "" {
			data[k] = v
		}
	}
	// What reopening it will cost, once its cache has run out.
	if w, ok := x.tally.wake(x.conversation()); ok && !x.local {
		r.Wake = w
		wakeData(data, w)
	}
	// Its work that had not landed, which the dog lists for a while.
	if n, ok := notLive[id]; ok && n.Ahead > 0 {
		r.NotLive = n.Ahead
		data["not_live"] = n.Ahead
	}
	c.record(x, event.Event{TS: event.Stamp(r.Closed), Source: source, Kind: event.UIClosed, Data: data})
	c.addClosed(r)
	delete(c.sessions, id)
	// Its ledger stays for the router and the costs report: once out of
	// the registry, its tally is the writer's alone.
	go c.keepSpent(x.id, &x.tally, slices.Clone(x.track.transcripts), x.local)
	// A frozen agent's held tool calls go through, so it ends gently.
	os.Remove(c.paths.FreezeFile(id))
	c.end(*p)
	// Its agent may have handed the conversation to Claude Code's
	// background, whose supervisor keeps it going: that copy ends too.
	if away := x.track.away; away != "" {
		go c.endCopy(x.id, x.profile, away)
	}
	// Its Dock controls' copies end with it (dock.go).
	go c.dockEnd(x.id)
	c.log.Info("closed", "session", x.id, "name", x.name, "resume", r.Resume, "auto", auto, "not_live", r.NotLive)
	return c.refresh()
}

// endCopy ends the copy of closed session id's conversation that runs in
// Claude Code's background (stopCopies), and logs what it did. It runs
// Claude Code's CLI, so the caller does not hold c.mu.
func (c *Core) endCopy(id, profile, vendorID string) {
	p, ok := c.profile(profile)
	if !ok || p.Harness != "claude-code" {
		if _, p, ok = c.claudeProfile(); !ok {
			return
		}
	}
	stopped, err := c.stopCopies(p, vendorID, moveWait)
	switch {
	case err != nil:
		c.log.Warn("close: its copy in Claude Code's background", "session", id, "resume", vendorID, "err", err)
	case len(stopped) > 0:
		c.log.Info("close: stopped its copy in Claude Code's background", "session", id, "resume", vendorID, "ids", stopped)
	}
}

// reopenable says how reopen will bring x back: by carrying its conversation
// on, under the id the agent last named, or, when it never got that far, by
// starting its task again. A harness that cannot resume always starts again,
// and a session that came in without a task, as a move does, always resumes.
// The caller holds c.mu.
func (c *Core) reopenable(x *session) closedRecord {
	r := closedRecord{ID: x.id, Name: x.name, Named: x.named, Created: x.created, VendorID: x.vendorID}
	if x.track.vendorID != "" {
		r.VendorID = x.track.vendorID
	}
	h, err := harnessFor(x.harness)
	_, resumes := h.(harness.Resumer)
	_, taskErr := os.Stat(c.paths.TaskFile(x.id))
	r.Resume = err == nil && resumes && r.VendorID != "" && (taskErr != nil || x.track.conversed())
	return r
}

// closeWhenDone closes x once close_delay passes with nothing more from it:
// no event, no message typed to it, nothing it needs from you, no helper or
// teammate at work and no freeze. A session that gets one more prompt is
// not done, one the user paused stays, and one whose answer is for you to
// read, such as a status report or a question, is not closed at all
// (awaitsLook); nor is one whose work is not over (finished). Turning
// close_when_done off in the settings stops the closes still waiting. The
// close is Irie's, so the session is not done the way one you close is:
// the review dock and the jot dog still show it, and reopen brings it
// back. The caller holds c.mu.
func (c *Core) closeWhenDone(x *session) {
	// A pinned session closes only at your own word (pin.go).
	if x.track.pinned || c.awaitsLook(x) || !finished(x) {
		return
	}
	id, quiet := x.id, x.quiet
	time.AfterFunc(c.cfg().Spawn.CloseDelay, func() {
		c.mu.Lock()
		x := c.sessions[id]
		if !c.cfg().Spawn.CloseWhenDone || x == nil || x.track.pinned || x.quiet != quiet || x.dead || x.track.activity != done || len(x.track.needs) > 0 || x.track.frozen != nil || !finished(x) {
			c.mu.Unlock()
			return
		}
		if _, waits := x.subsAtWork(); waits {
			c.mu.Unlock()
			return
		}
		err := c.closeLocked(id, true)
		frames := c.changes()
		c.mu.Unlock()
		c.publish(frames)
		if err != nil {
			c.log.Warn("close when done", "session", id, "err", err)
			return
		}
		c.publishClosed()
	})
}

// finished reports whether x's work is over as far as Irie can tell: its
// last turn left nothing of its own in the background, or a wakeup, that
// will wake it, and the jot dog found nothing of a jot's work left to land
// (DESIGN §4.8). Closing it sooner would end that work, or hide what is
// left from its row. The caller holds c.mu.
func finished(x *session) bool {
	return x.track.waiting == 0 && len(x.dog.loose) == 0
}

// lookTask matches a task that asks for something to read: a status, a
// report, a summary, a review, an explanation, a lookup or a question. The
// session's answer is the point, so it waits for the user.
var lookTask = regexp.MustCompile(`(?i)\b(status|report|summar(y|ize|ise)|recap|review|explain|investigate|audit|compare|research|what|where|which|why|how|who|when|list|show|tell|check|find|look (at|into)|walk me through)\b`)

// awaitsLook reports whether x is waiting for the user to read its answer,
// so it must not close by itself: its task asks for a status, report or
// other answer (lookTask), or its last message asks the user something.
// The caller holds c.mu.
func (c *Core) awaitsLook(x *session) bool {
	if msg, err := os.ReadFile(filepath.Join(c.paths.Session(x.id), resultFile)); err == nil &&
		strings.HasSuffix(strings.TrimSpace(string(msg)), "?") {
		return true
	}
	task, err := os.ReadFile(c.paths.TaskFile(x.id))
	if err != nil {
		return false
	}
	first, _, _ := strings.Cut(strings.TrimSpace(string(task)), "\n")
	return lookTask.MatchString(first)
}

// reap gives every closing pane an ending, such as those a restarted core
// finds. The caller holds c.mu.
func (c *Core) reap() {
	for _, p := range c.snap.ByRole(tmux.RoleClosing) {
		c.end(p)
	}
}

// end ends what a closing pane runs, unless that is under way. A frozen
// session is stopped, its host too, so everything the host runs is
// continued first, the host last; then SIGTERM goes to the host, which
// passes it on to the agent's process group. Whatever still runs after
// closeWait is killed. Then the pane's window goes. The caller holds c.mu.
func (c *Core) end(p tmux.Pane) {
	if c.closes.ending == nil {
		c.closes.ending = map[string]*ending{}
	}
	if c.closes.ending[p.ID] != nil {
		return
	}
	e := &ending{session: p.IrieSession, done: make(chan struct{})}
	c.closes.ending[p.ID] = e
	running := !p.Dead && p.PID > 1
	go func() {
		defer close(e.done)
		if running {
			signalAll(p.PID, syscall.SIGCONT)
			if err := syscall.Kill(p.PID, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
				c.log.Warn("close: SIGTERM", "pane", p.ID, "pid", p.PID, "err", err)
			}
			for deadline := time.Now().Add(closeWait); alive(p.PID) && time.Now().Before(deadline); {
				time.Sleep(50 * time.Millisecond)
			}
			if alive(p.PID) && c.hosts(p) {
				c.log.Warn("close: the agent did not exit after SIGTERM, so it is killed", "session", p.IrieSession, "pid", p.PID, "wait", closeWait)
				signalAll(p.PID, syscall.SIGKILL)
				for i := 0; i < 40 && alive(p.PID); i++ {
					time.Sleep(50 * time.Millisecond)
				}
			}
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if _, err := c.tmux.Run("kill-window", "-t", p.ID); err != nil && !strings.Contains(err.Error(), "can't find") {
			c.log.Warn("close: kill-window", "pane", p.ID, "err", err)
		}
		delete(c.closes.ending, p.ID)
		// Nothing of its agent runs now, so its worktree may go (drop.go).
		c.dropOnEnd(e.session)
	}()
}

// hosts reports whether p's process still runs in p, by tmux's word, so a
// SIGKILL never reaches a process that took the pid of a host that exited.
func (c *Core) hosts(p tmux.Pane) bool {
	c.mu.Lock()
	out, err := c.tmux.Run("display-message", "-p", "-t", p.ID, "#{pane_pid} #{pane_dead}")
	c.mu.Unlock()
	return err == nil && strings.TrimSpace(out) == strconv.Itoa(p.PID)+" 0"
}

// signalAll sends sig to what a host runs, the host last: the agent's
// process group, then every process under the host, from one ps listing.
func signalAll(hostPID int, sig syscall.Signal) {
	out, err := exec.Command("ps", "-A", "-o", "pid=", "-o", "ppid=", "-o", "pgid=").Output()
	if err == nil {
		kids, group := map[int][]int{}, map[int]int{}
		for _, line := range strings.Split(string(out), "\n") {
			f := strings.Fields(line)
			if len(f) != 3 {
				continue
			}
			pid, err1 := strconv.Atoi(f[0])
			ppid, err2 := strconv.Atoi(f[1])
			pgid, err3 := strconv.Atoi(f[2])
			if err1 == nil && err2 == nil && err3 == nil {
				kids[ppid] = append(kids[ppid], pid)
				group[pid] = pgid
			}
		}
		// The host starts the agent as the leader of a group of its own.
		for _, agent := range kids[hostPID] {
			if agent > 1 && group[agent] == agent {
				syscall.Kill(-agent, sig)
			}
		}
		seen := map[int]bool{hostPID: true}
		for todo := kids[hostPID]; len(todo) > 0; {
			pid := todo[0]
			todo = todo[1:]
			if pid <= 1 || pid == os.Getpid() || seen[pid] {
				continue
			}
			seen[pid] = true
			syscall.Kill(pid, sig)
			todo = append(todo, kids[pid]...)
		}
	}
	syscall.Kill(hostPID, sig)
}

// endings returns what to wait for before session id runs again: the ends of
// its closing panes. The caller holds c.mu.
func (c *Core) endings(id string) []chan struct{} {
	c.reap()
	var out []chan struct{}
	for _, e := range c.closes.ending {
		if e.session == id {
			out = append(out, e.done)
		}
	}
	return out
}

// reopen brings a closed session back in its own row, with its name, folder
// and profile, once the agent it ran has ended: `claude --resume` carries its
// conversation on, or its task starts again when the agent never got that
// far. With no session named, it reopens the one closed last.
func (c *Core) reopen(a ipc.ReopenArgs) (ipc.Session, error) {
	// One spawn, move or reopen at a time, so two never bring back the same
	// session or take the same name.
	c.spawnMu.Lock()
	defer c.spawnMu.Unlock()
	c.mu.Lock()
	err := c.refresh()
	var r closedRecord
	if err == nil {
		r, err = c.findClosed(a.Session)
	}
	var waits []chan struct{}
	if err == nil {
		waits = c.endings(r.ID)
	}
	c.mu.Unlock()
	if err != nil {
		return ipc.Session{}, err
	}
	// Two agents must never share one conversation.
	for _, w := range waits {
		<-w
	}

	j, err := c.reopenJob(r)
	if err == nil {
		// Its worktree comes back first, if Irie dropped it (drop.go).
		err = c.restoreWorktree(r, &j)
	}
	if err == nil && j.resume != "" {
		err = c.freeConversation(r, j)
	}
	// A session comes back resting only with a conversation to carry on
	// once it wakes; one whose task never started starts it again.
	j.rest = a.Rest && j.resume != ""
	if err != nil {
		c.log.Warn("reopen", "session", r.ID, "err", err)
		return ipc.Session{}, err
	}
	// A new run writes its own exit status.
	os.Remove(c.paths.ExitFile(r.ID))
	if r.Old {
		r.Name, r.Named = c.oldName(r.ID, r.Name)
	}
	c.mu.Lock()
	j.place.name, j.place.named = unique(r.Name, c.names(nil)), r.Named
	// A closed session keeps its place in the order for its reopen; a lost
	// one keeps it when the layout's copy brought the order back.
	placed := slices.Contains(c.order, r.ID)
	x, err := c.launch(j)
	if err == nil {
		c.dropClosed(r.ID)
		c.record(x, event.Event{Source: event.SourceUser, Kind: event.UIReopened, Data: map[string]any{"name": x.name, "resume": j.resume != ""}})
		c.comeHome(x, r)
		if r.Lost {
			c.comeBack(x, r, placed)
		}
		if j.rest {
			c.restBack(x, r)
		}
	}
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	if err != nil {
		c.log.Warn("reopen", "session", r.ID, "err", err)
		return ipc.Session{}, err
	}
	c.publishClosed()
	c.log.Info("reopened", "session", x.id, "name", x.name, "resume", j.resume)
	return x.wire(), nil
}

// freeConversation makes sure that nothing else runs the conversation j
// carries on, so two agents never share it. An Irie session that runs it,
// as one moved in from Claude Code's background does, makes reopen refuse.
// A copy in Claude Code's background ends first, as a move ends one; if
// it does not, reopen refuses. When Claude Code cannot list its sessions,
// reopen goes ahead, as before it looked.
func (c *Core) freeConversation(r closedRecord, j job) error {
	c.mu.Lock()
	for _, id := range slices.Sorted(maps.Keys(c.sessions)) {
		x := c.sessions[id]
		current := x.track.vendorID
		if current == "" {
			current = x.vendorID
		}
		// A resting session wakes in its own pane, once its agent ended
		// (rest.go).
		if !x.dead && current == j.resume && id != r.ID {
			c.mu.Unlock()
			return ipc.Errorf(ipc.CodeBusy, "%s carries on in %q now, so it cannot reopen as well", r.Name, x.name)
		}
	}
	c.mu.Unlock()
	if j.h.ID() != "claude-code" {
		return nil
	}
	stopped, err := c.stopCopies(j.run, j.resume, moveWait)
	switch {
	case errors.Is(err, errNoList):
		c.log.Warn("reopen: cannot tell whether its conversation runs in Claude Code's background", "session", r.ID, "err", err)
	case err != nil:
		return ipc.Errorf(ipc.CodeBusy, "%s still runs in Claude Code's background, so it cannot reopen: %v", r.Name, err)
	case len(stopped) > 0:
		c.log.Info("reopen: stopped its copy in Claude Code's background", "session", r.ID, "resume", j.resume, "ids", stopped)
	}
	return nil
}

// findClosed returns the closed session id names, or the one closed last.
// The caller holds c.mu.
func (c *Core) findClosed(id string) (closedRecord, error) {
	if id != "" && c.sessions[id] != nil {
		return closedRecord{}, ipc.Errorf(ipc.CodeBadArgs, "%s is open", c.sessions[id].name)
	}
	for _, r := range c.closes.list {
		if c.sessions[r.ID] == nil && (id == "" || r.ID == id) {
			return r, nil
		}
	}
	if id == "" {
		return closedRecord{}, ipc.Errorf(ipc.CodeNotFound, "no closed session to reopen")
	}
	return closedRecord{}, ipc.Errorf(ipc.CodeNotFound, "no closed session %q", id)
}

// reopenJob is the launch that brings r back: in its folder, on its profile,
// resuming its conversation or with its task.
func (c *Core) reopenJob(r closedRecord) (job, error) {
	spec, err := host.ReadSpec(c.paths.SpecFile(r.ID))
	if err != nil {
		return job{}, fmt.Errorf("cannot reopen %s: %v", r.Name, err)
	}
	// A worktree Irie dropped comes back as the session reopens.
	c.mu.Lock()
	back := c.restorable(spec)
	c.mu.Unlock()
	if fi, err := os.Stat(spec.Cwd); (err != nil || !fi.IsDir()) && !back {
		return job{}, ipc.Errorf(ipc.CodeBadArgs, "%s ran in %s, which is gone", r.Name, spec.Cwd)
	}
	profile, ok := c.profile(spec.Profile)
	if !ok {
		return job{}, ipc.Errorf(ipc.CodeNotFound, "%s ran on +%s, which is no longer a profile; the profiles are %s", r.Name, spec.Profile, strings.Join(slices.Sorted(maps.Keys(c.cfg().Profiles)), ", "))
	}
	// The mode the spawn chose, such as a delegated task's, stays.
	if i := slices.Index(spec.Argv, "--permission-mode"); i >= 0 && i+1 < len(spec.Argv) {
		profile.PermissionMode = spec.Argv[i+1]
	}
	h, err := harnessFor(profile.Harness)
	if err != nil {
		return job{}, ipc.Errorf(ipc.CodeBadArgs, "profile %s: %v", spec.Profile, err)
	}
	// A local profile's model is checked, and may be loaded, as for a spawn.
	var warnings []string
	if profile.Local != nil {
		if warnings, err = c.prepareLocal(spec.Profile, &profile, false); err != nil {
			return job{}, err
		}
	}
	var probe harness.Probe
	if p, ok := h.(harness.Prober); ok {
		probe = p.Probe(profile, c.agentEnv(profile))
	}
	probe.Warnings = append(warnings, probe.Warnings...)
	j := job{
		place:   place{cwd: spec.Cwd, repo: spec.Repo, worktree: spec.Worktree, branch: spec.Branch, copied: spec.Copied},
		profile: spec.Profile, run: profile, h: h, probe: probe, reopen: &r, data: map[string]any{"reopen": true}, tutor: spec.Tutor,
	}
	// An anonymous session comes back in its compartment, with its shut
	// paths, or not at all (DESIGN §4.16).
	if spec.Anon != "" {
		comp, notes, err := c.prepareAnon(spec.Anon, "")
		if err != nil {
			return job{}, err
		}
		j.anon, j.probe.Warnings = &comp, append(j.probe.Warnings, notes...)
	}
	if _, resumes := h.(harness.Resumer); r.Resume && resumes {
		j.resume = r.VendorID
		j.data["resume"] = r.VendorID
		return j, nil
	}
	task, err := os.ReadFile(c.paths.TaskFile(r.ID))
	if err != nil {
		return job{}, fmt.Errorf("cannot reopen %s: its task is gone: %v", r.Name, err)
	}
	j.task = string(task)
	return j, nil
}
