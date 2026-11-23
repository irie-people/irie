package core

import (
	"time"

	"github.com/irie-people/irie/internal/ipc"
	"github.com/irie-people/irie/internal/paths"
	"github.com/irie-people/irie/internal/route"
)

// Socket is where the core listens: core.sock in Irie's runtime folder,
// which is $XDG_RUNTIME_DIR/irie, else $TMPDIR/irie-<uid>, else
// /tmp/irie-<uid>, and /tmp/irie-<uid> whenever the socket's path would
// pass the 104 bytes macOS allows (DESIGN §5.4). `irie up` starts the core
// there; a client never starts one itself.
func Socket() (string, error) {
	p, err := paths.FromEnv()
	if err != nil {
		return "", err
	}
	return p.Socket(), nil
}

// Dial connects to the core listening at path, waiting up to timeout. One
// Client carries one request at a time, or one subscription.
func Dial(path string, timeout time.Duration) (*Client, error) { return ipc.Dial(path, timeout) }

// Call sends one request to the core at path, waiting up to timeout to
// connect, and decodes its result into result, which may be nil. A failed
// request's error is an *Error, whose Code a client may act on and whose
// message it shows as it is.
func Call(path string, timeout time.Duration, op string, args, result any) error {
	return ipc.Call(path, timeout, op, args, result)
}

// Errorf makes an error with a code, as the core replies with one.
func Errorf(code, format string, a ...any) *Error { return ipc.Errorf(code, format, a...) }

// GroupPath is group id of groups and the groups around it, the outermost
// first, as the header's Groups have them (Group.Parent).
func GroupPath(groups []Group, id string) []Group { return ipc.GroupPath(groups, id) }

// GroupTitle is group id's path of names, the outermost first, joined by
// sep, as "Work / Irie".
func GroupTitle(groups []Group, id, sep string) string { return ipc.GroupTitle(groups, id, sep) }

// FindGroup is where among groups the group ref names is, by id, name or
// path such as "Work/Irie", as the core finds it; -1 when none has it.
func FindGroup(groups []Group, ref string) int { return ipc.FindGroup(groups, ref) }

// GroupWords are the names a path such as "Work / Irie" holds.
func GroupWords(path string) []string { return ipc.GroupWords(path) }

// FindSpace is where among spaces the space ref names is, by id or by name
// in any case, as the core finds it; -1 when none has it.
func FindSpace(spaces []Space, ref string) int { return ipc.FindSpace(spaces, ref) }

// ReachOf is where op's effect goes (DESIGN §4.5, "Inside and outside"):
// a read, inside Irie, which undo takes back and so never asks, inside
// with outside effects, or outside, which the person confirms and Mikael
// weighs when an agent runs it. An op this version does not know is
// outside.
func ReachOf(op string) Reach { return ipc.ReachOf(op) }

// RouteChoice is what the route op answers: where +auto would run a task,
// and why, with every profile it weighed (DESIGN §6.4).
type RouteChoice = route.Choice

// RouteCandidate is one profile a RouteChoice weighed.
type RouteCandidate = route.Candidate
