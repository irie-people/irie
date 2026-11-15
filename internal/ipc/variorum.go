package ipc

import "time"

// VariorumArgs ask the variorum op for one of its actions
// (docs/proposals/writers.md).
type VariorumArgs struct {
	Action  string `json:"action,omitempty"`  // status (default), connect, forget, notes, read, show
	Session string `json:"session,omitempty"` // read: whose note, when Note is empty
	Note    string `json:"note,omitempty"`    // read, show
	Block   string `json:"block,omitempty"`   // show
	Target  string `json:"target,omitempty"`  // show: note (default) or takes
	Setup   string `json:"setup,omitempty"`   // connect: the setup as copied
}

// What the variorum op does.
const (
	VariorumStatus  = "status"
	VariorumConnect = "connect"
	VariorumForget  = "forget"
	VariorumNotes   = "notes"
	VariorumRead    = "read"
	VariorumShow    = "show"
)

// VariorumResult is the variorum op's answer: where things stand, and what
// the action brought.
type VariorumResult struct {
	State    string `json:"state"`              // ready, setup (a setup missing), needs (Variorum missing, or a kept setup's program fails its checks), off (plugin off or not in this build)
	Text     string `json:"text"`               // where things stand, in words: what is missing and what fixes it
	Sessions bool   `json:"sessions,omitempty"` // Claude Code's setup is kept, for writing sessions
	Tab      bool   `json:"tab,omitempty"`      // Any MCP client's setup is kept, for the tab
	// Installed is true when a Variorum.app is in an Applications folder:
	// the Mac app shows the tab only when it, Sessions or Tab is.
	Installed bool   `json:"installed,omitempty"`
	Error     string `json:"error,omitempty"` // the action failed: the door's own sentence, or Irie's
	// Notes answers notes: [] when none is shared, and left out of every
	// other answer.
	Notes []VariorumNoteRef `json:"notes,omitzero"`
	Note  *VariorumNote     `json:"note,omitempty"`  // read
	Shown string            `json:"shown,omitempty"` // show: shown or offered
}

// The states of Irie's part of Variorum.
const (
	VariorumReady = "ready" // both setups kept, their programs Variorum's door
	VariorumSetup = "setup" // a setup is missing: the text says which, and how to copy it
	VariorumNeeds = "needs" // Variorum is missing, or a kept setup's program fails its checks
	VariorumOff   = "off"   // the writing plugin is off, or not in this build
)

// VariorumNoteRef is a note shared with Irie, as list_notes names it.
type VariorumNoteRef struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Edited string `json:"edited,omitempty"`
}

// VariorumNote is a note as Variorum holds it when read: each paragraph a
// block, with its takes and comments.
type VariorumNote struct {
	ID     string          `json:"id"`
	Title  string          `json:"title"`
	Blocks []VariorumBlock `json:"blocks"`
	More   bool            `json:"more,omitempty"`
	Read   time.Time       `json:"read"`
}

// VariorumBlock is one paragraph of a note, with every take of it that
// isn't archived.
type VariorumBlock struct {
	ID       string            `json:"id"`
	Name     string            `json:"name,omitempty"`
	Shown    string            `json:"shown,omitempty"` // the take id the note shows
	Takes    []VariorumTake    `json:"takes"`
	Comments []VariorumComment `json:"comments,omitempty"`
}

// VariorumTake is one version of a paragraph.
type VariorumTake struct {
	ID     string `json:"id"`
	Label  string `json:"label,omitempty"`
	Text   string `json:"text"`
	Chosen bool   `json:"chosen,omitempty"`
}

// VariorumComment is a comment on a paragraph.
type VariorumComment struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}
