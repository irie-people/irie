// Package generic runs any CLI agent at level G (DESIGN §3.1): Irie launches
// it in a pane host, can freeze and thaw it, and sees it alive or exited. It
// has no hooks, so nothing tells Irie it is working or needs you. A profile
// with task = "arg" gets its task as the last argument; with task = "paste"
// the core pastes the task into its terminal once it runs, then presses
// Enter.
package generic

import (
	"errors"
	"maps"
	"slices"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/event"
	"github.com/irie-people/irie/internal/harness"
)

type Harness struct{}

func (Harness) ID() string { return "generic" }

func (Harness) Levels() harness.Levels { return harness.Generic }

// Launch builds <command> <args...>, and the task argument unless the
// profile pastes its task. The task file is written either way.
func (Harness) Launch(p config.Profile, s harness.Spawn) ([]string, map[string]string, map[string][]byte, error) {
	if p.Command == "" {
		return nil, nil, nil, errors.New("generic: the profile has no command")
	}
	// With no output styles, a tutor mode is a note before the task.
	task := harness.WithTutor(s.Tutor, s.Visual, s.Task)
	argv := append([]string{p.Command}, slices.Clone(p.Args)...)
	if p.Task != config.TaskPaste {
		argv = append(argv, harness.TaskArg(task, s.Dir))
	}
	env := maps.Clone(p.Env)
	if env == nil {
		env = map[string]string{}
	}
	return argv, env, map[string][]byte{harness.TaskFile: []byte(task)}, nil
}

// Normalize fails: a generic harness has no hooks.
func (Harness) Normalize([]byte) ([]event.Event, error) {
	return nil, errors.New("generic: no hooks")
}

// Respond says nothing.
func (Harness) Respond(event.Event, harness.HookReply) []byte { return nil }
