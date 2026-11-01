package core

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/jot"
)

// More for the last jot (DESIGN §4.2): a jot typed just after another, that
// reads as more for it, goes to that jot's agent as a message instead of
// starting an agent of its own. Context remembered a moment too late then
// reaches the agent doing the work, even one still starting.

// followTries is how often typing an added jot may fail before it is
// dropped.
const followTries = 5

// lastJot is the jot sent last from a task bar or the command bar.
type lastJot struct {
	args    ipc.SpawnArgs // as asked: the place and agent a jot added to it shares
	at      time.Time     // when it came, or was last added to
	session string        // its session, once its spawn is over and it started
	done    chan struct{} // closed when its spawn is over
	err     error         // why it did not start
	early   []followUp    // what was added to it before its session was made
}

// followUp is a jot added to a session's own, waiting to be typed to the
// session's agent.
type followUp struct {
	Text  string    `json:"text"`
	After time.Time `json:"after"` // not before then, so it can still be taken back
	Tries int       `json:"tries,omitempty"`
}

// jot answers a spawn of a task the user typed in a task bar or the
// command bar. One that reads as more for the jot sent just before it is
// added to that jot, and waits until that jot's spawn is over, so the
// answer names its session; anything else spawns, and is the jot the next
// one may add to. A spawn with Apart first takes its task back from the
// session it was added to.
func (c *Core) jot(a ipc.SpawnArgs) (ipc.SpawnResult, error) {
	now := time.Now()
	c.mu.Lock()
	late := false
	if a.Apart != "" {
		late = !c.takeBack(a.Apart, a.Task)
	} else if last, why, ok := c.addable(a, now); ok {
		// It waits spawn.follow_up_delay, so it can still be taken back.
		f := followUp{Text: a.Task, After: now.Add(c.cfg().Spawn.FollowUpDelay)}
		last.at = now
		if last.session == "" {
			last.early = append(last.early, f)
		} else {
			c.queueFollowUp(c.sessions[last.session], f)
		}
		c.log.Info("jot added", "to", last.session, "why", why, "chars", len(a.Task))
		c.mu.Unlock()
		<-last.done
		c.mu.Lock()
		defer c.mu.Unlock()
		if last.err != nil {
			return ipc.SpawnResult{}, ipc.Errorf(ipc.CodeBusy, "this reads as more for “%s”, which did not start: %v", title(last.args.Task), last.err)
		}
		x := c.sessions[last.session]
		if x == nil {
			return ipc.SpawnResult{}, ipc.Errorf(ipc.CodeBusy, "this reads as more for “%s”, which has ended", title(last.args.Task))
		}
		return ipc.SpawnResult{Session: x.wire(), Added: true, Until: f.After}, nil
	}
	last := &lastJot{args: a, at: now, done: make(chan struct{})}
	c.last = last
	c.mu.Unlock()

	s, err := c.spawn(a, true)
	c.mu.Lock()
	last.session, last.err = s.ID, err
	if x := c.sessions[s.ID]; err == nil && x != nil {
		for _, f := range last.early {
			c.queueFollowUp(x, f)
		}
	}
	last.early = nil
	close(last.done)
	c.mu.Unlock()
	if err != nil {
		return ipc.SpawnResult{}, err
	}
	c.recordAction(actSpawn, s.ID, nil)
	return ipc.SpawnResult{Session: s, Late: late}, nil
}

// addable finds the jot that a, typed now, adds to, if a reads as more for
// it: the last jot, sent or added to within spawn.follow_up_window, in the
// same place and for the same agent, still starting or with its agent
// still there. The caller holds c.mu.
func (c *Core) addable(a ipc.SpawnArgs, now time.Time) (*lastJot, string, bool) {
	last, window := c.last, c.cfg().Spawn.FollowUpWindow
	if last == nil || window <= 0 || now.Sub(last.at) > window || !samePlace(last.args, a) {
		return nil, "", false
	}
	select {
	case <-last.done:
		x := c.sessions[last.session]
		if last.err != nil || x == nil {
			return nil, "", false
		}
		if st := x.track.state(x.dead); st == ipc.StateExited || st == ipc.StateFailed {
			return nil, "", false
		}
	default:
		// Its spawn is not over yet.
	}
	why, ok := jot.Follows(a.Task)
	return last, why, ok
}

// samePlace reports whether jot b asks for the place and agent that jot a
// did. A jot that names a place, an agent, a model or a name of its own is
// a task of its own.
func samePlace(a, b ipc.SpawnArgs) bool {
	return a.Cwd == b.Cwd && a.Repo == b.Repo && a.InPlace == b.InPlace && a.Profile == b.Profile && a.Model == b.Model && b.Name == ""
}

// queueFollowUp adds f to what waits for session x's agent. The caller
// holds c.mu.
func (c *Core) queueFollowUp(x *session, f followUp) {
	if x == nil {
		return
	}
	x.followUps = append(x.followUps, f)
	c.saveFollowUps(x)
}

// takeBack takes a jot added to session id back, and says whether it was
// still waiting: if not, the agent has it. The caller holds c.mu.
func (c *Core) takeBack(id, text string) bool {
	x := c.sessions[id]
	if x == nil {
		// Whatever waited for it went with it.
		return true
	}
	for i, f := range x.followUps {
		if f.Text == text {
			x.followUps = append(x.followUps[:i], x.followUps[i+1:]...)
			c.saveFollowUps(x)
			c.log.Info("jot taken back", "from", id)
			return true
		}
	}
	return false
}

// sendFollowUps starts typing the jots added to sessions whose agents can
// take them now. A session that ended drops its own. The caller holds c.mu.
func (c *Core) sendFollowUps(now time.Time) {
	for _, x := range c.sessions {
		if len(x.followUps) == 0 || x.sending {
			continue
		}
		if st := x.track.state(x.dead); st == ipc.StateExited || st == ipc.StateFailed {
			c.log.Warn("jots not sent: the session ended", "session", x.id, "jots", len(x.followUps))
			x.followUps = nil
			c.saveFollowUps(x)
			continue
		}
		if f := x.followUps[0]; now.Before(f.After) || sendable(x, f.Text) != nil {
			continue
		}
		x.sending = true
		go c.typeFollowUps(x.id)
	}
}

// typeFollowUps types the jots added to session id to its agent, in order,
// while it can take them, as send does a message.
func (c *Core) typeFollowUps(id string) {
	defer func() {
		c.mu.Lock()
		if x := c.sessions[id]; x != nil {
			x.sending = false
		}
		c.mu.Unlock()
	}()
	for {
		c.mu.Lock()
		x := c.sessions[id]
		if x == nil || len(x.followUps) == 0 || time.Now().Before(x.followUps[0].After) || sendable(x, x.followUps[0].Text) != nil {
			c.mu.Unlock()
			return
		}
		f := x.followUps[0]
		x.followUps = x.followUps[1:]
		c.saveFollowUps(x)
		c.mu.Unlock()

		err := c.send(ipc.SendArgs{Session: id, Text: f.Text})
		if err == nil {
			c.log.Info("jot sent", "session", id, "chars", len(f.Text))
			continue
		}
		f.Tries++
		c.log.Warn("jot not sent", "session", id, "tries", f.Tries, "err", err)
		c.mu.Lock()
		if x := c.sessions[id]; x != nil && f.Tries < followTries {
			x.followUps = append([]followUp{f}, x.followUps...)
			c.saveFollowUps(x)
		}
		c.mu.Unlock()
		return
	}
}

// saveFollowUps keeps what waits for session x's agent in its folder, so a
// core that restarts meanwhile still types it. The caller holds c.mu.
func (c *Core) saveFollowUps(x *session) {
	file := c.paths.FollowUpFile(x.id)
	if len(x.followUps) == 0 {
		if err := os.Remove(file); err != nil && !errors.Is(err, fs.ErrNotExist) {
			c.log.Warn("jots", "session", x.id, "err", err)
		}
		return
	}
	b, err := json.Marshal(x.followUps)
	if err == nil {
		err = writeFileAtomic(file, b)
	}
	if err != nil {
		c.log.Warn("jots", "session", x.id, "err", err)
	}
}

// loadFollowUps reads back what waits for session x's agent, as a restarted
// core adopts it.
func (c *Core) loadFollowUps(x *session) {
	b, err := os.ReadFile(c.paths.FollowUpFile(x.id))
	if err != nil {
		return
	}
	if err := json.Unmarshal(b, &x.followUps); err != nil {
		c.log.Warn("jots", "session", x.id, "err", err)
	}
}
