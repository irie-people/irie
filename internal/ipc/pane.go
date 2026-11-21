package ipc

// The panes beside a session (docs/proposals/workspaces.md §4): the Mac app
// shows them as a rolodex to the right of the agent's terminal. A pane can be
// a shell of the user's own, which the core runs in the session's folder on
// the dock server, the tmux server the Dock's commands run on, so it never
// resizes an agent, outlives a restart of the app or the core, and ends with
// its session. Served when hello's features name OpPane.
const (
	// OpPane says how a pane's shell stands, and starts, restarts or ends it.
	OpPane = "pane"
	// OpCard asks the Mac app to show a card of a kind beside a session,
	// adding one when the session has none of that kind: what `irie ctl
	// card` and `irie ctl open` send, as for an agent its person asked to
	// open their mail beside it, or to pull up a file. The app alone reads
	// what the card shows. It answers with nothing, or an error when no Mac
	// app is open.
	OpCard = "card"
)

// CardArgs name the session and the kind of card to show beside it. A code
// card may name a file to open in it, and where in it to put the cursor.
type CardArgs struct {
	Session string `json:"session"`
	Kind    string `json:"kind"`
	// Path is a file a code card opens: absolute and clean, a regular file
	// of at most CardFileMax bytes. Only a code card takes one.
	Path string `json:"path,omitempty"`
	// Line and Col are where in Path the cursor goes, from 1; 0 for none.
	Line int `json:"line,omitempty"`
	Col  int `json:"col,omitempty"`
}

// CardFileMax is the largest file a code card opens: 8 MiB.
const CardFileMax = 8 << 20

// CardKinds are the kinds of card a client can be asked to show: the
// Mac app's PaneKind, by name.
var CardKinds = []string{"terminal", "browser", "markets", "mail", "code", "vault", "helpers", "dock"}

// PaneArgs name the session and one of its panes, by an id the client
// picks: one to twenty-four lower-case letters and digits. Start starts the
// pane's shell when none runs; Restart starts a new one in its place, as
// after the old one ended; End ends it. With none of them the result only
// says how it stands.
type PaneArgs struct {
	Session string `json:"session"`
	Pane    string `json:"pane"`
	Start   bool   `json:"start,omitempty"`
	Restart bool   `json:"restart,omitempty"`
	End     bool   `json:"end,omitempty"`
}

// PaneResult is how the pane's shell stands, and how a client attaches to
// it: tmux -S Socket attach-session -t =Target.
type PaneResult struct {
	// State is DockStateRunning, DockStateExited (its last screen stays
	// until it restarts) or DockStateStopped (none runs).
	State string `json:"state"`
	// Exit is an exited shell's code.
	Exit   *int   `json:"exit,omitempty"`
	Tmux   string `json:"tmux,omitempty"`
	Socket string `json:"socket,omitempty"`
	// Target is the shell's tmux session, while it has one.
	Target string `json:"target,omitempty"`
	// Cwd is the folder the shell starts in: the session's worktree, or
	// the folder it runs in.
	Cwd string `json:"cwd,omitempty"`
}

// OpSizes tells the core the size, in cells, the Mac app shows each
// session's terminal at. Whether a session's panes show is its own (the
// app's rolodex), so its terminal is narrower beside them than another's
// without. The core keeps each agent's window at its own session's size,
// so showing a session resizes no other agent, and the one shown comes in
// at its own size (DESIGN §2.3). It applies them only while a Mac app's
// view is open, and forgets them as the last one closes. Served when
// hello's features name it.
const OpSizes = "sizes"

// SizesArgs are every session's size, Size, and the sessions whose size
// differs from it, Sessions, by id. A zero Size sends every agent window
// back to the slot's size, as when the app can't tell its terminal's cells.
type SizesArgs struct {
	Size     Size            `json:"size"`
	Sessions map[string]Size `json:"sessions,omitempty"`
}

// Size is a terminal's size in cells: W columns, H rows.
type Size struct {
	W int `json:"w"`
	H int `json:"h"`
}

// SizeMax is the most columns or rows a size may have, as tmux's windows.
const SizeMax = 10000
