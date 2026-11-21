package ipc

import (
	"time"

	"github.com/irie-people/irie/internal/brand"
)

// The sidebar's chain (DESIGN §4.23, docs/proposals/local-chain.md). Every
// change to how the sessions are organised, their order, Pinned and
// Parked, your groups and spaces and the names you gave them, is a version:
// the whole organisation as canonical JSON, naming the version before it,
// and named by the SHA-512 of its bytes, as git names its commits. It stays
// on this computer, holds no one's identity, and history is never
// rewritten: undo, redo and restore add versions too.
const (
	// OpChain reads the chain, or brings a version back (ChainArgs' Do).
	OpChain = "chain"
)

// What OpChain does, ChainArgs' Do.
const (
	ChainStatus  = ""        // where the chain is kept and how it stands
	ChainLog     = "log"     // the versions, newest first
	ChainShow    = "show"    // one version
	ChainHead    = "head"    // the newest version's name
	ChainRestore = "restore" // a version's organisation, brought back as a step of the history
)

// ChainFormat names a version's form, version 1, under the product's own
// name (internal/brand): "irie-sidebar/1".
const ChainFormat = brand.Slug + "-sidebar/1"

// ChainLogDefault is how many versions a log gives when Limit is 0.
const ChainLogDefault = 20

// ChainArgs say what to do with the chain. Version is a version's name, or
// at least six hex digits of its start, for show and restore; Limit is how
// many versions log gives (ChainLogDefault for 0), the newest first.
type ChainArgs struct {
	Do      string `json:"do,omitempty"`
	Version string `json:"version,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

// ChainResult is the chain as it stands: where it is kept, its newest
// version's name, and what is wrong with it, if anything, such as a
// version that does not match its name or a write that failed. Log fills
// Versions, newest first; show and restore fill Version, show with the
// version's canonical bytes as Raw, which its name is the SHA-512 of, and
// restore with what it changed.
type ChainResult struct {
	Dir   string `json:"dir"`
	Head  string `json:"head,omitempty"`
	Count int    `json:"count"` // the versions kept
	// Vault is where the key that opens the versions is kept: "keychain",
	// "secret-service" or "file" (chain.KeptInKeychain and so on); "" while
	// the vault is not open.
	Vault    string         `json:"vault,omitempty"`
	Problem  string         `json:"problem,omitempty"`
	Versions []ChainEntry   `json:"versions,omitempty"`
	Version  *ChainEntry    `json:"version,omitempty"`
	Raw      string         `json:"raw,omitempty"`
	Restored *ChainRestored `json:"restored,omitempty"`
}

// ChainEntry is one version as the core reads it: its name, the name of
// the version before it, when it was made, what changed in words, as the
// core names the sessions now, and how many cards, groups and spaces it
// holds. State names its organisation alone: every version of the same
// organisation shares it. Doc is the version itself, for show.
type ChainEntry struct {
	Name   string        `json:"name"`
	Parent string        `json:"parent,omitempty"`
	State  string        `json:"state"`
	At     time.Time     `json:"at"`
	What   string        `json:"what"`
	Cards  int           `json:"cards"`
	Pinned int           `json:"pinned"`
	Parked int           `json:"parked"`
	Groups int           `json:"groups"`
	Spaces int           `json:"spaces"`
	Doc    *ChainVersion `json:"doc,omitempty"`
	// Names are the names the sessions of Doc have now, by id, for a
	// reader: a version keeps only the names you gave.
	Names map[string]string `json:"names,omitempty"`
}

// ChainRestored is what a restore changed: the cards it put back as the
// version had them, those of the version that are closed now, which stay
// closed, and the sessions it does not name, which stay as they are; and
// the step of the history that undoes it.
type ChainRestored struct {
	Cards  int    `json:"cards"`
	Closed int    `json:"closed"`
	Kept   int    `json:"kept"`
	Label  string `json:"label"`
}

// ChainVersion is a version: what its canonical bytes, and so its name,
// come from. At is when it was made, in milliseconds since 1970 UTC: a
// version holds whole numbers only.
type ChainVersion struct {
	Format string    `json:"format"`
	Parent string    `json:"parent,omitempty"`
	At     int64     `json:"at"`
	What   ChainWhat `json:"what"`
	Org    ChainOrg  `json:"org"`
}

// ChainWhat is what made a version: the action (Do), by its kind in the
// history, such as park, or begin, undo, redo, restore, rename-group or
// new-space, rename-space, move-space and remove-space; for undo and redo,
// the kind of the step they took (Of); and what it acted on, by id only.
// Version is the version a restore brought back.
type ChainWhat struct {
	Do       string   `json:"do"`
	Of       string   `json:"of,omitempty"`
	Sessions []string `json:"sessions,omitempty"`
	Groups   []string `json:"groups,omitempty"`
	Spaces   []string `json:"spaces,omitempty"`
	Version  string   `json:"version,omitempty"`
}

// ChainOrg is the organisation: the spaces in their order, the first
// first; your groups, in the order they were made; and this machine's open
// sessions, as cards in the list's order.
type ChainOrg struct {
	Spaces []Space     `json:"spaces,omitempty"`
	Groups []Group     `json:"groups,omitempty"`
	Cards  []ChainCard `json:"cards,omitempty"`
}

// ChainCard is a session as a version keeps it: its id; the name you gave
// it, and none when Irie or its agent named it; whether it is pinned or
// parked; and its space and group, "" for the first space and for none.
type ChainCard struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Pinned bool   `json:"pinned,omitempty"`
	Parked bool   `json:"parked,omitempty"`
	Space  string `json:"space,omitempty"`
	Group  string `json:"group,omitempty"`
}
