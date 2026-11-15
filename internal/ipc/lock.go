package ipc

import (
	"time"

	"github.com/irie-people/irie/internal/brand"
)

// OpLock is the agent locks (DESIGN §4.21): each session's setup as its
// agent started, as its lock records it, and what the last check found for
// the harness and package versions they name. With check, it first asks
// OSV.dev about every one of them. It takes LockArgs and answers a
// LockResult.
const OpLock = "lock"

// Lock is one session's agent lock (DESIGN §4.21): how its agent was set up
// as it started, as a lock file records exact versions. The core writes it
// to sessions/<id>/lock.json at each spawn, resume, fork and reopen.
type Lock struct {
	Session string    `json:"session"`
	Written time.Time `json:"written"`
	At      string    `json:"at"` // what started it: LockSpawn, LockResume, LockFork or LockReopen

	Irie    LockPart `json:"irie"`    // Irie itself: its version and the hash of its binary
	Harness LockPart `json:"harness"` // the agent's CLI: its id, version, binary and package
	Profile string   `json:"profile"`
	Model   string   `json:"model,omitempty"`
	// PermissionMode is the mode the CLI was started in; "" = its own
	// default, which the user's settings may set.
	PermissionMode string `json:"permission_mode,omitempty"`

	MCP     []LockPart `json:"mcp,omitempty"`     // its MCP servers: Irie's own and those of Claude Code's config
	Plugins []LockPart `json:"plugins,omitempty"` // Irie's plugins that are on (DESIGN §4.17), and yours
	Skills  []LockPart `json:"skills,omitempty"`  // the skills of Irie's it gets
	Hooks   []LockPart `json:"hooks,omitempty"`   // Irie's hooks, then yours and its project's
	// Notes say what the lock could not read, and why.
	Notes []string `json:"notes,omitempty"`
}

// What started a session, as its lock's at.
const (
	LockSpawn  = "spawn"  // a new task
	LockResume = "resume" // a conversation carried on, as one moved into Irie
	LockFork   = "fork"   // a fork of a context (DESIGN §4.13)
	LockReopen = "reopen" // a closed session reopened
)

// Where a part of a lock comes from, as its from.
const (
	LockFromIrie    = brand.Slug // Irie brings it
	LockFromUser    = "user"     // the user's own config, for every folder
	LockFromLocal   = "local"    // the user's own config, for this folder only
	LockFromProject = "project"  // the project's files
)

// LockPart is one part of a lock: a harness, a server, a plugin, a skill
// or a hook.
type LockPart struct {
	Name    string `json:"name"`
	From    string `json:"from,omitempty"`    // LockFromIrie and the rest
	Version string `json:"version,omitempty"` // its version, where it has one
	// Package is the package it comes from, where Irie can tell, with an
	// exact version: what a check asks OSV.dev about.
	Package *LockPackage `json:"package,omitempty"`
	// Command is what runs it: the harness's binary, a server's program
	// and package or address, a hook's program. Its other words, which may
	// hold a secret, are only in its hash.
	Command string `json:"command,omitempty"`
	// Hash is "sha256:" and the hex of a plugin's or a skill's files, or
	// of a server's or a hook's whole command.
	Hash string `json:"hash,omitempty"`
	// Events are what Irie's hooks are for, as Claude Code names them.
	Events []string `json:"events,omitempty"`
}

// LockPackage is a package at one version, as OSV.dev names it, such as
// npm's @anthropic-ai/claude-code at 2.1.250. On a row it carries what the
// last check found.
type LockPackage struct {
	Ecosystem string `json:"ecosystem"` // npm or PyPI
	Name      string `json:"name"`
	Version   string `json:"version"`
	// Part is what it is in the session, in words: "Claude Code", "MCP
	// server github". The lock's part says so already, so its package
	// leaves this out.
	Part string `json:"part,omitempty"`

	// What the last check found, on a row: whether it has been checked,
	// how many advisories affect it, how many of those no version fixes
	// yet, and the version that fixes the others.
	Checked    bool   `json:"checked,omitempty"`
	Advisories int    `json:"advisories,omitempty"`
	Unfixed    int    `json:"unfixed,omitempty"`
	Fixed      string `json:"fixed,omitempty"`
}

// LockRow is what a row says of its session's lock (DESIGN §4.21): the
// packages a check asks about, each with what the last check found, and
// the card's words while an advisory affects one.
type LockRow struct {
	Packages []LockPackage `json:"packages"`
	// Advisories is how many known advisories affect them, by the last
	// check.
	Advisories int `json:"advisories,omitempty"`
	// Words say it for the card while there are any: "Claude Code
	// 2.1.250: 1 advisory, fixed in 2.1.251". "" while there are none.
	Words string `json:"words,omitempty"`
}

// LockArgs ask for the locks of every open session, or one; with Check,
// OSV.dev is asked about them first.
type LockArgs struct {
	Session string `json:"session,omitempty"` // an open session's id or name, or a closed one's id; "" = every open one
	Check   bool   `json:"check,omitempty"`
}

// LockResult is the locks, and what the last check found.
type LockResult struct {
	Sessions []LockSession `json:"sessions"`
	// Packages are the harness and package versions the locks name, each
	// with what the last check found for it.
	Packages []LockFound `json:"packages"`
	// Checked is when OSV.dev was last asked; zero = never. OSV is the
	// address it was asked at.
	Checked time.Time `json:"checked,omitzero"`
	OSV     string    `json:"osv"`
	// Asked says this call asked OSV.dev, as check does.
	Asked bool `json:"asked,omitempty"`
	// Daily is lock.check = "daily": the core asks OSV.dev once a day.
	Daily bool `json:"daily,omitempty"`
}

// LockSession is one session's lock. A peer's session has its lock on its
// own machine: here, only the packages its row names.
type LockSession struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Host string `json:"host,omitempty"`
	Open bool   `json:"open,omitempty"`
	Lock *Lock  `json:"lock,omitempty"`
	// Packages are what a check asks about for it, with what the last
	// check found.
	Packages []LockPackage `json:"packages,omitempty"`
	// None says why it has no lock, such as a session started before Irie
	// wrote locks.
	None string `json:"none,omitempty"`
}

// LockFound is one package version the locks name, with what the last
// check found for it: the advisories that affect it, and which sessions
// run it.
type LockFound struct {
	Ecosystem  string         `json:"ecosystem"`
	Name       string         `json:"name"`
	Version    string         `json:"version"`
	Part       string         `json:"part,omitempty"`
	Checked    time.Time      `json:"checked,omitzero"` // when OSV.dev said; zero = not yet
	Advisories []LockAdvisory `json:"advisories,omitempty"`
	Sessions   []string       `json:"sessions,omitempty"` // the names of the sessions whose locks name it
}

// LockAdvisory is one advisory OSV.dev lists for a package version: its
// id, what it is, and the first version after the one locked that fixes
// it.
type LockAdvisory struct {
	ID       string    `json:"id"`
	Summary  string    `json:"summary,omitempty"`
	Fixed    string    `json:"fixed,omitempty"`   // "" when no version fixes it yet
	Modified time.Time `json:"modified,omitzero"` // when OSV.dev last changed it
}
