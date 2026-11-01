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

// RouteChoice is what the route op answers: where +auto would run a task,
// and why, with every profile it weighed (DESIGN §6.4).
type RouteChoice = route.Choice

// RouteCandidate is one profile a RouteChoice weighed.
type RouteCandidate = route.Candidate
