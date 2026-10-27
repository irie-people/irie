// Package core is `irie core`, the one process that owns Irie's state and
// drives tmux (DESIGN §2.1). It keeps the layout, spawns and shows sessions,
// takes the agents' hook events, and serves the socket API. Its registry
// lives in tmux pane options and each row's state is derived from the
// journal, so a restarted core rebuilds both.
package core

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"reflect"
	"regexp"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/irie-hq/irie/internal/config"
	"github.com/irie-hq/irie/internal/event"
	"github.com/irie-hq/irie/internal/harness"
	"github.com/irie-hq/irie/internal/host"
	"github.com/irie-hq/irie/internal/ipc"
	"github.com/irie-hq/irie/internal/journal"
	"github.com/irie-hq/irie/internal/paths"
	"github.com/irie-hq/irie/internal/tmux"
)

// Options configure a core.
type Options struct {
	Config  config.Config
	Paths   paths.Paths
	Bin     string // the irie binary the core's panes run
	Version string
	Log     *slog.Logger
}

// Core is the running core.
type Core struct {
	cfg     config.Config
	paths   paths.Paths
	tmux    *tmux.Runner
	bin     string
	version string
	log     *slog.Logger

	spawnMu sync.Mutex // one spawn at a time, taken before mu

	mu        sync.Mutex // guards everything below, and serialises tmux calls
	sessions  map[string]*session
	snap      tmux.Snapshot
	published map[string]ipc.Session // what subscribers last heard
	subs      map[*subscriber]bool
	journal   *journal.Journal
	history   map[string][]event.Event // journal events of sessions not adopted yet
	recent    []string                 // repos used recently, newest first
	appViews  int                      // open subscriptions of the Mac app; while any, the slot is zoomed
	outside   outside                  // the Claude Code sessions outside Irie (outside.go)
}

// session is one registry entry. Its facts come from tmux pane options and
// the session's spec.json, its state from its events.
type session struct {
	id       string
	name     string
	agent    string
	created  time.Time
	cwd      string
	repo     string   // the main checkout of the task's repo, when it runs in one
	worktree string   // the worktree Irie made for the task
	branch   string   // the worktree's branch
	copied   []string // local files copied into the worktree
	profile  string
	harness  string
	observed bool // its harness reports through hooks; else the task is in once the agent runs
	paste    bool // its task is pasted into the terminal
	vendorID string
	warnings []string
	pane     string
	command  string // the pane's foreground command
	bracket  bool   // the agent asked for bracketed paste
	dead     bool
	exitCode int
	shown    bool
	track    track
	tally    tokenTally // read from the transcripts track names
	tokens   *ipc.Tokens
}

func (x *session) wire() ipc.Session {
	s := ipc.Session{
		ID: x.id, Name: x.name, Agent: x.agent, Created: x.created, Warnings: x.warnings,
		Cwd: x.cwd, Repo: x.repo, Worktree: x.worktree, Branch: x.branch, Copied: x.copied,
		Profile: x.profile, VendorID: x.vendorID, Pane: x.pane, Shown: x.shown, Tokens: x.tokens,
	}
	x.track.describe(&s, x.dead, x.exitCode)
	return s
}

// countTokens catches each session's tally up with its transcripts. The
// caller holds c.mu.
func (c *Core) countTokens() {
	for _, x := range c.sessions {
		if t, ok := x.tally.update(x.track.transcripts); ok {
			x.tokens = &t
		}
	}
}

func newCore(o Options, r *tmux.Runner) *Core {
	return &Core{
		cfg: o.Config, paths: o.Paths, tmux: r, bin: o.Bin, version: o.Version, log: o.Log,
		sessions: map[string]*session{}, published: map[string]ipc.Session{}, subs: map[*subscriber]bool{},
		history: map[string][]event.Event{},
	}
}

// Run is `irie core`. It holds core.lock, builds the layout, then serves the
// socket and polls tmux once a second until it is told to stop.
func Run(o Options) error {
	if err := o.Paths.EnsureRuntime(); err != nil {
		return err
	}
	if err := o.Paths.EnsureState(); err != nil {
		return err
	}
	lock, err := lockFile(o.Paths.CoreLock(), 3*time.Second)
	if err != nil {
		return fmt.Errorf("core.lock: %w", err)
	}
	defer lock.Close()
	bin, err := tmux.Find(o.Config.Tmux.Path)
	if err != nil {
		return err
	}
	c := newCore(o, &tmux.Runner{Bin: bin, Socket: o.Config.Tmux.Socket})
	c.log.Info("core starting", "version", c.version, "pid", os.Getpid())
	if c.journal, err = journal.Open(o.Paths.Journal(), journal.Options{KeepDays: o.Config.Journal.KeepDays}); err != nil {
		return fmt.Errorf("journal: %w", err)
	}
	defer c.journal.Close()
	c.loadHistory()
	c.loadRecent()

	// The layout, the registry and the rows' states are in place before the
	// first hello.
	c.tick()

	os.Remove(o.Paths.Socket()) // stale: core.lock is ours
	l, err := ipc.Listen(o.Paths.Socket())
	if err != nil {
		return err
	}
	defer l.Close()
	go ipc.Serve(l, c)
	go c.watchOutside()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT)
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			c.tick()
		case s := <-stop:
			c.log.Info("core stopping", "signal", s.String())
			return nil
		}
	}
}

// want is the layout to keep. The caller holds c.mu.
func (c *Core) want() tmux.Want {
	return tmux.Want{Bin: c.bin, SidebarWidth: c.cfg.UI.SidebarWidth, Version: c.version, Zoom: c.appViews > 0}
}

// tick polls tmux, repairs the layout, records what tmux and the clock say
// and tells subscribers what changed.
func (c *Core) tick() {
	c.mu.Lock()
	if err := c.refresh(); err != nil {
		c.log.Warn("poll", "err", err)
	}
	c.observe(time.Now())
	c.countTokens()
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
}

// refresh takes a snapshot, repairs the layout until it is sound and
// updates the registry. The caller holds c.mu.
func (c *Core) refresh() error {
	snap, err := c.tmux.Snapshot()
	if err != nil {
		return err
	}
	for range 10 {
		cmds := tmux.Plan(snap, c.want())
		if cmds == nil {
			break
		}
		c.log.Info("repair", "cmds", cmds)
		if _, err := c.tmux.RunList(cmds); err != nil {
			c.log.Warn("repair", "err", err)
			break
		}
		if snap, err = c.tmux.Snapshot(); err != nil {
			return err
		}
	}
	c.snap = snap
	c.sync(snap)
	return nil
}

var sessionID = regexp.MustCompile(`^s[0-9a-z]{12}$`)

// sync brings the registry in line with the agent panes in s.
func (c *Core) sync(s tmux.Snapshot) {
	slot := s.SlotPane()
	seen := map[string]bool{}
	for _, p := range s.ByRole(tmux.RoleAgent) {
		if !sessionID.MatchString(p.IrieSession) || seen[p.IrieSession] {
			continue
		}
		seen[p.IrieSession] = true
		x := c.sessions[p.IrieSession]
		if x == nil {
			x = c.adopt(p)
			c.sessions[x.id] = x
		}
		x.pane, x.dead, x.exitCode, x.command, x.bracket = p.ID, p.Dead, p.DeadStatus, p.Command, p.BracketPaste
		x.shown = slot != nil && slot.ID == p.ID
	}
	for id := range c.sessions {
		if !seen[id] {
			delete(c.sessions, id)
		}
	}
}

// adopt rebuilds a registry entry from an agent pane, as a restarted core
// does for every session, and replays the session's journal into its state.
func (c *Core) adopt(p tmux.Pane) *session {
	created, _ := time.Parse(time.RFC3339Nano, p.Created)
	x := &session{id: p.IrieSession, name: p.Name, agent: p.Agent, created: created, vendorID: p.VendorID}
	if x.name == "" {
		x.name = x.id
	}
	if spec, err := host.ReadSpec(c.paths.SpecFile(x.id)); err == nil {
		x.cwd, x.profile, x.harness, x.warnings = spec.Cwd, spec.Profile, spec.Harness, spec.Warnings
		x.repo, x.worktree, x.branch, x.copied, x.paste = spec.Repo, spec.Worktree, spec.Branch, spec.Copied, spec.Paste
	}
	if x.harness == "" {
		x.harness = c.cfg.Profiles[x.profile].Harness
	}
	x.observed = true
	if h, err := harnessFor(x.harness); err == nil {
		x.observed = h.Levels().Has(harness.Observed)
	}
	for _, e := range c.history[x.id] {
		x.track.apply(e)
	}
	delete(c.history, x.id)
	return x
}

// loadHistory reads the journal of the sessions tmux has, from the day the
// oldest of them was created, for adopt to replay.
func (c *Core) loadHistory() {
	snap, err := c.tmux.Snapshot()
	if err != nil {
		return
	}
	ids, oldest := map[string]bool{}, time.Now()
	for _, p := range snap.ByRole(tmux.RoleAgent) {
		ids[p.IrieSession] = true
		if t, err := time.Parse(time.RFC3339Nano, p.Created); err == nil && t.Before(oldest) {
			oldest = t
		}
	}
	if len(ids) == 0 {
		return
	}
	evs, err := journal.Read(c.paths.Journal(), oldest)
	if err != nil {
		c.log.Warn("journal", "err", err)
	}
	for _, e := range evs {
		if ids[e.Session] {
			c.history[e.Session] = append(c.history[e.Session], e)
		}
	}
}

// record journals one event of session x, folds it into x's state and sends
// it to the subscribers that asked for events. The caller holds c.mu.
func (c *Core) record(x *session, e event.Event) {
	e.Session = x.id
	if e.Harness == "" {
		e.Harness = x.harness
	}
	if e.Data == nil {
		e.Data = map[string]any{}
	}
	var err error
	if e, err = c.journal.Append(e); err != nil {
		c.log.Warn("journal", "kind", e.Kind, "err", err)
	}
	x.track.apply(e)
	for s := range c.subs {
		s.event(e)
	}
}

// list returns the rows in creation order. The caller holds c.mu.
func (c *Core) list() []ipc.Session {
	out := make([]ipc.Session, 0, len(c.sessions))
	for _, x := range c.sessions {
		out = append(out, x.wire())
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Created.Equal(out[j].Created) {
			return out[i].Created.Before(out[j].Created)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// changes returns frames for the rows that changed since the last call.
// The caller holds c.mu.
func (c *Core) changes() []ipc.Frame {
	var frames []ipc.Frame
	now := map[string]ipc.Session{}
	for _, s := range c.list() {
		now[s.ID] = s
		if old, ok := c.published[s.ID]; !ok || !reflect.DeepEqual(old, s) {
			frames = append(frames, ipc.Frame{T: ipc.FrameUpsert, Session: &s})
		}
	}
	for id := range c.published {
		if _, ok := now[id]; !ok {
			frames = append(frames, ipc.Frame{T: ipc.FrameRemove, ID: id})
		}
	}
	c.published = now
	return frames
}

// Call answers one request (ipc.Handler).
func (c *Core) Call(op string, raw json.RawMessage) (any, error) {
	switch op {
	case ipc.OpHello:
		var a ipc.HelloArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return ipc.HelloResult{Version: c.version, API: ipc.API, Features: []string{ipc.OpHello, ipc.OpList, ipc.OpSubscribe, ipc.OpSpawn, ipc.OpShow, ipc.OpHook, ipc.OpComplete, ipc.OpAttachInfo, ipc.OpMove}}, nil
	case ipc.OpList:
		// Whoever lists is looking, so the sessions outside Irie are
		// refreshed when they are old; a problem is in the result.
		c.refreshOutside(outsideEvery, false)
		c.mu.Lock()
		defer c.mu.Unlock()
		return ipc.ListResult{Sessions: c.list(), Outside: c.outsideList(), OutsideProblem: c.outside.problem}, nil
	case ipc.OpMove:
		var a ipc.MoveArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		s, err := c.move(a)
		if err != nil {
			return nil, err
		}
		return ipc.MoveResult{Session: s}, nil
	case ipc.OpSpawn:
		var a ipc.SpawnArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		s, err := c.spawn(a)
		if err != nil {
			return nil, err
		}
		return ipc.SpawnResult{Session: s}, nil
	case ipc.OpShow:
		var a ipc.ShowArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return nil, c.show(a)
	case ipc.OpHook:
		var a ipc.HookArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.hook(a)
	case ipc.OpComplete:
		var a ipc.CompleteArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return ipc.CompleteResult{Items: c.complete(a.Text)}, nil
	case ipc.OpAttachInfo:
		return c.attachInfo()
	}
	return nil, ipc.Errorf(ipc.CodeUnknownOp, "unknown op %q", op)
}

// attachInfo tells a terminal outside Irie, such as the Mac app's, how to
// attach to the session that holds the slot.
func (c *Core) attachInfo() (ipc.AttachInfoResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	sock, err := c.tmux.SocketPath()
	if err != nil {
		return ipc.AttachInfoResult{}, err
	}
	return ipc.AttachInfoResult{Tmux: c.tmux.Bin, Socket: sock, Target: tmux.MainSession}, nil
}

func decode(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return ipc.Errorf(ipc.CodeBadArgs, "bad args: %v", err)
	}
	return nil
}

// show swaps a session into the slot, and focuses it when asked.
func (c *Core) show(a ipc.ShowArgs) error {
	if a.Agent != "" && a.Agent != "main" {
		return ipc.Errorf(ipc.CodeNotFound, "session %s has no agent %q", a.Session, a.Agent)
	}
	c.mu.Lock()
	err := c.swapIn(a.Session, a.Focus)
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	return err
}

// swapIn shows a session, and records that the user viewed it. The caller
// holds c.mu.
func (c *Core) swapIn(id string, focus bool) error {
	if err := c.refresh(); err != nil {
		return err
	}
	x := c.sessions[id]
	if x == nil {
		return ipc.Errorf(ipc.CodeNotFound, "no session %q", id)
	}
	cmds, err := tmux.SwapIn(c.snap, x.pane, focus)
	if err != nil {
		return err
	}
	if len(cmds) > 0 {
		if _, err := c.tmux.RunList(cmds); err != nil {
			return err
		}
	}
	c.record(x, event.Event{Source: event.SourceUser, Kind: event.UIShown, Data: map[string]any{"focus": focus}})
	return c.refresh()
}
