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
}
