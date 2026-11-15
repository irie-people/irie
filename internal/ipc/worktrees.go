package ipc

// OpWorktrees is the sweep of the closed sessions' worktrees (DESIGN §4.5):
// each worktree of a closed session that is there, whether it could go,
// its branch being in its repo's land branch with nothing else in it or
// running in it, or why it stays. A dry run by default; with Drop it
// removes those that can go, keeping their branches, so that reopen adds
// each back. A drop asked for from inside an agent is refused. It answers
// a WorktreesResult.
const OpWorktrees = "worktrees"

// WorktreesArgs asks for the sweep.
type WorktreesArgs struct {
	Drop bool `json:"drop,omitempty"` // remove those that can go; else only say
}

// WorktreesResult is what the sweep found, and with Drop what it removed.
type WorktreesResult struct {
	Trees   []ClosedTree `json:"trees"`             // those that can go first, then the rest, each newest closed first
	Can     int          `json:"can"`               // how many can go
	Bytes   int64        `json:"bytes"`             // what those take on the disk
	Dropped int          `json:"dropped,omitempty"` // how many the sweep removed, with Drop
}

// ClosedTree is a closed session's worktree as the sweep found it.
type ClosedTree struct {
	Session string `json:"session"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Branch  string `json:"branch,omitempty"`
	Commit  string `json:"commit,omitempty"` // the branch's tip
	Bytes   int64  `json:"bytes"`            // what it takes on the disk
	// Keep says why it stays, in plain words; "" when it can go.
	Keep string `json:"keep,omitempty"`
	// Busy: what keeps it is that something runs in it now, which may end.
	Busy    bool `json:"busy,omitempty"`
	Dropped bool `json:"dropped,omitempty"` // this sweep removed it
}
