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
	"github.com/irie-people/irie/internal/learn"
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

	// Tutor is how the agent works with the user (learn.tutor,
	// docs/proposals/learning.md): config.TutorExplain or TutorTeach, or ""
	// for off.
	Tutor string
	// Visual asks an agent in a tutor mode to draw a diagram where a picture
	// helps (learn.visual).
	Visual bool
	// Tips are Irie's tips, for a harness that can show them, as Claude
	// Code does under its spinner (learn.agent_tips).
	Tips []learn.Tip
}

// TutorNote is what goes before the task of an agent whose harness has no
// output styles, for a tutor mode: the instruction alone.
func TutorNote(mode string) string {
	switch mode {
	case config.TutorExplain:
		return "As you work, explain the reasons for your choices in a few short notes, so that the user learns from what you do."
	case config.TutorTeach:
		return "The user wants to learn by doing. Do the routine work yourself. Where a piece of code holds a real decision, leave it to the user: mark the place with a TODO(human) comment, say what is built, what to write and what to weigh, then stop and wait for them."
	}
	return ""
}

// VisualNote asks an agent in a tutor mode for pictures. Words and a picture
// together teach better than words alone (docs/proposals/learning.md §2).
const VisualNote = "When a picture would help the user follow an explanation, such as how parts of the code connect, what happens in what order, or how branches and commits relate, draw it as a small Mermaid diagram in a ```mermaid code block, and go on explaining in words beside it."

// WithTutor puts mode's note before task, with VisualNote when visual.
func WithTutor(mode string, visual bool, task string) string {
	n := TutorNote(mode)
	if n == "" {
		return task
	}
	if visual {
		n += " " + VisualNote
	}
	return n + "\n\n" + task
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
