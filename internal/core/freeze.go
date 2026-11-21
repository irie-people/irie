package core

// Freeze (DESIGN §4.3, M6) stops a session's whole process tree at once:
// the agent, its subagents and everything any of them started. Thaw
// continues it. The core signals the processes itself and never sends
// SIGUSR1 or SIGUSR2 to a host: a host from before freeze handles only
// SIGHUP, SIGTERM and SIGINT, so either signal would end it and its agent.
//
// A host continues any stop of its agent that it sees, so that the agent
// never wedges, and tmux continues a pane's first process, the host, the
// same way: a stopped host runs again at once and continues its agent. So
// the host is never stopped. A host of this version leaves its agent's
// stops alone while the session's freeze.json exists (host.json says which
// host runs a pane). The core writes that file first, then stops the
// agent's whole process group at once, then every process under the host
// outside that group, parents before children, and walks the process table
// again until no new process turns up. A host from before would continue
// the agent, so there the agent runs on: everything under it stops at
// once, and the hook shim holds the agent at its next tool call while
// freeze.json exists. Thaw continues every stopped process under the host,
// children before parents, and only then removes freeze.json.
//
// Whether a session is frozen comes from the journal's freeze.on and
// freeze.off, so it stays frozen through a core restart, and the core never
// continues it on its own.
//
// A thaw of several sessions thaws them in turn, one every thawGap, so
// that each agent frozen long finds the sign-in the one before renewed
// (thawInTurn). Until its turn comes a session is frozen still, and counts
// as thawed: the history recorded its thaw, and freezing it again takes it
// out of the queue.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"syscall"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/proc"
	"github.com/irie-people/irie/internal/rebuild"
)

// freezePasses bounds the walks of the process table that one freeze makes.
const freezePasses = 5

// frozenNote is what an agent that was working is told at its first tool
// call after a thaw (DESIGN §5.5, notes.frozen).
const frozenNote = "[" + brand.Name + "] The user froze this session for %s (%s–%s UTC). Network calls or child processes may have failed meanwhile; re-check before you continue."

// freezeMark is freeze.json.
type freezeMark struct {
	At time.Time `json:"at"`
}

// freezeOp freezes (on) or thaws the sessions a names, and says which it
// froze or thawed. changed are those of them that were not frozen (on) or
// were: freezing a frozen session again only stops what started in it
// since. A thaw of them all, when that is several, thaws them in turn:
// r's Sessions are those thawed now, and Queued those still to come.
func (c *Core) freezeOp(a ipc.FreezeArgs, on bool) (r ipc.FreezeResult, changed []string, err error) {
	c.mu.Lock()
	ids, err := c.freezeTargets(a, on)
	if !on && a.All && len(ids) > 1 {
		thawed, queued := c.thawInTurn(ids, time.Now())
		frames := c.changes()
		c.mu.Unlock()
		c.publish(frames)
		return ipc.FreezeResult{Sessions: append([]string{}, thawed...), Queued: queued}, ids, nil
	}
	done := []string{}
	for _, id := range ids {
		x := c.sessions[id]
		was := c.frozenNow(x)
		var err1 error
		if on {
			err1 = c.freezeLocked(x, "")
		} else {
			err1 = c.thawLocked(x, "")
		}
		if err1 != nil {
			c.log.Warn("freeze", "session", id, "on", on, "err", err1)
			if !a.All {
				err = err1
			}
			continue
		}
		done = append(done, id)
		if was != on {
			changed = append(changed, id)
		}
	}
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	return ipc.FreezeResult{Sessions: done}, changed, err
}

// Action kinds of freeze (undo.go): a session frozen or thawed by the
// user. Freezing or thawing them all at once is one step, a batch of
// these, whose label names its sessions, `freeze "a", "b" and "c"`. What
// the trod bot freezes and thaws is its own doing and no step.
const (
	actFreeze = "freeze"
	actThaw   = "thaw"
)

func init() {
	freezeEff := effect{check: checkFreeze(true), do: doFreeze(true)}
	thawEff := effect{check: checkFreeze(false), do: doFreeze(false)}
	registerAction(actFreeze, actionKind{verb: "freeze", batch: namesOf("freeze"), undo: thawEff, redo: freezeEff})
	registerAction(actThaw, actionKind{verb: "thaw", batch: namesOf("thaw"), undo: freezeEff, redo: thawEff})
}

// namesOf labels a batch of verb's steps by the names of its sessions.
func namesOf(verb string) func(names []string) string {
	return func(names []string) string { return namesLabel(verb, names) }
}

// checkFreeze is what makes freezing a's session (on), or thawing it,
// uncertain: that it is closed or gone, that it is that way already, or,
// to freeze it, that its agent has ended. To thaw several, as an undo of
// freezing them all, it asks first, once, before the first of them, since
// each agent signs in again (DESIGN §4.5, "Inside and outside").
func checkFreeze(on bool) func(c *Core, a action) []problem {
	verb, already := "thaw", "is not frozen any more"
	if on {
		verb, already = "freeze", "is frozen already"
	}
	return func(c *Core, a action) []problem {
		x := c.sessions[a.Session]
		if x == nil {
			return notOpen(c, a, verb)
		}
		if c.frozenNow(x) == on {
			return []problem{alreadyDone(fmt.Sprintf("%q %s.", x.name, already))}
		}
		if st := x.track.state(x.dead); on && (st == ipc.StateExited || st == ipc.StateFailed) {
			return []problem{{code: "ended", question: fmt.Sprintf("%q has ended, so there is nothing to freeze.", x.name), recommend: ipc.ChoiceSkip}}
		}
		if !on {
			if names, ids := c.thawBatch(a); len(ids) > 1 {
				return []problem{c.signsIn(names, ids)}
			}
		}
		return nil
	}
}

// doFreeze freezes a's session (on) or thaws it the way the op does, with
// no step of its own, and takes the name it has now. A thaw that is one of
// a batch, as an undo of freezing them all, goes in turn with the rest.
func doFreeze(on bool) func(c *Core, a action) (action, error) {
	return func(c *Core, a action) (action, error) {
		if !on && a.Batch != "" {
			c.mu.Lock()
			x := c.sessions[a.Session]
			if x == nil {
				c.mu.Unlock()
				return a, ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Session)
			}
			c.thawInTurn([]string{a.Session}, time.Now())
			a.Name = x.name
			frames := c.changes()
			c.mu.Unlock()
			c.publish(frames)
			return a, nil
		}
		if _, _, err := c.freezeOp(ipc.FreezeArgs{Session: a.Session}, on); err != nil {
			return a, err
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		a.Name = c.nameOf(a.Session)
		return a, nil
	}
}

// freezeTargets are the sessions a names: one, or with All every live
// session that is not yet frozen (on) or is frozen (off), in list order.
// The caller holds c.mu.
func (c *Core) freezeTargets(a ipc.FreezeArgs, on bool) ([]string, error) {
	if err := c.refresh(); err != nil {
		return nil, err
	}
	if !a.All {
		if c.sessions[a.Session] == nil {
			return nil, ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Session)
		}
		return []string{a.Session}, nil
	}
	return c.allTargets(on), nil
}

// allTargets are this machine's live sessions that are not yet frozen
// (on), or are frozen and stay so (off), in list order. A paired
// machine's rows in the list are its own core's to freeze. The caller
// holds c.mu.
func (c *Core) allTargets(on bool) []string {
	var ids []string
	for _, s := range c.list() {
		x := c.sessions[s.ID]
		if x == nil {
			continue
		}
		if c.frozenNow(x) != on && (!on || s.State != ipc.StateExited && s.State != ipc.StateFailed) {
			ids = append(ids, s.ID)
		}
	}
	return ids
}

// freezeLocked stops session x's process tree. Freezing a frozen session
// again stops what runs in it that started since. why is "" for the
// user's own freeze; the trod bot gives its reason (trod.go). The caller
// holds c.mu.
func (c *Core) freezeLocked(x *session, why string) error {
	// Frozen now, it waits for no thaw in turn.
	c.unqueue(x.id)
	if st := x.track.state(x.dead); st == ipc.StateExited || st == ipc.StateFailed {
		return ipc.Errorf(ipc.CodeBusy, "%s has ended, so there is nothing to freeze", x.name)
	}
	p := c.snap.Pane(x.pane)
	if p == nil || p.Dead || p.PID <= 1 {
		return ipc.Errorf(ipc.CodeNotFound, "%s has no running pane", x.name)
	}
	whole := c.hostFreezes(x, p.PID)
	if f := x.track.frozen; f != nil {
		mark, _ := f.Int("mark")
		_, err := stopTree(p.PID, whole, int64(mark))
		return err
	}
	now := time.Now().UTC()
	if err := writeMark(c.paths.FreezeFile(x.id), now); err != nil {
		return err
	}
	t, err := proc.Read()
	if err != nil {
		os.Remove(c.paths.FreezeFile(x.id))
		return err
	}
	stopped, err := stopTree(p.PID, whole, 0)
	if err != nil && len(stopped) == 0 {
		os.Remove(c.paths.FreezeFile(x.id))
		return err
	}
	pids := make([]int, 0, len(stopped))
	for _, s := range stopped {
		pids = append(pids, s.PID)
	}
	on := event.Event{TS: event.Stamp(now), Source: event.SourceUser, Kind: event.FreezeOn, Data: map[string]any{
		"whole": whole, "pids": pids, "mark": t.Newest(), "working": x.track.live() == ipc.StateWorking || len(x.track.subs) > 0,
	}}
	if why != "" {
		on.Source, on.Data["by"], on.Data["reason"] = event.SourceIrie, trodBy, why
	}
	c.record(x, on)
	x.note = ""
	// Keys typed meanwhile would reach the agent all at once at the thaw.
	if _, err := c.tmux.Run("select-pane", "-d", "-t", x.pane); err != nil {
		c.log.Warn("freeze: input off", "session", x.id, "err", err)
	}
	c.log.Info("frozen", "session", x.id, "whole", whole, "stopped", len(pids))
	return nil
}

// thawLocked continues session x's process tree. Thawing a session that is
// not frozen continues whatever in it is stopped. why is "" for the user's
// own thaw, as for freezeLocked. The caller holds c.mu.
func (c *Core) thawLocked(x *session, why string) error {
	// Thawed now, it waits for no turn.
	c.unqueue(x.id)
	if p := c.snap.Pane(x.pane); p != nil && !p.Dead && p.PID > 1 {
		if err := continueTree(p.PID); err != nil {
			return err
		}
	}
	if err := os.Remove(c.paths.FreezeFile(x.id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := c.tmux.Run("select-pane", "-e", "-t", x.pane); err != nil {
		c.log.Warn("thaw: input on", "session", x.id, "err", err)
	}
	f := x.track.frozen
	if f == nil {
		return nil
	}
	now := time.Now().UTC()
	from := f.Time()
	// A thaw in turn waits thawGap after this one, as the trod bot's do.
	c.lastThaw = now
	off := event.Event{TS: event.Stamp(now), Source: event.SourceUser, Kind: event.FreezeOff, Data: map[string]any{
		"frozen_ms": now.Sub(from).Milliseconds(),
	}}
	if why != "" {
		off.Source, off.Data["by"], off.Data["reason"] = event.SourceIrie, trodBy, why
	}
	c.record(x, off)
	if working, _ := f.Data["working"].(bool); working {
		x.note = fmt.Sprintf(frozenNote, now.Sub(from).Round(time.Second), from.Format("15:04:05"), now.Format("15:04:05"))
	}
	c.log.Info("thawed", "session", x.id, "frozen", now.Sub(from).Round(time.Millisecond))
	return nil
}

// hostFreezes reports whether the host with pid hostPID that runs x leaves
// its agent stopped while x is frozen, which a host from before freeze does
// not. The caller holds c.mu.
func (c *Core) hostFreezes(x *session, hostPID int) bool {
	i, err := host.ReadInfo(c.paths.HostFile(x.id))
	return err == nil && i.PID == hostPID && i.Freeze >= 1
}

// keepFrozen stops what started in a frozen session since it was frozen,
// such as a hook a running agent runs when its host is from before freeze,
// and makes freeze.json agree with the journal, as after a restart. It
// never continues anything. The caller holds c.mu.
func (c *Core) keepFrozen() {
	for _, x := range c.sessions {
		f := x.track.frozen
		if f == nil {
			continue
		}
		p := c.snap.Pane(x.pane)
		if p == nil || p.Dead || p.PID <= 1 {
			continue
		}
		file := c.paths.FreezeFile(x.id)
		if _, err := os.Stat(file); errors.Is(err, os.ErrNotExist) {
			if err := writeMark(file, f.Time()); err != nil {
				c.log.Warn("freeze", "session", x.id, "err", err)
			}
		}
		mark, _ := f.Int("mark")
		if _, err := stopTree(p.PID, c.hostFreezes(x, p.PID), int64(mark)); err != nil {
			c.log.Warn("freeze", "session", x.id, "err", err)
		}
	}
}

// takeNote returns the notes an agent is due at the hook evs came from,
// once: at its first tool call or prompt after a thaw, and after zen began
// or ended (zen.go). The caller holds c.mu.
func (c *Core) takeNote(x *session, evs []event.Event) string {
	if x.note == "" && x.zenNote == "" || x.track.frozen != nil {
		return ""
	}
	for _, e := range evs {
		switch e.Kind {
		case event.ToolStarted, event.ToolFinished, event.ToolFailed, event.PromptSubmitted:
			n := joinNotes(x.note, x.zenNote)
			x.note, x.zenNote = "", ""
			return n
		}
	}
	return ""
}

// agentOf is the agent a host runs: its child that leads a process group of
// its own.
func agentOf(t proc.Table, hostPID int) (proc.Proc, bool) {
	for _, p := range t.Children(hostPID) {
		if p.PGID == p.PID && !p.Zombie {
			return p, true
		}
	}
	return proc.Proc{}, false
}

// stopTree stops what the host hostPID runs, parents before children, and
// returns what it stopped. With whole, the agent's process group stops at
// once; without, the agent itself is left running, since its host would
// continue it. Only processes that started after since stop. Each pass
// reads the process table again, until a pass finds nothing new to stop.
func stopTree(hostPID int, whole bool, since int64) ([]proc.Proc, error) {
	var stopped []proc.Proc
	sent := map[int]int{}
	group := false
	for range freezePasses {
		t, err := proc.Read()
		if err != nil {
			return stopped, err
		}
		agent, ok := agentOf(t, hostPID)
		n := 0
		groupNow := false
		if whole && ok && !agent.Stopped && !group && agent.Start > since {
			if err := syscall.Kill(-agent.PID, syscall.SIGSTOP); err == nil {
				group, groupNow = true, true
				n++
			}
		}
		for _, p := range t.Tree(hostPID) {
			switch {
			case p.Zombie || p.Stopped || p.Start <= since || sent[p.PID] >= 2:
				continue
			case ok && p.PID == agent.PID && !whole:
				continue
			case groupNow && ok && p.PGID == agent.PID:
				// The group's stop reached it.
				stopped = append(stopped, p)
				continue
			}
			if err := syscall.Kill(p.PID, syscall.SIGSTOP); err == nil {
				sent[p.PID]++
				stopped = append(stopped, p)
				n++
			}
		}
		if n == 0 {
			break
		}
	}
	return stopped, nil
}

// continueTree continues every stopped process the host hostPID runs,
// children before parents, so that no parent sees a child of its stopped.
func continueTree(hostPID int) error {
	t, err := proc.Read()
	if err != nil {
		return err
	}
	tree := t.Tree(hostPID)
	slices.Reverse(tree)
	for _, p := range tree {
		if p.Stopped {
			syscall.Kill(p.PID, syscall.SIGCONT)
		}
	}
	return nil
}

// writeMark writes freeze.json.
func writeMark(path string, at time.Time) error {
	b, err := json.Marshal(freezeMark{At: at})
	if err != nil {
		return err
	}
	return filetx.WriteFile(path, append(b, '\n'), 0o600)
}

// thawGap is how far apart Irie thaws sessions when it thaws several: a
// thaw of them all, an undo or a redo of one, and the trod bot's thaws
// (DESIGN §4.3, §4.10). An agent frozen for a day or more renews its
// sign-in with its first request, and many renewing at once can sign them
// all out: twenty-one thawed in the same second all came back "Not logged
// in". One at a time, each finds the sign-in the one before renewed, and
// the Mac takes them back one at a time too.
const thawGap = 30 * time.Second

// What a frozen row says while its thaw waits its turn.
const (
	thawsInTurn = "Thaws in turn, one session every 30 s"
	thawsHeld   = "Waits to thaw: an agent is signed out; sign it in again"
)

// thawQueue is a thaw of several sessions under way: those still to thaw,
// in turn, since when, as an agent signed out since holds the rest, and
// when the last of them thawed. state.json keeps it, so that a core that
// restarts goes on with it at the same pace.
type thawQueue struct {
	IDs   []string  `json:"ids"`
	Since time.Time `json:"since"`
	Last  time.Time `json:"last,omitzero"`
}

// queued reports whether session id waits its turn to thaw.
func (q thawQueue) queued(id string) bool { return slices.Contains(q.IDs, id) }

// frozenNow reports whether x is frozen and stays so: a session whose thaw
// waits its turn counts as thawed, as the history has it. The caller holds
// c.mu.
func (c *Core) frozenNow(x *session) bool {
	return x.track.frozen != nil && !c.steps.thaws.queued(x.id)
}

// thawInTurn thaws the frozen sessions ids one at a time: the first at
// once, unless Irie thawed one within thawGap, and each of the rest
// thawGap after the one before, at the core's tick (thawNext). It returns
// those it thawed now and those still to come. The caller holds c.mu.
func (c *Core) thawInTurn(ids []string, now time.Time) (thawed, queued []string) {
	q := &c.steps.thaws
	if len(q.IDs) == 0 {
		q.Since = now
	}
	for _, id := range ids {
		if !q.queued(id) {
			q.IDs = append(q.IDs, id)
		}
	}
	c.saveState()
	thawed = c.thawNext(now)
	for _, id := range ids {
		if q.queued(id) {
			queued = append(queued, id)
		}
	}
	c.log.Info("thaw in turn", "thawed", thawed, "queued", queued)
	return thawed, queued
}

// thawNext thaws the next session of a thaw under way once its turn has
// come: thawGap after Irie's last thaw, and while no agent's turn has
// failed for want of a sign-in since the thaw began. Such a turn holds
// the rest frozen until it is seen to: they would only wake into the same
// failure (DESIGN §4.10). A session closed, gone or thawed meanwhile
// leaves the queue. It returns what it thawed. The caller holds c.mu.
func (c *Core) thawNext(now time.Time) []string {
	q := &c.steps.thaws
	n := len(q.IDs)
	q.IDs = slices.DeleteFunc(q.IDs, func(id string) bool {
		x := c.sessions[id]
		return x == nil || x.track.frozen == nil
	})
	changed := len(q.IDs) != n
	// The last thaw: this core's, or before a restart the queue's own.
	last := c.lastThaw
	if q.Last.After(last) {
		last = q.Last
	}
	var thawed []string
	if len(q.IDs) > 0 && now.Sub(last) >= thawGap && !c.signedOut(q.Since) {
		x := c.sessions[q.IDs[0]]
		q.IDs = q.IDs[1:]
		changed = true
		if err := c.thawLocked(x, ""); err != nil {
			c.log.Warn("thaw in turn", "session", x.id, "err", err)
		} else {
			q.Last = now
			thawed = append(thawed, x.id)
		}
	}
	if len(q.IDs) == 0 && !q.Since.IsZero() {
		q.Since, changed = time.Time{}, true
	}
	if changed {
		c.saveState()
	}
	return thawed
}

// unqueue takes session id out of a thaw under way, as a freeze or a thaw
// of it by hand does. The caller holds c.mu.
func (c *Core) unqueue(id string) {
	q := &c.steps.thaws
	i := slices.Index(q.IDs, id)
	if i < 0 {
		return
	}
	q.IDs = slices.Delete(q.IDs, i, i+1)
	if len(q.IDs) == 0 {
		q.Since = time.Time{}
	}
	c.saveState()
}

// thawWait is what x's row says while its thaw waits its turn; "" while it
// does not. The caller holds c.mu.
func (c *Core) thawWait(x *session) string {
	q := c.steps.thaws
	if x.track.frozen == nil || !q.queued(x.id) {
		return ""
	}
	if c.signedOut(q.Since) {
		return thawsHeld
	}
	return thawsInTurn
}

// thawBatch is, when a is the first step of its batch that this undo or
// redo takes, the names and sessions of the steps of that batch, which
// it will thaw together with a, in the order they were taken; none for a
// step of no batch, or once the batch is under way. The caller holds c.mu.
func (c *Core) thawBatch(a action) (names, ids []string) {
	if a.Batch == "" {
		return nil, nil
	}
	// A thaw undoes a freeze or redoes a thaw.
	dir := dirRedo
	if a.Kind == actFreeze {
		dir = dirUndo
	}
	h := &c.steps.hist
	from, to := h.stack(dir)
	if t, ok := h.top(dir); !ok || t.ID != a.ID ||
		slices.ContainsFunc(*to, func(b action) bool { return b.Batch == a.Batch }) {
		return nil, nil
	}
	n := h.batched(dir)
	for _, b := range (*from)[len(*from)-n:] {
		ids = append(ids, b.Session)
	}
	return h.batchNames(dir, n), ids
}

// signsIn is the question a thaw of several asks before the first of
// them: each agent signs in again, so they thaw in turn, and waking those
// whose cache has run out costs what it costs. The caller holds c.mu.
func (c *Core) signsIn(names, ids []string) problem {
	detail := "Each carries on where it stopped, one at a time, so that each agent finds the sign-in the one before renewed."
	if w := c.coldOfThese(ids, time.Now()); w != "" {
		detail += " " + w
	}
	return problem{code: "signs-in",
		question: fmt.Sprintf("Thawing %s signs each agent in again, so they thaw one every 30 s.", namesList(names)),
		options: []ipc.ChoiceOption{{Key: ipc.ChoiceApply, Label: fmt.Sprintf("Thaw these %d, one every 30 s", len(ids)),
			Detail: detail}},
		recommend: ipc.ChoiceApply}
}

// coldOfThese says what waking those of the sessions ids whose prompt
// cache has run out costs (DESIGN §6.3); "" when none has. The caller
// holds c.mu.
func (c *Core) coldOfThese(ids []string, now time.Time) string {
	n, usd := 0, 0.0
	for _, id := range ids {
		if x := c.sessions[id]; x != nil {
			if w, ok := c.wakePrice(x, now); ok {
				n++
				usd += w.USD
			}
		}
	}
	switch n {
	case 0:
		return ""
	case 1:
		return "Waking the one gone cold costs about " + rebuild.Dollars(usd) + "."
	}
	return fmt.Sprintf("Waking the %d gone cold costs about %s.", n, rebuild.Dollars(usd))
}
