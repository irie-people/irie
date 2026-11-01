// Package harness defines how Irie launches an agent CLI and reads its hooks.
// Each supported harness has an adapter in a subpackage (DESIGN §3.4); later
// milestones add reopening and subagent transcripts to the interface.
package harness

import (
	"crypto/rand"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
)

// Levels are what Irie can do with a harness (DESIGN §3.1).
type Levels uint8

const (
	Generic   Levels = 1 << iota // G: launch, freeze and thaw, alive or exited, typed notes
	Observed                     // O: working, needs you and done from its own events
	Held                         // H: pause at the next tool call, notes at tool boundaries
	Subagents                    // S: a pane, a pause and notes per subagent
)

// Has reports whether l includes every level of x.
func (l Levels) Has(x Levels) bool { return l&x == x }

func (l Levels) String() string {
	var b strings.Builder
	for i, c := range "GOHS" {
		if l.Has(1 << i) {
			b.WriteRune(c)
		}
	}
	if b.Len() == 0 {
		return "-"
	}
	return b.String()
}

// InlineMax is the largest task passed inline as an argument. It keeps the
// argument under Linux's 128 KiB per-argument limit and macOS's 1 MiB total.
const InlineMax = 100 << 10

// TaskFile is the file in the session dir that holds the task verbatim.
const TaskFile = "task.md"

// TaskArg is the task argument: the task itself when it fits, otherwise a
// pointer to the task file in dir. An argument cannot hold a NUL byte, so such
// a task also goes through the file.
func TaskArg(task, dir string) string {
	if len(task) > InlineMax || strings.IndexByte(task, 0) >= 0 {
		return "Your full task is in " + filepath.Join(dir, TaskFile) + ". Read all of it before doing anything else."
	}
	// A leading space keeps the CLI from reading the task as a flag.
	if strings.HasPrefix(task, "-") {
		return " " + task
	}
	return task
}

// Spawn describes one new session.
type Spawn struct {
	Session  string        // Irie session id
	Name     string        // the name the user chose for the session; "" lets the agent title it
	Task     string        // the task, verbatim
	Cwd      string        // working directory
	Repo     string        // the main checkout of the task's repo, when it runs in one
	Dir      string        // the session's state dir, sessions/<id>
	VendorID string        // vendor session id, assigned before launch
	Bin      string        // the irie binary, which hook commands run
	HoldMax  time.Duration // hold.max; hook timeouts sit above it
	Probe    Probe         // what probing the CLI found

	// StatusLine makes Irie the session's status line, to learn its cost
	// (usage.status_line). Home and Environ, the user's home and the
	// environment the agent starts with, find the user's own.
	StatusLine bool
	Home       string
	Environ    []string

	// Teams lets the agent start agent teams, each teammate in a tmux pane
	// on Irie's team server (spawn.teammates).
	Teams bool

	// PluginDir is the folder of Irie's skills for this session, which a
	// harness whose probe offers --plugin-dir passes on (internal/skills);
	// "" = none.
	PluginDir string

	// Arch is what Irie's arch agents bring to the session (DESIGN §4.11).
	Arch Arch
}

// Arch is what Irie's arch agents bring to a session (DESIGN §4.11). The
// core asks for it; the adapter gives what its CLI can take and writes that
// to ArchFile, which the core reads to put their word in front of the
// session and its subagents as they start.
type Arch struct {
	With   []string `json:"with,omitempty"`    // the arch agents with the session, by rank
	As     string   `json:"as,omitempty"`      // the arch agent the session runs as; "" = none
	Good   []string `json:"good,omitempty"`    // the good the user wants to see in the world, in their words
	OnCall bool     `json:"on_call,omitempty"` // the agent can call on them, and on As
	Door   bool     `json:"door,omitempty"`    // those with a door weigh each call that could do harm before it runs
}

// ArchFile is the file in the session dir that says what the arch agents
// brought to the session: Arch, as JSON.
const ArchFile = "arch.json"

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
	// Unfinished is what is left of the task when its turn ends: the
	// agent goes on to finish it, as the jot dog asks (DESIGN §4.8).
	Unfinished string
}

// Harness is one agent CLI.
type Harness interface {
	ID() string
	Levels() Levels
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

// Resumer is a harness that can carry one of its CLI's conversations on in a
// new pane, such as a Claude Code session moved into Irie (DESIGN §4.6).
// Reopening a closed session (§4.5) will build on it.
type Resumer interface {
	// Resume returns argv, extra env and files to write for a session that
	// continues the conversation s.VendorID instead of starting one. It
	// takes no task: the agent waits for input.
	Resume(p config.Profile, s Spawn) (argv []string, env map[string]string, files map[string][]byte, err error)
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
