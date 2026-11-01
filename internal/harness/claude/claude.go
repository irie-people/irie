// Package claude is the Claude Code adapter (DESIGN §3.3).
package claude

import (
	"encoding/json"
	"errors"
	"maps"
	"path/filepath"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/harness"
)

// InlineMax and TaskFile are the task-size rule every adapter shares.
const (
	InlineMax = harness.InlineMax
	TaskFile  = harness.TaskFile
)

// SettingsFile is the file in the session dir passed with --settings. It
// adds Irie's hooks for this session only; Claude Code merges hook lists, so
// the user's own hooks still run.
const SettingsFile = "claude-settings.json"

type Harness struct{}

func (Harness) ID() string { return "claude-code" }

// Levels: Claude Code supports every level (DESIGN §3.2).
func (Harness) Levels() harness.Levels {
	return harness.Generic | harness.Observed | harness.Held | harness.Subagents
}

// Launch builds:
//
//	<command> <args...> --model M --effort E --permission-mode P \
//	    --settings <dir>/claude-settings.json [--plugin-dir <skills>] \
//	    --session-id <uuid> [--name <name>] <task>
//
// The profile's own args come right after the command. A flag whose value is
// empty in the profile is left out. --name goes only with a name the user
// chose, and only to a CLI whose --help lists it: without it, Claude Code
// titles the session itself.
func (Harness) Launch(p config.Profile, s harness.Spawn) ([]string, map[string]string, map[string][]byte, error) {
	argv, env, settings, err := base(p, s)
	if err != nil {
		return nil, nil, nil, err
	}
	argv = append(argv, "--session-id", s.VendorID)
	if s.Probe.Flags["--name"] && s.Name != "" {
		argv = append(argv, "--name", s.Name)
	}
	argv = append(argv, harness.TaskArg(s.Task, s.Dir))
	return argv, env, map[string][]byte{TaskFile: []byte(s.Task), SettingsFile: settings}, nil
}

// Resume builds, for a conversation that carries on in an Irie pane:
//
//	<command> <args...> --model M --effort E --permission-mode P \
//	    --settings <dir>/claude-settings.json --resume <uuid>
//
// A resumed session keeps its id unless --fork-session is passed, so there
// is no --session-id, and it waits for input, so there is no task. --name is
// left out too: the conversation keeps its own name.
func (Harness) Resume(p config.Profile, s harness.Spawn) ([]string, map[string]string, map[string][]byte, error) {
	if strings.HasPrefix(s.VendorID, "-") {
		return nil, nil, nil, errors.New("claude: a session id cannot start with -")
	}
	argv, env, settings, err := base(p, s)
	if err != nil {
		return nil, nil, nil, err
	}
	return append(argv, "--resume", s.VendorID), env, map[string][]byte{SettingsFile: settings}, nil
}

// base is what a launch and a resume share: the profile's command, args and
// flags, and Irie's settings file with the flag that passes it.
func base(p config.Profile, s harness.Spawn) ([]string, map[string]string, []byte, error) {
	if s.VendorID == "" {
		return nil, nil, nil, errors.New("claude: no session id")
	}
	if s.Bin == "" {
		return nil, nil, nil, errors.New("claude: no irie binary for the hooks")
	}
	var status *StatusLine
	if s.StatusLine {
		user, ok := UserStatusLine(s.Cwd, s.Repo, s.Home, s.Environ)
		l := statusLine(s.Bin, user, ok)
		status = &l
	}
	settings, err := Settings(s.Bin, s.HoldMax, AtLeast(s.Probe.Version, MinVersion), status, s.Teams)
	if err != nil {
		return nil, nil, nil, err
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
	argv = append(argv, "--settings", filepath.Join(s.Dir, SettingsFile))
	// Irie's skills, as a plugin for this session only (internal/skills),
	// only to a CLI the probe saw load one.
	if s.PluginDir != "" && s.Probe.Flags["--plugin-dir"] {
		argv = append(argv, "--plugin-dir", s.PluginDir)
	}
	env := maps.Clone(p.Env)
	if env == nil {
		env = map[string]string{}
	}
	return argv, env, settings, nil
}

// heldExtra is how far the tool hooks' timeout sits above hold.max, so the
// shim always releases a held call itself before Claude Code gives up on it.
const heldExtra = 120 * time.Second

// hooks is the hook table of DESIGN §3.3. A zero timeout is hold.max plus
// heldExtra.
var hooks = []struct {
	event, matcher string
	timeout        time.Duration
	block          bool // onFailure: "block", so a failed or timed-out hook never lets the call through
}{
	{"SessionStart", "", 10 * time.Second, false},
	{"UserPromptSubmit", "", 10 * time.Second, false},
	{"PreToolUse", "*", 0, true},
	{"PostToolUse", "*", 0, false},
	{"PostToolUseFailure", "*", 0, false},
	{"PermissionRequest", "*", 10 * time.Second, false},
	{"Notification", "", 10 * time.Second, false},
	{"Elicitation", "", 10 * time.Second, false},
	{"Stop", "", 10 * time.Second, false},
	{"StopFailure", "", 10 * time.Second, false},
	{"SubagentStart", "", 10 * time.Second, false},
	{"SubagentStop", "", 10 * time.Second, false},
	{"SessionEnd", "", time.Second, false},
}

type hookCommand struct {
	Type      string `json:"type"`
	Command   string `json:"command"`
	Timeout   int    `json:"timeout"` // seconds
	OnFailure string `json:"onFailure,omitempty"`
}

type matcherGroup struct {
	Matcher string        `json:"matcher,omitempty"`
	Hooks   []hookCommand `json:"hooks"`
}

// Settings returns the settings file that sends every hook of the table to
// `<bin> hook`, and makes status the session's status line when it is set.
// onFailure goes only to a Claude Code that knows it. With teams, agent
// teams open their teammates in tmux panes, which the shim on the agent's
// PATH takes to Irie's team server.
func Settings(bin string, holdMax time.Duration, onFailure bool, status *StatusLine, teams bool) ([]byte, error) {
	command := shellQuote(bin) + " hook"
	file := struct {
		Hooks        map[string][]matcherGroup `json:"hooks"`
		StatusLine   *StatusLine               `json:"statusLine,omitempty"`
		TeammateMode string                    `json:"teammateMode,omitempty"`
	}{Hooks: map[string][]matcherGroup{}, StatusLine: status}
	if teams {
		file.TeammateMode = "tmux"
	}
	for _, h := range hooks {
		timeout := h.timeout
		if timeout == 0 {
			timeout = holdMax + heldExtra
		}
		c := hookCommand{Type: "command", Command: command, Timeout: int(timeout / time.Second)}
		if h.block && onFailure {
			c.OnFailure = "block"
		}
		file.Hooks[h.event] = []matcherGroup{{Matcher: h.matcher, Hooks: []hookCommand{c}}}
	}
	b, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// shellQuote quotes s for sh, which runs a command hook.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
