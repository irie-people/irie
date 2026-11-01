package ipc

import "time"

// OpPeers says how the cores this one is paired with are, as it last heard
// from each (docs/proposals/multi-host.md §4.1). With reload it first reads
// peers.json again, as `irie peer add` and `remove` ask after they change it.
const OpPeers = "peers"

// CodePeerDown is a call for a session on a machine whose core is not
// answering.
const CodePeerDown = "peer_down"

type PeersArgs struct {
	Reload bool `json:"reload,omitempty"`
}

type PeersResult struct {
	// Name is this machine's: remote.name, else its host name.
	Name  string     `json:"name"`
	Peers []PeerInfo `json:"peers"`
}

// PeerInfo is one of the cores this one is paired with.
type PeerInfo struct {
	Name     string    `json:"name"`
	URL      string    `json:"url"`
	Up       bool      `json:"up"`                // its stream runs
	Since    time.Time `json:"since"`             // when it came up or went down
	Problem  string    `json:"problem,omitempty"` // why it is down
	Version  string    `json:"version,omitempty"`
	Sessions int       `json:"sessions"`
	Load     *Load     `json:"load,omitempty"`
	Memory   *Memory   `json:"memory,omitempty"`
	// Refuses says why no task starts there now, by ^name or by auto,
	// such as hooks it lacks; empty when it takes them.
	Refuses string `json:"refuses,omitempty"`
}

// Machine is what a core says of its machine that another core needs
// before it starts a task there (docs/proposals/multi-host.md §4.2, §4.4).
type Machine struct {
	Name string `json:"name"`
	OS   string `json:"os"`
	// Claude says whether Claude Code is signed in here: ok, missing or
	// unknown; empty before the first check.
	Claude string `json:"claude,omitempty"`
	// Profiles are the profiles a task here may name.
	Profiles []string `json:"profiles,omitempty"`
	// Hooks are the user's own Claude Code hooks here, from
	// ~/.claude/settings.json: each the SHA-256 of its event, matcher and
	// command, with the program's folder and the home folder left out, so
	// two machines can tell that they run the same hooks without either
	// saying what they are.
	Hooks []string `json:"hooks,omitempty"`
	// HooksMissing are the programs those hooks start that this machine
	// lacks.
	HooksMissing []string `json:"hooks_missing,omitempty"`
	// Disk is the free space where worktrees go, in bytes.
	Disk int64 `json:"disk,omitempty"`
}
