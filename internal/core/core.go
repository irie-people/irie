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
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"regexp"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/harness"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/journal"
	"github.com/irie-people/irie/internal/localmodel"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/statusline"
	"github.com/irie-people/irie/internal/team"
	"github.com/irie-people/irie/internal/tmux"
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
	conf    atomic.Pointer[config.Config] // the config in effect, which settings swap whole (settings.go)
	confMu  sync.Mutex                    // one edit of the config file at a time
	paths   paths.Paths
	tmux    *tmux.Runner
	bin     string
	version string
	log     *slog.Logger

	spawnMu  turns        // one spawn at a time, a promoted session's first; taken before mu
	sendMu   turns        // one message typed to an agent at a time, likewise
	localMu  sync.Mutex   // one start of the local server at a time, taken before mu
	undoMu   sync.Mutex   // one undo or redo at a time, taken before spawnMu and mu
	http     *http.Client // for local servers
	sampling atomic.Bool  // a memory sample is running
	sweeping atomic.Bool  // the jot dog looks at the finished jots (dog.go)
	fdog     *freezeDog   // the freeze dog, which watches Irie's own windows (freezedog.go); it has a lock of its own

	trodSweeping atomic.Bool // the trod bot looks at the stopped sessions (trod.go)
	awakeMu      sync.Mutex  // guards awake; taken after mu
	awake        *exec.Cmd   // what keeps the Mac awake for the trod bot, while it does

	modelMu    sync.Mutex // guards modelCache; taken before mu
	modelCache modelList

	llamaMu sync.Mutex // one start of Irie's llama-server at a time, taken before modelMu and mu
	ggufMu  sync.Mutex // guards ggufs and builds; taken last
	ggufs   map[string]ggufEntry
	builds  map[string]buildEntry

	mu         sync.Mutex // guards everything below, and serialises tmux calls
	sessions   map[string]*session
	snap       tmux.Snapshot
	published  map[string]ipc.Session // what subscribers last heard
	lastHeader ipc.Header             // the header subscribers last heard
	subs       map[*subscriber]bool
	journal    *journal.Journal
	history    map[string][]event.Event // journal events of sessions not adopted yet
	recent     []string                 // repos used recently, newest first
	appViews   int                      // open subscriptions of the Mac app; while any, the slot is zoomed
	appTheme   string                   // the Mac app's palette, which the status line's agent colours follow; ui.theme until it says
	outside    outside                  // the Claude Code sessions outside Irie (outside.go)
	local      localState               // local.json
	machine    localmodel.Machine       // the machine's memory, last read
	localMem   int64                    // what the models of Irie's own server take
	llamaMem   int64                    // what Irie's own llama-server takes
	plan       ipc.Plan                 // the plan's use, from the newest status update
	planAt     time.Time
	outcomes   map[string]*outcome // every session's route and outcome, those gone included
	order      []string            // the list's order from the top, which the user sets (order.go)
	bg         map[int]bgProc      // the processes Irie put in the background, by pid (priority.go)
	closes     closes              // closed sessions, and agents still ending (close.go)
	steps      stateFile           // state.json: the undo and redo stacks (undo.go)
	selected   string              // the session you picked, whose conversation or terminal the slot shows (view.go)
	raw        bool                // the slot shows the picked session's raw terminal, not its conversation
	split      string              // the session the Mac app shows beside its teammates, in the view session (view.go)
	team       *tmux.Runner        // the team server, where sessions' teammates run (team.go)
	teamSocket string              // its socket path, once known
	trodNow    trodNow             // the trod bot as its last sweep found it (trod.go)
	trodReset  time.Time           // when the plan's usage limit last reset, as far as the trod bot saw
	trodHand   trodHand            // the run of the trod bot you started by hand last (trod.go)
	last       *lastJot            // the jot sent last from a task bar, which the next may add to (follow.go)
}

// session is one registry entry. Its facts come from tmux pane options and
// the session's spec.json, its state from its events.
type session struct {
	id       string
	name     string
	named    bool // the user chose the name, by rename or at spawn: Claude Code's titles leave it be (title.go)
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
	quiet    int        // counts the session's events and messages; a close-when-done timer fires only if it has not moved
	tally    tokenTally // read from the transcripts track names
	tokens   *ipc.Tokens
	model    string                 // the model the agent runs, as its profile said
	local    bool                   // a model of a local runtime
	usage    statusline.Usage       // its last status line update
	memory   int64                  // what its processes take, last measured
	out      *outcome               // its route and how it went, shared with Core.outcomes
	conv     *conversation          // its conversation, read while clients follow it (conversation.go)
	note     string                 // what its agent is told at its next tool call, once (freeze.go)
	demoted  bool                   // its process tree runs in the background, for a promoted session (priority.go)
	convSubs int                    // the subscriptions that follow its conversation
	helpers  map[string]*helperFeed // its helpers' conversations that clients follow, by agent (helpers.go)
	dog      dogState               // what the jot dog found its jot left (dog.go)
	team     []team.Teammate        // its teammates on the team server, last listed (team.go)
	// Jots added to its own, to type to its agent once it can take them,
	// and whether that is under way (follow.go).
	followUps []followUp
	sending   bool
}

func (x *session) wire() ipc.Session {
	s := ipc.Session{
		ID: x.id, Name: x.name, Agent: x.agent, Created: x.created, Warnings: x.warnings,
		Cwd: x.cwd, Repo: x.repo, Worktree: x.worktree, Branch: x.branch, Copied: x.copied,
		Profile: x.profile, VendorID: x.vendorID, Pane: x.pane, Shown: x.shown, Tokens: x.tokens,
		Model: x.model, Local: x.local, Context: x.usage.Context, Memory: x.memory, Team: wireTeam(x.team),
	}
	if s.Model == "" {
		s.Model = x.usage.Model
	}
	// Claude Code prices tokens at Anthropic's list price, which a local
	// model does not charge.
	if !x.local {
		s.CostUSD = x.usage.CostUSD
	}
	if o := x.out; o != nil {
		s.Class, s.Routed, s.Parent, s.Rated = o.class, o.routed, o.parent, o.rated
	}
	x.track.describe(&s, x.dead, x.exitCode)
	// Once its turn is over, a jot's row says what it left that has not
	// landed (DESIGN §4.8).
	if l := wireLoose(x.dog.loose, x.track.nudged); l != nil && dogShows(s.State) {
		s.Loose = l
		if s.State == ipc.StateExited {
			s.Detail += " · not through: " + l.Words()
		} else {
			s.Detail = joinDetail("Not through: "+l.Words(), s.Detail)
		}
	}
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
	c := &Core{
		paths: o.Paths, tmux: r, bin: o.Bin, version: o.Version, log: o.Log, http: httpClient(),
		team:     &tmux.Runner{Bin: r.Bin, Socket: tmux.TeamSocket(r.Socket), Env: teamServerEnv(os.Environ())},
		sessions: map[string]*session{}, published: map[string]ipc.Session{}, subs: map[*subscriber]bool{},
		history: map[string][]event.Event{}, outcomes: map[string]*outcome{},
		fdog: newFreezeDog(),
	}
	c.useConfig(o.Config)
	return c
}

// cfg is the config in effect. A change in the settings swaps in a new one
// whole, so two reads may see two configs, each of them sound.
func (c *Core) cfg() *config.Config { return c.conf.Load() }

func (c *Core) useConfig(cfg config.Config) { c.conf.Store(&cfg) }

// Run is `irie core`. It holds core.lock, builds the layout, then serves the
// socket and polls tmux once a second until it is told to stop.
func Run(o Options) error {
	if err := o.Paths.EnsureRuntime(); err != nil {
		return err
	}
	if err := o.Paths.EnsureState(); err != nil {
		return err
	}
	lock, err := filetx.Lock(o.Paths.CoreLock(), 3*time.Second)
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
	c.loadLocal()
	c.loadOutcomes()
	c.loadOrder()
	c.loadClosed()
	c.loadState()
	c.loadTrodHand()

	// The layout, the registry and the rows' states are in place before the
	// first hello.
	c.mu.Lock()
	c.loadView()
	c.mu.Unlock()
	c.tick()
	go c.sample()
	go c.dogSweep()
	go c.trodSweep()

	os.Remove(o.Paths.Socket()) // stale: core.lock is ours
	l, err := ipc.Listen(o.Paths.Socket())
	if err != nil {
		return err
	}
	defer l.Close()
	go ipc.Serve(l, c)
	go c.watchOutside()
	go c.watchFreezes()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT)
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for n := 1; ; n++ {
		select {
		case <-t.C:
			c.tick()
			if n%sampleEvery == 0 {
				go c.sample()
			}
			if n%dogEvery == 0 {
				go c.dogSweep()
			}
			if n%trodEvery == 0 {
				go c.trodSweep()
			}
		case s := <-stop:
			c.log.Info("core stopping", "signal", s.String())
			return nil
		}
	}
}

// want is the layout to keep. The caller holds c.mu.
func (c *Core) want() tmux.Want {
	w := tmux.Want{Bin: c.bin, SidebarWidth: c.cfg().UI.SidebarWidth, Version: c.version, Zoom: c.appViews > 0}
	if x := c.sessions[c.split]; x != nil && c.appViews > 0 {
		w.Split = x.pane
	}
	return w
}

// tick polls tmux, repairs the layout, records what tmux and the clock say
// and tells subscribers what changed.
func (c *Core) tick() {
	c.mu.Lock()
	if err := c.refresh(); err != nil {
		c.log.Warn("poll", "err", err)
	}
	c.keepView()
	c.followTeams()
	c.reap()
	c.observe(time.Now())
	c.sendFollowUps(time.Now())
	c.keepFrozen()
	c.background()
	c.countTokens()
	c.followTitles()
	c.followConversations()
	c.keepRemote()
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
	var old []*session
	for _, p := range s.ByRole(tmux.RoleAgent) {
		if !sessionID.MatchString(p.IrieSession) || seen[p.IrieSession] {
			continue
		}
		seen[p.IrieSession] = true
		x := c.sessions[p.IrieSession]
		if x == nil {
			x = c.adopt(p)
			c.sessions[x.id] = x
			if p.Named == "" {
				old = append(old, x)
			}
		}
		x.pane, x.dead, x.exitCode, x.command, x.bracket = p.ID, p.Dead, p.DeadStatus, p.Command, p.BracketPaste
		// Shown: its pane is in the slot, or its conversation is, which
		// the placeholder draws.
		x.shown = slot != nil && (slot.ID == p.ID || slot.Role == tmux.RolePlaceholder && !c.rawView() && c.selected == x.id) ||
			c.split == x.id && c.appViews > 0
	}
	for id := range c.sessions {
		if !seen[id] {
			delete(c.sessions, id)
		}
	}
	// A pane from before @irie_named has its name settled once every row
	// is in, so a new title is numbered against them all.
	for _, x := range old {
		c.settle(x)
	}
}

// adopt rebuilds a registry entry from an agent pane, as a restarted core
// does for every session, and replays the session's journal into its state.
func (c *Core) adopt(p tmux.Pane) *session {
	created, _ := time.Parse(time.RFC3339Nano, p.Created)
	x := &session{id: p.IrieSession, name: p.Name, named: p.Named == "1", agent: p.Agent, created: created, vendorID: p.VendorID, out: c.outcome(p.IrieSession)}
	if x.name == "" {
		x.name = x.id
	}
	if spec, err := host.ReadSpec(c.paths.SpecFile(x.id)); err == nil {
		x.cwd, x.profile, x.harness, x.warnings = spec.Cwd, spec.Profile, spec.Harness, spec.Warnings
		x.repo, x.worktree, x.branch, x.copied, x.paste = spec.Repo, spec.Worktree, spec.Branch, spec.Copied, spec.Paste
		x.model, x.local = spec.Model, spec.Local
	}
	c.loadStatus(x)
	if x.harness == "" {
		x.harness = c.cfg().Profiles[x.profile].Harness
	}
	x.observed = true
	if h, err := harnessFor(x.harness); err == nil {
		x.observed = h.Levels().Has(harness.Observed)
	}
	for _, e := range c.history[x.id] {
		x.track.apply(e)
	}
	delete(c.history, x.id)
	c.loadFollowUps(x)
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
	if x.out == nil {
		x.out = c.outcome(x.id)
	}
	x.out.apply(e)
	for s := range c.subs {
		s.event(e)
	}
}

// list returns the rows in the list's order (order.go), each with what
// the trod bot did for it in its last shift. The caller holds c.mu.
func (c *Core) list() []ipc.Session {
	out := make([]ipc.Session, 0, len(c.sessions))
	var from time.Time
	if n := c.cfg().Trod; n.On {
		from, _, _ = n.Shift(time.Now())
	}
	for _, x := range c.sessions {
		s := x.wire()
		if !from.IsZero() {
			s.Trod = trodRow(&x.track, from)
		}
		out = append(out, s)
	}
	c.sortRows(out)
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
		return ipc.HelloResult{Version: c.version, API: ipc.API, Features: []string{
			ipc.OpHello, ipc.OpList, ipc.OpSubscribe, ipc.OpSpawn, ipc.OpShow, ipc.OpHook, ipc.OpComplete,
			ipc.OpAttachInfo, ipc.OpMove, ipc.OpConversation, ipc.OpSend, ipc.OpRename, ipc.OpFlip,
			ipc.OpPreflight, ipc.OpLocal, ipc.OpStatus, ipc.OpRate, ipc.OpRoute, ipc.OpResult, ipc.OpTool,
			ipc.OpClose, ipc.OpReopen, ipc.OpUndo, ipc.OpRedo, ipc.FeatureSpawnModel, ipc.OpTheme,
			ipc.OpFreeze, ipc.OpThaw, ipc.OpPriority, ipc.OpReorder, ipc.OpDog, ipc.OpConfig,
			ipc.OpPark, ipc.OpUnpark, ipc.OpInput, ipc.OpTrod, ipc.FeatureWatch, ipc.OpScreen, ipc.OpKeys, ipc.OpPair, ipc.OpReady,
		}}, nil
	case ipc.OpList:
		// Whoever lists is looking, so the sessions outside Irie are
		// refreshed when they are old; a problem is in the result.
		c.refreshOutside(outsideEvery, false)
		c.mu.Lock()
		defer c.mu.Unlock()
		return ipc.ListResult{Sessions: c.list(), Outside: c.outsideList(), OutsideProblem: c.outside.problem, Closed: c.closedList()}, nil
	case ipc.OpTheme:
		var a ipc.ThemeArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return nil, c.theme(a)
	case ipc.OpClose:
		var a ipc.CloseArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		if err := c.close(a); err != nil {
			return nil, err
		}
		c.recordAction(actClose, a.Session, nil)
		return nil, nil
	case ipc.OpDog:
		return c.dogList()
	case ipc.OpReady:
		return c.ready(), nil
	case ipc.OpTrod:
		var a ipc.TrodArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.trodOp(a)
	case ipc.OpConfig:
		var a ipc.ConfigArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.settings(a)
	case ipc.OpReopen:
		var a ipc.ReopenArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		s, err := c.reopen(a)
		if err != nil {
			return nil, err
		}
		c.recordAction(actReopen, s.ID, nil)
		return ipc.ReopenResult{Session: s}, nil
	case ipc.OpUndo, ipc.OpRedo:
		var a ipc.UndoArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.undo(op, a)
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
		// A jot may add to the one before it (follow.go).
		if (a.Jot || a.Apart != "") && !a.Delegate {
			r, err := c.jot(a)
			if err != nil {
				return nil, err
			}
			return r, nil
		}
		s, err := c.spawn(a, true)
		if err != nil {
			return nil, err
		}
		// A subtask an agent delegates is the agent's doing, not the user's.
		if !a.Delegate {
			c.recordAction(actSpawn, s.ID, nil)
		}
		return ipc.SpawnResult{Session: s}, nil
	case ipc.OpShow:
		var a ipc.ShowArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return nil, c.show(a)
	case ipc.OpRename:
		var a ipc.RenameArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		s, err := c.rename(a)
		if err != nil {
			return nil, err
		}
		return ipc.RenameResult{Session: s}, nil
	case ipc.OpHook:
		var a ipc.HookArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.hook(a)
	case ipc.OpInput:
		var a ipc.InputArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.input(a)
	case ipc.OpComplete:
		var a ipc.CompleteArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		items, notes := c.complete(a.Text)
		return ipc.CompleteResult{Items: items, Notes: notes}, nil
	case ipc.OpFlip:
		return c.flip()
	case ipc.OpSend:
		var a ipc.SendArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return nil, c.send(a)
	case ipc.OpScreen:
		var a ipc.ScreenArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.screen(a)
	case ipc.OpPair:
		var a ipc.PairArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.pair(a)
	case ipc.OpKeys:
		var a ipc.KeysArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return nil, c.keys(a)
	case ipc.OpConversation:
		var a ipc.ConversationArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.conversation(a)
	case ipc.OpAttachInfo:
		return c.attachInfo()
	case ipc.OpPreflight:
		var a ipc.PreflightArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.preflight(a)
	case ipc.OpLocal:
		var a ipc.LocalArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.localStatus(a)
	case ipc.OpStatus:
		var a ipc.StatusArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.status(a)
	case ipc.OpRate:
		var a ipc.RateArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return nil, c.rate(a)
	case ipc.OpRoute:
		var a ipc.RouteArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.routeDry(a)
	case ipc.OpResult:
		var a ipc.ResultArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.result(a)
	case ipc.OpTool:
		var a ipc.ToolArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		s, err := c.tool(a)
		if err != nil {
			return nil, err
		}
		return ipc.SpawnResult{Session: s}, nil
	}
	if r, ok, err := c.control(op, raw); ok {
		return r, err
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
	r := ipc.AttachInfoResult{Tmux: c.tmux.Bin, Socket: sock, Target: tmux.MainSession, View: tmux.ViewSession}
	if c.teamSocket == "" {
		c.teamSocket = filepath.Join(filepath.Dir(sock), c.team.Socket)
	}
	r.Teams = c.teamSocket
	return r, nil
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
	c.split = ""
	if a.Split {
		c.split = a.Session
	}
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
	c.selected = id
	cmds, err := c.viewCmds(x, focus)
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
