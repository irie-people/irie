package core

// What agents leave running (DESIGN §4.11, Raphael). Each minute's sample
// gives Raphael the processes launchd took as its own that an agent may
// have left (raphael.Candidates), each with what it runs and what may say
// whose it is, and Irie's sessions, open and closed; raphael.LeftRunning
// says which an agent left running. He flags those, and stops one only on
// the person's OK.
//
// A process is read once: its arguments and environment with a sysctl,
// and the files that may name its session (its folder, its output) with
// one lsof for every process new since the last sample. On 2026-11-23
// there were 15 candidates, 17 processes with what they ran, read in about
// 100 ms; after that a sample reads none until one starts.

import (
	"context"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/proc"
	"github.com/irie-people/irie/internal/raphael"
)

// seenKey is one process, by its pid, when it started and its name: exec
// keeps the first two and changes the name, so a process that turns into
// another program is read afresh.
type seenKey struct {
	pid   int
	start int64
	name  string
}

// leftState is what the look at what agents left running keeps from one
// sample to the next: what each process showed, and the closed sessions'
// worktree and folder, from their specs, each read once. Only the sweep
// uses it, one at a time.
type leftState struct {
	seen map[seenKey]raphael.Seen
	dirs map[string][2]string
}

// leftRunning is what agents left running, by the minute's sample: ps's
// processes and the process table.
func (c *Core) leftRunning(ctx context.Context, ps []raphael.Proc, t proc.Table, now time.Time) []raphael.Left {
	l := &c.raph.left
	next := map[seenKey]raphael.Seen{}
	var fresh []int // the pids whose files are to read
	see := func(p proc.Proc) seenKey {
		k := seenKey{p.PID, p.Start, p.Name}
		if _, ok := next[k]; ok {
			return k
		}
		if s, ok := l.seen[k]; ok {
			next[k] = s
			return k
		}
		s := raphael.Seen{PID: p.PID, Start: p.Start, Name: p.Name}
		s.Started, _ = p.Started()
		if argv, env, err := proc.Args(p.PID); err == nil {
			s.Args, s.Session, s.Socket = argv, envValue(env, brand.Env("SESSION")), envValue(env, brand.Env("SOCKET"))
		}
		next[k] = s
		fresh = append(fresh, p.PID)
		return k
	}
	type tree struct {
		root seenKey
		path string
		kids []seenKey
	}
	var trees []tree
	for _, cand := range raphael.Candidates(ps, os.Getuid(), c.bin) {
		root, ok := t.Get(cand.PID)
		if !ok || root.Zombie {
			continue
		}
		tr := tree{root: see(root), path: cand.Path}
		for _, k := range t.Tree(root.PID) {
			if !k.Zombie {
				tr.kids = append(tr.kids, see(k))
			}
		}
		trees = append(trees, tr)
	}
	keep := next
	if len(fresh) > 0 {
		files, err := raphael.ReadFiles(ctx, c.raph.run(), fresh)
		if err != nil {
			c.log.Warn("raphael: left running", "err", err)
			// What lsof did not read is read at the next sample.
			keep = maps.Clone(next)
			maps.DeleteFunc(keep, func(k seenKey, _ raphael.Seen) bool { return slices.Contains(fresh, k.pid) })
		}
		for k, s := range next {
			if f, ok := files[k.pid]; ok && slices.Contains(fresh, k.pid) {
				s.Cwd, s.Out = f.Cwd, f.Out
				next[k] = s
			}
		}
	}
	l.seen = keep
	if len(trees) == 0 {
		return nil
	}
	orphans := make([]raphael.Orphan, 0, len(trees))
	for _, tr := range trees {
		o := raphael.Orphan{Root: next[tr.root], Path: tr.path}
		for _, k := range tr.kids {
			o.Kids = append(o.Kids, next[k])
		}
		orphans = append(orphans, o)
	}
	here := raphael.Here{Socket: c.paths.Socket(), Worktrees: c.paths.Worktrees(), Gone: socketGone}
	return raphael.LeftRunning(orphans, c.leftSessions(), here, now)
}

// leftSessions are the sessions as raphael.Whose takes them: the open
// first, then the closed, the last closed first.
func (c *Core) leftSessions() []raphael.Session {
	var ss []raphael.Session
	c.mu.Lock()
	for _, id := range slices.Sorted(maps.Keys(c.sessions)) {
		x := c.sessions[id]
		s := raphael.Session{ID: x.id, Name: x.name, Worktree: x.worktree, Cwd: x.cwd, Open: true,
			Working: x.track.state(x.dead) == ipc.StateWorking}
		if x.vendorID != "" {
			s.Conversations = append(s.Conversations, x.vendorID)
		}
		// Its earlier conversations, before a /clear, have scratchpads too.
		for _, tr := range x.track.own {
			if v := strings.TrimSuffix(filepath.Base(tr), ".jsonl"); v != x.vendorID && !slices.Contains(s.Conversations, v) {
				s.Conversations = append(s.Conversations, v)
			}
		}
		for _, at := range []time.Time{x.created, x.track.requested, x.track.heard, x.userWord} {
			if at.After(s.Quiet) {
				s.Quiet = at
			}
		}
		ss = append(ss, s)
	}
	closed := slices.Clone(c.closes.list)
	c.mu.Unlock()
	l := &c.raph.left
	if l.dirs == nil {
		l.dirs = map[string][2]string{}
	}
	for _, r := range closed {
		if slices.ContainsFunc(ss, func(s raphael.Session) bool { return s.ID == r.ID }) {
			continue
		}
		dirs, ok := l.dirs[r.ID]
		if !ok {
			if spec, err := host.ReadSpec(c.paths.SpecFile(r.ID)); err == nil {
				dirs = [2]string{spec.Worktree, spec.Cwd}
			}
			l.dirs[r.ID] = dirs
		}
		s := raphael.Session{ID: r.ID, Name: r.Name, Worktree: dirs[0], Cwd: dirs[1], Closed: r.Closed}
		if r.VendorID != "" {
			s.Conversations = []string{r.VendorID}
		}
		ss = append(ss, s)
	}
	return ss
}

// socketGone reports whether a core's socket is gone, and so the core.
func socketGone(sock string) bool {
	_, err := os.Lstat(sock)
	return errors.Is(err, fs.ErrNotExist)
}

// envValue is key's value in an environment; "" when it has none.
func envValue(env []string, key string) string {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			return v
		}
	}
	return ""
}
