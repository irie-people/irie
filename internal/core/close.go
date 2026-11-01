package core

// Closing and reopening sessions (DESIGN §4.5, M4). Close swaps the session's
// pane out of the slot and marks it closing, so its row goes at once, then
// ends the agent gently: SIGCONT to all it runs, in case it is frozen, then
// SIGTERM through its host, and SIGKILL after closeWait. A conversation its
// agent handed to Claude Code's background ends too. The worktree and the
// session's files stay. A session you close is done: nothing brings it back
// but you, and the jot dog lets it be. Reopen, close's undo, brings the
// session back in the same row: it carries the conversation on, or starts
// the task again when the agent never got that far. Which sessions are
// closed, and which closed on their own, is derived from the journal's
// ui.closed and ui.reopened events, so it survives a restart.

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
	ending map[string]*ending // closing panes whose agents are still ending, by pane id
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
	named, ok := e.Data["named"].(bool)
	r.Named, r.Old = named, !ok
	return r
}

// addClosed puts r first among the closed sessions. The caller holds c.mu.
func (c *Core) addClosed(r closedRecord) {
	c.dropClosed(r.ID)
	c.closes.list = slices.Insert(c.closes.list, 0, r)
	if len(c.closes.list) > maxClosed {
		c.closes.list = c.closes.list[:maxClosed]
	}
}

// dropClosed forgets that a session is closed. The caller holds c.mu.
func (c *Core) dropClosed(id string) {
	c.closes.list = slices.DeleteFunc(c.closes.list, func(r closedRecord) bool { return r.ID == id })
}

// closedList is what clients hear of the closed sessions, the last closed
// first. A session that runs again is not closed. The caller holds c.mu.
func (c *Core) closedList() []ipc.ClosedSession {
	out := []ipc.ClosedSession{}
	for _, r := range c.closes.list {
		if c.sessions[r.ID] == nil {
			out = append(out, ipc.ClosedSession{ID: r.ID, Name: r.Name, Closed: r.Closed, Auto: r.Auto})
		}
	}
	return out
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
// and its agent ends in the background.
func (c *Core) close(a ipc.CloseArgs) error {
	c.mu.Lock()
	err := c.closeLocked(a.Session, false)
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	if err != nil {
		c.log.Warn("close", "session", a.Session, "err", err)
		return err
	}
	c.publishClosed()
	return nil
}

// closeLocked closes session id: at your word, or on its own (auto) when
// spawn.close_when_done closes it. The caller holds c.mu.
func (c *Core) closeLocked(id string, auto bool) error {
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
	// Who closed it is the event's source: you, or Irie on its own.
	source := event.SourceUser
	if auto {
		source = event.SourceIrie
	}
	c.record(x, event.Event{TS: event.Stamp(r.Closed), Source: source, Kind: event.UIClosed, Data: map[string]any{
		"name": x.name, "named": r.Named, "created": x.created.Format(time.RFC3339Nano), "vendor_id": r.VendorID, "resume": r.Resume,
	}})
	c.addClosed(r)
	delete(c.sessions, id)
	// A frozen agent's held tool calls go through, so it ends gently.
	os.Remove(c.paths.FreezeFile(id))
	c.end(*p)
	// Its agent may have handed the conversation to Claude Code's
	// background, whose supervisor keeps it going: that copy ends too.
	if away := x.track.away; away != "" {
		go c.endCopy(x.id, x.profile, away)
	}
	c.log.Info("closed", "session", x.id, "name", x.name, "resume", r.Resume, "auto", auto)
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
// no event, no message typed to it, nothing it needs from you, no
// subagent running and no freeze. A session that gets one more prompt is
// not done, one the user paused stays, and one whose answer is for you to
// read, such as a status report or a question, is not closed at all
// (awaitsLook); nor is one whose work is not over (finished). Turning
// close_when_done off in the settings stops the closes still waiting. The
// close is Irie's, so the session is not done the way one you close is:
// the review dock and the jot dog still show it, and reopen brings it
// back. The caller holds c.mu.
func (c *Core) closeWhenDone(x *session) {
	if c.awaitsLook(x) || !finished(x) {
		return
	}
	id, quiet := x.id, x.quiet
	time.AfterFunc(c.cfg().Spawn.CloseDelay, func() {
		c.mu.Lock()
		x := c.sessions[id]
		if !c.cfg().Spawn.CloseWhenDone || x == nil || x.quiet != quiet || x.dead || x.track.activity != done || len(x.track.needs) > 0 || len(x.track.subs) > 0 || x.track.frozen != nil || !finished(x) {
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
	if err == nil && j.resume != "" {
		err = c.freeConversation(r, j)
	}
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
	x, err := c.launch(j)
	if err == nil {
		c.dropClosed(r.ID)
		c.record(x, event.Event{Source: event.SourceUser, Kind: event.UIReopened, Data: map[string]any{"name": x.name, "resume": j.resume != ""}})
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
		if !x.dead && current == j.resume {
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
	if fi, err := os.Stat(spec.Cwd); err != nil || !fi.IsDir() {
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
		profile: spec.Profile, run: profile, h: h, probe: probe, reopen: &r, data: map[string]any{"reopen": true},
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
