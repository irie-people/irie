package ipc

// The right sidebar's Dock (docs/proposals/right-dock.md §5.2): the controls
// a session's repo lists and the user's own, which the core runs on a tmux
// server of their own, the dock server, and a client shows. Served when
// hello's features name OpDock.
const (
	// OpDock lists a session's Dock controls and their state, and starts,
	// stops or restarts them, or trusts its repo's file.
	OpDock = "dock"
)

// Dock control states.
const (
	DockStateUntrusted = "untrusted" // its repo file waits for the user's trust
	DockStateStopped   = "stopped"   // a terminal control with no copy running
	DockStateRunning   = "running"
	DockStateExited    = "exited" // its command ended; its last screen stays
	DockStateReady     = "ready"  // a browser control, to show
)

// DockArgs name the session, and at most one control to act on, by its key.
// Ensure starts every trusted terminal control of the session with no copy
// running, as a client does when it shows the Dock. Trust trusts the repo
// file whose hash it gives, as the untrusted part of a result names it.
type DockArgs struct {
	Session string `json:"session"`
	Start   string `json:"start,omitempty"`
	Stop    string `json:"stop,omitempty"`
	Restart string `json:"restart,omitempty"`
	Ensure  bool   `json:"ensure,omitempty"`
	Trust   string `json:"trust,omitempty"`
}

// DockResult is the session's Dock: its controls in order, the repo's
// first, and what a client needs to attach to a terminal control's copy.
type DockResult struct {
	Controls []DockControl `json:"controls"`
	// Tmux and Socket attach a client to a running copy's Target, while the
	// dock server runs.
	Tmux   string `json:"tmux,omitempty"`
	Socket string `json:"socket,omitempty"`
	// Files says where the controls came from.
	Files []DockFile `json:"files"`
	// Untrusted is the repo file waiting for the user's trust, with what it
	// would run.
	Untrusted *DockUntrusted `json:"untrusted,omitempty"`
	// Mine is where the user's own Dock file goes, to create or open.
	Mine string `json:"mine"`
}

// DockControl is one control as a client draws it.
type DockControl struct {
	Key    string  `json:"key"`
	ID     string  `json:"id"`
	Title  string  `json:"title"`
	Type   string  `json:"type"`
	URL    string  `json:"url,omitempty"`
	Chrome bool    `json:"chrome"`
	Height float64 `json:"height,omitempty"`
	Source string  `json:"source"`
	Scope  string  `json:"scope"`
	State  string  `json:"state"`
	// Exit is an exited command's code.
	Exit *int `json:"exit,omitempty"`
	// Target is the tmux session of its copy, while one exists.
	Target  string `json:"target,omitempty"`
	Cwd     string `json:"cwd,omitempty"`
	Command string `json:"command,omitempty"`
}

// DockFile is a Dock file the core read for the session.
type DockFile struct {
	Path    string `json:"path"`
	Source  string `json:"source"`
	Trusted bool   `json:"trusted"`
	Problem string `json:"problem,omitempty"`
}

// DockUntrusted is a repo file the user has not trusted: the core runs
// none of its commands until the user trusts this hash.
type DockUntrusted struct {
	Path     string   `json:"path"`
	Hash     string   `json:"hash"`
	Commands []string `json:"commands"`
}
