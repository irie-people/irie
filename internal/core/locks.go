package core

// Agent locks (DESIGN §4.21, internal/lock). As each session's agent starts,
// at a spawn, a resume, a fork or a reopen, the core writes its lock to
// sessions/<id>/lock.json: its harness and the harness's version, its
// profile, model and permission mode, its MCP servers with their packages,
// Irie's plugins and skills with a hash of each, and its hooks. A check asks
// OSV.dev about the harness and package versions the open sessions' locks
// name, and the peers' rows, only when asked (irie lock --check), or once a
// day with lock.check = "daily". What it found is kept in lock-check.json,
// and marks each row whose versions have a known advisory, a peer's too.

import (
	"context"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/harness"
	"github.com/irie-people/irie/internal/harness/claude"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/lock"
	"github.com/irie-people/irie/internal/plugins"
)

const (
	// lockEvery is how often the core looks whether the day's check is
	// due, in ticks.
	lockEvery = 600
	// lockDaily is how long a check lasts with lock.check = "daily".
	lockDaily = 24 * time.Hour
	// lockCheckMost bounds one check, every request to OSV.dev in it.
	lockCheckMost = 2 * time.Minute
)

// lockState is what the core knows of the checks: what they found, which
// rows show. c.mu guards it; checkMu lets one check run at a time and
// writeMu one session's lock file be written at a time.
type lockState struct {
	results lock.Results
	by      map[string]ipc.LockFound // results by lock.Key
	checkMu sync.Mutex
	writeMu sync.Mutex
}

// loadLocks reads what the last checks found, so the marks outlast a
// restart.
func (c *Core) loadLocks() {
	r, err := lock.LoadResults(c.lockResultsFile())
	if err != nil {
		c.log.Warn("locks", "err", err)
	}
	c.mu.Lock()
	c.locks.results, c.locks.by = r, r.By()
	c.mu.Unlock()
}

func (c *Core) lockResultsFile() string { return filepath.Join(c.paths.State, lock.ResultsFile) }

// lockAt is what started a job's session, for its lock.
func lockAt(j job) string {
	switch {
	case j.reopen != nil:
		return ipc.LockReopen
	case j.data["kind"] == "fork":
		return ipc.LockFork
	case j.resume != "":
		return ipc.LockResume
	}
	return ipc.LockSpawn
}

// lockLater writes the lock of x, which launch just started from j, s and
// its command line and files, without holding up the launch: what it
// reads, Claude Code's config and the files of Irie's skills among it, is
// read meanwhile. The caller holds c.mu.
func (c *Core) lockLater(x *session, j job, s harness.Spawn, argv []string, files map[string][]byte) {
	command := j.run.Command
	if command == "" && j.h.ID() == "claude-code" {
		command = "claude"
	}
	in := lock.Input{
		Session: x.id, At: lockAt(j), Now: time.Now(),
		Irie: c.version, IrieBin: c.bin,
		Harness: j.h.ID(), Version: j.probe.Version, Profile: j.profile, Model: x.model,
		Argv: slices.Clone(argv), Settings: files[claude.SettingsFile], Strict: s.StrictMCP,
		Home: s.Home, ClaudeDir: claude.ConfigDir(s.Home, s.Environ), Cwd: x.cwd,
	}
	for _, name := range slices.Sorted(maps.Keys(s.MCP)) {
		in.MCP = append(in.MCP, lock.Server{Name: name, From: ipc.LockFromIrie, Command: s.MCP[name].Command, Args: slices.Clone(s.MCP[name].Args)})
	}
	cfg, env := *c.cfg(), s.Environ
	go func() {
		// A lock is a record: what goes wrong making one never stops
		// the core.
		defer func() {
			if r := recover(); r != nil {
				c.log.Error("lock", "session", x.id, "panic", r)
			}
		}()
		if command != "" {
			in.Binary, _ = host.LookPath(command, env)
		}
		in.Plugins = lockPlugins(cfg, c.paths.Plugins())
		c.writeLock(x, lock.Gather(in))
	}()
}

// lockPlugins are Irie's plugins that are on in cfg, then yours from the
// manifests in dir that are on.
func lockPlugins(cfg config.Config, dir string) []lock.Plugin {
	var out []lock.Plugin
	for _, p := range plugins.All() {
		if p.IsOn(cfg) {
			out = append(out, lock.Plugin{Name: p.Name})
		}
	}
	mine, _ := plugins.Mine(dir)
	for _, p := range mine {
		if p.IsOn(cfg) {
			out = append(out, lock.Plugin{Name: p.Name, Manifest: p.Manifest})
		}
	}
	return out
}

// writeLock puts l in x's folder and on its row, while x is still the
// session of its id: a lock of a run since gone is never written over a
// newer one's. One lock file is written at a time, so the newest run's
// lock always comes last.
func (c *Core) writeLock(x *session, l ipc.Lock) {
	c.locks.writeMu.Lock()
	defer c.locks.writeMu.Unlock()
	c.mu.Lock()
	current := c.sessions[x.id] == x
	c.mu.Unlock()
	if !current {
		return
	}
	if err := lock.Write(c.paths.Session(x.id), l); err != nil {
		c.log.Warn("lock", "session", x.id, "err", err)
		return
	}
	c.mu.Lock()
	x.lockPkgs = lock.Packages(l)
	frames := c.changesOf(x)
	c.mu.Unlock()
	c.publish(frames)
	c.log.Info("locked", "session", x.id, "at", l.At, "harness", l.Harness.Name, "version", l.Harness.Version, "mcp", len(l.MCP), "notes", len(l.Notes))
}

// readLockPackages is what session id's lock names that a check asks
// about, read from its folder; none when it has no lock.
func (c *Core) readLockPackages(id string) []ipc.LockPackage {
	l, err := lock.Read(c.paths.Session(id))
	if err != nil {
		return nil
	}
	return lock.Packages(l)
}

// lockRow is what x's row says of its lock, marked by the last check. The
// caller holds c.mu.
func (c *Core) lockRow(pkgs []ipc.LockPackage) *ipc.LockRow {
	return lock.Row(lock.Mark(pkgs, c.locks.by))
}

// lockPackages are the package versions the open sessions' locks name and
// the peers' rows, each once, with the names of the sessions that run
// it: this machine's sessions the oldest first, then each peer's, the
// peers by name. The caller holds c.mu.
func (c *Core) lockPackages() ([]ipc.LockPackage, map[string][]string) {
	var pkgs []ipc.LockPackage
	names := map[string][]string{}
	add := func(name string, own []ipc.LockPackage) {
		for _, p := range own {
			p = ipc.LockPackage{Ecosystem: p.Ecosystem, Name: p.Name, Version: p.Version, Part: p.Part}
			k := lock.Key(p)
			if _, ok := names[k]; !ok {
				pkgs = append(pkgs, p)
			}
			if !slices.Contains(names[k], name) {
				names[k] = append(names[k], name)
			}
		}
	}
	// Ids sort by when their sessions were made (newID).
	for _, id := range slices.Sorted(maps.Keys(c.sessions)) {
		add(c.sessions[id].name, c.sessions[id].lockPkgs)
	}
	for _, name := range c.peerNames() {
		p := c.peers[name]
		rows := slices.SortedFunc(maps.Values(p.rows), func(a, b ipc.Session) int { return a.Order - b.Order })
		for _, s := range rows {
			if s.Host == "" && s.Lock != nil {
				add(s.Name, s.Lock.Packages)
			}
		}
	}
	return pkgs, names
}

// checkLocks asks OSV.dev about every package version the locks name,
// keeps what it found and marks the rows. One check runs at a time; a
// check asked for while another runs waits for it, then asks again.
func (c *Core) checkLocks() error {
	c.locks.checkMu.Lock()
	defer c.locks.checkMu.Unlock()
	return c.checkLocksHeld()
}

// checkLocksHeld is checkLocks for a caller that holds checkMu.
func (c *Core) checkLocksHeld() error {
	c.mu.Lock()
	pkgs, _ := c.lockPackages()
	prev := c.locks.by
	c.mu.Unlock()
	// With nothing to ask about, OSV.dev is not asked, and the last
	// check stays the last.
	if len(pkgs) == 0 {
		return nil
	}
	osv, now := c.cfg().Lock.OSV, time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), lockCheckMost)
	defer cancel()
	k := lock.Checker{Base: osv, Agent: brand.Slug + " (" + brand.Repo + ")"}
	found, err := k.Check(ctx, pkgs, prev, now)
	if err != nil {
		c.log.Warn("locks: check", "err", err)
		return ipc.Errorf(ipc.CodeInternal, "%v", err)
	}
	n := 0
	for _, f := range found {
		n += len(f.Advisories)
	}
	c.mu.Lock()
	c.locks.results = c.locks.results.Merge(found, osv, now)
	c.locks.by = c.locks.results.By()
	results := c.locks.results
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	if err := results.Save(c.lockResultsFile()); err != nil {
		c.log.Warn("locks: keep", "err", err)
	}
	c.log.Info("locks checked", "packages", len(pkgs), "advisories", n)
	return nil
}

// lockSweep runs the day's check with lock.check = "daily", once the last
// is a day old, unless a check runs already.
func (c *Core) lockSweep() {
	if c.cfg().Lock.Check != config.LockCheckDaily {
		return
	}
	if !c.locks.checkMu.TryLock() {
		return
	}
	defer c.locks.checkMu.Unlock()
	c.mu.Lock()
	last := c.locks.results.Checked
	c.mu.Unlock()
	if time.Since(last) >= lockDaily {
		c.checkLocksHeld()
	}
}

// lockOp answers the lock op: the locks of every open session, then the
// peers' rows, or of the one named, and what the last check found for the
// versions they name; with check, after asking OSV.dev first.
func (c *Core) lockOp(a ipc.LockArgs) (ipc.LockResult, error) {
	if a.Check {
		if err := c.checkLocks(); err != nil {
			return ipc.LockResult{}, err
		}
	}
	type want struct {
		s    ipc.LockSession
		pkgs []ipc.LockPackage
	}
	var wants []want
	c.mu.Lock()
	rows := c.list()
	ref := strings.TrimSpace(a.Session)
	found := ref == ""
	for _, s := range rows {
		if ref != "" && s.ID != ref && !strings.EqualFold(s.Name, ref) {
			continue
		}
		found = true
		w := want{s: ipc.LockSession{ID: s.ID, Name: s.Name, Host: s.Host, Open: true}}
		if s.Lock != nil {
			w.pkgs = s.Lock.Packages
		}
		wants = append(wants, w)
		if ref != "" {
			break
		}
	}
	if !found {
		// A closed session keeps its lock until it is reopened.
		for _, r := range c.closes.list {
			if r.ID == ref {
				found = true
				wants = append(wants, want{s: ipc.LockSession{ID: r.ID, Name: r.Name}})
				break
			}
		}
	}
	by := c.locks.by
	res := ipc.LockResult{Checked: c.locks.results.Checked, OSV: c.cfg().Lock.OSV, Asked: a.Check, Daily: c.cfg().Lock.Check == config.LockCheckDaily}
	_, names := c.lockPackages()
	c.mu.Unlock()
	if !found {
		return ipc.LockResult{}, ipc.Errorf(ipc.CodeNotFound, "no session %q", ref)
	}
	seen := map[string]bool{}
	for _, w := range wants {
		s := w.s
		if s.Host == "" {
			l, err := lock.Read(c.paths.Session(s.ID))
			if err == nil {
				s.Lock = &l
				w.pkgs = lock.Packages(l)
			} else {
				s.None = err.Error()
			}
		}
		s.Packages = lock.Mark(w.pkgs, by)
		for _, p := range s.Packages {
			k := lock.Key(p)
			if seen[k] {
				continue
			}
			seen[k] = true
			f := ipc.LockFound{Ecosystem: p.Ecosystem, Name: p.Name, Version: p.Version, Part: p.Part, Sessions: names[k]}
			if known, ok := by[k]; ok {
				f.Checked, f.Advisories = known.Checked, known.Advisories
			}
			if len(f.Sessions) == 0 {
				f.Sessions = []string{s.Name}
			}
			res.Packages = append(res.Packages, f)
		}
		res.Sessions = append(res.Sessions, s)
	}
	return res, nil
}
