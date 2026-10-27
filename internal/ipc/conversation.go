package ipc

import "time"

// OpConversation is an addition to the ops of DESIGN §2.4: a session's
// conversation, read from its Claude Code transcript, for the views that
// show it instead of the agent's raw terminal (docs/MAC.md).
const OpConversation = "conversation"

// FrameConversation carries a session's conversation to a client that
// subscribed to that session with SubscribeArgs.Conversation: all of it
// first, then what changed.
const FrameConversation = "conversation"

// ConversationArgs ask for a session's conversation, or only its newest
// Limit items.
type ConversationArgs struct {
	Session string `json:"session"`
	Limit   int    `json:"limit,omitempty"`
}

// Conversation is a session's conversation, or what changed in it.
type Conversation struct {
	Session string `json:"session"`
	// Transcript is the transcript it is read from; "" while there is none,
	// as for a session started before Irie followed transcripts.
	Transcript string `json:"transcript,omitempty"`
	Reset      bool   `json:"reset,omitempty"` // Items replace all the client has
	More       bool   `json:"more,omitempty"`  // older items were left out
	// Items are new or changed, in order. An item whose id the client has
	// replaces it; the others follow the items it has.
	Items []Item `json:"items"`
	// Needs is what the session waits on you for now: a permission, a
	// question, a plan, or a turn that failed. It is null when nothing is.
	Needs *Item `json:"needs"`
}

// Item kinds.
const (
	ItemUser  = "user"  // a message from you
	ItemText  = "text"  // the agent's text, in Markdown
	ItemTool  = "tool"  // a tool call
	ItemAgent = "agent" // a subagent it started
	ItemNote  = "note"  // an interruption, a compaction or a new conversation
	ItemNeeds = "needs" // what it waits on you for
)

// Tool call states.
const (
	ItemRunning = "running"
	ItemOK      = "ok"
	ItemError   = "error"
	ItemStopped = "stopped" // the turn ended before the call did
)

// Item is one entry of a conversation.
type Item struct {
	ID   string    `json:"id"`
	Kind string    `json:"kind"`
	Time time.Time `json:"time"`
	// Text is a message, the agent's Markdown, a note, or what a need asks.
	Text string `json:"text,omitempty"`
	// Phrase is a tool call or a subagent in plain words: "Running the
	// tests" while it runs, "Ran the tests" once it has.
	Phrase string `json:"phrase,omitempty"`
	// Detail is the exact command, path, pattern or URL of a tool call, a
	// subagent's task, or a plan to approve.
	Detail string `json:"detail,omitempty"`
	Tool   string `json:"tool,omitempty"`
	State  string `json:"state,omitempty"` // ItemRunning, ItemOK, ItemError or ItemStopped
	Ms     int64  `json:"ms,omitempty"`    // how long it ran
	// Added and Removed count the lines an edit changed.
	Added   int `json:"added,omitempty"`
	Removed int `json:"removed,omitempty"`
	// Group is read, edit or search for calls a view may gather into one
	// line ("Edited 3 files").
	Group     string   `json:"group,omitempty"`
	Output    string   `json:"output,omitempty"`     // the start of a failed call's output
	AgentType string   `json:"agent_type,omitempty"` // a subagent's kind, such as Explore
	Tools     int      `json:"tools,omitempty"`      // the tool calls a subagent made
	Options   []string `json:"options,omitempty"`    // a question's choices
	Reason    string   `json:"reason,omitempty"`     // a need's reason, as a row's
	Queued    bool     `json:"queued,omitempty"`     // a message typed while the agent worked
}
