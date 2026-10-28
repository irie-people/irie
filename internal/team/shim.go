package team

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// The tmux shim is `irie team-tmux`, the tmux that an agent of a session
// with teams finds first on its PATH. Claude Code drives its teammates'
// panes with tmux, on the server that TMUX names, which for such an agent
// is the team server, at the session's anchor. The shim passes those calls
// on, with two changes, so that every teammate gets a tmux session of its
// own, which a view can show alone:
//   - split-window makes a new session instead, with the environment the
//     core wrote for the session's teammates;
//   - list-panes of the anchor's window lists the anchor and its teammates,
//     as if they shared that window, as Claude Code expects.
//
// Claude Code also asks tmux about its terminal as it starts, and those
// questions go to the team server too. Any other call, such as the agent's
// own use of tmux, goes where it would without Irie: TMUX and TMUX_PANE
// are taken out, as spawn.hide_tmux does for every agent.

// Shim is one session's shim.
type Shim struct {
	Tmux    string // the real tmux
	Socket  string // the team server's socket path
	Session string // the Irie session
	EnvFile string // the environment its teammates start with: VAR=value, NUL-separated
	Now     func() time.Time
}

// Call is one tmux call as the shim passes it on.
type Call struct {
	Args []string // tmux's arguments
	Env  []string
}

// Global options of tmux that take a value, and those that take none.
const (
	globalsWithValue = "cfLST"
	globalsPlain     = "2CDhlNuVv"
)

// Route returns how one call goes on. query runs a call on the team server
// and returns what it prints.
func (s Shim) Route(args, env []string, query func(args ...string) (string, error)) (Call, error) {
	globals, cmd, rest := splitGlobals(args)
	plain := Call{Args: args, Env: without(env, "TMUX", "TMUX_PANE")}
	if cmd == "" || globals['c'] != "" || globals['L'] != "" {
		return plain, nil
	}
	pane := lookup(env, "TMUX_PANE")
	switch sock := globals['S']; {
	case sock == s.Socket:
	case sock != "":
		return plain, nil
	case asks(cmd, rest, pane):
		// Claude Code's questions about its terminal.
		return Call{Args: append([]string{"-S", s.Socket}, args...), Env: env}, nil
	default:
		return plain, nil
	}
	pre := args[:len(args)-len(rest)-1]
	switch cmd {
	case "split-window", "splitw":
		out, err := s.newSession(rest)
		if err != nil {
			return Call{}, err
		}
		return Call{Args: append(clone(pre), out...), Env: env}, nil
	case "list-panes", "lsp":
		out, ok := s.listTeam(rest, pane, query)
		if ok {
			return Call{Args: append(clone(pre), out...), Env: env}, nil
		}
	case "kill-server":
		return Call{}, errors.New("irie: the team server serves every session's teammates; it stops with Irie")
	}
	return Call{Args: args, Env: env}, nil
}

func clone(s []string) []string { return append([]string(nil), s...) }

// splitGlobals splits tmux's arguments into its global options, the
// command and the command's own arguments.
func splitGlobals(args []string) (map[byte]string, string, []string) {
	globals := map[byte]string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			if i+1 < len(args) {
				return globals, args[i+1], args[i+2:]
			}
			return globals, "", nil
		}
		if len(a) < 2 || a[0] != '-' {
			return globals, a, args[i+1:]
		}
		for j := 1; j < len(a); j++ {
			c := a[j]
			if strings.IndexByte(globalsWithValue, c) < 0 {
				globals[c] = "1"
				continue
			}
			if j+1 < len(a) {
				globals[c] = a[j+1:]
			} else if i+1 < len(args) {
				i++
				globals[c] = args[i]
			}
			break
		}
	}
	return globals, "", nil
}

// options parses a command's arguments with getopt's rules: spec lists the
// options, a colon after each that takes a value. It returns the values,
// several for an option given more than once, and what follows them.
func options(args []string, spec string) (map[byte][]string, []string) {
	opts := map[byte][]string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return opts, args[i+1:]
		}
		if len(a) < 2 || a[0] != '-' {
			return opts, args[i:]
		}
		for j := 1; j < len(a); j++ {
			c := a[j]
			k := strings.IndexByte(spec, c)
			if k < 0 || k+1 >= len(spec) || spec[k+1] != ':' {
				opts[c] = append(opts[c], "")
				continue
			}
			v := ""
			if j+1 < len(a) {
				v = a[j+1:]
			} else if i+1 < len(args) {
				i++
				v = args[i]
			}
			opts[c] = append(opts[c], v)
			break
		}
	}
	return opts, nil
}

// asks reports whether a call with no socket is Claude Code asking tmux
// about its own terminal: display-message about its pane, or an option.
func asks(cmd string, rest []string, pane string) bool {
	switch cmd {
	case "display-message", "display":
		opts, _ := options(rest, "c:d:t:F:")
		t := opts['t']
		return pane != "" && (len(t) == 0 || t[len(t)-1] == pane)
	case "show-options", "show", "show-window-options", "showw":
		opts, _ := options(rest, "t:")
		t := opts['t']
		return len(t) == 0 || t[len(t)-1] == pane
	}
	return false
}

// newSession turns split-window's arguments into new-session's: the new
// teammate gets a session of its own, detached, with the environment the
// core wrote and the split's own -e on top.
func (s Shim) newSession(rest []string) ([]string, error) {
	opts, command := options(rest, "bc:de:fF:hIl:p:Pt:vZ")
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	out := []string{"new-session", "-d", "-s", TeammateName(s.Session, now().UnixNano())}
	env, err := readEnv(s.EnvFile)
	if err != nil {
		return nil, err
	}
	for _, kv := range append(env, opts['e']...) {
		out = append(out, "-e", kv)
	}
	if c := opts['c']; len(c) > 0 {
		out = append(out, "-c", c[len(c)-1])
	}
	if _, ok := opts['P']; ok {
		out = append(out, "-P")
	}
	if f := opts['F']; len(f) > 0 {
		out = append(out, "-F", f[len(f)-1])
	}
	if len(command) > 0 {
		out = append(append(out, "--"), command...)
	}
	return out, nil
}

// listTeam turns a list-panes of the anchor's window into one of the
// anchor and its teammates, the anchor first.
func (s Shim) listTeam(rest []string, pane string, query func(args ...string) (string, error)) ([]string, bool) {
	opts, _ := options(rest, "asF:f:t:")
	if _, all := opts['a']; all {
		return nil, false
	}
	if _, session := opts['s']; session {
		return nil, false
	}
	if t := opts['t']; len(t) > 0 && t[len(t)-1] != pane {
		name, err := query("display-message", "-p", "-t", t[len(t)-1], "#{session_name}")
		if id, teammate := Owner(strings.TrimSpace(name)); err != nil || teammate || id != s.Session {
			return nil, false
		}
	}
	filter := "#{m:" + s.Session + "*,#{session_name}}"
	if f := opts['f']; len(f) > 0 {
		filter = "#{&&:" + filter + "," + f[len(f)-1] + "}"
	}
	out := []string{"list-panes", "-a", "-f", filter}
	if f := opts['F']; len(f) > 0 {
		out = append(out, "-F", f[len(f)-1])
	}
	return out, true
}

// readEnv reads an environment file: VAR=value entries, NUL-separated.
func readEnv(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var env []string
	for _, kv := range strings.Split(string(b), "\x00") {
		if strings.Contains(kv, "=") {
			env = append(env, kv)
		}
	}
	return env, nil
}

// WriteEnv writes env as an environment file for readEnv.
func WriteEnv(path string, env []string) error {
	return os.WriteFile(path, []byte(strings.Join(env, "\x00")), 0o600)
}

func lookup(env []string, name string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if v, ok := strings.CutPrefix(env[i], name+"="); ok {
			return v
		}
	}
	return ""
}

func without(env []string, names ...string) []string {
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

// Run is `irie team-tmux`: it routes one call and runs the real tmux in its
// place. It returns only when that fails.
func (s Shim) Run(args []string) int {
	query := func(q ...string) (string, error) {
		cmd := exec.Command(s.Tmux, append([]string{"-S", s.Socket}, q...)...)
		out, err := cmd.Output()
		return string(out), err
	}
	call, err := s.Route(args, os.Environ(), query)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	err = syscall.Exec(s.Tmux, append([]string{"tmux"}, call.Args...), call.Env)
	fmt.Fprintln(os.Stderr, "irie team-tmux:", err)
	return 1
}

// Script is the shim's file on an agent's PATH: a tmux that runs
// `irie team-tmux` with the session's settings.
func (s Shim) Script(bin string) string {
	q := func(v string) string { return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'" }
	return "#!/bin/sh\n# Generated by Irie: Claude Code's agent teams reach Irie's team server\n# through this tmux, and every other call goes to tmux as it would.\n" +
		"exec " + q(bin) + " team-tmux --tmux " + q(s.Tmux) + " --socket " + q(s.Socket) +
		" --session " + q(s.Session) + " --env-file " + q(s.EnvFile) + " -- \"$@\"\n"
}
