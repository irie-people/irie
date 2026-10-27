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
	// OpAttachInfo is the planned op of DESIGN §2.4, here for the Mac app:
	// how a terminal outside Irie attaches to the slot (docs/MAC.md).
	OpAttachInfo = "attach-info"
	// OpMove is an addition to the ops of DESIGN §2.4: it carries an idle
	// Claude Code session that runs outside Irie on in an Irie pane (§4.6).
	OpMove = "move"
)

// ViewApp is the SubscribeArgs.View of the Mac app. While one subscribes
// with it, the slot fills irie:0 (docs/MAC.md).
const ViewApp = "app"

// Error codes.
const (
	CodeBadRequest = "bad_request"
	CodeBadArgs    = "bad_args"
	CodeUnknownOp  = "unknown_op"
	CodeNotFound   = "not_found"
	CodeAmbiguous  = "ambiguous" // a name that fits more than one thing; the message lists them
	CodeBusy       = "busy"      // the session is busy or still running, so this cannot happen now
	CodeInternal   = "internal"
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
	// Tokens is what the session's model calls have used, from its
	// transcripts; nil until Irie has read any usage.
	Tokens *Tokens `json:"tokens,omitempty"`
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

// Header is the part of a snapshot that is not a row.
type Header struct {
	Version string `json:"version,omitempty"`
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
}

type SubscribeArgs struct {
	Events  bool   `json:"events,omitempty"`
	Session string `json:"session,omitempty"`
	Agent   string `json:"agent,omitempty"`
	View    string `json:"view,omitempty"` // "" or ViewApp
	// Conversation adds the session's conversation frames; it needs Session.
	Conversation bool `json:"conversation,omitempty"`
}

// AttachInfoResult is how to attach a tmux client to the session that holds
// the slot: tmux -S Socket attach-session -t Target.
type AttachInfoResult struct {
	Tmux   string `json:"tmux"`   // the tmux binary Irie runs
	Socket string `json:"socket"` // the server's socket path
	Target string `json:"target"` // the session to attach to
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
}

type SpawnResult struct {
	Session Session `json:"session"`
}

// CompleteArgs carry the command bar token to complete, such as "@web" or
// "+fa".
type CompleteArgs struct {
	Text string `json:"text"`
}

// CompleteResult lists the whole tokens that complete it, sorted.
type CompleteResult struct {
	Items []string `json:"items"`
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
	FrameEvent    = "event" // only with SubscribeArgs.Events
	// FrameOutside lists the sessions outside Irie (DESIGN §4.6): once
	// after the first snapshot, then whenever the list or its problem
	// changes. A subscription to one session gets none.
	FrameOutside = "outside"
)

// Frame is one streamed message. Which fields are set depends on T.
type Frame struct {
	T        string           `json:"t"`
	Sessions []Session        `json:"sessions,omitempty"` // snapshot
	Header   *Header          `json:"header,omitempty"`   // snapshot
	Session  *Session         `json:"session,omitempty"`  // upsert
	ID       string           `json:"id,omitempty"`       // remove
	Event    *event.Event     `json:"event,omitempty"`    // event
	Outside  []OutsideSession `json:"outside,omitempty"`  // outside
	Problem  string           `json:"problem,omitempty"`  // outside: why the list may be old

	Conversation *Conversation `json:"conversation,omitempty"` // conversation
}

// MarshalJSON writes only the fields of the frame's type, and always lists
// the sessions of a snapshot and of an outside frame, even when there are
// none.
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
