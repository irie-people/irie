package ipc

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/irie-hq/irie/internal/event"
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
)

// Error codes.
const (
	CodeBadRequest = "bad_request"
	CodeBadArgs    = "bad_args"
	CodeUnknownOp  = "unknown_op"
	CodeNotFound   = "not_found"
	CodeAmbiguous  = "ambiguous" // a name that fits more than one thing; the message lists them
	CodeInternal   = "internal"
	// CodeNeedsStronger refuses a delegated task that no profile cheaper
	// than its delegator is accurate enough for.
	CodeNeedsStronger = "needs_stronger"
)

// Session states (DESIGN §5.3), derived from the session's events.
const (
	StateFailed   = "failed"
	StateExited   = "exited"
	StateNeedsYou = "needs_you"
	StateStalled  = "stalled"
	StateWorking  = "working"
	StateStarting = "starting"
	StateDone     = "done"
	StateIdle     = "idle"
)

// Session is one row of the session list.
type Session struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Agent    string    `json:"agent,omitempty"` // "main"
	State    string    `json:"state"`
	Reason   string    `json:"reason,omitempty"`   // why it needs you, stalled or failed
	Detail   string    `json:"detail,omitempty"`   // one line for the status strip
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
}

// Header is what the sidebar shows that is not a row.
type Header struct {
	Version string  `json:"version,omitempty"`
	Plan    *Plan   `json:"plan,omitempty"`   // the Claude plan's use, from the newest status line
	Memory  *Memory `json:"memory,omitempty"` // the machine's memory
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
}

type SubscribeArgs struct {
	Events  bool   `json:"events,omitempty"`
	Session string `json:"session,omitempty"`
	Agent   string `json:"agent,omitempty"`
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
	Name    string `json:"name,omitempty"`
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
	// Continues is the session a failover carries the task on from.
	Continues string `json:"continues,omitempty"`
}

type SpawnResult struct {
	Session Session `json:"session"`
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
}

// LocalResult is the local server and the models it can run, weighed
// against the machine's memory.
type LocalResult struct {
	URL     string       `json:"url"`
	Own     bool         `json:"own"`     // Irie runs this server
	Running bool         `json:"running"` // it answers
	Version string       `json:"version,omitempty"`
	Error   string       `json:"error,omitempty"`  // why it cannot be used
	Chosen  string       `json:"chosen,omitempty"` // the model local profiles with no model of their own run
	Context int          `json:"context"`          // the context it loads models with, when Irie runs it
	KVCache string       `json:"kv_cache"`
	Memory  Memory       `json:"memory"`
	Models  []LocalModel `json:"models"` // the cheapest in memory first
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

type ShowArgs struct {
	Session string `json:"session"`
	Agent   string `json:"agent,omitempty"`
	Focus   bool   `json:"focus"`
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
}

// Frame types streamed after subscribe.
const (
	FrameSnapshot = "snapshot"
	FrameUpsert   = "upsert"
	FrameRemove   = "remove"
	FrameEvent    = "event"  // only with SubscribeArgs.Events
	FrameHeader   = "header" // the header changed; an addition to DESIGN §2.4
)

// Frame is one streamed message. Which fields are set depends on T.
type Frame struct {
	T        string       `json:"t"`
	Sessions []Session    `json:"sessions,omitempty"` // snapshot
	Header   *Header      `json:"header,omitempty"`   // snapshot, header
	Session  *Session     `json:"session,omitempty"`  // upsert
	ID       string       `json:"id,omitempty"`       // remove
	Event    *event.Event `json:"event,omitempty"`    // event
}

// MarshalJSON writes only the fields of the frame's type, and always lists
// the sessions of a snapshot, even when there are none.
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
	case FrameHeader:
		h := f.Header
		if h == nil {
			h = &Header{}
		}
		return json.Marshal(struct {
			T      string  `json:"t"`
			Header *Header `json:"header"`
		}{f.T, h})
	}
	return nil, fmt.Errorf("ipc: unknown frame type %q", f.T)
}
