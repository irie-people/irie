// Package harness defines how Irie launches an agent CLI. Each supported
// harness has an adapter in a subpackage (DESIGN §3.4); later milestones add
// event normalisation, hook replies and reopening to the interface.
package harness

import (
	"crypto/rand"
	"fmt"

	"github.com/irie-hq/irie/internal/config"
)

// Spawn describes one new session.
type Spawn struct {
	Session  string // Irie session id
	Name     string // slug shown in the sidebar
	Task     string // the task, verbatim
	Cwd      string // working directory
	Dir      string // the session's state dir, sessions/<id>
	VendorID string // vendor session id, assigned before launch
}

// Harness is one agent CLI.
type Harness interface {
	ID() string
	// Launch returns argv, extra env and files to write into the session's
	// state dir (name → contents) for a new session.
	Launch(p config.Profile, s Spawn) (argv []string, env map[string]string, files map[string][]byte, err error)
}

// NewUUID returns a random version 4 UUID (RFC 9562).
func NewUUID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
