package ipc

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/event"
)

// API is the protocol version. Additions keep backwards compatibility.
const API = 1

// Ops this version serves.
const (
	OpHello     = "hello"
	OpList      = "list"
	OpSubscribe = "subscribe"
	OpSpawn     = "spawn"
	OpShow      = "show"
	OpHook      = "hook"
	// OpComplete is an addition to the ops of DESIGN §2.4: the candidates
	// Tab offers for an @ or + token of the command bar (M3).
	OpComplete = "complete"
	// OpAttachInfo is the planned op of DESIGN §2.4, here for the Mac app:
	// how a terminal outside Irie attaches to the slot (docs/MAC.md).
	OpAttachInfo = "attach-info"
	// OpMove is an addition to the ops of DESIGN §2.4: it carries an idle
	// Claude Code session that runs outside Irie on in an Irie pane (§4.6).
	OpMove = "move"
	// OpRename gives a session a new name, which a restarted core keeps
	// and Claude Code's titles leave be (DESIGN §2.4, M4).
	OpRename = "rename"
	// OpPreflight checks a local profile's model before a spawn (M10).
	OpPreflight = "preflight"
	// OpLocal and OpStatus are additions for local models and cost
	// (DESIGN §6.3): what the local server and the machine's memory give,
	// and one status update of a Claude Code session.
	OpLocal  = "local"
	OpStatus = "status"
	// Routing (DESIGN §6.4): a rating of a session, a dry run of the
	// router, the result of a session's last turn, and a tool session such
	// as local setup.
	OpRate   = "rate"
	OpRoute  = "route"
	OpResult = "result"
	OpTool   = "tool"
	// OpClose ends a session's agent and removes its row; OpReopen, its
	// undo, carries the session on again (DESIGN §2.4, §4.5).
	OpClose  = "close"
	OpReopen = "reopen"
	// OpUndo and OpRedo step back and forth through the history of what
	// the user did (DESIGN §2.4, §4.5).
	OpUndo = "undo"
	OpRedo = "redo"
	// OpTheme gives the core the Mac app's palette, which the agents'
	// colours on Irie's status line follow (DESIGN §6.3).
	OpTheme = "theme"
	// OpDog lists the jots that are not through: what each left that has
	// not landed, by the jot dog's last look (DESIGN §4.8).
	OpDog = "dog"
	// OpConfig reads the config in effect, or edits the config file in
	// place and runs on the result at once: the Mac app's Settings window
	// (DESIGN §5.5).
	OpConfig = "config"
	// OpTrod is the trod bot's report: whether it works now, the
	// rules and guidelines it gives, and what it did (DESIGN §4.10).
	OpTrod = "trod"
)

// FeatureSpawnModel is in hello's features when spawn takes SpawnArgs.Model.
const FeatureSpawnModel = "spawn-model"

// FeatureWatch is in hello's features when subscribe takes ViewWatch: the
// freeze dog watches the window that asks (DESIGN §2.4).
const FeatureWatch = "watch"

// ViewApp is the SubscribeArgs.View of the Mac app. While one subscribes
// with it, the slot fills irie:0 (docs/MAC.md).
const ViewApp = "app"

// ViewWatch is the SubscribeArgs.View of a window that asks the freeze dog
// to watch it (DESIGN §2.4): the stream carries only pings, which the
// window answers with a Pong from its main loop. A ping left unanswered
// for ui.freeze_dog has its process force quit and, for the Mac app,
// opened again.
const ViewWatch = "watch"

// Error codes.
const (
	CodeBadRequest = "bad_request"
	CodeBadArgs    = "bad_args"
	CodeUnknownOp  = "unknown_op"
	CodeNotFound   = "not_found"
	CodeAmbiguous  = "ambiguous" // a name that fits more than one thing; the message lists them
	CodeBusy       = "busy"      // the session is busy or still running, so this cannot happen now
	CodeInternal   = "internal"
	CodeStale      = "stale" // an answer to a question whose situation has moved on; nothing was done
	// CodeNeedsStronger refuses a delegated task that no profile cheaper
	// than its delegator is accurate enough for.
	CodeNeedsStronger = "needs_stronger"
)

// Session states (DESIGN §5.3), derived from the session's events. Frozen is
// in control.go. The core derives paused and pausing once it can pause (M5);
// the sidebar already draws them.
const (
	StateFailed   = "failed"
	StateExited   = "exited"
	StateNeedsYou = "needs_you"
	StateStalled  = "stalled"
	StateWorking  = "working"
	StateStarting = "starting"
	StateDone     = "done"
	StateIdle     = "idle"
	StatePaused   = "paused"
	StatePausing  = "pausing"
)

// Session is one row of the session list.
type Session struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Agent    string    `json:"agent,omitempty"` // "main"
	State    string    `json:"state"`
	Reason   string    `json:"reason,omitempty"`   // why it needs you, stalled or failed
	Detail   string    `json:"detail,omitempty"`   // what it is doing or needs, in plain words, for its row
	Command  string    `json:"command,omitempty"`  // the tool call the detail is about, as "Bash · go test ./...", for its details
	Tail     []string  `json:"tail,omitempty"`     // the last screen lines of a stalled or failed spawn
	Warnings []string  `json:"warnings,omitempty"` // what Irie cannot do for this session
	ExitCode *int      `json:"exit_code,omitempty"`
	Created  time.Time `json:"created"`
	Cwd      string    `json:"cwd,omitempty"`
	Repo     string    `json:"repo,omitempty"`     // the main checkout of the repo the task runs in
	Worktree string    `json:"worktree,omitempty"` // the worktree Irie made for the task
	Branch   string    `json:"branch,omitempty"`   // the worktree's branch
	Copied   []string  `json:"copied,omitempty"`   // the local files copied into the worktree
	Profile  string    `json:"profile,omitempty"`
	VendorID string    `json:"vendor_id,omitempty"`
	Pane     string    `json:"pane,omitempty"`
	Shown    bool      `json:"shown,omitempty"`
	// Subagents are the session's running subagents, oldest first.
	Subagents []Subagent `json:"subagents,omitempty"`
	// Team is the session's teammates, each in a terminal pane of its own
	// on the team server, in the order they started (DESIGN §2.3).
	Team []Teammate `json:"team,omitempty"`
	// Tokens is what the session's model calls have used, from its
	// transcripts; nil until Irie has read any usage.
	Tokens *Tokens `json:"tokens,omitempty"`

	// What the session runs on and costs (DESIGN §6.3).
	Model   string   `json:"model,omitempty"`    // the model the agent runs
	Local   bool     `json:"local,omitempty"`    // a local model, which costs memory rather than tokens
	CostUSD *float64 `json:"cost_usd,omitempty"` // what it has cost at list price, from its status line
	Context *float64 `json:"context,omitempty"`  // how full its context is, in percent
	Memory  int64    `json:"memory,omitempty"`   // what its processes take in memory, in bytes

	// How it was routed and how it went (DESIGN §6.4).
	Class  string `json:"class,omitempty"`  // small, medium or hard
	Routed bool   `json:"routed,omitempty"` // auto chose its profile
	Parent string `json:"parent,omitempty"` // the session that delegated it
	Rated  string `json:"rated,omitempty"`  // good or bad, as the user said

	// Where the user put it and what they did to it (control.go).
	Order    int        `json:"order"`              // its place in the list, from 0 at the top
	Parked   bool       `json:"parked,omitempty"`   // in the Parked group at the end of the list
	Priority int        `json:"priority,omitempty"` // 1 while it is promoted
	Frozen   *time.Time `json:"frozen,omitempty"`   // when it was frozen, while it is
	Freezing bool       `json:"freezing,omitempty"` // frozen, but its agent runs until its next tool call

	// Loose is what its jot left that has not landed, by the jot dog's
	// last look, while its turn is over (DESIGN §4.8); nil when the jot is
	// through or the dog does not watch it.
	Loose *Loose `json:"loose,omitempty"`
	// Trod is what the trod bot did for it in this night's shift or
	// the last (DESIGN §4.10); nil when it did nothing.
	Trod *TrodRow `json:"trod,omitempty"`
}

// TrodRow is what the trod bot did for a session in one shift.
type TrodRow struct {
	Sent   int       `json:"sent"`   // how often it sent the session on
	Last   time.Time `json:"last"`   // when it last did
	Reason string    `json:"reason"` // why it last did: question, error, limit or loose
}

// TrodShift is the trod bot while it is in its hours, for the
// sidebars' header (DESIGN §4.10).
type TrodShift struct {
	Until *time.Time `json:"until,omitempty"` // when its hours end; nil for a shift around the clock
	Away  bool       `json:"away,omitempty"`  // you are away, so it steps in
	Awake bool       `json:"awake,omitempty"` // it keeps the Mac awake
	// Capped: the plan is past trod.max_plan_use, so it sends nothing on.
	Capped bool `json:"capped,omitempty"`
	// Hand: you started it by hand, so it works while you are at the Mac,
	// until nothing is left or the plan is as full as it may get.
	Hand bool `json:"hand,omitempty"`
}

// TrodArgs start a run of the trod bot by hand, which works at once, until
// Until if given, until no session has work left, or until the plan is as
// full as trod.max_plan_use; or stop one. With neither, the op only
// reports.
type TrodArgs struct {
	Start bool       `json:"start,omitempty"`
	Stop  bool       `json:"stop,omitempty"`
	Until *time.Time `json:"until,omitempty"`
}

// TrodHand is the run of the trod bot you started by hand last.
type TrodHand struct {
	From  time.Time  `json:"from"`
	Until *time.Time `json:"until,omitempty"`
	On    bool       `json:"on"`              // it still works
	Ended *time.Time `json:"ended,omitempty"` // when it ended
	Why   string     `json:"why,omitempty"`   // why it ended: nothing was left, the plan, its time, or you
}

// TrodResult is the trod bot's report (DESIGN §4.10).
type TrodResult struct {
	On          bool          `json:"on"`                     // trod.on
	In          bool          `json:"in"`                     // on, and in its hours now
	Away        bool          `json:"away"`                   // you are away, by trod.away
	Works       bool          `json:"works"`                  // in its hours and you are away: it steps in
	Awake       bool          `json:"awake"`                  // it keeps the Mac awake
	IdleSeconds *int64        `json:"idle_seconds,omitempty"` // how long the Mac has had no keyboard or mouse input, when Irie can tell
	Start       string        `json:"start"`                  // trod.start and trod.end
	End         string        `json:"end"`
	From        time.Time     `json:"from"` // this night's shift, or the last
	To          time.Time     `json:"to"`
	MaxPlanUse  float64       `json:"max_plan_use"`         // trod.max_plan_use, in percent
	FiveHours   *float64      `json:"five_hours,omitempty"` // how full the plan's five hours and week are, in percent, when the status line said
	Week        *float64      `json:"week,omitempty"`
	Capped      bool          `json:"capped,omitempty"` // past max_plan_use, so it sends nothing on
	Hand        *TrodHand     `json:"hand,omitempty"`   // the run you started by hand last
	Rules       []string      `json:"rules"`
	Guidelines  []string      `json:"guidelines"`
	Sessions    []TrodSession `json:"sessions"` // in the list's order
}

// TrodSession is what the trod bot did for one session in that shift,
// and why it leaves the session for you, if it does.
type TrodSession struct {
	ID    string     `json:"id"`
	Name  string     `json:"name"`
	State string     `json:"state"`
	Sent  []TrodSend `json:"sent,omitempty"`
	Left  string     `json:"left,omitempty"`
}

// TrodSend is one time the trod bot sent a session on.
type TrodSend struct {
	At     time.Time `json:"at"`
	Reason string    `json:"reason"` // question, error, limit or loose
	How    string    `json:"how"`    // stop (it sent the turn on), ask (it turned a question away) or typed
}

// Loose is what a jot left that has not landed (DESIGN §4.8).
type Loose struct {
	Dirty  int          `json:"dirty,omitempty"`  // files not committed
	Ahead  int          `json:"ahead,omitempty"`  // its own commits not in the land branch
	Land   string       `json:"land,omitempty"`   // that branch, when its repo has one (dog.land)
	Where  []LoosePlace `json:"where"`            // the worktrees that hold them
	Nudged int          `json:"nudged,omitempty"` // how often the dog sent it back to finish since your last prompt
}

// Words says what is left in plain words, such as "3 files not committed ·
// 2 commits not in live".
func (l Loose) Words() string {
	var parts []string
	if l.Dirty > 0 {
		parts = append(parts, counted(l.Dirty, "file")+" not committed")
	}
	if l.Ahead > 0 {
		parts = append(parts, counted(l.Ahead, "commit")+" not in "+l.Land)
	}
	return strings.Join(parts, " · ")
}

func counted(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return strconv.Itoa(n) + " " + thing + "s"
}

// LoosePlace is a worktree that holds work of a jot that has not landed.
type LoosePlace struct {
	Path   string `json:"path"`
	Branch string `json:"branch,omitempty"`
	Dirty  int    `json:"dirty,omitempty"`
	Ahead  int    `json:"ahead,omitempty"`
}

// DogResult lists the jots that are not through: the open ones in the
// list's order, then those that closed on their own in the last day, the
// last closed first. A jot you closed is done and never listed.
type DogResult struct {
	Jots []Jot `json:"jots"`
}

// Jot is a jot that is not through.
type Jot struct {
	ID     string     `json:"id"`
	Name   string     `json:"name"`
	State  string     `json:"state"`            // its row's state, or closed
	Closed *time.Time `json:"closed,omitempty"` // when it was closed
	Loose  Loose      `json:"loose"`
}

// Tokens counts a session's model usage: each reply once, from the usage
// Claude Code records in the session's transcript and its finished
// subagents' transcripts.
type Tokens struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
}

// Subagent is a subagent at work in a session (DESIGN §4.4): its hooks
// reported it started and not yet that it stopped.
type Subagent struct {
	Agent    string    `json:"agent"`          // sub:<vendor agent id>
	Type     string    `json:"type,omitempty"` // its kind, such as Explore
	Started  time.Time `json:"started"`
	Detail   string    `json:"detail,omitempty"`  // what it needs, or else what its newest running tool does, in plain words
	Command  string    `json:"command,omitempty"` // the tool call the detail is about
	NeedsYou bool      `json:"needs_you,omitempty"`
}

// Teammate is one of a session's agent-team teammates: a Claude Code
// session of its own, in a terminal pane on the team server.
type Teammate struct {
	Session  string `json:"session"`         // its tmux session on the team server, which a view attaches to
	Name     string `json:"name"`            // the name its team knows it by
	Color    string `json:"color,omitempty"` // the colour Claude Code gave it, such as blue
	Type     string `json:"type,omitempty"`  // the agent definition it runs
	Title    string `json:"title,omitempty"` // its terminal's title
	Working  bool   `json:"working,omitempty"`
	Dead     bool   `json:"dead,omitempty"`
	ExitCode *int   `json:"exit_code,omitempty"` // once it is dead
}

// Header is what the sidebar shows that is not a row.
type Header struct {
	Version string `json:"version,omitempty"`
	// RedOnDone is ui.red_on_done: a done session is drawn red, not green
	// (DESIGN §5.3), in every sidebar.
	RedOnDone bool `json:"red_on_done,omitempty"`
	// CloseWhenDone is spawn.close_when_done: a finished session closes
	// itself, so no sidebar keeps a Done group; closed ones are in history.
	CloseWhenDone bool `json:"close_when_done,omitempty"`
	// HelperStyle is ui.helper_style: how a view draws a helper beside its
	// session, as cards ("browser") or as a terminal ("terminal").
	HelperStyle string  `json:"helper_style,omitempty"`
	Plan        *Plan   `json:"plan,omitempty"`   // the Claude plan's use, from the newest status line
	Memory      *Memory `json:"memory,omitempty"` // the machine's memory
	// Trod is the trod bot while it is in its hours (DESIGN §4.10).
	Trod *TrodShift `json:"trod,omitempty"`
}

// Window is one window of a plan's use, such as the five hours.
type Window struct {
	Used     float64   `json:"used"` // percent, 0 to 100
	ResetsAt time.Time `json:"resets_at"`
}

// Plan is the use of a Claude Pro or Max plan's windows.
type Plan struct {
	FiveHour *Window `json:"five_hour,omitempty"`
	SevenDay *Window `json:"seven_day,omitempty"`
}

// Memory is the machine's memory and what Irie's work takes of it.
type Memory struct {
	Total     int64 `json:"total"`
	Available int64 `json:"available"`     // free now
	Agents    int64 `json:"agents"`        // what the sessions' processes take
	Local     int64 `json:"local"`         // what the models of Irie's own Ollama server take
	Budget    int64 `json:"budget"`        // the most local models may take
	Low       bool  `json:"low,omitempty"` // less is free than a model load must leave
}

type HelloArgs struct {
	Client  string `json:"client"`
	API     int    `json:"api"`
	Version string `json:"version,omitempty"`
}

type HelloResult struct {
	Version  string   `json:"version"`
	API      int      `json:"api"`
	Features []string `json:"features"`
}

type ListResult struct {
	Sessions []Session `json:"sessions"`
	// Outside lists the Claude Code sessions that run outside Irie, oldest
	// first (DESIGN §4.6). OutsideProblem says why that list may be old.
	Outside        []OutsideSession `json:"outside"`
	OutsideProblem string           `json:"outside_problem,omitempty"`
	// Closed lists the sessions reopen can carry on, the last closed first.
	Closed []ClosedSession `json:"closed"`
}

// ClosedSession is a closed session that reopen can carry on. Auto says
// that it closed on its own, by spawn.close_when_done; one closed by you,
// or by an agent at your word, is done (DESIGN §4.5).
type ClosedSession struct {
	ID     string    `json:"id"`
	Name   string    `json:"name"`
	Closed time.Time `json:"closed"`
	Auto   bool      `json:"auto,omitempty"`
}

// CloseArgs name the session to close.
type CloseArgs struct {
	Session string `json:"session"`
}

// ReopenArgs name the closed session to reopen: the one closed last when
// Session is empty.
type ReopenArgs struct {
	Session string `json:"session,omitempty"`
}

type ReopenResult struct {
	Session Session `json:"session"`
}

// UndoArgs ask to undo or redo the latest step of the history. On their
// own they undo it, unless that is uncertain: then nothing changes and the
// result asks a Choice. ChoiceID and Choice answer it: the question's id,
// which the core checks against the history's head, and the key picked.
type UndoArgs struct {
	ChoiceID string `json:"choice_id,omitempty"`
	Choice   string `json:"choice,omitempty"`
}

// What an undo or redo did (UndoResult.Did).
const (
	DidUndo   = "undone"
	DidRedo   = "redone"
	DidSettle = "settled"   // things were already that way: the step moved to the other stack
	DidSkip   = "skipped"   // the step left the history; nothing else changed
	DidCancel = "cancelled" // nothing changed
	DidAsk    = "asked"     // nothing changed: Choice asks what to do
)

// The keys every Choice offers, and those its other options use.
const (
	ChoiceApply  = "apply"  // go ahead anyway
	ChoiceSettle = "settle" // count the step done as things stand
	ChoiceSkip   = "skip"
	ChoiceCancel = "cancel"
)

// UndoResult is what an undo or redo did, to the step labelled Label.
type UndoResult struct {
	Label  string  `json:"label"`
	Did    string  `json:"did"`
	Choice *Choice `json:"choice,omitempty"` // when Did is DidAsk
}

// Choice is a question the core asks before an uncertain undo or redo:
// two to four options in plain words, the recommended one first, and always
// skip and cancel among them.
type Choice struct {
	ID       string         `json:"id"`
	Question string         `json:"question"`
	Options  []ChoiceOption `json:"options"`
}

// ChoiceOption is one answer to a Choice: its key, what it says, and what
// it will do.
type ChoiceOption struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Detail      string `json:"detail"`
	Recommended bool   `json:"recommended,omitempty"`
}

type SubscribeArgs struct {
	Events  bool   `json:"events,omitempty"`
	Session string `json:"session,omitempty"`
	// Agent keeps event frames to one agent of the session, main or
	// sub:<id>. With Conversation, sub:<id> follows that helper's own
	// conversation instead of the session's.
	Agent string `json:"agent,omitempty"`
	View  string `json:"view,omitempty"` // "", ViewApp or ViewWatch
	// Conversation adds the session's conversation frames; it needs Session.
	Conversation bool `json:"conversation,omitempty"`
	// With ViewWatch: the window's process, which the freeze dog force
	// quits, and the Mac app it then opens again, if any.
	PID    int    `json:"pid,omitempty"`
	Bundle string `json:"bundle,omitempty"`
}

// Pong answers a FramePing on the same connection. A watched window sends
// it from its main loop, so it says the window still answers its user, and
// with how the window stands, so that one opened again stands the same way.
type Pong struct {
	Pong   uint64 `json:"pong"`
	Front  bool   `json:"front,omitempty"`  // the window's app is the active one
	Hidden bool   `json:"hidden,omitempty"` // its app is hidden
}

// AttachInfoResult is how to attach a tmux client to the session that holds
// the slot: tmux -S Socket attach-session -t Target.
type AttachInfoResult struct {
	Tmux   string `json:"tmux"`   // the tmux binary Irie runs
	Socket string `json:"socket"` // the server's socket path
	Target string `json:"target"` // the session to attach to
	// View is the session of the same server that shows the session the
	// app splits, beside its teammates: tmux -S Socket attach-session -t
	// View. Teams is the team server's socket path, where each teammate is
	// a session of its own (Teammate.Session); "" before any session has
	// teams.
	View  string `json:"view,omitempty"`
	Teams string `json:"teams,omitempty"`
}

// SpawnArgs start a task. Cwd runs it in place in a directory. Repo is the
// name or path of an @ token: a git repo gets a new worktree, or with InPlace
// runs in its own checkout; any other directory runs in place. Profile picks
// the profile instead of spawn.default_profile.
type SpawnArgs struct {
	Task    string `json:"task"`
	Cwd     string `json:"cwd,omitempty"`
	Repo    string `json:"repo,omitempty"`
	InPlace bool   `json:"in_place,omitempty"`
	Profile string `json:"profile,omitempty"`
	// Name names the session as the user chose: shown as given, kept from
	// Claude Code's titles and passed to it with --name. Without one the
	// task's first line names the session until Claude Code titles it.
	Name string `json:"name,omitempty"`
	// Force spawns a local profile even when its model's context or the
	// machine's memory falls short, with a warning (M10).
	Force bool `json:"force,omitempty"`
	// Delegate is a subtask that the session Parent hands on (DESIGN
	// §6.4): auto picks only profiles cheaper than the parent's, and refuses
	// the task when none is accurate enough. PermissionMode overrides the
	// profile's.
	Delegate       bool   `json:"delegate,omitempty"`
	Parent         string `json:"parent,omitempty"`
	PermissionMode string `json:"permission_mode,omitempty"`
	// Model runs this task on a model other than its profile's: an alias
	// such as opus or sonnet, or a model's full name. Only a Claude Code
	// profile takes it; a local model has its own +ollama: profile. The
	// Mac app's new-task sheet sends it (docs/MAC.md).
	Model string `json:"model,omitempty"`
	// Continues is the session a failover carries the task on from.
	Continues string `json:"continues,omitempty"`
	// Jot is a task the user typed in a task bar or the command bar. One
	// that reads as more for the jot sent just before it, in the same place
	// and for the same agent, within spawn.follow_up_window, goes to that
	// jot's agent instead of starting a session (DESIGN §4.2), and the
	// result says Added.
	Jot bool `json:"jot,omitempty"`
	// Apart takes a jot that was added to session Apart back, if its agent
	// has not had it yet, and starts it as a task of its own.
	Apart string `json:"apart,omitempty"`
}

type SpawnResult struct {
	Session Session `json:"session"`
	// Added says the task went to Session's agent, as more for its jot,
	// rather than starting Session. It is typed to the agent once the
	// agent can take it, and not before Until, so that a spawn with Apart
	// can still take it back.
	Added bool      `json:"added,omitempty"`
	Until time.Time `json:"until,omitzero"`
	// Late says, for a spawn with Apart, that the agent of the session the
	// task had been added to has it already.
	Late bool `json:"late,omitempty"`
}

// CompleteArgs carry the command bar token to complete, such as "@web" or
// "+fa".
type CompleteArgs struct {
	Text string `json:"text"`
}

// CompleteResult lists the whole tokens that complete it, sorted, and for
// a + token what each profile runs and costs.
type CompleteResult struct {
	Items []string `json:"items"`
	Notes []string `json:"notes,omitempty"` // one per item
}

// PreflightArgs check a local profile's model before a spawn (DESIGN §6.1).
type PreflightArgs struct {
	Profile  string `json:"profile"`
	Model    string `json:"model,omitempty"`    // this model instead of the profile's
	Remember bool   `json:"remember,omitempty"` // when it passes, local profiles with no model of their own run it
	Force    bool   `json:"force,omitempty"`    // load it even when the memory free now is short
}

// PreflightResult is what the check found. Problems say why a spawn would
// be refused; with none it may go ahead.
type PreflightResult struct {
	OK       bool     `json:"ok"`
	URL      string   `json:"url,omitempty"`
	Model    string   `json:"model,omitempty"`
	Context  int      `json:"context,omitempty"` // the context the model was loaded with
	Need     int64    `json:"need,omitempty"`    // what it takes in memory
	VRAM     int64    `json:"vram,omitempty"`    // the part of it in GPU memory
	Problems []string `json:"problems,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// LocalArgs ask about the local server. Start starts Irie's own when it is
// not running.
type LocalArgs struct {
	Start bool `json:"start,omitempty"`
	Llama bool `json:"llama,omitempty"` // also say what Irie's llama-server runs and may run
}

// LocalResult is the local server and the models it can run, weighed
// against the machine's memory.
type LocalResult struct {
	URL     string       `json:"url"`
	Own     bool         `json:"own"`     // Irie runs this server
	Running bool         `json:"running"` // it answers
	Version string       `json:"version,omitempty"`
	Error   string       `json:"error,omitempty"`  // why it cannot be used
	Chosen  string       `json:"chosen,omitempty"` // the model `irie setup local` chose for local profiles with no model of their own
	Pick    string       `json:"pick,omitempty"`   // the model those profiles run: Chosen, else the best one here that fits
	Context int          `json:"context"`          // the context it loads models with, when Irie runs it
	KVCache string       `json:"kv_cache"`
	Memory  Memory       `json:"memory"`
	Disk    int64        `json:"disk,omitempty"` // the disk space free where the server keeps its models; 0 = unknown
	Models  []LocalModel `json:"models"`         // the cheapest in memory first
	Llama   *LlamaStatus `json:"llama,omitempty"`
}

// LlamaStatus is Irie's own llama-server, for profiles with runtime =
// "llama-server" (DESIGN §6.5).
type LlamaStatus struct {
	URL     string       `json:"url"`
	Bin     string       `json:"bin,omitempty"`     // the llama-server binary
	Build   string       `json:"build,omitempty"`   // which build it is, such as "build 10180"
	Error   string       `json:"error,omitempty"`   // why it cannot run, such as none being installed
	Running string       `json:"running,omitempty"` // the model it runs now
	Pick    string       `json:"pick,omitempty"`    // the model +big runs
	Dir     string       `json:"dir"`               // the folder of GGUF files it may run besides Ollama's
	Models  []LocalModel `json:"models"`            // the cheapest in memory first
}

// LocalModel is one model of the local server.
type LocalModel struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`               // its weights
	Need     int64  `json:"need"`               // what it takes in memory at the server's context
	Measured bool   `json:"measured,omitempty"` // Need is what it took when it was loaded, not an estimate
	Loaded   bool   `json:"loaded,omitempty"`
	Fits     bool   `json:"fits"`             // Need is within the budget
	Why      string `json:"why,omitempty"`    // why a coding agent cannot run on it, such as no tools
	Remote   bool   `json:"remote,omitempty"` // a cloud model
	About    string `json:"about,omitempty"`  // what it is, for a model Irie suggests, such as "Qwen3.8-27B, ternary"
}

// RateArgs rate a session's work, which teaches the router (DESIGN §6.4).
type RateArgs struct {
	Session string `json:"session"`
	Verdict string `json:"verdict"` // good or bad
}

// Verdicts of a rating.
const (
	Good = "good"
	Bad  = "bad"
)

// RouteArgs ask the router where a task would run, without running it.
type RouteArgs struct {
	Task   string `json:"task"`
	Parent string `json:"parent,omitempty"` // as a subtask of this session
}

// ResultArgs ask for what a session's agent said at the end of its last
// turn.
type ResultArgs struct {
	Session string `json:"session"`
}

type ResultResult struct {
	Text string `json:"text"`
}

// ToolArgs start a tool session: setup (`irie setup local`) or models
// (`irie ctl local`).
type ToolArgs struct {
	Name string `json:"name"`
}

// Tools.
const (
	ToolSetup  = "setup"
	ToolModels = "models"
)

// StatusArgs carry one status line update of a Claude Code session: its
// JSON, as Claude Code gave it (DESIGN §6.3).
type StatusArgs struct {
	Session string          `json:"session"`
	Payload json.RawMessage `json:"payload"`
}

// StatusReply is the session's state and running subagents, which Irie's
// own status line shows.
type StatusReply struct {
	State     string     `json:"state"`
	Subagents []Subagent `json:"subagents,omitempty"`
	// Color is the session's own colour while it works, as "#rrggbb": its
	// card's mark in the Mac app's dark mode, which reads on its terminal.
	Color string `json:"color,omitempty"`
	// Parked is whether the session is in the Parked group, out of the
	// way: a turn of it that is over is not to verify.
	Parked bool `json:"parked,omitempty"`
}

// ThemeArgs name the Mac app's palette, one of config.Themes.
type ThemeArgs struct {
	Theme string `json:"theme"`
}

// ConfigArgs are changes to make to the config file, in order. With none,
// config only reads.
type ConfigArgs struct {
	Set []ConfigSet `json:"set,omitempty"`
}

// ConfigSet gives a dotted key, such as spawn.default_profile, a value of
// the type the key takes; a duration is a string such as "30s". A null or
// missing value takes the key out of the file, so its default applies.
type ConfigSet struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value,omitempty"`
}

// ConfigResult is the config in effect, by the file's keys.
type ConfigResult struct {
	Path   string         `json:"path"`   // the config file
	Config map[string]any `json:"config"` // every key, defaults too; durations as strings such as "30s"
	// Problem says why the file would not load as it stands, when it would
	// not, as after a hand edit gone wrong; the core runs on the config it
	// loaded last.
	Problem string `json:"problem,omitempty"`
}

type ShowArgs struct {
	Session string `json:"session"`
	Agent   string `json:"agent,omitempty"`
	Focus   bool   `json:"focus"`
	// Split shows the session beside its teammates, for the Mac app: its
	// window goes into AttachInfoResult.View, at that client's size, and
	// the slot shows its conversation.
	Split bool `json:"split,omitempty"`
}

// RenameArgs give a session a new name. The core makes its whitespace and
// control characters single spaces, and numbers a name another row has.
type RenameArgs struct {
	Session string `json:"session"`
	Name    string `json:"name"`
}

// RenameResult is the session with the name it got.
type RenameResult struct {
	Session Session `json:"session"`
}

// HookArgs carry one hook event from the shim: the harness's own payload,
// whose strings the shim has capped.
type HookArgs struct {
	Harness string          `json:"harness"`
	Session string          `json:"session"`
	Payload json.RawMessage `json:"payload"`
}

// HookResult is what the shim tells the agent: context for it, or a reason
// to deny the call. Both are empty when there is nothing to say.
type HookResult struct {
	Context string `json:"context,omitempty"`
	Deny    string `json:"deny,omitempty"`
	// Unfinished answers a Stop: what is left of the jot, which its agent
	// goes on to finish (DESIGN §4.8).
	Unfinished string `json:"unfinished,omitempty"`
}

// Frame types streamed after subscribe.
const (
	FrameSnapshot = "snapshot"
	FrameUpsert   = "upsert"
	FrameRemove   = "remove"
	FrameEvent    = "event"  // only with SubscribeArgs.Events
	FrameHeader   = "header" // the header changed; an addition to DESIGN §2.4
	// FrameOutside lists the sessions outside Irie (DESIGN §4.6): once
	// after the first snapshot, then whenever the list or its problem
	// changes. A subscription to one session gets none.
	FrameOutside = "outside"
	// FrameClosed lists the closed sessions reopen can carry on, the last
	// closed first: once after the first snapshot, then whenever they
	// change. A subscription to one session gets none.
	FrameClosed = "closed"
	// FrameHistory carries the labels of what undo and redo would do next,
	// empty when nothing: once after the first snapshot, then whenever they
	// change. A subscription to one session gets none.
	FrameHistory = "history"
	// FramePing is the freeze dog's ping to a window it watches, the only
	// frame a ViewWatch subscription gets. The window answers it with a
	// Pong.
	FramePing = "ping"
)

// Frame is one streamed message. Which fields are set depends on T.
type Frame struct {
	T        string           `json:"t"`
	Sessions []Session        `json:"sessions,omitempty"` // snapshot
	Header   *Header          `json:"header,omitempty"`   // snapshot, header
	Session  *Session         `json:"session,omitempty"`  // upsert
	ID       string           `json:"id,omitempty"`       // remove
	Event    *event.Event     `json:"event,omitempty"`    // event
	Outside  []OutsideSession `json:"outside,omitempty"`  // outside
	Problem  string           `json:"problem,omitempty"`  // outside: why the list may be old
	Closed   []ClosedSession  `json:"closed,omitempty"`   // closed
	Undo     string           `json:"undo,omitempty"`     // history
	Redo     string           `json:"redo,omitempty"`     // history
	Ping     uint64           `json:"ping,omitempty"`     // ping

	Conversation *Conversation `json:"conversation,omitempty"` // conversation
}

// MarshalJSON writes only the fields of the frame's type, and always lists
// the sessions of a snapshot, an outside frame and a closed frame, even when
// there are none, and both labels of a history frame.
func (f Frame) MarshalJSON() ([]byte, error) {
	switch f.T {
	case FrameSnapshot:
		h := f.Header
		if h == nil {
			h = &Header{}
		}
		s := f.Sessions
		if s == nil {
			s = []Session{}
		}
		return json.Marshal(struct {
			T        string    `json:"t"`
			Sessions []Session `json:"sessions"`
			Header   *Header   `json:"header"`
		}{f.T, s, h})
	case FrameUpsert:
		return json.Marshal(struct {
			T       string   `json:"t"`
			Session *Session `json:"session"`
		}{f.T, f.Session})
	case FrameRemove:
		return json.Marshal(struct {
			T  string `json:"t"`
			ID string `json:"id"`
		}{f.T, f.ID})
	case FrameEvent:
		return json.Marshal(struct {
			T     string       `json:"t"`
			Event *event.Event `json:"event"`
		}{f.T, f.Event})
	case FrameOutside:
		o := f.Outside
		if o == nil {
			o = []OutsideSession{}
		}
		return json.Marshal(struct {
			T       string           `json:"t"`
			Outside []OutsideSession `json:"outside"`
			Problem string           `json:"problem,omitempty"`
		}{f.T, o, f.Problem})
	case FrameClosed:
		c := f.Closed
		if c == nil {
			c = []ClosedSession{}
		}
		return json.Marshal(struct {
			T      string          `json:"t"`
			Closed []ClosedSession `json:"closed"`
		}{f.T, c})
	case FrameHistory:
		return json.Marshal(struct {
			T    string `json:"t"`
			Undo string `json:"undo"`
			Redo string `json:"redo"`
		}{f.T, f.Undo, f.Redo})
	case FramePing:
		return json.Marshal(struct {
			T    string `json:"t"`
			Ping uint64 `json:"ping"`
		}{f.T, f.Ping})
	case FrameHeader:
		h := f.Header
		if h == nil {
			h = &Header{}
		}
		return json.Marshal(struct {
			T      string  `json:"t"`
			Header *Header `json:"header"`
		}{f.T, h})
	case FrameConversation:
		c := f.Conversation
		if c == nil {
			c = &Conversation{}
		}
		if c.Items == nil {
			cc := *c
			cc.Items = []Item{}
			c = &cc
		}
		return json.Marshal(struct {
			T            string        `json:"t"`
			Conversation *Conversation `json:"conversation"`
		}{f.T, c})
	}
	return nil, fmt.Errorf("ipc: unknown frame type %q", f.T)
}
