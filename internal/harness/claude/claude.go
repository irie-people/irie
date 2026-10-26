// Package claude is the Claude Code adapter (DESIGN §3.3).
package claude

import (
	"errors"
	"maps"
	"path/filepath"
	"strings"

	"github.com/irie-hq/irie/internal/config"
	"github.com/irie-hq/irie/internal/harness"
)

// InlineMax is the largest task passed inline as an argument. It keeps the
// argument under Linux's 128 KiB per-argument limit and macOS's 1 MiB total.
const InlineMax = 100 << 10

// TaskFile is the file in the session dir that holds the task verbatim.
const TaskFile = "task.md"

type Harness struct{}

func (Harness) ID() string { return "claude-code" }

// Launch builds:
//
//	<command> <args...> --model M --effort E --permission-mode P --session-id <uuid> <task>
//
// The profile's own args come right after the command. A flag whose value is
// empty in the profile is left out.
func (Harness) Launch(p config.Profile, s harness.Spawn) ([]string, map[string]string, map[string][]byte, error) {
	if s.VendorID == "" {
		return nil, nil, nil, errors.New("claude: no session id")
	}
	command := p.Command
	if command == "" {
		command = "claude"
	}
	argv := append([]string{command}, p.Args...)
	for _, f := range [][2]string{{"--model", p.Model}, {"--effort", p.Effort}, {"--permission-mode", p.PermissionMode}} {
		if f[1] != "" {
			argv = append(argv, f[0], f[1])
		}
	}
	argv = append(argv, "--session-id", s.VendorID, TaskArg(s.Task, s.Dir))
	env := maps.Clone(p.Env)
	if env == nil {
		env = map[string]string{}
	}
	return argv, env, map[string][]byte{TaskFile: []byte(s.Task)}, nil
}

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
