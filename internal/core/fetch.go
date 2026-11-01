package core

// Fetching work back (docs/proposals/multi-host.md §4.4, step 5, as the
// owner chose): a task in a repo that ran on a peer leaves its work on a
// branch of that machine's checkout, which the owner keeps there, with its
// hooks, under the same @name as here. Once such a session's turn has
// ended (done, idle, exited or failed), the core fetches its branch over
// ssh into the repo of that name here, as refs/remotes/<peer>/<branch>, so
// it never moves a branch of this machine's; the row then says so. `irie
// peer fetch` does it at once. The ssh name is the one `irie peer add
// --ssh` paired over, or peers.<name>.ssh.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/repo"
)

// fetchTimeout bounds one fetch.
const fetchTimeout = 2 * time.Minute

// peerGitURL is where git reaches a peer's checkout. Tests reach one on
// disk.
var peerGitURL = func(ssh, path string) string { return ssh + ":" + path }

// fetched is what the core last fetched of one of a peer's rows.
type fetched struct {
	ref, head string // the ref it fetched to, and its commit
	problem   string // why it did not fetch
}

// ended reports whether a row's turn is over, so its work is to be
// fetched.
func ended(s ipc.Session) bool {
	switch s.State {
	case ipc.StateDone, ipc.StateIdle, ipc.StateExited, ipc.StateFailed:
		return true
	}
	return false
}

// queueFetch asks for a row's work to be fetched, once its turn ended and
// it names a branch: a row that just ended, or one that had ended before
// this core first heard of it. The caller holds c.mu.
func (c *Core) queueFetch(p *peerState, s ipc.Session, was *ipc.Session) {
	if s.Repo == "" || s.Branch == "" || !ended(s) || p.fetchq == nil {
		return
	}
	if p.done[s.ID] && (was == nil || ended(*was)) {
		return // asked for already, and its turn has not run again since
	}
	p.done[s.ID] = true
	select {
	case p.fetchq <- s:
	default: // a long queue: the next end of a turn asks again
	}
}

// fetchLoop fetches a peer's rows' work one at a time, until stop closes.
func (c *Core) fetchLoop(p *peerState, stop <-chan struct{}) {
	for {
		select {
		case s := <-p.fetchq:
			f := c.fetchRow(p, s)
			c.mu.Lock()
			if c.peers[p.name] == p {
				p.fetched[s.ID] = f
			}
			frames := c.changes()
			c.mu.Unlock()
			c.publish(frames)
		case <-stop:
			return
		}
	}
}

// fetchRow fetches one of a peer's rows' branch into the repo of the same
// name here. The caller holds no lock.
func (c *Core) fetchRow(p *peerState, s ipc.Session) fetched {
	c.mu.Lock()
	ssh := p.conf.SSH
	if n := c.cfg().Peers[p.name].SSH; n != "" {
		ssh = n
	}
	c.mu.Unlock()
	name := filepath.Base(s.Repo)
	ref := "refs/remotes/" + p.name + "/" + s.Branch
	switch {
	case ssh == "":
		return fetched{problem: fmt.Sprintf("not fetched: pair with irie peer add %s --ssh HOST, or set peers.%s.ssh", p.name, p.name)}
	case strings.HasPrefix(s.Branch, "-") || strings.Contains(s.Branch, ".."):
		return fetched{problem: "not fetched: its branch has an odd name"}
	}
	local, err := repo.Find(name, c.repoOptions())
	if err != nil {
		return fetched{problem: fmt.Sprintf("not fetched: no repo here called %s", name)}
	}
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", local, "fetch", "--no-tags", "--quiet", "--end-of-options", peerGitURL(ssh, s.Repo), "+refs/heads/"+s.Branch+":"+ref)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=ssh -o BatchMode=yes -o ConnectTimeout=10")
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if i := strings.LastIndexByte(msg, '\n'); i >= 0 {
			msg = msg[i+1:]
		}
		c.log.Warn("peer: fetch", "peer", p.name, "session", s.ID, "err", err, "out", msg)
		return fetched{problem: "not fetched: " + firstNonEmpty(msg, err.Error())}
	}
	head, err := exec.Command("git", "-C", local, "rev-parse", "--verify", "--quiet", ref).Output()
	if err != nil {
		return fetched{problem: "not fetched: " + err.Error()}
	}
	c.log.Info("peer: fetched", "peer", p.name, "session", s.ID, "repo", local, "ref", ref)
	return fetched{ref: ref, head: strings.TrimSpace(string(head))}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// fetchNote is what a row's detail says of its fetch, or "". The caller
// holds c.mu.
func (p *peerState) fetchNote(id string) string {
	f, ok := p.fetched[id]
	switch {
	case !ok:
		return ""
	case f.problem != "":
		return f.problem
	}
	return "fetched as " + strings.TrimPrefix(f.ref, "refs/remotes/")
}

// fetchNow fetches what id names at once: one of the peers' rows, or every
// ended row of every peer for "all", and says how each went.
func (c *Core) fetchNow(id string) []ipc.PeerFetch {
	type job struct {
		p *peerState
		s ipc.Session
	}
	var jobs []job
	c.mu.Lock()
	for _, name := range c.peerNames() {
		p := c.peers[name]
		for _, s := range p.rows {
			if (id == "all" && ended(s) || id == name+"/"+s.ID) && s.Repo != "" && s.Branch != "" {
				jobs = append(jobs, job{p, s})
			}
		}
	}
	c.mu.Unlock()
	var out []ipc.PeerFetch
	for _, j := range jobs {
		f := c.fetchRow(j.p, j.s)
		c.mu.Lock()
		if c.peers[j.p.name] == j.p {
			j.p.fetched[j.s.ID] = f
		}
		c.mu.Unlock()
		out = append(out, ipc.PeerFetch{Session: j.p.name + "/" + j.s.ID, Ref: strings.TrimPrefix(f.ref, "refs/remotes/"), Head: f.head, Problem: f.problem})
	}
	frames := func() []ipc.Frame { c.mu.Lock(); defer c.mu.Unlock(); return c.changes() }()
	c.publish(frames)
	return out
}
