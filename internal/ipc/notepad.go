package ipc

import "time"

// The notepad (DESIGN §4.20): notes a person keeps for later, each one line
// of a checklist they tick off, which a sidebar shows between the selected
// session's details and the task bar. A note goes to the task bar as if it
// were typed there and sent: InputArgs.Note names it, and the core marks it
// sent once the input went through. The core keeps the notes in
// notepad.json, written as a transaction on every change, a copy of it each
// day, and the notes deleted in the last 30 days, which restore brings
// back.
const (
	// OpNotepad lists the notes, or adds, edits, checks, moves, deletes or
	// restores them.
	OpNotepad = "notepad"
	// FrameNotepad carries the notepad: once after the first snapshot,
	// then whenever it changes. A subscription to one session gets none.
	FrameNotepad = "notepad"
)

// What OpNotepad does, NotepadArgs' Do.
const (
	NotepadList    = ""
	NotepadAdd     = "add"
	NotepadEdit    = "edit"
	NotepadCheck   = "check"
	NotepadMove    = "move"
	NotepadDelete  = "delete"
	NotepadRestore = "restore"
)

// MaxNoteText is the most bytes a note's text holds, and MaxNotes the most
// notes the notepad holds, not counting the deleted ones. Past them a
// change is refused, never cut short.
const (
	MaxNoteText = 64 << 10
	MaxNotes    = 5000
)

// Note is one note of the notepad.
type Note struct {
	ID      string    `json:"id"`
	Text    string    `json:"text"`
	Done    bool      `json:"done,omitempty"`
	Created time.Time `json:"created"`
	Changed time.Time `json:"changed"`
	// Sent is when the note last went to the task bar, and Session the
	// session it started or went to, when there was one.
	Sent    time.Time `json:"sent,omitzero"`
	Session string    `json:"session,omitempty"`
	// Deleted is when a note in Recently Deleted was deleted.
	Deleted time.Time `json:"deleted,omitzero"`
}

// NotepadArgs say what to do with the notes. Each change is the same
// however often it is sent, so a client may send again whatever it is not
// sure went through:
//   - add puts a note with Text after the note After, or last, as ID when
//     given (the client's own id, which another add of the same id leaves
//     be), else under an id of the core's. An empty one is a line to type
//     in, as Return in a note of Notes makes, and a delete of a note still
//     empty keeps nothing;
//   - edit sets note ID's text; an edit of a deleted note brings it back
//     with the text, and of one the core never had adds it, so no typing
//     is ever lost;
//   - check ticks note ID off when Done, or on again;
//   - move puts note ID just before Before, or last;
//   - delete moves notes IDs (or ID) to Recently Deleted, and restore
//     brings them back to where they were.
type NotepadArgs struct {
	Do     string   `json:"do,omitempty"`
	ID     string   `json:"id,omitempty"`
	IDs    []string `json:"ids,omitempty"`
	Text   string   `json:"text,omitempty"`
	Done   bool     `json:"done,omitempty"`
	After  string   `json:"after,omitempty"`
	Before string   `json:"before,omitempty"`
}

// Notepad is the notes in their order, the person's, and Recently Deleted,
// the last deleted first. Problem says, when there is one, what went wrong
// with the notepad's file and what the core did about it; ReadOnly is set
// while the file is a newer Irie's, which this core only shows.
type Notepad struct {
	Notes    []Note `json:"notes"`
	Deleted  []Note `json:"deleted"`
	Problem  string `json:"problem,omitempty"`
	ReadOnly bool   `json:"read_only,omitempty"`
	// Path is the notepad's file, for a client to show in its file
	// manager; its copies are in the backups folder beside it.
	Path string `json:"path,omitempty"`
	// Rev counts the changes the file has had. A reply and a frame come on
	// connections of their own, so a client keeps the notepad with the
	// highest rev it has heard, and only a new subscription's first frame
	// may go back.
	Rev uint64 `json:"rev"`
}

// NotepadResult is the notepad after the op, and Note the note an add or
// an edit left.
type NotepadResult struct {
	Notepad
	Note *Note `json:"note,omitempty"`
}
