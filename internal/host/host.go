// Package host is `irie host`, the first process of every agent pane
// (DESIGN §2.2, §4.2). It starts the agent in its own process group, makes
// that group the terminal's foreground, continues stops it did not cause,
// and records the exit status in exit.json.
package host

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Spec is spec.json: what the host runs. The core also keeps facts about
// the launch in it, which the host ignores.
type Spec struct {
	Session  string            `json:"session"`
	Profile  string            `json:"profile,omitempty"`
	Harness  string            `json:"harness,omitempty"`
	Argv     []string          `json:"argv"`
	Env      map[string]string `json:"env,omitempty"`   // added to the pane's environment
	Unset    []string          `json:"unset,omitempty"` // removed from it, such as TMUX
	Cwd      string            `json:"cwd"`
	Warnings []string          `json:"warnings,omitempty"` // what Irie cannot do for this session
	Repo     string            `json:"repo,omitempty"`     // the main checkout of the task's repo
	Worktree string            `json:"worktree,omitempty"` // the worktree Irie made for the task
	Branch   string            `json:"branch,omitempty"`
	Copied   []string          `json:"copied,omitempty"` // local files copied into the worktree
	Paste    bool              `json:"paste,omitempty"`  // the core pastes the task into the terminal once the agent runs
	Model    string            `json:"model,omitempty"`  // the model the agent runs
	Local    bool              `json:"local,omitempty"`  // a model of a local runtime
}

// Exit is exit.json, written when the agent exits.
type Exit struct {
	Code   int       `json:"code"`             // exit code; 128+n for signal n
	Signal int       `json:"signal,omitempty"` // the signal that ended the agent
	At     time.Time `json:"at"`
}

// ExitFile is the name of the exit status file next to spec.json.
const ExitFile = "exit.json"

// WriteSpec writes s to path with mode 0600.
func WriteSpec(path string, s Spec) error { return writeJSON(path, s) }

// ReadExit reads an exit file.
func ReadExit(path string) (Exit, error) {
	var x Exit
	b, err := os.ReadFile(path)
	if err != nil {
		return x, err
	}
	if err := json.Unmarshal(b, &x); err != nil {
		return x, fmt.Errorf("%s: %w", path, err)
	}
	return x, nil
}

// ReadSpec reads a spec file.
func ReadSpec(path string) (Spec, error) {
	var s Spec
	b, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("%s: %w", path, err)
	}
	if len(s.Argv) == 0 {
		return s, fmt.Errorf("%s: no argv", path)
	}
	return s, nil
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Run runs the agent described by the spec at specPath and returns the
// host's own exit code, which is the agent's.
func Run(specPath string) int {
	exitPath := filepath.Join(filepath.Dir(specPath), ExitFile)
	spec, err := ReadSpec(specPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "irie host: %v\n", err)
		return 126
	}
	finish := func(x Exit) int {
		x.At = time.Now().UTC()
		if err := writeJSON(exitPath, x); err != nil {
			fmt.Fprintf(os.Stderr, "irie host: %v\n", err)
		}
		return x.Code
	}

	env := environ(os.Environ(), spec.Env, spec.Unset, spec.Cwd)
	path, err := LookPath(spec.Argv[0], env)
	if err != nil {
		fmt.Fprintf(os.Stderr, "irie: %s: command not found\n", spec.Argv[0])
		return finish(Exit{Code: 127})
	}

	// The agent gets its own process group and the terminal's foreground, so
	// the host can signal its whole group and stays out of the way of its
	// job control. Without a terminal there is no foreground to hand over.
	attr := &syscall.ProcAttr{
		Dir:   spec.Cwd,
		Env:   env,
		Files: []uintptr{0, 1, 2},
		Sys:   &syscall.SysProcAttr{Setpgid: true, Foreground: true, Ctty: 0},
	}
	foreground := true
	pid, err := syscall.ForkExec(path, spec.Argv, attr)
	if errors.Is(err, syscall.ENOTTY) {
		foreground = false
		attr.Sys = &syscall.SysProcAttr{Setpgid: true}
		pid, err = syscall.ForkExec(path, spec.Argv, attr)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "irie: %s: %v\n", spec.Argv[0], err)
		return finish(Exit{Code: 126})
	}

	// From here the host is a background process on its own terminal.
	// Ignoring these keeps it from being stopped when it touches the
	// terminal, and it is set only now so the agent does not inherit it.
	signal.Ignore(syscall.SIGTTOU, syscall.SIGTTIN)
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		for s := range sigs {
			syscall.Kill(-pid, s.(syscall.Signal))
		}
	}()

	ws, err := wait(pid)
	if foreground {
		setForeground(0, syscall.Getpgrp())
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "irie host: wait: %v\n", err)
		return finish(Exit{Code: 126})
	}
	if ws.Signaled() {
		return finish(Exit{Code: 128 + int(ws.Signal()), Signal: int(ws.Signal())})
	}
	return finish(Exit{Code: ws.ExitStatus()})
}

// wait waits for the agent to exit. The host never stops the agent itself,
// so any stop is someone else's, such as the agent's own Ctrl+Z handling;
// the agent's group is continued at once so it never wedges.
func wait(pid int) (syscall.WaitStatus, error) {
	for {
		var ws syscall.WaitStatus
		_, err := syscall.Wait4(pid, &ws, syscall.WUNTRACED, nil)
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return ws, err
		}
		if stopped(ws) {
			syscall.Kill(-pid, syscall.SIGCONT)
			continue
		}
		if ws.Exited() || ws.Signaled() {
			return ws, nil
		}
	}
}

// stopped reports whether ws is a stop. It reads the bits itself because on
// darwin WaitStatus.Stopped misses a stop by SIGSTOP (it reports it as
// Continued). Without WCONTINUED, wait4 reports no continues, so the low
// seven bits being all set always means a stop.
func stopped(ws syscall.WaitStatus) bool { return ws&0x7f == 0x7f }

// setForeground makes pgid the foreground process group of the terminal on
// fd (tcsetpgrp).
func setForeground(fd, pgid int) error {
	p := int32(pgid)
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(syscall.TIOCSPGRP), uintptr(unsafe.Pointer(&p)))
	if errno != 0 {
		return errno
	}
	return nil
}

// environ returns base without the unset names and with extra set, and PWD
// set to cwd as a shell would.
func environ(base []string, extra map[string]string, unset []string, cwd string) []string {
	set := map[string]string{}
	for k, v := range extra {
		set[k] = v
	}
	if cwd != "" {
		set["PWD"] = cwd
	}
	env := make([]string, 0, len(base)+len(set))
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if _, ok := set[k]; !ok && !slices.Contains(unset, k) {
			env = append(env, kv)
		}
	}
	for k, v := range set {
		env = append(env, k+"="+v)
	}
	return env
}

// LookPath finds name like a shell would, but in env's PATH.
func LookPath(name string, env []string) (string, error) {
	if strings.Contains(name, "/") {
		if isExecutable(name) {
			return name, nil
		}
		return "", os.ErrNotExist
	}
	path := ""
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok {
			path = v
		}
	}
	for _, dir := range filepath.SplitList(path) {
		if !filepath.IsAbs(dir) {
			continue
		}
		if p := filepath.Join(dir, name); isExecutable(p) {
			return p, nil
		}
	}
	return "", os.ErrNotExist
}

func isExecutable(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0
}
