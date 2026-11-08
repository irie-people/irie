package core

// The right sidebar's Dock (docs/proposals/right-dock.md §5.2). A session's
// controls come from its repo's Dock file, once the user trusts it, and the
// user's own (internal/dock). A terminal control's command runs in a tmux
// session of its own on the dock server, beside Irie's own server, so a
// control never resizes an agent, and it outlives a restart of the app or
// the core. A copy of a session's control ends once the session is gone; a
// repo's, once no session works in that repo.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/irie-people/irie/internal/dock"
	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/tmux"
	"github.com/irie-people/irie/internal/worktree"
)

// dockTrust is what dock-trust.json keeps: the hashes of the repo Dock files
// the user trusted, the newest last.
type dockTrust struct {
	Hashes []string `json:"hashes"`
}

// dockTrustKeep bounds the hashes kept.
const dockTrustKeep = 200

// dockCopy is a running copy of a control, as the dock server lists it.
type dockCopy struct {
	dead    bool
	status  int
	session string // the session a copy of a session control is for
	repo    string // the repo key a copy of a repo control is for
}

// dockEntry is one control of a session's Dock, with its file.
type dockEntry struct {
	file    dock.File
	control dock.Control
	trusted bool
	name    string // its copy's tmux session
	cwd     string
}

// dockOp answers ipc.OpDock.
func (c *Core) dockOp(a ipc.DockArgs) (ipc.DockResult, error) {
	c.mu.Lock()
	x := c.sessions[a.Session]
	if x == nil {
		c.mu.Unlock()
		return ipc.DockResult{}, ipc.Errorf(ipc.CodeNotFound, "no session %q", a.Session)
	}
	dir, main := x.worktree, x.repo
	if dir == "" {
		dir = x.cwd
	}
	c.mu.Unlock()

	home, _ := os.UserHomeDir()
	base, top := dir, ""
	if dir != "" && filepath.IsAbs(dir) {
		if r, ok, err := worktree.Find(dir); err == nil && ok {
			base, top = r.Top, r.Top
			if main == "" {
				main = r.Main
			}
		}
	}
	if main == "" {
		main = base
	}
	repoKey := shortHash(main)

	trust := c.readDockTrust()
	res := ipc.DockResult{Controls: []ipc.DockControl{}, Files: []ipc.DockFile{}, Mine: c.paths.DockFile()}
	var entries []dockEntry
	if dir != "" {
		if p, src := dock.Find(dir, top); p != "" {
			f := dock.Read(p, src)
			if a.Trust != "" && a.Trust == f.Hash && f.Problem == "" && !slices.Contains(trust.Hashes, f.Hash) {
				trust.Hashes = append(trust.Hashes, f.Hash)
				if len(trust.Hashes) > dockTrustKeep {
					trust.Hashes = trust.Hashes[len(trust.Hashes)-dockTrustKeep:]
				}
				if err := c.writeDockTrust(trust); err != nil {
					return res, err
				}
				c.log.Info("dock trusted", "session", a.Session, "file", p, "hash", f.Hash)
			}
			trusted := f.Problem == "" && slices.Contains(trust.Hashes, f.Hash)
			res.Files = append(res.Files, ipc.DockFile{Path: p, Source: src, Trusted: trusted, Problem: f.Problem})
			if f.Problem == "" && !trusted && len(f.Controls) > 0 {
				res.Untrusted = &ipc.DockUntrusted{Path: p, Hash: f.Hash, Commands: dockCommands(f.Controls)}
			}
			for _, ctl := range f.Controls {
				scopeBase := base
				if ctl.Scope == dock.ScopeRepo {
					scopeBase = main
				} else if ctl.Scope == dock.ScopeGlobal {
					scopeBase = home
				}
				entries = append(entries, dockEntry{file: f, control: ctl, trusted: trusted,
					name: dock.Instance(ctl, src, a.Session, repoKey), cwd: dock.Cwd(ctl, scopeBase, scopeBase, home)})
			}
		}
	}
	if p, src := dock.FindMine(c.paths.DockFile()); p != "" {
		f := dock.Read(p, src)
		res.Files = append(res.Files, ipc.DockFile{Path: p, Source: src, Trusted: true, Problem: f.Problem})
		for _, ctl := range f.Controls {
			scopeBase := base
			if ctl.Scope == dock.ScopeRepo {
				scopeBase = main
			} else if ctl.Scope == dock.ScopeGlobal || base == "" {
				scopeBase = home
			}
			entries = append(entries, dockEntry{file: f, control: ctl, trusted: true,
				name: dock.Instance(ctl, src, a.Session, repoKey), cwd: dock.Cwd(ctl, scopeBase, home, home)})
		}
	}

	copies := c.dockCopies()
	find := func(key string) (*dockEntry, error) {
		for i := range entries {
			if dock.Key(entries[i].file.Source, entries[i].control.ID) == key {
				e := &entries[i]
				if e.control.Type != dock.Terminal {
					return nil, ipc.Errorf(ipc.CodeBadArgs, "%s is a web page, not a command", e.control.Title)
				}
				if !e.trusted {
					return nil, ipc.Errorf(ipc.CodeBusy, "%s waits for you to trust %s", e.control.Title, e.file.Path)
				}
				return e, nil
			}
		}
		return nil, ipc.Errorf(ipc.CodeNotFound, "no Dock control %q for this session", key)
	}
	start := func(e *dockEntry) error {
		return c.dockStart(e, a.Session, repoKey)
	}
	switch {
	case a.Stop != "":
		e, err := find(a.Stop)
		if err != nil {
			return res, err
		}
		if _, ok := copies[e.name]; ok {
			if _, err := c.dock.Run("kill-session", "-t", "="+e.name); err != nil && !errors.Is(err, tmux.ErrNoServer) {
				return res, err
			}
		}
	case a.Start != "":
		e, err := find(a.Start)
		if err != nil {
			return res, err
		}
		if cp, ok := copies[e.name]; !ok || cp.dead {
			if ok {
				_, _ = c.dock.Run("kill-session", "-t", "="+e.name)
			}
			if err := start(e); err != nil {
				return res, err
			}
		}
	case a.Restart != "":
		e, err := find(a.Restart)
		if err != nil {
			return res, err
		}
		if _, ok := copies[e.name]; ok {
			_, _ = c.dock.Run("kill-session", "-t", "="+e.name)
		}
		if err := start(e); err != nil {
			return res, err
		}
	}
	if a.Ensure {
		// A copy that ended stays, with its last screen, until it is
		// restarted; only a control with no copy at all starts.
		for i := range entries {
			e := &entries[i]
			if e.trusted && e.control.Type == dock.Terminal {
				if _, ok := copies[e.name]; !ok {
					if err := start(e); err != nil {
						c.log.Warn("dock start", "session", a.Session, "control", e.control.ID, "err", err)
					}
				}
			}
		}
	}
	if a.Stop != "" || a.Start != "" || a.Restart != "" || a.Ensure {
		copies = c.dockCopies()
	}

	for _, e := range entries {
		ctl := e.control
		w := ipc.DockControl{Key: dock.Key(e.file.Source, ctl.ID), ID: ctl.ID, Title: ctl.Title, Type: ctl.Type, URL: ctl.URL,
			Chrome: ctl.Chrome == nil || *ctl.Chrome, Height: ctl.Height, Source: e.file.Source, Scope: ctl.Scope, Cwd: e.cwd, Command: ctl.Command}
		switch {
		case !e.trusted:
			w.State = ipc.DockStateUntrusted
		case ctl.Type == dock.Browser:
			w.State = ipc.DockStateReady
		default:
			if cp, ok := copies[e.name]; ok {
				w.Target = e.name
				w.State = ipc.DockStateRunning
				if cp.dead {
					w.State = ipc.DockStateExited
					code := cp.status
					w.Exit = &code
				}
			} else {
				w.State = ipc.DockStateStopped
			}
		}
		res.Controls = append(res.Controls, w)
	}
	if len(copies) > 0 {
		res.Tmux = c.tmux.Bin
		res.Socket = c.dockSocketPath()
	}
	return res, nil
}

// dockStart starts a copy of e's command in its own tmux session on the dock
// server, in the user's login shell as cmux does, and starts the server
// first when it is not running.
func (c *Core) dockStart(e *dockEntry, session, repoKey string) error {
	if st, err := os.Stat(e.cwd); err != nil || !st.IsDir() {
		return ipc.Errorf(ipc.CodeBadArgs, "%s: %s is not a folder", e.control.Title, e.cwd)
	}
	conf := c.paths.DockConf()
	if old, _ := os.ReadFile(conf); string(old) != tmux.DockConf() {
		if err := filetx.WriteFile(conf, []byte(tmux.DockConf()), 0o600); err != nil {
			return err
		}
	}
	shell := os.Getenv("SHELL")
	if shell == "" || !filepath.IsAbs(shell) {
		shell = "/bin/sh"
	}
	args := []string{"new-session", "-d", "-s", e.name, "-x", "100", "-y", "30", "-c", e.cwd}
	keys := make([]string, 0, len(e.control.Env))
	for k := range e.control.Env {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		args = append(args, "-e", k+"="+e.control.Env[k])
	}
	args = append(args, "--", shell, "-lc", e.control.Command)
	if _, err := c.dock.StartWith(conf, args...); err != nil {
		return fmt.Errorf("start %s: %w", e.control.Title, err)
	}
	c.log.Info("dock started", "session", session, "control", e.control.ID, "copy", e.name, "cwd", e.cwd)
	// What the copy is for, so the sweep can end it once that is gone.
	owner, who := "@irie_dock_session", session
	switch e.control.Scope {
	case dock.ScopeRepo:
		owner, who = "@irie_dock_repo", repoKey
	case dock.ScopeGlobal:
		return nil
	}
	_, err := c.dock.Run("set-option", "-t", "="+e.name+":", owner, who)
	return err
}

// dockCopies lists the copies on the dock server, by tmux session; none
// while it is not running.
func (c *Core) dockCopies() map[string]dockCopy {
	out, err := c.dock.Run("list-sessions", "-F", "#{session_name}\t#{pane_dead}\t#{pane_dead_status}\t#{@irie_dock_session}\t#{@irie_dock_repo}")
	if err != nil {
		if !errors.Is(err, tmux.ErrNoServer) {
			c.log.Warn("dock list", "err", err)
		}
		return map[string]dockCopy{}
	}
	return parseDockCopies(out)
}

func parseDockCopies(out string) map[string]dockCopy {
	copies := map[string]dockCopy{}
	// Only newlines end a line: an empty last field leaves a tab at its end.
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if f[0] == "" {
			continue
		}
		for len(f) < 5 {
			f = append(f, "")
		}
		status, _ := strconv.Atoi(f[2])
		copies[f[0]] = dockCopy{dead: f[1] == "1", status: status, session: f[3], repo: f[4]}
	}
	return copies
}

// dockSweep ends the copies whose session is gone, and the copies of repo
// controls once no session works in their repo (DESIGN §2.3). It runs git,
// so it never holds c.mu while it does.
func (c *Core) dockSweep() {
	copies := c.dockCopies()
	if len(copies) == 0 {
		return
	}
	c.mu.Lock()
	live := map[string]bool{}
	var places []string
	for id, x := range c.sessions {
		live[id] = true
		p := x.repo
		if p == "" {
			p = x.worktree
		}
		if p == "" {
			p = x.cwd
		}
		places = append(places, p)
	}
	c.mu.Unlock()
	repos := map[string]bool{}
	for _, p := range places {
		repos[shortHash(p)] = true
		if r, ok, err := worktree.Find(p); err == nil && ok {
			repos[shortHash(r.Main)] = true
		}
	}
	for name, cp := range copies {
		if (cp.session != "" && !live[cp.session]) || (cp.repo != "" && !repos[cp.repo]) {
			if _, err := c.dock.Run("kill-session", "-t", "="+name); err != nil && !errors.Is(err, tmux.ErrNoServer) {
				c.log.Warn("dock sweep", "copy", name, "err", err)
			}
		}
	}
}

// dockEnd ends the copies of session id's controls, as it closes. The
// sweep would end them too, a little later.
func (c *Core) dockEnd(id string) {
	for name, cp := range c.dockCopies() {
		if cp.session == id {
			if _, err := c.dock.Run("kill-session", "-t", "="+name); err != nil && !errors.Is(err, tmux.ErrNoServer) {
				c.log.Warn("dock end", "copy", name, "err", err)
			}
		}
	}
}

// dockSocketPath is the dock server's socket, beside Irie's own.
func (c *Core) dockSocketPath() string {
	if p, err := c.dock.SocketPath(); err == nil && p != "" {
		return p
	}
	if sock, err := c.tmux.SocketPath(); err == nil {
		return filepath.Join(filepath.Dir(sock), c.dock.Socket)
	}
	return ""
}

func (c *Core) readDockTrust() dockTrust {
	var t dockTrust
	data, err := os.ReadFile(c.paths.DockTrust())
	if err == nil {
		_ = json.Unmarshal(data, &t)
	}
	return t
}

func (c *Core) writeDockTrust(t dockTrust) error {
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return filetx.WriteFile(c.paths.DockTrust(), append(data, '\n'), 0o600)
}

// dockCommands says what a file's controls would run or show, for the trust
// prompt.
func dockCommands(cs []dock.Control) []string {
	out := make([]string, 0, len(cs))
	for _, ctl := range cs {
		what := ctl.Command
		if ctl.Type == dock.Browser {
			what = "shows " + ctl.URL
		}
		out = append(out, ctl.Title+": "+what)
	}
	return out
}

// shortHash names a folder in a tmux session's name.
func shortHash(s string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(s)))
	return hex.EncodeToString(sum[:])[:10]
}
