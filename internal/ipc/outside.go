package ipc

import (
	"fmt"
	"time"
)

// Kinds and statuses of a session outside Irie, as `claude agents --json`
// reports them. Other values are kept as they come.
const (
	KindInteractive = "interactive"
	KindBackground  = "background"

	StatusBusy    = "busy"
	StatusWaiting = "waiting"
	StatusIdle    = "idle"
)

// OutsideSession is a Claude Code session that runs outside Irie, such as
// one in another terminal: an entry of `claude agents --json` that is none
// of Irie's own (DESIGN §4.6). Its strings have no control characters,
// except Cwd, which is kept exactly, because Claude Code finds a
// conversation by its folder.
type OutsideSession struct {
	VendorID   string    `json:"vendor_id,omitempty"`   // the full session id, which claude --resume and move take
	ID         string    `json:"id,omitempty"`          // a background session's short id, which claude stop takes
	Name       string    `json:"name,omitempty"`        // its display name
	Kind       string    `json:"kind"`                  // interactive or background
	Status     string    `json:"status,omitempty"`      // busy, waiting, idle or another value; empty when its process is not running
	WaitingFor string    `json:"waiting_for,omitempty"` // what a waiting session waits for, such as "permission prompt"
	State      string    `json:"state,omitempty"`       // a background session's state, such as working or done
	PID        int       `json:"pid,omitempty"`
	Cwd        string    `json:"cwd"`
	Started    time.Time `json:"started,omitzero"`  // left out when Claude Code gave none
	Movable    bool      `json:"movable,omitempty"` // move would take it now
	Why        string    `json:"why,omitempty"`     // why move would not take it now
}

// Key names an outside session within a list: its vendor id, else its
// short id, else its pid, else its folder and start.
func (o OutsideSession) Key() string {
	switch {
	case o.VendorID != "":
		return o.VendorID
	case o.ID != "":
		return "id:" + o.ID
	case o.PID != 0:
		return fmt.Sprintf("pid:%d", o.PID)
	}
	return fmt.Sprintf("%s@%d", o.Cwd, o.Started.UnixMilli())
}

// MoveArgs pick the outside session to move into Irie by its vendor id,
// and the Claude Code profile it runs on there: by default
// spawn.default_profile, or the first Claude Code profile when that one is
// not.
type MoveArgs struct {
	VendorID string `json:"vendor_id"`
	Profile  string `json:"profile,omitempty"`
}

// MoveResult is the Irie session that carries the conversation on.
type MoveResult struct {
	Session Session `json:"session"`
}
