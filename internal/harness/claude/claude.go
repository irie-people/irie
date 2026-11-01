// Package claude is the Claude Code adapter (DESIGN §3.3).
package claude

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/irie-people/irie/internal/arch"
	"github.com/irie-people/irie/internal/config"
	"github.com/irie-people/irie/internal/harness"
	"github.com/irie-people/irie/internal/learn"
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
	argv, env, files, err := base(p, s)
	if err != nil {
		return nil, nil, nil, err
	}
	argv = append(argv, "--session-id", s.VendorID)
	if s.Probe.Flags["--name"] && s.Name != "" {
		argv = append(argv, "--name", s.Name)
	}
	argv = append(argv, harness.TaskArg(s.Task, s.Dir))
	files[TaskFile] = []byte(s.Task)
	return argv, env, files, nil
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
	argv, env, files, err := base(p, s)
	if err != nil {
		return nil, nil, nil, err
	}
	return append(argv, "--resume", s.VendorID), env, files, nil
}

// base is what a launch and a resume share: the profile's command, args and
// flags, Irie's settings file with the flag that passes it, and what Irie's
// arch agents bring to the session (DESIGN §4.11), with the file that says
// so.
func base(p config.Profile, s harness.Spawn) ([]string, map[string]string, map[string][]byte, error) {
	if s.VendorID == "" {
		return nil, nil, nil, errors.New("claude: no session id")
	}
	if s.Bin == "" {
		return nil, nil, nil, errors.New("claude: no irie binary for the hooks")
	}
	taken, err := TakeArch(p, s)
	if err != nil {
		return nil, nil, nil, err
	}
	var status *StatusLine
	if s.StatusLine {
		user, ok := UserStatusLine(s.Cwd, s.Repo, s.Home, s.Environ)
		l := statusLine(s.Bin, user, ok)
		status = &l
	}
	var doors []arch.Door
	if taken.Door {
		for _, name := range taken.With {
			if a, ok := arch.Get(name); ok && a.Door != nil {
				doors = append(doors, *a.Door)
			}
		}
	}
	x := Extra{Status: status, Teams: s.Teams, Style: Styles[s.Tutor], Doors: doors}
	// Tips as objects, with an id and a cooldown, need 2.1.247.
	if AtLeast(s.Probe.Version, TipsVersion) {
		x.Tips = s.Tips
	}
	settings, err := Settings(s.Bin, s.HoldMax, AtLeast(s.Probe.Version, MinVersion), x)
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
	// In a tutor mode, the ask for diagrams goes after Claude Code's own
	// instructions and its output style, taking nothing away.
	if s.Visual && s.Tutor != "" && s.Probe.Flags["--append-system-prompt"] {
		argv = append(argv, "--append-system-prompt", harness.VisualNote)
	}
	files := map[string][]byte{SettingsFile: settings}
	if taken.OnCall {
		names := taken.With
		if taken.As != "" && !slices.Contains(names, taken.As) {
			names = append(slices.Clone(names), taken.As)
		}
		defs, err := arch.Definitions(names, taken.Good)
		if err != nil {
			return nil, nil, nil, err
		}
		if defs != "" {
			argv = append(argv, "--agents", defs)
		}
		if taken.As != "" {
			argv = append(argv, "--agent", taken.As)
		}
	}
	if len(taken.With) > 0 || taken.As != "" {
		b, err := json.Marshal(taken)
		if err != nil {
			return nil, nil, nil, err
		}
		files[harness.ArchFile] = append(b, '\n')
	}
	env := maps.Clone(p.Env)
	if env == nil {
		env = map[string]string{}
	}
	return argv, env, files, nil
}

// TakeArch is what of s.Arch a session of p takes (DESIGN §4.11). The arch
// agents go on call through --agents, which the CLI's --help must list and
// the profile's args must not pass themselves. They keep the door only on
// a Claude Code of MinVersion or later, whose prompt hooks take if and
// continueOnBlock. A session that runs as an arch agent fails to start
// when the CLI cannot take him, rather than start as someone else.
func TakeArch(p config.Profile, s harness.Spawn) (harness.Arch, error) {
	a := harness.Arch{With: slices.Clone(s.Arch.With), As: s.Arch.As, Good: slices.Clone(s.Arch.Good)}
	if len(a.With) == 0 && a.As == "" {
		return harness.Arch{}, nil
	}
	flags := s.Probe.Flags["--agents"] && !hasFlag(p.Args, "--agents")
	if a.As != "" && (!flags || !s.Probe.Flags["--agent"] || hasFlag(p.Args, "--agent")) {
		return harness.Arch{}, fmt.Errorf("claude: this session runs as the arch agent %s, which needs a Claude Code whose --help lists --agents and --agent, and a profile that passes neither itself", a.As)
	}
	a.OnCall = flags
	a.Door = s.Arch.Door && len(a.With) > 0 && AtLeast(s.Probe.Version, MinVersion)
	return a, nil
}

// hasFlag reports whether args pass flag, alone or as flag=value.
func hasFlag(args []string, flag string) bool {
	return slices.ContainsFunc(args, func(a string) bool { return a == flag || strings.HasPrefix(a, flag+"=") })
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

// hookHandler is one handler of the settings file: Irie's own command, or
// the prompt of an arch agent's door.
type hookHandler struct {
	Type            string `json:"type"`
	Command         string `json:"command,omitempty"`
	If              string `json:"if,omitempty"` // a permission rule the call must match
	Prompt          string `json:"prompt,omitempty"`
	Timeout         int    `json:"timeout"` // seconds
	OnFailure       string `json:"onFailure,omitempty"`
	StatusMessage   string `json:"statusMessage,omitempty"`
	ContinueOnBlock bool   `json:"continueOnBlock,omitempty"`
}

type matcherGroup struct {
	Matcher string        `json:"matcher,omitempty"`
	Hooks   []hookHandler `json:"hooks"`
}

// Styles are Claude Code's output styles for Irie's tutor modes
// (learn.tutor, docs/proposals/learning.md). Claude Code matches the names
// exactly, and a name it does not know gives its Default style.
var Styles = map[string]string{config.TutorExplain: "Explanatory", config.TutorTeach: "Learning"}

// TipsVersion is the first Claude Code that takes a spinner tip as an object
// with an id and a cooldown.
const TipsVersion = "2.1.247"

// TipsLabel is what Claude Code shows before Irie's tips.
const TipsLabel = "Irie tip"

// tipCooldown is how many sessions Claude Code waits before it shows one of
// Irie's tips again, so they come seldom among its own.
const tipCooldown = 5

// Extra is what a session's settings file carries besides Irie's hooks.
type Extra struct {
	Status *StatusLine // Irie as the session's status line
	Teams  bool        // agent teams open their teammates in tmux panes
	Style  string      // the session's output style, such as Explanatory; "" leaves the user's own
	Tips   []learn.Tip // Irie's tips under the spinner, with Claude Code's own
	Doors  []arch.Door // the doors of the session's arch agents (DESIGN §4.11)
}

type spinnerTips struct {
	Label string       `json:"label"`
	Tips  []spinnerTip `json:"tips"`
}

type spinnerTip struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	Cooldown int    `json:"cooldownSessions"`
}

// Settings returns the settings file that sends every hook of the table to
// `<bin> hook`. onFailure goes only to a Claude Code that knows it. With
// x.Status, Irie is the session's status line. With x.Teams, agent teams
// open their teammates in tmux panes, which the shim on the agent's PATH
// takes to Irie's team server. Each of x.Doors, an arch agent's door, adds
// a prompt hook before each tool call its rules match (doorHooks). x.Style
// sets the session's output style above the user's own files, and x.Tips go
// under the spinner.
func Settings(bin string, holdMax time.Duration, onFailure bool, x Extra) ([]byte, error) {
	command := shellQuote(bin) + " hook"
	file := struct {
		Hooks        map[string][]matcherGroup `json:"hooks"`
		StatusLine   *StatusLine               `json:"statusLine,omitempty"`
		TeammateMode string                    `json:"teammateMode,omitempty"`
		OutputStyle  string                    `json:"outputStyle,omitempty"`
		Tips         *spinnerTips              `json:"spinnerTipsOverride,omitempty"`
	}{Hooks: map[string][]matcherGroup{}, StatusLine: x.Status, OutputStyle: x.Style}
	if x.Teams {
		file.TeammateMode = "tmux"
	}
	if len(x.Tips) > 0 {
		file.Tips = &spinnerTips{Label: TipsLabel}
		for _, t := range x.Tips {
			file.Tips.Tips = append(file.Tips.Tips, spinnerTip{ID: t.ID, Text: t.Text, Cooldown: tipCooldown})
		}
	}
	for _, h := range hooks {
		timeout := h.timeout
		if timeout == 0 {
			timeout = holdMax + heldExtra
		}
		c := hookHandler{Type: "command", Command: command, Timeout: int(timeout / time.Second)}
		if h.block && onFailure {
			c.OnFailure = "block"
		}
		file.Hooks[h.event] = []matcherGroup{{Matcher: h.matcher, Hooks: []hookHandler{c}}}
	}
	file.Hooks["PreToolUse"] = append(file.Hooks["PreToolUse"], doorHooks(x.Doors)...)
	b, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// doorHooks are the PreToolUse prompt hooks of the doors, one for each
// rule, grouped by the tool the rule names (DESIGN §4.11). Claude Code's
// small background model weighs each call a rule matches, which runs at
// once while Irie's own hook may hold it. A call it stops comes back to the
// agent as the tool's error, with the reason, and the turn goes on; a
// judgment that fails or takes too long lets the call run. Write calls are
// checked against Edit rules, as permissions are.
func doorHooks(doors []arch.Door) []matcherGroup {
	var groups []matcherGroup
	at := map[string]int{}
	for _, d := range doors {
		for _, r := range d.Rules {
			tool, _, _ := strings.Cut(r, "(")
			if tool == "Edit" {
				tool = "Edit|Write"
			}
			i, ok := at[tool]
			if !ok {
				i = len(groups)
				at[tool] = i
				groups = append(groups, matcherGroup{Matcher: tool})
			}
			groups[i].Hooks = append(groups[i].Hooks, hookHandler{
				Type: "prompt", If: r, Prompt: d.Prompt, Timeout: d.Timeout,
				StatusMessage: d.Status, ContinueOnBlock: true,
			})
		}
	}
	return groups
}

// shellQuote quotes s for sh, which runs a command hook.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
