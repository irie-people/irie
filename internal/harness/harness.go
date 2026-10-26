// Package harness defines how Irie launches an agent CLI and reads its hooks.
// Each supported harness has an adapter in a subpackage (DESIGN §3.4); later
// milestones add reopening and subagent transcripts to the interface.
package harness

import (
	"crypto/rand"
	"fmt"
	"time"

	"github.com/irie-hq/irie/internal/config"
	"github.com/irie-hq/irie/internal/event"
)

// Spawn describes one new session.
type Spawn struct {
	Session  string        // Irie session id
	Name     string        // slug shown in the sidebar
	Task     string        // the task, verbatim
	Cwd      string        // working directory
	Dir      string        // the session's state dir, sessions/<id>
	VendorID string        // vendor session id, assigned before launch
	Bin      string        // the irie binary, which hook commands run
	HoldMax  time.Duration // hold.max; hook timeouts sit above it
	Probe    Probe         // what probing the CLI found
}

// Probe is what a harness learned about its CLI before a launch.
type Probe struct {
	Version  string          // the CLI's version; "" when unknown
	Flags    map[string]bool // optional flags the CLI offers
	Warnings []string        // what Irie cannot do with this CLI, and why
}

// HookReply is the core's answer to a hook: context for the agent, or a
// reason to deny the call it is about to make. Both empty means nothing to
// say.
type HookReply struct {
	Context string
	Deny    string
}

// Harness is one agent CLI.
type Harness interface {
	ID() string
	// Launch returns argv, extra env and files to write into the session's
	// state dir (name → contents) for a new session.
	Launch(p config.Profile, s Spawn) (argv []string, env map[string]string, files map[string][]byte, err error)
	// Normalize turns one raw hook payload into envelope events (DESIGN
	// §5.1). The core stamps their session, seq and time.
	Normalize(raw []byte) ([]event.Event, error)
	// Respond renders the core's reply in the harness's own hook output
	// dialect; nil means print nothing.
	Respond(e event.Event, r HookReply) []byte
}

// Prober is a harness that inspects its CLI before a launch.
type Prober interface {
	// Probe runs the profile's CLI with env to learn its version and flags.
	Probe(p config.Profile, env []string) Probe
}

// NewUUID returns a random version 4 UUID (RFC 9562).
func NewUUID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
