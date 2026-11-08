package ipc

import "time"

// Ops for agents' contexts in git (DESIGN §4.13). They are served when
// hello's features name them.
const (
	// OpContext reads or names a point of a session's context: its turns,
	// a sync now, a tag, or the commit a point resolves to.
	OpContext = "context"
	// OpFork starts a session that carries a context on from any of its
	// turns, in a worktree of its own at that turn's code.
	OpFork = "fork"
)

// Context actions.
const (
	ContextLog     = "log"     // the turns, newest first
	ContextSync    = "sync"    // store what is new now
	ContextResolve = "resolve" // the commit a point names
	ContextTag     = "tag"     // name a point
	ContextWhere   = "where"   // the store's path, for git
	ContextClean   = "clean"   // let go now of the contexts past context.keep_days
)

// ContextArgs ask about a session's context. Session is an open session's
// id or name, a closed one's id, or any branch of the store. At names a
// point of it: "" its newest turn, N its turn N, -N the turn N before its
// newest, a tag, a commit, or the uuid of an entry of its conversation.
type ContextArgs struct {
	Action  string `json:"action"`
	Session string `json:"session,omitempty"`
	At      string `json:"at,omitempty"`
	Name    string `json:"name,omitempty"`  // the tag's name
	Limit   int    `json:"limit,omitempty"` // turns at most; 0 for all
}

// ContextResult is the store's path and what the action found.
type ContextResult struct {
	Repo    string        `json:"repo"`
	Session string        `json:"session,omitempty"` // the session's id, which names its branch
	Branch  string        `json:"branch,omitempty"`
	Commit  string        `json:"commit,omitempty"` // the point resolved, or the branch's head
	Cut     string        `json:"cut,omitempty"`    // the entry a point inside a turn ends at
	Turns   []ContextTurn `json:"turns,omitempty"`
	// What clean let go of, and the setting it went by: context.keep_days,
	// 0 when contexts are kept for good.
	Removed  []string `json:"removed,omitempty"`
	KeepDays int      `json:"keep_days,omitempty"`
}

// ContextTurn is one commit of a context: one turn of its conversation.
type ContextTurn struct {
	Commit       string    `json:"commit"`
	Time         time.Time `json:"time"`
	Turn         int       `json:"turn"`
	Prompt       string    `json:"prompt"`
	Said         string    `json:"said,omitempty"`
	Session      string    `json:"session"`
	Name         string    `json:"name,omitempty"`
	Conversation string    `json:"conversation,omitempty"`
	Leaf         string    `json:"leaf,omitempty"`
	Model        string    `json:"model,omitempty"`
	Code         string    `json:"code,omitempty"`
	Dirty        int       `json:"dirty,omitempty"`
	Partial      bool      `json:"partial,omitempty"`
	Cut          string    `json:"cut,omitempty"`
}

// ForkArgs fork an open session's context at a point (At, as in
// ContextArgs). The fork works in a new worktree of the session's repo at
// the code its turn recorded, or with Here in the session's own folder,
// sharing its files. Task is what the fork does first; with none it waits
// for you. Profile picks the agent, one that runs Claude Code, and Model
// the model for this fork alone; by default it runs as its source did.
type ForkArgs struct {
	Session string `json:"session"`
	At      string `json:"at,omitempty"`
	Task    string `json:"task,omitempty"`
	Name    string `json:"name,omitempty"`
	Profile string `json:"profile,omitempty"`
	Model   string `json:"model,omitempty"`
	Here    bool   `json:"here,omitempty"`
}

// ForkResult is the new session and the commit it carries on from.
type ForkResult struct {
	Session Session `json:"session"`
	From    string  `json:"from"`
	Turn    int     `json:"turn"`
}
