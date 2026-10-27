package core

// Closing and reopening sessions (DESIGN §4.5, M4). Close swaps the session's
// pane out of the slot and marks it closing, so its row goes at once, then
// ends the agent gently: SIGTERM through its host, which also continues a
// stopped agent, and SIGKILL after closeWait. The worktree and the session's
// files stay. Reopen, close's undo, brings the session back in the same row:
// it carries the conversation on, or starts the task again when the agent
// never got that far. Which sessions are closed is derived from the journal's
// ui.closed and ui.reopened events, so it survives a restart.

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/irie-hq/irie/internal/event"
	"github.com/irie-hq/irie/internal/harness"
	"github.com/irie-hq/irie/internal/host"
	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/journal"
	"github.com/irie-hq/irie/internal/tmux"
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
	Created  time.Time
	Closed   time.Time
	VendorID string // the conversation reopen carries on
	Resume   bool   // the agent got as far as a conversation; else its task starts again
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
	r := closedRecord{ID: e.Session, Name: e.Str("name"), Closed: e.Time(), VendorID: e.Str("vendor_id")}
	r.Created, _ = time.Parse(time.RFC3339Nano, e.Str("created"))
	r.Resume, _ = e.Data["resume"].(bool)
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
			out = append(out, ipc.ClosedSession{ID: r.ID, Name: r.Name, Closed: r.Closed})
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

// close closes a session (DESIGN §4.5): its row goes at once, its pane leaves
// the slot before anything ends it, and its agent ends in the background.
func (c *Core) close(a ipc.CloseArgs) error {
	c.mu.Lock()
	err := c.closeLocked(a.Session)
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

func (c *Core) closeLocked(id string) error {
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
	r.Closed = time.Now().UTC().Truncate(time.Millisecond)
	c.record(x, event.Event{TS: event.Stamp(r.Closed), Source: event.SourceUser, Kind: event.UIClosed, Data: map[string]any{
		"name": x.name, "created": x.created.Format(time.RFC3339Nano), "vendor_id": r.VendorID, "resume": r.Resume,
	}})
	c.addClosed(r)
	delete(c.sessions, id)
	c.end(*p)
	c.log.Info("closed", "session", x.id, "name", x.name, "resume", r.Resume)
	return c.refresh()
}

// reopenable says how reopen will bring x back: by carrying its conversation
// on, under the id the agent last named, or, when it never got that far, by
// starting its task again. A harness that cannot resume always starts again,
// and a session that came in without a task, as a move does, always resumes.
// The caller holds c.mu.
func (c *Core) reopenable(x *session) closedRecord {
	r := closedRecord{ID: x.id, Name: x.name, Created: x.created, VendorID: x.vendorID}
	if x.track.vendorID != "" {
		r.VendorID = x.track.vendorID
	}
	h, err := harnessFor(x.harness)
	_, resumes := h.(harness.Resumer)
	_, taskErr := os.Stat(c.paths.TaskFile(x.id))
	r.Resume = err == nil && resumes && r.VendorID != "" && (taskErr != nil || x.track.conversed())
	return r
}

// reap gives every closing pane an ending, such as those a restarted core
// finds. The caller holds c.mu.
func (c *Core) reap() {
	for _, p := range c.snap.ByRole(tmux.RoleClosing) {
		c.end(p)
	}
}

// end ends what a closing pane runs, unless that is under way: SIGTERM to
// its host, which passes it on to the agent's process group and continues
// the group in case it is stopped, then SIGKILL after closeWait. Then the
// pane's window goes. The caller holds c.mu.
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
	if running {
		if err := syscall.Kill(p.PID, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			c.log.Warn("close: SIGTERM", "pane", p.ID, "pid", p.PID, "err", err)
		}
	}
	go func() {
		defer close(e.done)
		if running {
			for deadline := time.Now().Add(closeWait); alive(p.PID) && time.Now().Before(deadline); {
				time.Sleep(50 * time.Millisecond)
			}
			if alive(p.PID) {
				c.log.Warn("close: the agent did not exit after SIGTERM, so it is killed", "session", p.IrieSession, "pid", p.PID, "wait", closeWait)
				forceEnd(p.PID)
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

// forceEnd kills with SIGKILL what a host runs: the agent's process group,
// every process under the host, then the host.
func forceEnd(hostPID int) {
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
				syscall.Kill(-agent, syscall.SIGKILL)
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
			syscall.Kill(pid, syscall.SIGKILL)
			todo = append(todo, kids[pid]...)
		}
	}
	syscall.Kill(hostPID, syscall.SIGKILL)
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
	if err != nil {
		c.log.Warn("reopen", "session", r.ID, "err", err)
		return ipc.Session{}, err
	}
	// A new run writes its own exit status.
	os.Remove(c.paths.ExitFile(r.ID))
	c.mu.Lock()
	j.place.name = unique(r.Name, c.names())
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
	profile, ok := c.cfg.Profiles[spec.Profile]
	if !ok {
		return job{}, ipc.Errorf(ipc.CodeNotFound, "%s ran on +%s, which is no longer a profile; the profiles are %s", r.Name, spec.Profile, strings.Join(slices.Sorted(maps.Keys(c.cfg.Profiles)), ", "))
	}
	h, err := harnessFor(profile.Harness)
	if err != nil {
		return job{}, ipc.Errorf(ipc.CodeBadArgs, "profile %s: %v", spec.Profile, err)
	}
	var probe harness.Probe
	if p, ok := h.(harness.Prober); ok {
		probe = p.Probe(profile, c.agentEnv(profile))
	}
	j := job{
		place:   place{cwd: spec.Cwd, repo: spec.Repo, worktree: spec.Worktree, branch: spec.Branch, copied: spec.Copied},
		profile: spec.Profile, h: h, probe: probe, reopen: &r, data: map[string]any{"reopen": true},
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
