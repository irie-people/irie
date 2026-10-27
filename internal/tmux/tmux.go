// Package tmux drives Irie's private tmux server (DESIGN §2.3): a runner
// that always passes -L, the generated tmux.conf, pane snapshots, layout
// repair and the swap-in.
package tmux

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Sessions and fixed targets of Irie's server.
const (
	MainSession   = "irie"        // what the user sees
	AgentsSession = "irie-agents" // never attached
	CoreWindow    = "core"
	Sidebar       = "irie:0.0"
	Slot          = "irie:0.1"
)

// Pane roles, kept in the @irie_role pane option.
const (
	RoleUI          = "ui"
	RoleCore        = "core"
	RolePlaceholder = "placeholder"
	RoleAgent       = "agent"
	RoleMirror      = "mirror"
	RoleRuntime     = "runtime" // Irie's own Ollama server (DESIGN §6.3), in its own window of irie-agents
	// RoleClosing is an agent pane of a closed session while its agent
	// ends. It is out of the slot, alone in its window and in no row.
	RoleClosing = "closing"
)

// Pane user options. They travel with a pane through swaps, so a restarted
// core rebuilds its registry from them.
const (
	OptSession  = "@irie_session"
	OptAgent    = "@irie_agent"
	OptRole     = "@irie_role"
	OptName     = "@irie_name"
	OptCreated  = "@irie_created"
	OptVendorID = "@irie_vendor_id"
	OptVersion  = "@irie_version"
	// OptNamed is "1" when the user chose the session's name, else "0". A
	// pane from before Irie kept it has none.
	OptNamed = "@irie_named"
)

// ErrNoServer means the server is not running.
var ErrNoServer = errors.New("tmux: no server running")

// Find returns the tmux binary: the configured path, else tmux on PATH, else
// the usual install dirs (a GUI launch may lack the user's PATH).
func Find(configured string) (string, error) {
	if configured != "" {
		if isExec(configured) {
			return configured, nil
		}
		return "", fmt.Errorf("tmux.path %s is not an executable", configured)
	}
	if p, err := exec.LookPath("tmux"); err == nil {
		return p, nil
	}
	for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin"} {
		if p := filepath.Join(dir, "tmux"); isExec(p) {
			return p, nil
		}
	}
	return "", errors.New("tmux not found: install it or set tmux.path")
}

func isExec(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
}

// Runner runs tmux commands against one server.
type Runner struct {
	Bin    string // tmux binary
	Socket string // -L socket name
}

// command builds a tmux command. Only start may start the server: every
// other command gets -N, so one that would start a server, such as the
// new-session of a layout repair that runs while the server goes away,
// fails instead of bringing a server back without its core. -u: without a
// UTF-8 locale, as when Irie.app starts the server, tmux would print every
// non-ASCII character of a session's name, or any option, as "_".
func (r *Runner) command(start bool, args ...string) *exec.Cmd {
	flags := []string{"-u", "-L", r.Socket}
	if !start {
		flags = append(flags, "-N")
	}
	cmd := exec.Command(r.Bin, append(flags, args...)...)
	// TMUX would make tmux guess targets from the caller's own pane.
	cmd.Env = WithoutEnv(os.Environ(), "TMUX", "TMUX_PANE")
	return cmd
}

// Run runs one tmux command and returns its output. It never starts the
// server.
func (r *Runner) Run(args ...string) (string, error) { return r.run(false, args...) }

func (r *Runner) run(start bool, args ...string) (string, error) {
	cmd := r.command(start, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		// With -N tmux says "Error connecting to"; without, "error
		// connecting to" or "no server running".
		if low := strings.ToLower(msg); strings.Contains(low, "no server running") || strings.Contains(low, "error connecting to") {
			return "", ErrNoServer
		}
		return string(out), fmt.Errorf("tmux %s: %s", strings.Join(args, " "), msg)
	}
	return string(out), nil
}

// RunList runs commands as one tmux command list, so no client draws the
// state between them.
func (r *Runner) RunList(cmds [][]string) (string, error) {
	return r.Run(listArgs(cmds)...)
}

// Literal makes s plain text in an argument that tmux expands as a format,
// such as a window's name, where #{…} is replaced and #(…) runs a shell
// command: ## is a literal #.
func Literal(s string) string { return strings.ReplaceAll(s, "#", "##") }

// listArgs joins commands with ";" arguments. tmux reads an argument that
// ends in ";" as the end of a command, so such a semicolon is escaped.
func listArgs(cmds [][]string) []string {
	var args []string
	for i, c := range cmds {
		if i > 0 {
			args = append(args, ";")
		}
		for _, a := range c {
			if strings.HasSuffix(a, ";") {
				a = a[:len(a)-1] + `\;`
			}
			args = append(args, a)
		}
	}
	return args
}

// StartServer starts the server with conf and a detached irie-agents
// session whose core window runs cmd. It is the one command that may start
// the server.
func (r *Runner) StartServer(conf string, cmd []string) error {
	args := []string{"-f", conf, "new-session", "-d", "-s", AgentsSession, "-n", CoreWindow, "-x", "80", "-y", "24", "--"}
	_, err := r.run(true, append(args, cmd...)...)
	return err
}

// SocketPath returns the running server's socket path.
func (r *Runner) SocketPath() (string, error) {
	out, err := r.Run("list-sessions", "-F", "#{socket_path}")
	if err != nil {
		return "", err
	}
	line, _, _ := strings.Cut(out, "\n")
	return strings.TrimSpace(line), nil
}

// WithoutEnv returns env without the named variables.
func WithoutEnv(env []string, names ...string) []string {
	out := make([]string, 0, len(env))
outer:
	for _, kv := range env {
		for _, n := range names {
			if strings.HasPrefix(kv, n+"=") {
				continue outer
			}
		}
		out = append(out, kv)
	}
	return out
}

// Pane is one row of list-panes.
type Pane struct {
	Session      string
	WindowID     string
	WindowIndex  int
	WindowPanes  int
	WindowWidth  int
	WindowHeight int
	ID           string
	Index        int
	PID          int
	Dead         bool
	DeadStatus   int
	Width        int
	Height       int
	Command      string // the pane's foreground command, such as irie for an agent's host
	BracketPaste bool   // the program in the pane asked for bracketed paste
	Zoomed       bool   // the pane's window is zoomed
	Active       bool   // the pane is its window's active pane; in a zoomed window, the zoomed one
	Attached     int    // how many clients are attached to the pane's session

	// Irie's pane options.
	Role        string
	IrieSession string
	Agent       string
	Name        string
	Named       string
	Created     string
	VendorID    string
	Version     string
}

var paneFormat = []string{
	"session_name", "window_id", "window_index", "window_panes", "window_width", "window_height",
	"pane_id", "pane_index", "pane_pid", "pane_dead", "pane_dead_status", "pane_width", "pane_height",
	OptRole, OptSession, OptAgent, OptName, OptNamed, OptCreated, OptVersion, OptVendorID,
	"pane_current_command", "bracket_paste_flag", "window_zoomed_flag", "pane_active", "session_attached",
}

// Snapshot is every pane of the server at one moment.
type Snapshot struct {
	Panes []Pane
}

// Snapshot lists every pane. It returns ErrNoServer when the server is down.
func (r *Runner) Snapshot() (Snapshot, error) {
	out, err := r.Run("list-panes", "-a", "-F", "#{"+strings.Join(paneFormat, "}\t#{")+"}")
	if err != nil {
		return Snapshot{}, err
	}
	var s Snapshot
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\t")
		if len(f) != len(paneFormat) {
			continue
		}
		n := func(i int) int { v, _ := strconv.Atoi(f[i]); return v }
		s.Panes = append(s.Panes, Pane{
			Session: f[0], WindowID: f[1], WindowIndex: n(2), WindowPanes: n(3), WindowWidth: n(4), WindowHeight: n(5),
			ID: f[6], Index: n(7), PID: n(8), Dead: f[9] == "1", DeadStatus: n(10), Width: n(11), Height: n(12),
			Role: f[13], IrieSession: f[14], Agent: f[15], Name: f[16], Named: f[17], Created: f[18], Version: f[19], VendorID: f[20],
			Command: f[21], BracketPaste: f[22] == "1", Zoomed: f[23] == "1", Active: f[24] == "1", Attached: n(25),
		})
	}
	return s, nil
}

// Attached returns how many clients are attached to the session.
func (s Snapshot) Attached(session string) int {
	for _, p := range s.Panes {
		if p.Session == session {
			return p.Attached
		}
	}
	return 0
}

// HasSession reports whether the session has any pane.
func (s Snapshot) HasSession(name string) bool {
	for _, p := range s.Panes {
		if p.Session == name {
			return true
		}
	}
	return false
}

// Window returns the panes of session:index in pane order.
func (s Snapshot) Window(session string, index int) []Pane {
	var out []Pane
	for _, p := range s.Panes {
		if p.Session == session && p.WindowIndex == index {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out
}

// WindowOf returns the panes of the window with the given id.
func (s Snapshot) WindowOf(windowID string) []Pane {
	var out []Pane
	for _, p := range s.Panes {
		if p.WindowID == windowID {
			out = append(out, p)
		}
	}
	return out
}

func (s Snapshot) at(index int) *Pane {
	for _, p := range s.Window(MainSession, 0) {
		if p.Index == index {
			return &p
		}
	}
	return nil
}

// Sidebar returns the pane at irie:0.0, whatever it runs.
func (s Snapshot) Sidebar() *Pane { return s.at(0) }

// SlotPane returns the pane at irie:0.1, whatever it runs.
func (s Snapshot) SlotPane() *Pane { return s.at(1) }

// Pane returns the pane with the given id.
func (s Snapshot) Pane(id string) *Pane {
	for _, p := range s.Panes {
		if p.ID == id {
			return &p
		}
	}
	return nil
}

// ByRole returns the panes with the given @irie_role.
func (s Snapshot) ByRole(role string) []Pane {
	var out []Pane
	for _, p := range s.Panes {
		if p.Role == role {
			out = append(out, p)
		}
	}
	return out
}
