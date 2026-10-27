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
// Limit items. With Agent "sub:<id>" they ask for the conversation of that
// helper, one of the session's subagents, read from its own transcript.
type ConversationArgs struct {
	Session string `json:"session"`
	Agent   string `json:"agent,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

// Conversation is a session's conversation, or what changed in it.
type Conversation struct {
	Session string `json:"session"`
	Agent   string `json:"agent,omitempty"` // the helper's, as sub:<id>; "" for the session's own
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
	// Pending are the messages sent to the agent that its transcript does
	// not have yet, oldest first; a view shows them after the items.
	Pending []Item `json:"pending"`
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
	// Text is a message, the agent's Markdown, a note, what a need asks, or
	// a subagent's task in a few words.
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
	Group     string `json:"group,omitempty"`
	Output    string `json:"output,omitempty"`     // the start of a failed call's output
	AgentType string `json:"agent_type,omitempty"` // a subagent's kind, such as Explore
	Agent     string `json:"agent,omitempty"`      // the helper a subagent item started, as sub:<id>, once known
	Tools     int    `json:"tools,omitempty"`      // the tool calls a subagent made
	// From is "parent" on a helper's first message: its task, from the
	// session that started it, not a message from you.
	From    string   `json:"from,omitempty"`
	Options []string `json:"options,omitempty"` // a question's choices
	Reason  string   `json:"reason,omitempty"`  // a need's reason, as a row's
	Queued  bool     `json:"queued,omitempty"`  // a message typed while the agent worked
	Pending bool     `json:"pending,omitempty"` // a message sent that the agent has not taken yet
}

// OpSend is an addition to the ops of DESIGN §2.4: it types a message to a
// session's agent the way a person would, a bracketed paste and Enter
// (DESIGN §4.7). It refuses with CodeBusy while the agent waits on you in
// its terminal, or cannot take a message.
const OpSend = "send"

// SendArgs carry the message to type to a session's agent.
type SendArgs struct {
	Session string `json:"session"`
	Text    string `json:"text"`
}

// OpFlip is an addition to the ops of DESIGN §2.4: it turns the slot's view
// of the session you picked between its conversation and its raw terminal
// (DESIGN §4.7), and gives the slot the keyboard.
const OpFlip = "flip"

// FlipResult says what the slot shows now.
type FlipResult struct {
	Terminal bool `json:"terminal"` // the raw terminal; else the conversation
}
