// Package core is `irie core`, the one process that owns Irie's state and
// drives tmux (DESIGN §2.1). It keeps the layout, spawns and shows sessions,
// takes the agents' hook events, and serves the socket API. Its registry
// lives in tmux pane options and each row's state is derived from the
// journal, so a restarted core rebuilds both.
package core

import (
	"context"
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

	"github.com/irie-people/irie/internal/brand"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/filetx"
	"github.com/irie-people/irie/internal/harness"
	"github.com/irie-people/irie/internal/host"
	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/journal"
	"github.com/irie-people/irie/internal/localmodel"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/perf"
	"github.com/irie-people/irie/internal/statusline"
	"github.com/irie-people/irie/internal/sysload"
	"github.com/irie-people/irie/internal/team"
	"github.com/irie-people/irie/internal/tmux"
	"github.com/irie-people/irie/internal/ui"
)

// Options configure a core.
type Options struct {
	Config  config.Config
	Paths   paths.Paths
	Bin     string // the irie binary the core's panes run
	Version string
	Log     *slog.Logger
	// ClaudeDir is Claude Code's config folder, where the Mac app's
	// palette goes as Irie's theme of Claude Code's (the theme op); ""
	// writes none, as in tests.
	ClaudeDir string
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
	claude  string // Claude Code's config folder (Options.ClaudeDir)

	spawnMu     turns        // one spawn at a time, a promoted session's first; taken before mu
	sendMu      turns        // one message typed to an agent at a time, likewise
	switchMu    sync.Mutex   // one model switch at a usage limit at a time, taken before sendMu and mu (fallback.go)
	localMu     sync.Mutex   // one start of the local server at a time, taken before mu
	undoMu      sync.Mutex   // one undo or redo at a time, taken before spawnMu and mu
	http        *http.Client // for local servers
	sampling    atomic.Bool  // a memory sample is running
	gabSweeping atomic.Bool  // Gabriel's sweep is running (gabriel.go)
	sweeping    atomic.Bool  // the jot dog looks at the finished jots (dog.go)
	fdog        *freezeDog   // the freeze dog, Raphael's watch of Irie's own windows (freezedog.go); it has a lock of its own
	raph        raphState    // what Raphael knows of the computer and does for it (raphael.go); it has a lock of its own
	ctxs        contexts     // the context store and the sessions waiting to be synced into it (ctx.go); locks of its own
	pad         notepad      // the person's notes for later (notepad.go); it has a lock of its own, never held with mu

	trodSweeping atomic.Bool // the trod bot looks at the stopped sessions (trod.go)
	awakeMu      sync.Mutex  // guards awake; taken after mu
	awake        *exec.Cmd   // what keeps the Mac awake for the trod bot, while it does

	lidSweeping atomic.Bool // the lid's look runs (lid.go)
	lidMu       sync.Mutex  // guards lid; taken after mu and awakeMu
	lid         lidHold     // what the core did to the Mac's sleep, for a closed lid

	leftSweeping atomic.Bool // the look for Iries whose run is over (leftovers.go)

	// What the freeze dog knows of Irie's own processes, to keep them
	// running (steady.go): the tmux server's pid, which only the dog reads
	// and writes, and the sidebar's process group, which each tick sets.
	server       int
	sidebarGroup atomic.Int64

	modelMu    sync.Mutex // guards modelCache; taken before mu
	modelCache modelList

	llamaMu sync.Mutex // one start of Irie's llama-server at a time, taken before modelMu and mu
	ggufMu  sync.Mutex // guards ggufs and builds; taken last
	ggufs   map[string]ggufEntry
	builds  map[string]buildEntry

	started time.Time // when the core started counting (perf.go)
	perfOps perf.Set  // each request's time to answer, by op
	perf    perf.Set  // the core's own work and what it sent (ipc.PerfResult)

	mu         perf.Mutex // guards everything below, and serialises tmux calls; it times its waits and holds
	sessions   map[string]*session
	snap       tmux.Snapshot
	published  map[string]ipc.Session // what subscribers last heard
	lastHeader ipc.Header             // the header subscribers last heard
	subs       map[*subscriber]bool
	journal    *journal.Journal
	history    map[string][]event.Event // journal events of sessions not adopted yet
	recent     []string                 // repos used recently, newest first
	appViews   int                      // open subscriptions of the Mac app; while any, the slot is zoomed
	sizes      slotSizes                // the sizes the app shows each session's terminal at (sizes.go)
	appTheme   string                   // the Mac app's palette, which the status line's agent colours follow; ui.theme until it says
	appPalette *ui.Palette              // the app's palette, which appTheme names, by its colours
	appColors  *ipc.Theme               // the same, as the header sends it to the other clients
	outside    outside                  // the Claude Code sessions outside Irie (outside.go)
	local      localState               // local.json
	machine    localmodel.Machine       // the machine's memory, last read
	load       sysload.Load             // how busy the machine is, last read
	mach       *ipc.Machine             // what this core says of its machine to its peers (machine.go)
	machAt     time.Time                // when it was last checked
	signInAt   time.Time                // when Claude Code last said whether it is signed in (machine.go)
	localMem   int64                    // what the models of Irie's own server take
	llamaMem   int64                    // what Irie's own llama-server takes
	plan       ipc.Plan                 // the plan's use: each window's newest figure (status.go)

	outcomes   map[string]*outcome   // every session's route and outcome, those gone included
	labels     routeLabels           // the tasks given a class, which the router sorts by (classify.go); its own lock
	limits     map[string]time.Time  // when each model family's own usage limit last stopped a turn, such as fable's (fallback.go)
	order      []string              // the list's order from the top, which the user sets (order.go)
	spaces     []ipc.Space           // the spaces, the first first (spaces.go)
	groups     []ipc.Group           // your groups, in the order they were made (groups.go)
	hues       map[string]int        // the colours the core gave the sessions, the closed ones' too (hue.go)
	bg         map[int]bgProc        // the processes Irie put in the background, by pid (priority.go)
	gab        gabState              // what Gabriel sees and holds back (gabriel.go)
	closes     closes                // closed sessions, and agents still ending (close.go)
	steps      stateFile             // state.json: the undo and redo stacks (undo.go)
	selected   string                // the session you picked, whose conversation or terminal the slot shows (view.go)
	peers      map[string]*peerState // the other machines' cores this one is paired with, by name (peers.go)
	benches    benchState            // the benches this core started, and the playbooks (bench.go); a lock of its own, but its playbooks are under mu
	tunnelAt   map[string]time.Time  // when each peer's tunnel last started (tunnel.go)
	ownHelpers ownHelperKeep         // your helpers that the core keeps running (ownhelpers.go)
	raw        bool                  // the slot shows the picked session's raw terminal, not its conversation
	split      string                // the session the Mac app shows beside its teammates, in the view session (view.go)
	team       *tmux.Runner          // the team server, where sessions' teammates run (team.go)
	dock       *tmux.Runner          // the dock server, where the Dock's terminal controls run (dock.go)
	teamSocket string                // its socket path, once known
	trodNow    trodNow               // the trod bot as its last sweep found it (trod.go)
	trodReset  time.Time             // when the plan's usage limit last reset, as far as the trod bot saw
	trodHand   trodHand              // the run of the trod bot you started by hand last (trod.go)
	lastThaw   time.Time             // when Irie last thawed a session, which a thaw in turn waits thawGap after: the trod bot's and a thaw of several (freeze.go)
	trodHeld   bool                  // whether its last sweep held its thaws for a signed-out agent
	meter      meter                 // the prompt-cache meter's record of the last week's rebuilds (rebuilds.go)
	locks      lockState             // what the agent locks' checks found (locks.go)
	last       *lastJot              // the jot sent last from a task bar, which the next may add to (follow.go)
	vpn        vpnState              // Mullvad VPN, while vpn.on (vpn.go)
	variorum   variorumState         // Variorum's door: the note each session named last (variorum.go)
	mail       mailState             // the mail plugin's accounts and connections (mail.go)
	yoink      yoinkState            // the yoink plugin's downloads (yoink.go)
}

// session is one registry entry. Its facts come from tmux pane options and
// the session's spec.json, its state from its events.
type session struct {
	id        string
	name      string
	named     bool // the user chose the name, by rename or at spawn: Claude Code's titles leave it be (title.go)
	agent     string
	created   time.Time
	cwd       string
	repo      string   // the main checkout of the task's repo, when it runs in one
	worktree  string   // the worktree Irie made for the task
	branch    string   // the worktree's branch
	copied    []string // local files copied into the worktree
	profile   string
	harness   string
	observed  bool // its harness reports through hooks; else the task is in once the agent runs
	paste     bool // its task is pasted into the terminal
	vendorID  string
	warnings  []string
	pane      string
	command   string // the pane's foreground command
	bracket   bool   // the agent asked for bracketed paste
	dead      bool
	exitCode  int
	shown     bool
	track     track
	quiet     int        // counts the session's events and messages; a close-when-done timer fires only if it has not moved
	tally     tokenTally // read from the transcripts track names
	statusN   uint64     // its status line updates, counted for perf
	tokens    *ipc.Tokens
	model     string                 // the model the agent runs, as its profile said; track.model once it switched
	switching *modelSwitch           // Irie's switch of its model at a usage limit, under way (fallback.go)
	local     bool                   // a model of a local runtime
	tutor     string                 // its tutor mode, explain or teach; "" when off (learn.tutor)
	anon      string                 // the compartment it researches in, anonymously; "" when it doesn't (anon.go)
	anonPorts [2]int                 // its forwarder's SOCKS5 and HTTP ports on 127.0.0.1, while anon is set
	usage     statusline.Usage       // its last status line update
	memory    int64                  // what its processes take, last measured
	cpu       float64                // its processes' share of the CPUs, last measured, in percent of one (memory.go)
	out       *outcome               // its route and how it went, shared with Core.outcomes
	conv      *conversation          // its conversation, read while clients follow it (conversation.go)
	note      string                 // what its agent is told at its next tool call, once (freeze.go)
	refreeze  bool                   // a lost session that was frozen, to freeze again once its agent starts (lost.go)
	demoted   bool                   // its process tree runs in the background, for a promoted session or Gabriel (priority.go)
	userWord  time.Time              // when the user last messaged or started it, which keeps it in their focus (gabriel.go)
	convSubs  int                    // the subscriptions that follow its conversation
	screen    *screenFeed            // its terminal, read while clients watch it live (screenfeed.go)
	helpers   map[string]*helperFeed // its helpers' conversations that clients follow, by agent (helpers.go)
	dog       dogState               // what the jot dog found its jot left (dog.go)
	trodCold  string                 // why the trod bot's last sweep left it be for the price of waking it (trod.go); "" when it did not
	team      []team.Teammate        // its teammates on the team server, last listed (team.go)
	stopped   map[string]bool        // its helpers you stopped, whose tool calls are turned away until they end (sub.go)
	hue       int                    // which of the agents' colours it takes, from 1 (hue.go); 0 until it has one
	// Jots added to its own, to type to its agent once it can take them,
	// and whether that is under way (follow.go).
	followUps []followUp
	sending   bool
	// keptOut says its agent and Irie's helpers came out of the background
	// once since this core found the session (keepup.go).
	keptOut bool
	// lockPkgs are what its agent lock names that a check asks OSV.dev
	// about: its harness's package and its servers' (locks.go).
	lockPkgs []ipc.LockPackage
}

func (x *session) wire() ipc.Session {
	s := ipc.Session{
		ID: x.id, Name: x.name, Agent: x.agent, Created: x.created, Warnings: x.warnings,
		Cwd: x.cwd, Repo: x.repo, Worktree: x.worktree, Branch: x.branch, Copied: x.copied,
		Profile: x.profile, VendorID: x.vendorID, Pane: x.pane, Shown: x.shown, Tokens: x.tokens,
		Model: x.model, Local: x.local, Context: x.usage.Context, Memory: shownMemory(x.memory), Team: wireTeam(x.team), Hue: x.hue,
		Tutor: x.tutor, Anon: x.anon,
	}
	if m := x.track.model; m != "" {
		s.Model = m
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
	// Its own turn is over while its helpers or teammates still work: it
	// waits on them, and its work is not over yet (DESIGN §5.3). Work in
	// the background that its Stop named says so already (describe); else
	// the row says on whom it waits.
	if s.State == ipc.StateDone || s.State == ipc.StateIdle {
		if subs, ok := x.subsAtWork(); ok {
			s.Subs = &subs
			if s.Detail == "" {
				s.Detail = "Waiting on " + subs.Words()
			}
		}
	}
	// Once its turn is over, a jot's row says what it left that has not
	// landed (DESIGN §4.8).
	if l := wireLoose(x.dog.loose, x.track.nudged, x.track.unproven); l != nil && dogShows(s.State) {
		s.Loose = l
		if s.State == ipc.StateExited {
			s.Detail += " · not through: " + l.Words()
		} else {
			s.Detail = joinDetail("Not through: "+l.Words(), s.Detail)
		}
	}
	return s
}

// subsAtWork counts its running helpers and its teammates at work, and
// reports whether there are any. A teammate waiting for a message is not
// at work, nor is one whose pane has died.
func (x *session) subsAtWork() (ipc.Subs, bool) {
	subs := ipc.Subs{Helpers: len(x.track.subs)}
	for _, m := range x.team {
		if m.Working && !m.Dead {
			subs.Teammates++
		}
	}
	return subs, subs.Helpers+subs.Teammates > 0
}

// waitsOnSubs reports whether its own turn is over, done or idle, while its
// helpers or teammates still work: it waits on them (DESIGN §5.3), as its
// row's subs says.
func (x *session) waitsOnSubs() bool {
	switch x.track.state(x.dead) {
	case ipc.StateDone, ipc.StateIdle:
		_, ok := x.subsAtWork()
		return ok
	}
	return false
}

// countTokens catches each session's tally up with its transcripts. The
// caller holds c.mu.
func (c *Core) countTokens() {
	now := time.Now()
	for _, x := range c.sessions {
		if t, ok := x.tally.update(x.track.transcripts); ok {
			x.tokens = &t
		}
		c.noteRebuilds(x, now)
	}
}

func newCore(o Options, r *tmux.Runner) *Core {
	c := &Core{
		paths: o.Paths, tmux: r, bin: o.Bin, version: o.Version, log: o.Log, http: httpClient(), claude: o.ClaudeDir,
		team:     &tmux.Runner{Bin: r.Bin, Socket: tmux.TeamSocket(r.Socket), Env: teamServerEnv(os.Environ())},
		dock:     &tmux.Runner{Bin: r.Bin, Socket: tmux.DockSocket(r.Socket)},
		sessions: map[string]*session{}, published: map[string]ipc.Session{}, subs: map[*subscriber]bool{},
		history: map[string][]event.Event{}, outcomes: map[string]*outcome{}, limits: map[string]time.Time{}, hues: map[string]int{},
		fdog: newFreezeDog(), started: time.Now(),
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
	// A core that an older `irie up` started from an agent's shell has the
	// agent's PATH, with its tmux shim first. Its agents and everything it
	// runs take the PATH without Irie's shims (tmux.ShimMark).
	if p, ok := os.LookupEnv("PATH"); ok {
		if clean := tmux.CleanPath(p); clean != p {
			os.Setenv("PATH", clean)
		}
	}
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
	c.loadBenches()
	c.loadOrder()
	c.loadSpaces()
	c.loadGroups()
	c.loadHues()
	c.loadClosed()
	c.loadDrops()
	c.loadMeter(time.Now())
	c.loadLocks()
	c.loadState()
	c.loadTrodHand()
	c.loadRaphael()
	c.loadNotepad()

	// The freeze dog, Irie's one watchdog, runs first: the first tick
	// waits for tmux, and a stopped server would hold it until the dog
	// continues it (steady.go). Windows subscribe to its watch later.
	go c.watchFreezes()

	// The layout, the registry and the rows' states are in place before the
	// first hello. Every tree comes out of the background once, since only
	// the core that put one there knows, so an install never leaves an agent
	// slowed; so does what Gabriel slowed outside Irie (gabriel.go).
	c.mu.Lock()
	c.loadView()
	for _, x := range c.sessions {
		x.demoted = true
	}
	// Until Gabriel first looks, the session on show is the tab on top: a
	// promotion a core before this one left must not put it in the
	// background for the seconds until he does (gabriel.go).
	if x := c.sessions[c.selected]; x != nil && gabrielOn(c.cfg().Arch.Gabriel) {
		c.gab.top = map[string]bool{x.id: true}
	}
	c.mu.Unlock()
	c.releaseOutside()
	c.tick()
	// Once every pane is adopted, what the server lost with it is closed
	// on its own, for history and reopen.
	c.findLost(time.Now())
	c.pruneSkills()
	go c.sample()
	go c.dogSweep()
	go c.trodSweep()
	c.lidAdopt()
	go c.lidSweep()
	if err := c.loadPeers(); err != nil {
		c.log.Warn("peers", "err", err)
	}
	go c.checkMachine()
	go c.ctxSweep()
	go c.leftoverSweep()
	go c.lockSweep()

	os.Remove(o.Paths.Socket()) // stale: core.lock is ours
	l, err := ipc.Listen(o.Paths.Socket())
	if err != nil {
		return err
	}
	defer l.Close()
	go ipc.Serve(l, c)
	go c.watchOutside()
	// Terms in short's page, for the Mac app's Terms tab (terms.go).
	go c.serveTerms(context.Background())
	// Raphael's look at the core's own loop (watchCore) counts from here.
	c.beat()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT)
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for n := 1; ; n++ {
		select {
		case <-t.C:
			c.tick()
			// What the agents' hooks kept while no core listened.
			c.takeMissed(time.Now())
			c.beat()
			if n%raphaelEvery == 0 {
				go c.raphaelSweep()
			}
			if n%sampleEvery == 0 {
				go c.sample()
				go c.gabrielSweep()
			}
			if n%dogEvery == 0 {
				go c.dogSweep()
				go c.dockSweep()
			}
			if n%trodEvery == 0 {
				go c.trodSweep()
			}
			if n%benchEvery == 0 {
				go c.benchSweep()
			}
			if n%lidEvery == 0 {
				go c.lidSweep()
			}
			if n%machineEvery == 0 {
				go c.checkMachine()
			}
			if n%leftoverEvery == 0 {
				go c.leftoverSweep()
			}
			if n%lockEvery == 0 {
				go c.lockSweep()
			}
		case s := <-stop:
			c.log.Info("core stopping", "signal", s.String())
			// A hook from here on finds no core, so its shim keeps its event
			// for the next one (missed.go), rather than this one taking it
			// and stopping before it is recorded.
			l.Close()
			// Nothing stays slowed after the core is gone: its agents run on.
			c.letGo()
			// Nor does the Mac stay awake with its lid closed for a core that
			// is gone; the next one keeps it awake again if there is work.
			c.lidLetGo()
			c.mailClose()
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
	if c.appViews > 0 {
		// Each agent window at the size the app shows its session at
		// (sizes.go).
		w.Size, w.Sizes = c.sizes.all, c.sizes.by
		w.Hold = time.Since(c.sizes.swapped) < sizeHold
	}
	return w
}

// tick polls tmux, repairs the layout, records what tmux and the clock say
// and tells subscribers what changed.
func (c *Core) tick() {
	defer c.perf.Since("tick", time.Now())
	c.mu.Lock()
	if err := c.refresh(); err != nil {
		c.log.Warn("poll", "err", err)
	}
	c.noteSidebar()
	c.keepView()
	c.followTeams()
	c.reap()
	c.observe(time.Now())
	c.sendFollowUps(time.Now())
	c.keepFrozen()
	c.thawNext(time.Now())
	c.refreezeLost()
	c.endBoosts(time.Now())
	c.background()
	c.countTokens()
	c.followTitles()
	c.followConversations()
	c.keepRemote()
	c.keepTunnels()
	c.keepOwnHelpers(time.Now())
	c.keepVPN()
	frames := c.changes()
	c.mu.Unlock()
	c.publish(frames)
	c.closedGoesCold(time.Now())
}

// refresh takes a snapshot, repairs the layout until it is sound and
// updates the registry. The caller holds c.mu.
func (c *Core) refresh() error {
	snap, err := c.snapshot()
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
		if snap, err = c.snapshot(); err != nil {
			return err
		}
	}
	c.snap = snap
	c.sync(snap)
	return nil
}

// snapshot runs `tmux list-panes`, timed. The caller holds c.mu.
func (c *Core) snapshot() (tmux.Snapshot, error) {
	defer c.perf.Since("tmux.snapshot", time.Now())
	return c.tmux.Snapshot()
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
		x.model, x.local, x.tutor, x.anon = spec.Model, spec.Local, spec.Tutor, spec.Anon
	}
	c.loadStatus(x)
	x.lockPkgs = c.readLockPackages(x.id)
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
	c.ctxOnEvent(x, e)
	if x.out == nil {
		x.out = c.outcome(x.id)
	}
	x.out.apply(e)
	for s := range c.subs {
		s.event(e)
	}
}

// list returns the rows in the list's order (order.go), each with its
// colour (hue.go) and what the trod bot did for it in its last shift. The
// caller holds c.mu.
func (c *Core) list() []ipc.Session {
	out := make([]ipc.Session, 0, len(c.sessions))
	from := c.trodFrom(time.Now())
	for _, x := range c.sessions {
		out = append(out, c.row(x, from))
	}
	c.sortRows(out)
	out = append(out, c.peerRows(len(out))...)
	c.giveHues(out)
	return out
}

// trodFrom is when the trod bot's shift began, this night's or the last,
// or zero while the bot is off. The caller holds c.mu.
func (c *Core) trodFrom(now time.Time) time.Time {
	if n := c.cfg().Trod; n.On {
		from, _, _ := n.Shift(now)
		return from
	}
	return time.Time{}
}

// row is x's row as the list has it, before the list gives it its place
// and colour. The caller holds c.mu.
func (c *Core) row(x *session, from time.Time) ipc.Session {
	s := x.wire()
	s.Space = c.spaceOf(x)
	s.Group = c.groupOf(x)
	if !from.IsZero() {
		s.Trod = trodRow(&x.track, from)
	}
	now := time.Now()
	s.Rebuilds = c.rebuildsRow(x.id, now)
	s.Cold = c.coldRow(x, s.State, now)
	s.Lock = c.lockRow(x.lockPkgs)
	// A frozen row whose thaw waits its turn says so (freeze.go).
	if w := c.thawWait(x); w != "" {
		s.Detail = joinDetail(w, s.Detail)
	}
	return s
}

// changes returns frames for the rows that changed since the last call.
// The caller holds c.mu.
func (c *Core) changes() []ipc.Frame {
	defer c.perf.Since("changes", time.Now())
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

// changesOf is changes for what only one session can change, such as its
// status line's figures: a frame for its row when the row changed since
// subscribers last heard of it, and no diff of the others, which wait for
// the next tick. A row they never heard of, or one with no colour yet,
// takes the whole diff, which places it and gives it one. The caller holds
// c.mu.
func (c *Core) changesOf(x *session) []ipc.Frame {
	old, ok := c.published[x.id]
	if !ok || x.hue == 0 {
		return c.changes()
	}
	defer c.perf.Since("changes.one", time.Now())
	s := c.row(x, c.trodFrom(time.Now()))
	s.Order = old.Order
	if reflect.DeepEqual(old, s) {
		return nil
	}
	c.published[x.id] = s
	return []ipc.Frame{{T: ipc.FrameUpsert, Session: &s}}
}

// CallFrom answers one request from the process peer (ipc.PeerCaller): as
// Call does, but the ops that change what is the person's alone, the VPN's,
// whether asked for or typed, learn who asks.
func (c *Core) CallFrom(peer int, op string, raw json.RawMessage) (any, error) {
	defer c.perfOps.Since(op, time.Now())
	switch op {
	case ipc.OpInput:
		var a ipc.InputArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.input(a, peer)
	case ipc.OpVPN:
		var a ipc.VPNArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.vpnOp(peer, a)
	case ipc.OpRaphael:
		var a ipc.RaphaelArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.raphaelOp(peer, a)
	case ipc.OpVariorum:
		var a ipc.VariorumArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.variorumOp(peer, a)
	case ipc.OpMail:
		var a ipc.MailArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.mailOp(peer, a)
	case ipc.OpYoink:
		var a ipc.YoinkArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.yoinkOp(peer, a)
	case ipc.OpConfig:
		var a ipc.ConfigArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.settingsFrom(peer, a)
	case ipc.OpWorktrees:
		var a ipc.WorktreesArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.worktreesOp(peer, a)
	}
	return c.Call(op, raw)
}

// Call answers one request (ipc.Handler). A call about one of the peers'
// sessions goes on to its own core (peers.go).
func (c *Core) Call(op string, raw json.RawMessage) (any, error) {
	if op != ipc.OpInput {
		if r, ok, err := c.forward(op, raw); ok {
			return r, err
		}
	}
	switch op {
	case ipc.OpPeers:
		var a ipc.PeersArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.peersOp(a)
	case ipc.OpHello:
		var a ipc.HelloArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return ipc.HelloResult{Version: c.version, API: ipc.API, Features: builtFeatures([]string{
			ipc.OpHello, ipc.OpList, ipc.OpSubscribe, ipc.OpSpawn, ipc.OpShow, ipc.OpHook, ipc.OpComplete,
			ipc.OpAttachInfo, ipc.OpMove, ipc.OpConversation, ipc.OpSend, ipc.OpContinue, ipc.OpRename, ipc.OpFlip,
			ipc.OpPreflight, ipc.OpLocal, ipc.OpStatus, ipc.OpRate, ipc.OpRoute, ipc.OpLabel, ipc.OpResult, ipc.OpTool,
			ipc.OpClose, ipc.OpReopen, ipc.OpUndo, ipc.OpRedo, ipc.FeatureUndoConfirm, ipc.FeatureSpawnModel, ipc.FeatureSpawnTop, ipc.OpTheme,
			ipc.OpFreeze, ipc.OpThaw, ipc.OpPriority, ipc.OpReorder, ipc.OpDog, ipc.OpConfig,
			ipc.OpPark, ipc.OpUnpark, ipc.OpPin, ipc.OpUnpin, ipc.OpStopSub, ipc.OpSpaces, ipc.OpSpace, ipc.OpGroup, ipc.OpInput, ipc.OpTrod, ipc.OpGabriel, ipc.OpArch, ipc.OpRaphael, ipc.FeatureWatch, ipc.OpScreen, ipc.FeatureScreenLive, ipc.OpKeys, ipc.OpPair, ipc.OpReady,
			ipc.OpVPN, ipc.OpAnon, ipc.OpPeers, ipc.OpDock, ipc.OpPane, ipc.OpPlugins, ipc.OpHelpers, ipc.OpMarkets, ipc.OpMail, ipc.OpYoink, ipc.OpVariorum, ipc.OpCard, ipc.OpSizes, ipc.OpLink,
			ipc.OpLearn, ipc.FeatureTutor, ipc.OpPerf, ipc.OpContext, ipc.OpFork, ipc.FeatureBoostAlways, ipc.OpBench, ipc.OpRebuilds, ipc.OpNotepad,
			ipc.OpLock,
			ipc.OpWorktrees,
		})}, nil
	case ipc.OpPerf:
		return c.perfResult(), nil
	case ipc.OpBench:
		var a ipc.BenchArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.benchOp(a)
	case ipc.OpList:
		// Whoever lists is looking, so the sessions outside Irie are
		// refreshed when they are old; a problem is in the result.
		c.refreshOutside("list", outsideEvery, false)
		c.mu.Lock()
		defer c.mu.Unlock()
		return ipc.ListResult{Sessions: c.list(), Outside: c.outsideList(), OutsideProblem: c.outside.problem, Closed: c.closedList()}, nil
	case ipc.OpTheme:
		var a ipc.ThemeArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return nil, c.theme(a)
	case ipc.OpSpaces:
		var a ipc.SpacesArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.spacesOp(a)
	case ipc.OpNotepad:
		var a ipc.NotepadArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.notepadOp(a)
	case ipc.OpSpace:
		var a ipc.SpaceArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.spaceOp(a)
	case ipc.OpGroup:
		var a ipc.GroupArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.groupOp(a)
	case ipc.OpStopSub:
		var a ipc.StopSubArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return nil, c.stopSub(a)
	case ipc.OpClose:
		var a ipc.CloseArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		if len(a.Sessions) == 0 && !a.Finished {
			if a.Session == "" {
				return nil, ipc.Errorf(ipc.CodeBadArgs, "close needs a session, sessions or finished")
			}
			if err := c.close(a); err != nil {
				return nil, err
			}
			c.recordAction(actClose, a.Session, nil)
			return ipc.CloseResult{Sessions: []string{a.Session}}, nil
		}
		// Several close as one step of the history, which one undo takes
		// back (undo.go), even when one of them failed to close.
		ids, err := c.closeMany(a)
		c.recordSteps(actClose, ids, nil)
		return ipc.CloseResult{Sessions: ids}, err
	case ipc.OpDog:
		return c.dogList()
	case ipc.OpRebuilds:
		return c.rebuildsOp(), nil
	case ipc.OpLock:
		var a ipc.LockArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.lockOp(a)
	case ipc.OpReady:
		return c.ready(), nil
	case ipc.OpTrod:
		var a ipc.TrodArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.trodOp(a)
	case ipc.OpLearn:
		var a ipc.LearnArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.learnOp(a)
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
			c.markWord(r.Session.ID)
			return r, nil
		}
		s, err := c.spawn(a, true)
		if err != nil {
			return nil, err
		}
		// A subtask an agent delegates is the agent's doing, not the user's.
		if !a.Delegate {
			c.recordAction(actSpawn, s.ID, nil)
			c.markWord(s.ID)
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
		s, step, err := c.rename(a)
		if err != nil {
			return nil, err
		}
		if step != nil {
			c.recordAction(actRename, a.Session, step)
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
		return c.input(a, 0)
	case ipc.OpVPN:
		var a ipc.VPNArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.vpnOp(0, a)
	case ipc.OpAnon:
		var a ipc.AnonArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.anonOp(a)
	case ipc.OpPlugins:
		var a ipc.PluginsArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.pluginsOp(a)
	case ipc.OpHelpers:
		var a ipc.HelpersArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.helpersOp(a)
	case ipc.OpVariorum:
		var a ipc.VariorumArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.variorumOp(0, a)
	case ipc.OpMarkets:
		var a ipc.MarketsArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.marketsOp(a)
	case ipc.OpCard:
		var a ipc.CardArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return nil, c.cardOp(a)
	case ipc.OpSizes:
		var a ipc.SizesArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return nil, c.sizesOp(a)
	case ipc.OpLink:
		var a ipc.LinkArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.linkOp(a)
	case ipc.OpMail:
		// A call with no process to it, such as one from within the core.
		var a ipc.MailArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.mailOp(0, a)
	case ipc.OpYoink:
		// A call with no process to it, such as one from within the core.
		var a ipc.YoinkArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.yoinkOp(0, a)
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
		if err := c.send(a); err != nil {
			return nil, err
		}
		c.markWord(a.Session)
		return nil, nil
	case ipc.OpContinue:
		var a ipc.ContinueArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		if err := c.continueAgent(a); err != nil {
			return nil, err
		}
		c.markWord(a.Session)
		return nil, nil
	case ipc.OpArch:
		return c.archList(), nil
	case ipc.OpRaphael:
		// Not from a socket, which CallFrom answers: no one to tell from
		// an agent, so it reports, looks, and fixes nothing.
		var a ipc.RaphaelArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		if a.Fix != "" || a.Mine != "" {
			return nil, ipc.Errorf(ipc.CodeBadArgs, "Raffi takes a fix or a mark only from you, over "+brand.Name+"'s socket.")
		}
		return c.raphaelOp(0, a)
	case ipc.OpGabriel:
		var a ipc.GabrielArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.gabrielOp(a)
	case ipc.OpDock:
		var a ipc.DockArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.dockOp(a)
	case ipc.OpPane:
		var a ipc.PaneArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.paneOp(a)
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
	case ipc.OpLabel:
		var a ipc.LabelArgs
		if err := decode(raw, &a); err != nil {
			return nil, err
		}
		return c.label(a)
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
	if r, ok, err := c.contextOp(op, raw); ok {
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
	// While Gabriel holds back, the session now on show comes out of the
	// background before this returns (gabriel.go).
	c.refocus()
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
		c.noteSwap()
		if _, err := c.tmux.RunList(cmds); err != nil {
			return err
		}
	}
	c.record(x, event.Event{Source: event.SourceUser, Kind: event.UIShown, Data: map[string]any{"focus": focus}})
	return c.refresh()
}
